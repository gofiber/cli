package v3

import (
	"go/ast"
	"go/token"
)

// exprType returns the type of the expression e written in the file mf, as far as
// the declarations around it say: a variable declared with a type or initialized
// with a value whose type is known, the field of a struct value, the result of a
// function of the module or of a method of one of its types, a composite
// literal, new(T). It reports false for anything else, and for a file outside of
// a module. stack holds the ancestors of e in the syntax tree of mf (nil for an
// expression at package level).
//
// The type is the one the declaration writes, together with the file that holds
// it, so that its names are resolved where they were written.
func exprType(mf *modFile, e ast.Expr, stack []ast.Node, depth int) (modType, bool) {
	if mf == nil || depth > modMaxDepth {
		return modType{}, false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return exprType(mf, x.X, stack, depth+1)
	case *ast.StarExpr:
		return exprType(mf, x.X, stack, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return exprType(mf, x.X, stack, depth+1)
		}
	case *ast.CompositeLit:
		if x.Type != nil {
			return modType{mf, x.Type}, true
		}
	case *ast.CallExpr:
		return callType(mf, x, stack, depth)
	case *ast.Ident:
		return identType(mf, x, stack, depth)
	case *ast.SelectorExpr:
		return selectorType(mf, x, stack, depth)
	}
	return modType{}, false
}

// callType is the type a call returns.
func callType(mf *modFile, call *ast.CallExpr, stack []ast.Node, depth int) (modType, bool) {
	fun := ast.Unparen(call.Fun)

	if id, ok := fun.(*ast.Ident); ok && id.Name == modBuiltinNew && len(call.Args) == 1 && !modDeclaredLocally(id.Name, id.Pos(), stack) && mf.declOf(id) == nil {
		return modType{mf, call.Args[0]}, true
	}

	if d := declAt(mf, fun, stack); d != nil && d.kind == modFunc {
		if results := d.results(); len(results) > 0 {
			return results[0], true
		}
		return modType{}, false
	}

	// a method of a value whose type is known
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if recv, known := exprType(mf, sel.X, stack, depth+1); known {
			if m := recv.method(sel.Sel.Name); m != nil {
				if results := m.results(); len(results) > 0 {
					return results[0], true
				}
			}
		}
	}
	return modType{}, false
}

// declAt returns the declaration at package level that e names, unless a
// declaration inside a function hides the name.
func declAt(mf *modFile, e ast.Expr, stack []ast.Node) *modDecl {
	switch x := e.(type) {
	case *ast.Ident:
		if modDeclaredLocally(x.Name, x.Pos(), stack) {
			return nil
		}
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && modDeclaredLocally(id.Name, id.Pos(), stack) {
			return nil
		}
	}
	return mf.declOf(e)
}

// identType is the type of a variable.
func identType(mf *modFile, id *ast.Ident, stack []ast.Node, depth int) (modType, bool) {
	if d, ok := resolveIdent(id.Name, id.Pos(), stack); ok {
		switch {
		case d.typ != nil:
			return modType{mf, d.typ}, true
		case d.init != nil:
			return exprType(mf, d.init, stack, depth+1)
		}
		return modType{}, false
	}
	return declType(declAt(mf, id, stack), depth)
}

// selectorType is the type of pkg.Var, or of the field of a struct value.
func selectorType(mf *modFile, sel *ast.SelectorExpr, stack []ast.Node, depth int) (modType, bool) {
	if d := declAt(mf, sel, stack); d != nil {
		return declType(d, depth)
	}
	if recv, ok := exprType(mf, sel.X, stack, depth+1); ok {
		if fld, found := recv.field(sel.Sel.Name); found {
			return fld.typ, true
		}
	}
	return modType{}, false
}

// declType is the type of the variable a declaration declares.
func declType(d *modDecl, depth int) (modType, bool) {
	if d == nil || d.dup || d.kind != modVar {
		return modType{}, false
	}
	if t, ok := d.varType(); ok {
		return t, true
	}
	if e, idx, ok := d.varInit(); ok && idx == 0 {
		return exprType(d.file, e, nil, depth+1)
	}
	return modType{}, false
}
