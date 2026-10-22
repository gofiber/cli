package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3ExtractorsPath is the import path of the extractors package.
const mw3ExtractorsPath = "github.com/gofiber/fiber/v3/extractors"

// mw3StringsPkg is the import path, and the name, of the standard strings package.
const mw3StringsPkg = "strings"

// MigrateSessionExtractor replaces the session KeyLookup string, and the
// deprecated CookieName, with the Extractor that v3 takes. A lookup whose value
// is known is turned into the extractor itself. One that is only known when the
// program runs (an environment variable, a function result) gets a function that
// reads it the way the v2 constructor did. The combinations that leaves open (a
// computed CookieName next to a KeyLookup, or the function not being writable in
// the file) are left as they are, so that the build fails on them instead of the
// session quietly using the default cookie, and are reported.
func MigrateSessionExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs := newMw3Pkgs()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return mw3SessionExtractorContent(content, fi, pkgs)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session extractor config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating session KeyLookup config")
	return nil
}

// mw3SessionExtractorPass is the rewrite of one file by MigrateSessionExtractor.
type mw3SessionExtractorPass struct {
	f          *mw3SessionFile
	fi         internal.FileInfo
	extractors string
	imports    mw3ImportEdits
	strs       mw3Strings
	edits      editSet
}

func mw3SessionExtractorContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	f, ok := mw3OpenSessionFile(fi, src, pkgs)
	if !ok {
		return src
	}

	p := &mw3SessionExtractorPass{f: f, fi: fi, strs: f.strings()}
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			if lit, ok := f.isConfigLit(n, stack); ok {
				p.configLit(lit, stack)
			}
		case *ast.AssignStmt:
			p.assign(n, stack)
		case *ast.SelectorExpr:
			p.selector(n, stack)
		}
		return true
	})

	out := p.edits.apply(src)
	if out == src {
		return src
	}
	return p.imports.apply(out)
}

// extractorsName returns the identifier the file uses for the extractors
// package, importing the package when the file does not yet.
func (p *mw3SessionExtractorPass) extractorsName() string {
	if p.extractors == "" {
		p.extractors = mw3ImportName(p.f.g.src, mw3ExtractorsPath)
		if p.extractors == "" {
			declared := mw3DeclaredNames(p.f.g.file)
			p.extractors = mw3FreeName(func(name string) bool {
				_, taken := declared[name]
				return taken
			}, "extractors", "fiberextractors")
			p.imports.require(mw3ExtractorsPath, p.extractors)
		}
	}
	return p.extractors
}

// extractor returns the v3 extractor that reads a session ID where the v2
// lookup did, and whether it is the query lookup, which v3 treats differently.
// ok is false for a lookup that v2 itself would have refused.
func (p *mw3SessionExtractorPass) extractor(lookup string) (expr string, query, ok bool) {
	source, name, found := strings.Cut(lookup, ":")
	if !found || name == "" || strings.Contains(name, ":") {
		return "", false, false
	}

	pkg := p.extractorsName()
	quoted := strconv.Quote(name)
	switch source {
	case mw3SourceCookie:
		return pkg + ".FromCookie(" + quoted + ")", false, true
	case mw3SourceHeader:
		return pkg + ".FromHeader(" + quoted + ")", false, true
	case mw3SourceQuery:
		// v2 read the cookie first, then the query, and always answered with a
		// cookie: a bare FromQuery would never set one.
		return pkg + ".Chain(" + pkg + ".FromCookie(" + quoted + "), " + pkg + ".FromQuery(" + quoted + "))", true, true
	}
	return "", false, false
}

// The sources of a v2 session lookup, and the fields that name it.
const (
	mw3SourceCookie = "cookie"
	mw3SourceHeader = "header"
	mw3SourceQuery  = "query"
	mw3KeyLookup    = "KeyLookup"
	mw3CookieName   = "CookieName"
	// mw3DefaultCookie is the cookie a v2 session read when it was given no lookup.
	mw3DefaultCookie = "session_id"
)

const mw3QueryLookupNote = "a `query:` lookup now reads the cookie first and then the query, as v2 did, but v3 no longer copies a session ID that arrived in the query into the cookie " +
	"(session fixation protection), so clients must keep sending it."

// configLit migrates the lookup of a session.Config literal.
func (p *mw3SessionExtractorPass) configLit(lit *ast.CompositeLit, stack []ast.Node) {
	g := p.f.g
	if mw3Field(lit, "Extractor") != nil {
		return
	}
	lookup := mw3Field(lit, mw3KeyLookup)
	cookie := mw3Field(lit, mw3CookieName)

	// v2 let CookieName win over KeyLookup.
	if cookie != nil {
		name, ok := p.strs.value(cookie.Value, stack)
		switch {
		case !ok && lookup == nil:
			p.computed(cookie, mw3CookieName)
			return
		case !ok:
			p.unresolved(cookie, mw3CookieName)
			return
		case name != "":
			p.edits.replaceNode(g, cookie, "Extractor: "+p.extractorsName()+".FromCookie("+strconv.Quote(name)+")")
			p.imports.releaseIn(g, cookie)
			if lookup != nil {
				mw3RemoveElement(g, &p.edits, lookup)
				p.imports.releaseIn(g, lookup)
			}
			return
		}
		mw3RemoveElement(g, &p.edits, cookie) // an empty name is no name
		p.imports.releaseIn(g, cookie)
	}
	if lookup == nil {
		return
	}

	value, ok := p.strs.value(lookup.Value, stack)
	if !ok {
		if cookie == nil {
			p.computed(lookup, mw3KeyLookup)
		} else {
			p.unresolved(lookup, mw3KeyLookup)
		}
		return
	}
	if value == "" {
		mw3RemoveElement(g, &p.edits, lookup) // v2 took an empty lookup for the default
		p.imports.releaseIn(g, lookup)
		return
	}
	expr, query, ok := p.extractor(value)
	if !ok {
		internal.Notef(p.fi, g.line(lookup.Pos()),
			"KeyLookup %q is not a \"cookie:name\", \"header:name\" or \"query:name\" lookup, which is all v2 accepted, so it was left as it is. Set Config.Extractor instead.", value)
		return
	}
	p.edits.replaceNode(g, lookup, "Extractor: "+expr)
	p.imports.releaseIn(g, lookup)
	if query {
		internal.Notef(p.fi, g.line(lookup.Pos()), "KeyLookup %q: %s", value, mw3QueryLookupNote)
	}
}

// computed migrates the lookup of a literal whose value is only known when the
// program runs, or reports it when the function that reads it cannot be written.
func (p *mw3SessionExtractorPass) computed(kv *ast.KeyValueExpr, field string) {
	g := p.f.g
	fn, ok := p.lookupFunc(field, g.text(kv.Value))
	if !ok {
		p.unresolved(kv, field)
		return
	}
	p.edits.replaceNode(g, kv, "Extractor: "+fn)
	p.noteComputed(field, g.line(kv.Pos()))
}

// noteComputed tells what the function that reads a computed lookup stands for.
func (p *mw3SessionExtractorPass) noteComputed(field string, line int) {
	if field == mw3CookieName {
		return
	}
	internal.Notef(p.fi, line, "KeyLookup is computed, so it is read by a function when the program starts, as the v2 constructor did; %s", mw3QueryLookupNote)
}

// lookupFunc writes the function that turns the value of a computed field,
// given as the source text arg, into the extractor v2 built from it. ok is false
// when the file uses a name the function needs for something else.
func (p *mw3SessionExtractorPass) lookupFunc(field, arg string) (fn string, ok bool) {
	pkg := p.extractorsName()

	if field == mw3CookieName {
		// v2 took an empty CookieName for no CookieName, that is the default cookie.
		return strings.Join([]string{
			"func(name string) " + pkg + ".Extractor {",
			"if name == \"\" {",
			"name = " + strconv.Quote(mw3DefaultCookie),
			"}",
			"return " + pkg + ".FromCookie(name)",
			"}(" + arg + ")",
		}, "\n"), true
	}

	str := mw3ImportName(p.f.g.src, mw3StringsPkg)
	if str == "" {
		if _, taken := mw3DeclaredNames(p.f.g.file)[mw3StringsPkg]; taken {
			return "", false
		}
		str = mw3StringsPkg
		p.imports.require(mw3StringsPkg, "")
	}

	return strings.Join([]string{
		"func(lookup string) " + pkg + ".Extractor {",
		"if lookup == \"\" {",
		"lookup = " + strconv.Quote(mw3SourceCookie+":"+mw3DefaultCookie),
		"}",
		"source, name, found := " + str + ".Cut(lookup, \":\")",
		"if !found || " + str + ".Contains(name, \":\") {",
		"panic(\"[session] KeyLookup must in the form of <source>:<name>\")",
		"}",
		"switch source {",
		"case " + strconv.Quote(mw3SourceCookie) + ":",
		"return " + pkg + ".FromCookie(name)",
		"case " + strconv.Quote(mw3SourceHeader) + ":",
		"return " + pkg + ".FromHeader(name)",
		"case " + strconv.Quote(mw3SourceQuery) + ":",
		"return " + pkg + ".Chain(" + pkg + ".FromCookie(name), " + pkg + ".FromQuery(name))",
		"}",
		"panic(\"[session] source is not supported\")",
		"}(" + arg + ")",
	}, "\n"), true
}

// unresolved reports a lookup whose value is not known at migration time.
func (p *mw3SessionExtractorPass) unresolved(kv *ast.KeyValueExpr, field string) {
	g := p.f.g
	internal.Notef(p.fi, g.line(kv.Pos()),
		"%s is not a constant string (%s), so it could not be turned into an Extractor and was left as it is, which does not build on v3. "+
			"Set Config.Extractor: extractors.FromCookie(name), extractors.FromHeader(name) or "+
			"extractors.Chain(extractors.FromCookie(name), extractors.FromQuery(name)) for a query lookup.",
		field, g.text(kv.Value))
}

// assign migrates cfg.KeyLookup = ... and cfg.CookieName = ... on a config.
func (p *mw3SessionExtractorPass) assign(as *ast.AssignStmt, stack []ast.Node) {
	g := p.f.g
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 || as.Tok != token.ASSIGN {
		return
	}
	sel, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != mw3KeyLookup && sel.Sel.Name != mw3CookieName) || !p.f.isConfig(sel.X, stack) {
		return
	}
	if !p.f.isConfigValue(sel.X, stack) {
		return // a store: selector reports the read
	}

	value, ok := p.strs.value(as.Rhs[0], stack)
	if ok && sel.Sel.Name == mw3KeyLookup {
		if expr, query, valid := p.extractor(value); valid {
			p.edits.replaceNode(g, sel.Sel, "Extractor")
			p.edits.replaceNode(g, as.Rhs[0], expr)
			p.imports.releaseIn(g, as.Rhs[0])
			if query {
				internal.Notef(p.fi, g.line(as.Pos()), "KeyLookup %q: %s", value, mw3QueryLookupNote)
			}
			return
		}
	}
	if ok && sel.Sel.Name == mw3CookieName && value != "" {
		p.edits.replaceNode(g, sel.Sel, "Extractor")
		p.edits.replaceNode(g, as.Rhs[0], p.extractorsName()+".FromCookie("+strconv.Quote(value)+")")
		p.imports.releaseIn(g, as.Rhs[0])
		return
	}
	if !ok {
		// The function reads the field the assignment sets, and nothing else of the config.
		if fn, written := p.lookupFunc(sel.Sel.Name, g.text(as.Rhs[0])); written {
			p.edits.replaceNode(g, sel.Sel, "Extractor")
			p.edits.replaceNode(g, as.Rhs[0], fn)
			p.noteComputed(sel.Sel.Name, g.line(as.Pos()))
			return
		}
	}

	internal.Notef(p.fi, g.line(as.Pos()),
		"%s is not a constant \"cookie:name\", \"header:name\" or \"query:name\" lookup, so it could not be turned into an Extractor and was left as it is, which does not build on v3. "+
			"Set Config.Extractor instead.", g.text(as))
}

// selector reports the session identifiers that v3 no longer has: the lookup
// fields of a store and the Source constants.
func (p *mw3SessionExtractorPass) selector(sel *ast.SelectorExpr, stack []ast.Node) {
	g := p.f.g
	if id, ok := sel.X.(*ast.Ident); ok && p.f.isPackage(id, stack) {
		switch sel.Sel.Name {
		case "Source", "SourceCookie", "SourceHeader", "SourceURLQuery":
			internal.Notef(p.fi, g.line(sel.Pos()),
				"%s was removed: a session reads its ID through Config.Extractor now (extractors.FromCookie, FromHeader, FromQuery), and extractors.Source tells where an ID came from.",
				g.text(sel))
		}
		return
	}

	if sel.Sel.Name != mw3KeyLookup && sel.Sel.Name != mw3CookieName {
		return
	}
	if len(stack) > 0 {
		if as, ok := stack[len(stack)-1].(*ast.AssignStmt); ok && slices.ContainsFunc(as.Lhs, func(e ast.Expr) bool { return e == ast.Expr(sel) }) && p.f.isConfigValue(sel.X, stack) {
			return // assign handles it
		}
	}
	if p.f.isConfig(sel.X, stack) {
		internal.Notef(p.fi, g.line(sel.Pos()),
			"%s no longer exists: the session config has an Extractor instead of KeyLookup and CookieName.", g.text(sel))
	}
}
