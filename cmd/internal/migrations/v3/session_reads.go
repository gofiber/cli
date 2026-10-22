package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3ExtractorField is the field of the v3 session config that holds the lookup.
const mw3ExtractorField = "Extractor"

const (
	mw3KeyLookupReadNote = "%s is rebuilt from the Extractor, which replaces KeyLookup in v3: it gives \"cookie:name\", \"header:name\" or \"query:name\" " +
		"for the lookups this migration writes, and describes the last source of an extractor that you wrote yourself."
	mw3CookieNameReadNote = "%s is the name of the cookie the Extractor reads, and empty when it reads none, because v3 has no CookieName. " +
		"v2 returned the CookieName that the config was given, which is empty when the config used KeyLookup, so the value differs in that case."
)

// mw3SessionReadsPass is the rewrite of the reads of one file by MigrateSessionExtractor.
type mw3SessionReadsPass struct {
	f *mw3SessionFile
	// handled are the reads that another rewrite has taken care of.
	handled map[*ast.SelectorExpr]struct{}
	fi      internal.FileInfo
	imports mw3ImportEdits
	edits   editSet
}

// mw3SessionReadsContent rewrites what a program reads from the config of a
// session store. The v2 store embeds its (defaulted) Config, and so does the v3
// one, but the lookup is kept in the Extractor there and KeyLookup and
// CookieName are gone:
//
//	store.KeyLookup  ->  the string "cookie:name", "header:name" or "query:name"
//	store.CookieName ->  the name of the cookie the store reads, "" if it reads none
//
// are written as a function literal that is called with store.Extractor, which
// gives exactly the strings the lookups of this migration had. It works on
// stores, on variables that hold a session.Config, and on session.ConfigDefault,
// in files that do not import the session package as well, when they use a store
// that another file of the package declares.
//
// A lookup that is copied from one config to another needs no string at all:
//
//	session.Config{KeyLookup: other.KeyLookup}  ->  session.Config{Extractor: other.Extractor}
//
// Assigning to the field, or taking its address, is not a read and is left to
// the extractor rewrite to report.
func mw3SessionReadsContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	f, ok := mw3OpenStoreUser(fi, src, pkgs, func(src string) bool {
		return strings.Contains(src, "."+mw3KeyLookup) || strings.Contains(src, "."+mw3CookieName)
	})
	if !ok {
		return src
	}

	p := &mw3SessionReadsPass{f: f, fi: fi, handled: map[*ast.SelectorExpr]struct{}{}}
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			p.copiedInLiteral(n, stack)
		case *ast.AssignStmt:
			p.copiedInAssignment(n, stack)
		case *ast.SelectorExpr:
			p.read(n, stack)
		}
		return true
	})

	out := p.edits.apply(src)
	if out == src {
		return src
	}
	return p.imports.apply(out)
}

// owner tells whether x holds a config that carries a lookup: a store, a
// variable that holds a session.Config, or session.ConfigDefault. unset is
// whether the config may have no extractor, which only one built by hand can.
func (p *mw3SessionReadsPass) owner(x ast.Expr, stack []ast.Node) (unset, ok bool) {
	switch {
	case p.f.isStore(x, stack), p.f.isMember(x, mw3ConfigDefault, stack):
		return false, true
	case p.f.isConfigValue(x, stack):
		return true, true
	}
	return false, false
}

// read rewrites a read of store.KeyLookup or store.CookieName.
func (p *mw3SessionReadsPass) read(sel *ast.SelectorExpr, stack []ast.Node) {
	field := sel.Sel.Name
	if field != mw3KeyLookup && field != mw3CookieName {
		return
	}
	if _, done := p.handled[sel]; done {
		return
	}
	unset, ok := p.owner(sel.X, stack)
	g := p.f.g
	if !ok || mw3IsWritten(sel, stack) || mw3ImportHidden(g, mw3ExtractorsPath, sel.Pos(), stack) {
		return
	}

	pkg := mw3NeedImport(g, &p.imports, mw3ExtractorsPath, "extractors", "fiberextractors")
	start, end := g.offset(sel.Pos()), g.offset(sel.End())
	p.edits.replace(start, start, mw3LookupReader(field, pkg, unset))
	p.edits.replaceNode(g, sel.Sel, mw3ExtractorField)
	p.edits.replace(end, end, ")")

	note := mw3KeyLookupReadNote
	if field == mw3CookieName {
		note = mw3CookieNameReadNote
	}
	internal.Notef(p.fi, g.line(sel.Pos()), note, g.text(sel))
}

// copiedInLiteral takes the extractor of another config for KeyLookup:
// other.KeyLookup in a session.Config literal. A literal that sets CookieName as
// well keeps the generic rewrite: v2 let CookieName win.
func (p *mw3SessionReadsPass) copiedInLiteral(kv *ast.KeyValueExpr, stack []ast.Node) {
	if len(stack) == 0 || !isIdentNamed(kv.Key, mw3KeyLookup) {
		return
	}
	value, ok := kv.Value.(*ast.SelectorExpr)
	if !ok || value.Sel.Name != mw3KeyLookup {
		return
	}
	lit, ok := stack[len(stack)-1].(*ast.CompositeLit)
	if !ok {
		return
	}
	if _, isConfig := p.f.isConfigLit(lit, stack[:len(stack)-1]); !isConfig ||
		mw3Field(lit, mw3CookieName) != nil || mw3Field(lit, mw3ExtractorField) != nil {
		return
	}
	if _, ok := p.owner(value.X, stack); !ok {
		return
	}

	g := p.f.g
	p.edits.replaceNode(g, kv.Key, mw3ExtractorField)
	p.edits.replaceNode(g, value.Sel, mw3ExtractorField)
	p.handled[value] = struct{}{}
}

// copiedInAssignment takes the extractor of another config for KeyLookup:
// cfg.KeyLookup = other.KeyLookup, on a variable that holds a config.
func (p *mw3SessionReadsPass) copiedInAssignment(as *ast.AssignStmt, stack []ast.Node) {
	if as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return
	}
	dst, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok || dst.Sel.Name != mw3KeyLookup || !p.f.isConfigValue(dst.X, stack) {
		return
	}
	value, ok := as.Rhs[0].(*ast.SelectorExpr)
	if !ok || value.Sel.Name != mw3KeyLookup {
		return
	}
	if _, ok := p.owner(value.X, stack); !ok {
		return
	}

	g := p.f.g
	p.edits.replaceNode(g, dst.Sel, mw3ExtractorField)
	p.edits.replaceNode(g, value.Sel, mw3ExtractorField)
	p.handled[value] = struct{}{}
}

// mw3LookupReader returns the beginning of a call of a function literal that
// turns the Extractor of a v3 config into the string v2 had in the field, which
// is the argument: the caller writes the argument and the closing parenthesis.
//
// KeyLookup is the source and the name of the extractor, and a query lookup is
// the chain of cookie and query that this migration writes for it, which is told
// by its last element. CookieName is the name of the cookie that is read first.
// An extractor that is not set at all, which only a config built by hand can
// have, is the empty lookup, as it was in v2.
func mw3LookupReader(field, pkg string, unset bool) string {
	lines := []string{"func(e " + pkg + ".Extractor) string {"}
	if field == mw3CookieName {
		lines = append(lines,
			"if e.Source == "+pkg+".SourceCookie {",
			"return e.Key",
			"}",
			`return ""`,
		)
	} else {
		if unset {
			lines = append(lines, "if e.Extract == nil {", `return ""`, "}")
		}
		lines = append(lines,
			"if n := len(e.Chain); n > 0 {",
			"e = e.Chain[n-1]",
			"}",
			"switch e.Source {",
			"case "+pkg+".SourceHeader:",
			"return "+strconv.Quote(mw3SourceHeader+":")+" + e.Key",
			"case "+pkg+".SourceQuery:",
			"return "+strconv.Quote(mw3SourceQuery+":")+" + e.Key",
			"}",
			"return "+strconv.Quote(mw3SourceCookie+":")+" + e.Key",
		)
	}
	return strings.Join(append(lines, "}("), "\n")
}

// mw3IsWritten reports whether the selector, whose ancestors are stack, is
// assigned to or has its address taken: a call cannot take its place.
func mw3IsWritten(sel *ast.SelectorExpr, stack []ast.Node) bool {
	var node ast.Expr = sel
	i := len(stack) - 1
	for i >= 0 {
		paren, ok := stack[i].(*ast.ParenExpr)
		if !ok || paren.X != node {
			break
		}
		node = paren
		i--
	}
	if i < 0 {
		return false
	}

	switch parent := stack[i].(type) {
	case *ast.AssignStmt:
		return slices.Contains(parent.Lhs, node)
	case *ast.UnaryExpr:
		return parent.Op == token.AND
	case *ast.IncDecStmt:
		return true
	case *ast.RangeStmt:
		return parent.Key == node || parent.Value == node
	}
	return false
}
