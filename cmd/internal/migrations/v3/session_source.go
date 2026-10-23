package v3

import (
	"go/ast"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3SessionSource is the type v2 had for the source of a session lookup:
//
//	type Source string
//
//	const (
//		SourceCookie   Source = "cookie"
//		SourceHeader   Source = "header"
//		SourceURLQuery Source = "query"
//	)
//
// v3 has neither the type nor the constants, a lookup is an Extractor there.
const mw3SessionSource = "Source"

// mw3SessionSourceValues are the constants of the removed Source type, with the
// strings they stood for.
var mw3SessionSourceValues = func() map[string]string {
	values := map[string]string{}
	for _, c := range mw3SourceConstants {
		values[c.name] = c.value
	}
	return values
}()

// mw3SessionSourceContent writes the Source type of the v2 session middleware as
// the string it was, and its constants as the strings they stood for:
//
//	func lookup(src session.Source) string { return string(src) + ":sid" }
//	lookup(session.SourceCookie)
//
// becomes
//
//	func lookup(src string) string { return string(src) + ":sid" }
//	lookup("cookie")
//
// Nothing else changes with it: the constants were untyped strings in all but
// name, and every use of the type, in a signature, a field, a conversion or a
// switch, means the same with string in its place. A lookup that is built from
// the constants is a plain string afterwards, which the extractor rewrite then
// reads.
//
// One thing does change: v2 told a Source from a plain string where the type
// is asked for, in a type assertion or a type switch, and string does not. A
// module with such a place keeps the type, as SessionSource of its package
// <module>/internal/fibermigrate (see sessionShimModuleFile), in every file that
// uses it, so that the two stay apart. So does a module with a file that declares
// the identifier string itself, where the type cannot be written as string.
//
// An embedded field is called after its type, Source, and stays so:
//
//	type lookups struct{ session.Source }  ->  type lookups struct{ Source string }
//
// What cannot be written is left for the extractor rewrite to report: a module
// that needs the type and cannot have the package for it is told, and gets the
// type as a string where that can be done.
func mw3SessionSourceContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	if !strings.Contains(src, mw3SessionSource) {
		return src
	}
	f, ok := mw3OpenSessionFile(fi, src, pkgs)
	if !ok {
		return src
	}
	g := f.g
	mode := pkgs.sourceMode(fi)
	if _, hidden := mw3DeclaredNames(g.file)["string"]; hidden && mode.path == "" {
		return src
	}

	p := &mw3SessionSourcePass{f: f, fi: fi, mode: mode, twice: map[*ast.SelectorExpr]struct{}{}}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch n := n.(type) {
		case *ast.TypeSwitchStmt:
			if mode.path == "" {
				for _, sel := range mw3SourcesNextToString(f, n, stack) {
					p.twice[sel] = struct{}{}
				}
			}
			return true
		case *ast.TypeAssertExpr:
			if mode.path == "" && n.Type != nil && f.isMember(n.Type, mw3SessionSource, stack) {
				internal.Notef(fi, g.line(n.Pos()), mw3SourceTypeTestNote, g.text(n.Type))
			}
			return true
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

// mw3SessionSourcePass is the rewrite of one file by mw3SessionSourceContent.
type mw3SessionSourcePass struct {
	f    *mw3SessionFile
	mode *mw3SourceMode
	// twice are the Source types of a type switch that has a case string as well:
	// the two would be one case.
	twice   map[*ast.SelectorExpr]struct{}
	imports mw3ImportEdits
	edits   editSet
	fi      internal.FileInfo
}

// typeName returns the text that takes the place of session.Source: string, or the
// type of the module package.
func (p *mw3SessionSourcePass) typeName(sel *ast.SelectorExpr, stack []ast.Node, name string) string {
	if p.mode.path == "" {
		return "string"
	}
	pkg := p.f.importAt(&p.imports, p.mode.path, sel.Pos(), stack, nil, moduleShimPkg)
	return pkg + "." + name
}

// selector rewrites session.Source and its constants.
func (p *mw3SessionSourcePass) selector(sel *ast.SelectorExpr, stack []ast.Node) {
	g := p.f.g
	id, ok := sel.X.(*ast.Ident)
	if !ok || !p.f.isPackage(id, stack) {
		return
	}

	switch name := sel.Sel.Name; {
	case name == mw3SessionSource:
		if _, dup := p.twice[sel]; dup {
			return
		}
		if field, pointer := mw3EmbeddedSource(stack); field != nil {
			// The field is called Source, whatever the type is written as.
			text := "Source " + p.typeName(sel, stack, sessionShimType)
			if pointer {
				text = "Source *" + p.typeName(sel, stack, sessionShimType)
			}
			p.edits.replaceNode(g, field.Type, text)
			p.imports.releaseIn(g, field.Type)
			return
		}
		p.edits.replaceNode(g, sel, p.typeName(sel, stack, sessionShimType))
		p.imports.releaseIn(g, sel)
		if p.mode.path == "" && mw3IsTypeSwitchCase(stack) {
			internal.Notef(p.fi, g.line(sel.Pos()), mw3SourceTypeTestNote, g.text(sel))
		}
	case mw3SessionSourceValues[name] != "":
		literal := strconv.Quote(mw3SessionSourceValues[name])
		if conversion := mw3StringConversion(sel, stack); conversion != nil {
			// string(session.SourceCookie) is the string itself.
			p.edits.replaceNode(g, conversion, literal)
			p.imports.releaseIn(g, conversion)
			return
		}
		if p.mode.path == "" {
			p.edits.replaceNode(g, sel, literal)
		} else {
			p.edits.replaceNode(g, sel, p.typeName(sel, stack, sessionShimConstants[name]))
		}
		p.imports.releaseIn(g, sel)
	}
}

const mw3SourceTypeTestNote = "%s is string now, and no type of its own: this also matches a plain string, which it did not in v2."

// mw3IsTypeSwitchCase reports whether the type whose ancestors are stack is one
// of the cases of a type switch.
func mw3IsTypeSwitchCase(stack []ast.Node) bool {
	n := len(stack)
	if n < 3 {
		return false
	}
	if _, ok := stack[n-1].(*ast.CaseClause); !ok {
		return false
	}
	_, ok := stack[n-3].(*ast.TypeSwitchStmt)
	return ok
}

// mw3SourcesNextToString returns the Source types that are cases of a type
// switch which has a case string as well. v2 told them apart, and a type switch
// cannot have the same type twice.
func mw3SourcesNextToString(f *mw3SessionFile, sw *ast.TypeSwitchStmt, stack []ast.Node) []*ast.SelectorExpr {
	var sources []*ast.SelectorExpr
	hasString := false
	for _, stmt := range sw.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, e := range clause.List {
			if isIdentNamed(e, "string") {
				hasString = true
			}
			if sel, isSel := e.(*ast.SelectorExpr); isSel && f.isMember(sel, mw3SessionSource, stack) {
				sources = append(sources, sel)
			}
		}
	}
	if !hasString {
		return nil
	}
	return sources
}

// mw3StringConversion returns the call string(arg) when arg, whose ancestors are
// stack, is the only argument of one.
func mw3StringConversion(arg ast.Expr, stack []ast.Node) *ast.CallExpr {
	if len(stack) == 0 {
		return nil
	}
	call, ok := stack[len(stack)-1].(*ast.CallExpr)
	if !ok || !isIdentNamed(call.Fun, "string") || len(call.Args) != 1 || call.Args[0] != arg || call.Ellipsis.IsValid() {
		return nil
	}
	return call
}

// mw3EmbeddedSource returns the embedded field of a struct whose type is the one
// whose ancestors are stack, and whether the type is a pointer. It is nil when the
// type is not that of an embedded field.
func mw3EmbeddedSource(stack []ast.Node) (field *ast.Field, pointer bool) {
	i := len(stack) - 1
	for i >= 0 {
		switch stack[i].(type) {
		case *ast.StarExpr:
			pointer = true
			i--
			continue
		case *ast.ParenExpr:
			i--
			continue
		}
		break
	}
	if i < 2 {
		return nil, false
	}
	field, ok := stack[i].(*ast.Field)
	if !ok || len(field.Names) > 0 {
		return nil, false
	}
	if _, inStruct := stack[i-2].(*ast.StructType); !inStruct {
		return nil, false
	}
	return field, pointer
}
