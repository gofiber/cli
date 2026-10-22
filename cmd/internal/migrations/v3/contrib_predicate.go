package v3

import (
	"go/ast"
	"go/token"
)

// contribPredicate is a func literal of the shape func(ctx) bool { return expr }:
// the only shape whose result can be changed in place.
type contribPredicate struct {
	ret *ast.ReturnStmt
	// param is the name of the context parameter, "" when it has none.
	param string
}

// contribPredicateOf recognizes the func literal e.
func contribPredicateOf(e ast.Expr) (p contribPredicate, ok bool) {
	lit, ok := e.(*ast.FuncLit)
	if !ok || lit.Type.Results == nil || len(lit.Type.Results.List) != 1 || len(lit.Type.Results.List[0].Names) > 1 {
		return contribPredicate{}, false
	}
	if lit.Body == nil || len(lit.Body.List) != 1 {
		return contribPredicate{}, false
	}
	ret, ok := lit.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return contribPredicate{}, false
	}

	p.ret = ret
	if params := lit.Type.Params; params != nil && len(params.List) == 1 && len(params.List[0].Names) == 1 {
		if name := params.List[0].Names[0].Name; name != "_" {
			p.param = name
		}
	}
	return p, true
}

// contribNegate writes the negation of the boolean expression x.
func contribNegate(g *goSource, x ast.Expr) string {
	switch e := x.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			return g.text(e.X)
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.EQL:
			return g.text(e.X) + " != " + g.text(e.Y)
		case token.NEQ:
			return g.text(e.X) + " == " + g.text(e.Y)
		default:
			return "!(" + g.text(x) + ")"
		}
	case *ast.Ident, *ast.CallExpr, *ast.SelectorExpr, *ast.IndexExpr, *ast.ParenExpr:
		return "!" + g.text(x)
	}
	return "!(" + g.text(x) + ")"
}

// contribFreeName returns the first of names that is not an identifier of e.
func contribFreeName(e ast.Expr, names ...string) string {
	used := map[string]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for _, name := range names {
		if !used[name] {
			return name
		}
	}
	return names[len(names)-1] + "_"
}

// contribCallText writes e so that it can be called: func literals and other
// expressions that would not call as they are get parentheses.
func contribCallText(g *goSource, e ast.Expr) string {
	switch e.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return g.text(e)
	}
	return "(" + g.text(e) + ")"
}

// contribIsNil reports whether e is the identifier nil.
func contribIsNil(e ast.Expr) bool {
	return isIdentNamed(e, "nil")
}
