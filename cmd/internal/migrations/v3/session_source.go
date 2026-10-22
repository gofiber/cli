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
var mw3SessionSourceValues = map[string]string{
	"SourceCookie":   mw3SourceCookie,
	"SourceHeader":   mw3SourceHeader,
	"SourceURLQuery": mw3SourceQuery,
}

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
// is asked for, in a type assertion or a type switch, and string does not. Those
// are reported.
//
// What cannot be written that way is left for the extractor rewrite to report:
// an embedded field (its name would change), a file that declares string itself,
// and the Source in a type switch that has a case string, which would be there
// twice.
func mw3SessionSourceContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	if !strings.Contains(src, mw3SessionSource) {
		return src
	}
	f, ok := mw3OpenSessionFile(fi, src, pkgs)
	if !ok {
		return src
	}
	g := f.g
	if _, hidden := mw3DeclaredNames(g.file)["string"]; hidden {
		return src
	}

	var (
		edits   editSet
		imports mw3ImportEdits
		twice   = map[*ast.SelectorExpr]struct{}{}
	)
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch n := n.(type) {
		case *ast.TypeSwitchStmt:
			for _, sel := range mw3SourcesNextToString(f, n, stack) {
				twice[sel] = struct{}{}
			}
			return true
		case *ast.TypeAssertExpr:
			if n.Type != nil && f.isMember(n.Type, mw3SessionSource, stack) {
				internal.Notef(fi, g.line(n.Pos()), mw3SourceTypeTestNote, g.text(n.Type))
			}
			return true
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || !f.isPackage(id, stack) {
			return true
		}

		switch name := sel.Sel.Name; {
		case name == mw3SessionSource:
			if _, dup := twice[sel]; !dup && !mw3IsEmbeddedField(stack) {
				edits.replaceNode(g, sel, "string")
				imports.releaseIn(g, sel)
				if mw3IsTypeSwitchCase(stack) {
					internal.Notef(fi, g.line(sel.Pos()), mw3SourceTypeTestNote, g.text(sel))
				}
			}
		case mw3SessionSourceValues[name] != "":
			literal := strconv.Quote(mw3SessionSourceValues[name])
			if conversion := mw3StringConversion(sel, stack); conversion != nil {
				// string(session.SourceCookie) is the string itself.
				edits.replaceNode(g, conversion, literal)
				imports.releaseIn(g, conversion)
				return true
			}
			edits.replaceNode(g, sel, literal)
			imports.releaseIn(g, sel)
		}
		return true
	})

	out := edits.apply(src)
	if out == src {
		return src
	}
	return imports.apply(out)
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

// mw3IsEmbeddedField reports whether the type whose ancestors are stack is the
// type of an embedded field of a struct, whose name is the name of the type.
func mw3IsEmbeddedField(stack []ast.Node) bool {
	i := len(stack) - 1
	for i >= 0 {
		switch stack[i].(type) {
		case *ast.StarExpr, *ast.ParenExpr:
			i--
			continue
		}
		break
	}
	if i < 2 {
		return false
	}
	field, ok := stack[i].(*ast.Field)
	if !ok || len(field.Names) > 0 {
		return false
	}
	_, inStruct := stack[i-2].(*ast.StructType)
	return inStruct
}
