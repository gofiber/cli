package v3

import (
	"go/ast"
	"go/token"
	"slices"
)

// ctxSite is a place that declares a name, with what is known about its type.
type ctxSite struct {
	// scope is the node the name lives in: the function of a parameter, the block
	// of a variable, the statement of a range variable.
	scope ast.Node
	// typ is the type it is declared with.
	typ ast.Expr
	// init is the value it starts with, or the call it is one of the results of.
	init ast.Expr
	// over is the value a range statement loops over, for the variable that
	// takes its elements, and key whether the name is the key and not the value.
	over ast.Expr
	// stack holds the ancestors of init and over.
	stack []ast.Node
	// index is the position of the name among the results of init when that is
	// a call with several, -1 otherwise.
	index int
	// key is set for the key variable of a range statement.
	key bool
}

// ctxResolveSite finds the declaration that name refers to at pos, given the
// ancestors of the use (see goSource.walk). ok is false when no declaration of a
// function body declares it, which is the case for the names of the package,
// the imports and the predeclared ones.
func ctxResolveSite(name string, pos token.Pos, stack []ast.Node) (site ctxSite, ok bool) {
	for i, node := range slices.Backward(stack) {
		scope := stack[:i+1]
		var stmts []ast.Stmt

		switch n := node.(type) {
		case *ast.BlockStmt:
			stmts = n.List
		case *ast.CaseClause:
			if site, ok = ctxTypeSwitchSite(name, n, stack[:i]); ok {
				return site, true
			}
			stmts = n.Body
		case *ast.CommClause:
			stmts = append([]ast.Stmt{n.Comm}, n.Body...)
		case *ast.IfStmt:
			stmts = []ast.Stmt{n.Init}
		case *ast.ForStmt:
			stmts = []ast.Stmt{n.Init}
		case *ast.SwitchStmt:
			stmts = []ast.Stmt{n.Init}
		case *ast.TypeSwitchStmt:
			stmts = []ast.Stmt{n.Init}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE && pos > n.Body.Lbrace {
				switch {
				case isIdentNamed(n.Value, name):
					return ctxSite{scope: n, over: n.X, stack: scope, index: -1}, true
				case isIdentNamed(n.Key, name):
					return ctxSite{scope: n, over: n.X, stack: scope, index: -1, key: true}, true
				}
			}
		case *ast.FuncLit:
			if site, ok = ctxParamSite(name, pos, n, n.Body, n.Type, nil); ok {
				return site, true
			}
		case *ast.FuncDecl:
			if site, ok = ctxParamSite(name, pos, n, n.Body, n.Type, n.Recv); ok {
				return site, true
			}
		}

		if site, ok = ctxStmtSite(name, pos, stmts, scope); ok {
			return site, true
		}
	}
	return ctxSite{}, false
}

// ctxTypeSwitchSite finds the variable that the type switch binds in the case
// clause: it has the type of the case when that names one type, and the type of
// the switched value otherwise. outer holds the ancestors of the clause.
func ctxTypeSwitchSite(name string, clause *ast.CaseClause, outer []ast.Node) (ctxSite, bool) {
	if len(outer) < 2 {
		return ctxSite{}, false
	}
	ts, ok := outer[len(outer)-2].(*ast.TypeSwitchStmt)
	if !ok {
		return ctxSite{}, false
	}
	as, ok := ts.Assign.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 || !isIdentNamed(as.Lhs[0], name) {
		return ctxSite{}, false
	}

	site := ctxSite{scope: clause, index: -1}
	if len(clause.List) == 1 && !isIdentNamed(clause.List[0], "nil") {
		site.typ = clause.List[0]
		return site, true
	}
	if assert, isAssert := as.Rhs[0].(*ast.TypeAssertExpr); isAssert {
		site.init, site.stack = assert.X, slices.Concat(outer[:len(outer)-2], []ast.Node{ts})
	}
	return site, true
}

// ctxParamSite looks name up among the receiver, parameters and results of the
// function fn, whose body holds pos.
func ctxParamSite(name string, pos token.Pos, fn ast.Node, body *ast.BlockStmt, ft *ast.FuncType, recv *ast.FieldList) (ctxSite, bool) {
	if body == nil || pos <= body.Lbrace {
		return ctxSite{}, false
	}

	for _, fields := range []*ast.FieldList{recv, ft.Params, ft.Results} {
		if fields == nil {
			continue
		}
		for _, f := range fields.List {
			if slices.ContainsFunc(f.Names, func(id *ast.Ident) bool { return id.Name == name }) {
				return ctxSite{scope: fn, typ: f.Type, index: -1}, true
			}
		}
	}
	return ctxSite{}, false
}

// ctxStmtSite looks for the declaration of name among the statements that end
// before pos, the closest first. scope holds the ancestors of the statements.
func ctxStmtSite(name string, pos token.Pos, stmts []ast.Stmt, scope []ast.Node) (ctxSite, bool) {
	for _, stmt := range slices.Backward(stmts) {
		if stmt == nil || stmt.End() > pos {
			continue
		}

		switch s := stmt.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				continue
			}
			for j, lhs := range s.Lhs {
				if !isIdentNamed(lhs, name) {
					continue
				}
				site := ctxSite{scope: scope[len(scope)-1], stack: slices.Concat(scope, []ast.Node{s}), index: -1}
				switch {
				case len(s.Rhs) == len(s.Lhs):
					site.init = s.Rhs[j]
				case len(s.Rhs) == 1:
					site.init, site.index = s.Rhs[0], j
				}
				return site, true
			}
		case *ast.DeclStmt:
			gd, ok := s.Decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			if site, found := ctxSpecSite(name, gd, slices.Concat(scope, []ast.Node{s, gd})); found {
				site.scope = scope[len(scope)-1]
				return site, true
			}
		}
	}
	return ctxSite{}, false
}

// ctxSpecSite looks for the declaration of name among the specs of gd, whose
// ancestors, itself included, are stack.
func ctxSpecSite(name string, gd *ast.GenDecl, stack []ast.Node) (ctxSite, bool) {
	for _, spec := range gd.Specs {
		switch sp := spec.(type) {
		case *ast.TypeSpec:
			if sp.Name.Name == name {
				return ctxSite{index: -1}, true
			}
		case *ast.ValueSpec:
			for j, id := range sp.Names {
				if id.Name != name {
					continue
				}
				site := ctxSite{index: -1}
				if gd.Tok == token.VAR {
					site.typ, site.stack = sp.Type, slices.Concat(stack, []ast.Node{sp})
					switch {
					case len(sp.Values) == len(sp.Names):
						site.init = sp.Values[j]
					case len(sp.Values) == 1:
						site.init, site.index = sp.Values[0], j
					}
				}
				return site, true
			}
		}
	}
	return ctxSite{}, false
}

// ctxVariableStable reports whether the variable that name refers to in the use
// whose ancestors are stack keeps the value it got when it was declared: nothing
// assigns it again, takes its address or declares the name once more in the scope
// it lives in. A function literal that uses such a variable by name sees what a
// method value bound to it when it was made saw. The scan is by name over the
// whole scope, so a variable it can not vouch for (a package-level one, one that
// is assigned anywhere, one whose name is declared again below) is not stable.
func ctxVariableStable(name string, pos token.Pos, stack []ast.Node) bool {
	site, ok := ctxResolveSite(name, pos, stack)
	if !ok || site.scope == nil {
		return false
	}

	declarations, stable := 0, true
	declare := func(define bool) {
		if define {
			declarations++
		} else {
			stable = false
		}
	}

	ast.Inspect(site.scope, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if isIdentNamed(ast.Unparen(lhs), name) {
					declare(x.Tok == token.DEFINE)
				}
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{x.Key, x.Value} {
				if e != nil && isIdentNamed(ast.Unparen(e), name) {
					declare(x.Tok == token.DEFINE)
				}
			}
		case *ast.IncDecStmt:
			if isIdentNamed(ast.Unparen(x.X), name) {
				stable = false
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND && isIdentNamed(ast.Unparen(x.X), name) {
				stable = false
			}
		case *ast.ValueSpec:
			for _, id := range x.Names {
				if id.Name == name {
					declarations++
				}
			}
		case *ast.FuncLit:
			declarations += ctxCountNames(name, x.Type.Params, x.Type.Results)
		case *ast.FuncDecl:
			declarations += ctxCountNames(name, x.Recv, x.Type.Params, x.Type.Results)
		case *ast.StructType, *ast.InterfaceType, *ast.FuncType:
			// the field, method and parameter names of a type declare no variable (the
			// parameters of a function are counted with the function)
			return false
		}
		return stable
	})

	// The declaration the use refers to is part of the scope (a type switch clause
	// is the exception, its variable is declared by the switch around it); a second
	// one may or may not be a different variable, and is not taken for one.
	return stable && declarations <= 1
}

// ctxCountNames returns how many of the names declared by the field lists are name.
func ctxCountNames(name string, lists ...*ast.FieldList) int {
	n := 0
	for _, list := range lists {
		if list == nil {
			continue
		}
		for _, f := range list.List {
			for _, id := range f.Names {
				if id.Name == name {
					n++
				}
			}
		}
	}
	return n
}
