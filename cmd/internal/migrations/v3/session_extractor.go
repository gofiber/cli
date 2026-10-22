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
//
// The program may have built a lookup from the Source constants, or read the
// lookup of a store back. Neither exists in v3, and both are written as what they
// were before the lookups are rewritten: the Source type is string and its
// constants are the strings they stood for, or the type of the module package
// where the program asks for the difference (mw3SessionSourceContent), and
// store.KeyLookup and store.CookieName are the strings the program gave them, or
// computed from store.Extractor (mw3SessionReadsContent). The assignments to the
// two fields of a store are dropped, as v2 only changed what the store reported
// with them (storeWrites). Each step is a pass over the project of its own, so
// that the next one reads the whole project as the step before left it.
func MigrateSessionExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	// Every step reads the project as the one before left it, with a view of the
	// package of its own.
	run := func(rewrite func(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string) (bool, error) {
		pkgs := newMw3Pkgs()
		return internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
			return rewrite(content, fi, pkgs)
		})
	}

	sources, err := run(mw3SessionSourceContent)
	if err != nil {
		return fmt.Errorf("failed to migrate session sources: %w", err)
	}
	if sources {
		cmd.Println("Migrating session Source constants")
	}

	reads, err := run(mw3SessionReadsContent)
	if err != nil {
		return fmt.Errorf("failed to migrate session lookup reads: %w", err)
	}
	lookups, err := run(mw3SessionExtractorContent)
	if err != nil {
		return fmt.Errorf("failed to migrate session extractor config: %w", err)
	}
	if reads || lookups {
		cmd.Println("Migrating session KeyLookup config")
	}
	return nil
}

// mw3SessionExtractorPass is the rewrite of one file by MigrateSessionExtractor.
type mw3SessionExtractorPass struct {
	f  *mw3SessionFile
	fi internal.FileInfo
	// handled are the lookup fields that an assignment took care of.
	handled map[*ast.SelectorExpr]struct{}
	imports mw3ImportEdits
	strs    mw3Strings
	edits   editSet
}

// mw3Site is a place of the file: the position of a node and its ancestors (see
// goSource.walk). What is written there may only use the names that are free
// there.
type mw3Site struct {
	stack []ast.Node
	pos   token.Pos
}

func mw3SessionExtractorContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	f, ok := mw3OpenStoreUser(fi, src, pkgs, func(src string) bool {
		return strings.Contains(src, "middleware/session") || strings.Contains(src, "."+mw3KeyLookup) || strings.Contains(src, "."+mw3CookieName)
	})
	if !ok {
		return src
	}

	p := &mw3SessionExtractorPass{f: f, fi: fi, strs: f.strings(), handled: map[*ast.SelectorExpr]struct{}{}}
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

// extractorsAt returns the identifier that code written at the site uses for the
// extractors package, importing the package when the file does not yet, or
// importing it a second time where the name the file gives it is hidden.
// reserved are the names the code declares itself.
func (p *mw3SessionExtractorPass) extractorsAt(at mw3Site, reserved ...string) string {
	return p.f.importAt(&p.imports, mw3ExtractorsPath, at.pos, at.stack, reserved, "extractors", "fiberextractors")
}

// extractor returns the v3 extractor that reads a session ID where the v2
// lookup did, and whether it is the query lookup, which v3 treats differently.
// ok is false for a lookup that v2 itself would have refused.
func (p *mw3SessionExtractorPass) extractor(lookup string, at mw3Site) (expr string, query, ok bool) {
	source, name, found := strings.Cut(lookup, ":")
	if !found || name == "" || strings.Contains(name, ":") {
		return "", false, false
	}

	pkg := p.extractorsAt(at)
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
	if mw3Field(lit, mw3ExtractorField) != nil {
		return
	}
	at := mw3Site{pos: lit.Pos(), stack: stack}
	lookup := mw3Field(lit, mw3KeyLookup)
	cookie := mw3Field(lit, mw3CookieName)

	// v2 let CookieName win over KeyLookup.
	if cookie != nil {
		name, ok := p.strs.value(cookie.Value, stack)
		switch {
		case !ok && lookup == nil:
			p.computed(cookie, mw3CookieName, at)
			return
		case !ok:
			p.unresolved(cookie, mw3CookieName)
			return
		case name != "":
			p.edits.replaceNode(g, cookie, "Extractor: "+p.extractorsAt(at)+".FromCookie("+strconv.Quote(name)+")")
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
			p.computed(lookup, mw3KeyLookup, at)
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
	expr, query, ok := p.extractor(value, at)
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
// program runs.
func (p *mw3SessionExtractorPass) computed(kv *ast.KeyValueExpr, field string, at mw3Site) {
	g := p.f.g
	p.edits.replaceNode(g, kv, "Extractor: "+p.lookupFunc(field, g.text(kv.Value), at))
	p.noteComputed(field, g.line(kv.Pos()))
}

// noteComputed tells what the function that reads a computed lookup stands for.
func (p *mw3SessionExtractorPass) noteComputed(field string, line int) {
	if field == mw3CookieName {
		return
	}
	internal.Notef(p.fi, line, "KeyLookup is computed, so it is read by a function when the program starts, as the v2 constructor did; %s", mw3QueryLookupNote)
}

// The names that the functions of lookupFunc declare: a package they use must
// not be called one of them.
var (
	mw3CookieFuncNames = []string{mw3NameVar}
	mw3LookupFuncNames = []string{"lookup", "source", mw3NameVar, "found"}
)

// mw3NameVar is the variable of the generated functions that holds the name of
// a lookup.
const mw3NameVar = "name"

// lookupFunc writes the function that turns the value of a computed field,
// given as the source text arg, into the extractor v2 built from it.
func (p *mw3SessionExtractorPass) lookupFunc(field, arg string, at mw3Site) string {
	if field == mw3CookieName {
		pkg := p.extractorsAt(at, mw3CookieFuncNames...)
		// v2 took an empty CookieName for no CookieName, that is the default cookie.
		return strings.Join([]string{
			"func(name string) " + pkg + ".Extractor {",
			"if name == \"\" {",
			"name = " + strconv.Quote(mw3DefaultCookie),
			"}",
			"return " + pkg + ".FromCookie(name)",
			"}(" + arg + ")",
		}, "\n")
	}

	pkg := p.extractorsAt(at, mw3LookupFuncNames...)
	str := p.f.importAt(&p.imports, mw3StringsPkg, at.pos, at.stack, mw3LookupFuncNames, "strings", "stdstrings")
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
	}, "\n")
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

// assign migrates cfg.KeyLookup = ... and cfg.CookieName = ... on a config, and
// drops the assignments to the lookup of a store.
func (p *mw3SessionExtractorPass) assign(as *ast.AssignStmt, stack []ast.Node) {
	g := p.f.g
	p.storeWrites(as, stack)
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 || as.Tok != token.ASSIGN {
		return
	}
	sel, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != mw3KeyLookup && sel.Sel.Name != mw3CookieName) {
		return
	}
	if m, isConfig := p.f.providedBy(sel.X, sel.Sel.Name, mw3KindConfig, stack); !isConfig || m.viaStore {
		return // a store: storeWrites reports it
	}
	p.handled[sel] = struct{}{}

	at := mw3Site{pos: as.Pos(), stack: stack}
	value, ok := p.strs.value(as.Rhs[0], stack)
	if ok && sel.Sel.Name == mw3KeyLookup {
		if expr, query, valid := p.extractor(value, at); valid {
			p.edits.replaceNode(g, sel.Sel, mw3ExtractorField)
			p.edits.replaceNode(g, as.Rhs[0], expr)
			p.imports.releaseIn(g, as.Rhs[0])
			if query {
				internal.Notef(p.fi, g.line(as.Pos()), "KeyLookup %q: %s", value, mw3QueryLookupNote)
			}
			return
		}
	}
	if ok && sel.Sel.Name == mw3CookieName && value != "" {
		p.edits.replaceNode(g, sel.Sel, mw3ExtractorField)
		p.edits.replaceNode(g, as.Rhs[0], p.extractorsAt(at)+".FromCookie("+strconv.Quote(value)+")")
		p.imports.releaseIn(g, as.Rhs[0])
		return
	}
	if !ok {
		// The function reads the field the assignment sets, and nothing else of the config.
		p.edits.replaceNode(g, sel.Sel, mw3ExtractorField)
		p.edits.replaceNode(g, as.Rhs[0], p.lookupFunc(sel.Sel.Name, g.text(as.Rhs[0]), at))
		p.noteComputed(sel.Sel.Name, g.line(as.Pos()))
		return
	}

	internal.Notef(p.fi, g.line(as.Pos()),
		"%s is not a constant \"cookie:name\", \"header:name\" or \"query:name\" lookup, so it could not be turned into an Extractor and was left as it is, which does not build on v3. "+
			"Set Config.Extractor instead.", g.text(as))
}

// mw3StoreWriteNote tells what became of an assignment to the lookup of a store.
const mw3StoreWriteNote = "%s: %s. v2 only changed what the store reports with it: the lookup is fixed when the store is made, from the config it is given, " +
	"so the store looks where it did before the assignment, and still does. Set Config.Extractor where the store is made to change it. " +
	"Reading the field back now gives the lookup the store really uses."

// storeWrites drops the assignments to the lookup of a store (store.KeyLookup and
// store.CookieName, also of the store a type embeds): the fields do not exist in
// v3, and v2 did not use them to look anything up. An assignment of a literal
// that stands alone is removed, any other keeps its value evaluated:
//
//	store.KeyLookup = "header:X-Session"  ->  (removed)
//	store.KeyLookup += suffix()           ->  _ = suffix()
//
// A variable that the assignment was the last use of stays used.
func (p *mw3SessionExtractorPass) storeWrites(as *ast.AssignStmt, stack []ast.Node) {
	g := p.f.g
	var targets []mw3Target
	for _, lhs := range as.Lhs {
		sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != mw3KeyLookup && sel.Sel.Name != mw3CookieName) {
			continue
		}
		if m, isConfig := p.f.providedBy(sel.X, sel.Sel.Name, mw3KindConfig, stack); isConfig && m.viaStore {
			p.handled[sel] = struct{}{}
			targets = append(targets, mw3Target{lhs: lhs, sel: sel})
		}
	}
	if len(targets) == 0 {
		return
	}

	line := g.line(as.Pos())
	inList := mw3InStatementList(stack)
	keep := p.keepsUsed(as, targets, stack)
	switch {
	case len(as.Lhs) == 1 && len(as.Rhs) == 1 && mw3IsLiteralOnly(as.Rhs[0]) && inList && keep != "":
		p.edits.replaceNode(g, as, "_ = "+keep)
		internal.Notef(p.fi, line, mw3StoreWriteNote, g.text(as), "the assignment was removed")
	case len(as.Lhs) == 1 && len(as.Rhs) == 1 && mw3IsLiteralOnly(as.Rhs[0]) && inList:
		mw3DeleteStatement(g, &p.edits, as)
		internal.Notef(p.fi, line, mw3StoreWriteNote, g.text(as), "the assignment was removed")
	default:
		for _, target := range targets {
			p.edits.replaceNode(g, target.lhs, "_")
			internal.Notef(p.fi, line, mw3StoreWriteNote, g.text(target.lhs)+" "+as.Tok.String()+" ...",
				"the target was replaced by _, so the value is still evaluated")
		}
		if as.Tok != token.ASSIGN {
			// store.KeyLookup += x  ->  _ = x
			p.edits.replace(g.offset(as.Lhs[0].End()), g.offset(as.TokPos)+len(as.Tok.String()), " =")
		}
		if keep != "" && inList {
			end := g.offset(as.End())
			p.edits.replace(end, end, "\n"+mw3Indent(g.src, g.offset(as.Pos()))+"_ = "+keep)
		}
	}
}

// keepsUsed returns the variable that the targets of an assignment are rooted in
// when the assignment is the last place the function uses it: taking the
// assignment out would leave a variable that is declared and not used, which Go
// refuses. It is empty when there is no such variable.
func (p *mw3SessionExtractorPass) keepsUsed(as *ast.AssignStmt, targets []mw3Target, stack []ast.Node) string {
	g := p.f.g
	var body *ast.BlockStmt
	var fields []*ast.FieldList
	for _, node := range slices.Backward(stack) {
		if fn, ok := node.(*ast.FuncDecl); ok {
			body, fields = fn.Body, []*ast.FieldList{fn.Recv, fn.Type.Params, fn.Type.Results}
			break
		}
		if fn, ok := node.(*ast.FuncLit); ok {
			body, fields = fn.Body, []*ast.FieldList{fn.Type.Params, fn.Type.Results}
			break
		}
	}
	if body == nil {
		return ""
	}

	for _, target := range targets {
		root := mw3RootIdent(target.sel.X)
		if root == nil || root.Name == "_" || mw3Declares(fields, root.Name) {
			continue
		}
		if mw3OnlyMention(body, as, root.Name) {
			return g.text(target.sel.X)
		}
	}
	return ""
}

// mw3Target is a lookup of a store that an assignment writes: the target as
// written, and the selector in it.
type mw3Target struct {
	lhs ast.Expr
	sel *ast.SelectorExpr
}

// mw3RootIdent returns the variable an expression such as a.b[i].c starts at.
func mw3RootIdent(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.UnaryExpr:
			e = x.X
		default:
			return nil
		}
	}
}

// mw3Declares reports whether one of the parameter lists declares the name.
func mw3Declares(lists []*ast.FieldList, name string) bool {
	for _, list := range lists {
		if _, found := declInFieldList(name, list); found {
			return true
		}
	}
	return false
}

// mw3OnlyMention reports whether the variable that a statement of the body uses
// is declared in the body, and the statement is the only place that uses it.
func mw3OnlyMention(body *ast.BlockStmt, stmt ast.Stmt, name string) bool {
	declared, mentions := mw3CountMentions(body, name, stmt)
	return declared && mentions == 0
}

// mw3CountMentions reports whether the node declares the variable, and how many
// times it uses it, the statement skip left out. A field called like the variable
// counts as a use, which only makes a variable look more used than it is.
func mw3CountMentions(root ast.Node, name string, skip ast.Stmt) (declared bool, mentions int) {
	defined := map[*ast.Ident]struct{}{}
	define := func(e ast.Expr) {
		if id, ok := e.(*ast.Ident); ok && id.Name == name {
			defined[id] = struct{}{}
		}
	}
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case nil:
			return false
		case *ast.AssignStmt:
			if skip != nil && x == skip {
				return false
			}
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					define(lhs)
				}
			}
		case *ast.ValueSpec:
			for _, id := range x.Names {
				define(id)
			}
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE {
				define(x.Key)
				define(x.Value)
			}
		case *ast.Ident:
			if _, isDecl := defined[x]; !isDecl && x.Name == name {
				mentions++
			}
		}
		return true
	})
	return len(defined) > 0, mentions
}

// mw3IsLiteralOnly reports whether the expression is made of literals alone, so
// that dropping it leaves nothing behind that was used for it.
func mw3IsLiteralOnly(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return mw3IsLiteralOnly(x.X)
	case *ast.BinaryExpr:
		return x.Op == token.ADD && mw3IsLiteralOnly(x.X) && mw3IsLiteralOnly(x.Y)
	}
	return false
}

// mw3InStatementList reports whether the statement whose ancestors are stack is
// one of a list of statements, where it can be removed.
func mw3InStatementList(stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	switch stack[len(stack)-1].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		return true
	}
	return false
}

// mw3DeleteStatement schedules the removal of a statement, with its line when it
// has the line to itself.
func mw3DeleteStatement(g *goSource, edits *editSet, stmt ast.Stmt) {
	src := g.src
	start, end := g.offset(stmt.Pos()), g.offset(stmt.End())
	lineStart := mw3LineStart(src, start)
	alone := strings.TrimLeft(src[lineStart:start], " \t") == ""
	if rest := strings.TrimLeft(src[end:mw3LineEnd(src, end)], " \t"); alone && rest != "" && !strings.HasPrefix(rest, "//") {
		alone = false
	}
	if !alone {
		edits.replace(start, end, "")
		return
	}
	lineEnd := mw3LineEnd(src, end)
	if lineEnd < len(src) {
		lineEnd++
	}
	edits.replace(lineStart, lineEnd, "")
}

// selector reports what the steps before this one left of the session
// identifiers that v3 no longer has: a Source that could not be written as a
// string, and a lookup field of a store or a config that is assigned to, whose
// address is taken, or that was read where the extractors package is hidden.
func (p *mw3SessionExtractorPass) selector(sel *ast.SelectorExpr, stack []ast.Node) {
	g := p.f.g
	if id, ok := sel.X.(*ast.Ident); ok && p.f.isPackage(id, stack) {
		if name := sel.Sel.Name; name == mw3SessionSource || mw3SessionSourceValues[name] != "" {
			internal.Notef(p.fi, g.line(sel.Pos()),
				"%s was removed, and could not be written here: the file declares string itself, or it is a case of a type switch that has a case string already, "+
					"and the module cannot have the package for a type that keeps a Source and a string apart (it has no go.mod, or %s holds code that is not from \"fiber migrate\"). "+
					"Source was a string type, and SourceCookie, SourceHeader and SourceURLQuery were \"cookie\", \"header\" and \"query\".",
				g.text(sel), moduleShimDir)
		}
		return
	}

	field := sel.Sel.Name
	if _, done := p.handled[sel]; done || (field != mw3KeyLookup && field != mw3CookieName) {
		return
	}
	if _, isConfig := p.f.providedBy(sel.X, field, mw3KindConfig, stack); !isConfig {
		return
	}
	if use, outer := mw3UseOf(sel, stack); use == mw3UseAddress {
		internal.Notef(p.fi, g.line(sel.Pos()), mw3ConfigAddressNote, g.text(outer))
		return
	}
	internal.Notef(p.fi, g.line(sel.Pos()),
		"%s no longer exists: the session config has an Extractor instead of KeyLookup and CookieName.", g.text(sel))
}

// mw3ConfigAddressNote reports the address of the lookup of a config, which v3
// has no field for.
const mw3ConfigAddressNote = "%s: the session Config has no KeyLookup or CookieName in v3 (it has an Extractor), so there is no string to point to. " +
	"Let the flag or the decoder fill a variable of your own, and set Config.Extractor from it where the store is made, " +
	"for instance extractors.FromHeader(name) or extractors.FromCookie(name)."
