package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
)

// clientList is a statement list together with the context needed to answer
// scoping questions about it: the names its owner binds and the function it
// belongs to.
type clientList struct {
	owner ast.Node
	fn    *clientFunc
	stmts []ast.Stmt
	// bound are the names bound by the construct that owns the list and that
	// live in the same scope as the list: parameters, results and receivers
	// of a function body, the binding of a type switch or select case.
	bound []string
}

// clientFunc is what the migration knows about the function a statement list
// belongs to.
type clientFunc struct {
	// mentions are the identifiers used anywhere in the top level declaration
	// (selector names excluded), names the migration introduced included.
	mentions map[string]bool
	// closureNames are the identifiers used inside function literals, defer
	// and go statements: they may observe a variable later than it is assigned.
	closureNames map[string]bool
	// results are the named results of the innermost function.
	results []string
	// bareReturns are the positions of the return statements of the innermost
	// function that name no values.
	bareReturns []token.Pos
}

// clientRef is one occurrence of a name that refers to the variable being
// analyzed.
type clientRef struct {
	id *ast.Ident
	// stack holds the ancestors of id, outermost first. A statement list that
	// starts the walk is not part of it.
	stack []ast.Node
}

// clientRefWalker finds the occurrences of one variable, honoring the scopes
// that redeclare its name.
type clientRefWalker struct {
	name  string
	refs  []clientRef
	stack []ast.Node
	// end is the index of the top level statement that redeclared the name,
	// len(stmts) when none did.
	end int
}

// clientRefsIn returns the occurrences of the variable name in stmts, the
// statements in the scope of its declaration. Occurrences inside a nested scope
// that declares the name again, and everything after a top level redeclaration,
// belong to another variable and are left out. end is the index of the top
// level statement that redeclared the name, len(stmts) when there is none. That
// statement is not searched, except for the initializers it evaluates before
// the new variable exists.
func clientRefsIn(name string, stmts []ast.Stmt) (refs []clientRef, end int) {
	return clientRefsInNodes(name, nil, stmts)
}

// clientRefsInNodes is clientRefsIn for a scope that starts with nodes that
// cannot redeclare the name, such as the condition and body of an if statement,
// followed by statements.
func clientRefsInNodes(name string, nodes []ast.Node, stmts []ast.Stmt) (refs []clientRef, end int) {
	w := &clientRefWalker{name: name, end: len(stmts)}
	for _, n := range nodes {
		w.walk(n)
	}
	for i, s := range stmts {
		if clientStmtDeclares(s, name) {
			w.walkInitializers(s)
			w.end = i
			break
		}
		w.walk(s)
	}
	return w.refs, w.end
}

// walk visits the nodes below root.
func (w *clientRefWalker) walk(root ast.Node) {
	if root == nil {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			w.stack = w.stack[:len(w.stack)-1]
			return true
		}
		if w.scope(n) {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == w.name {
			w.refs = append(w.refs, clientRef{id: id, stack: slices.Clone(w.stack)})
		}
		w.stack = append(w.stack, n)
		return true
	})
}

// walkExprs visits expressions.
func (w *clientRefWalker) walkExprs(exprs []ast.Expr) {
	for _, e := range exprs {
		w.walk(e)
	}
}

// walkInitializers visits what the declaring statement s evaluates before the
// declared names exist.
func (w *clientRefWalker) walkInitializers(s ast.Stmt) {
	switch x := s.(type) {
	case *ast.AssignStmt:
		w.walkExprs(x.Rhs)
	case *ast.DeclStmt:
		gd, ok := x.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, spec := range gd.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				w.walkExprs(vs.Values)
			}
		}
	case *ast.LabeledStmt:
		w.walkInitializers(x.Stmt)
	}
}

func (w *clientRefWalker) enter(n ast.Node) { w.stack = append(w.stack, n) }
func (w *clientRefWalker) leave()           { w.stack = w.stack[:len(w.stack)-1] }

// list visits a nested statement list. A statement that declares the name
// again ends the scope of the variable.
func (w *clientRefWalker) list(stmts []ast.Stmt) {
	for _, s := range stmts {
		if clientStmtDeclares(s, w.name) {
			w.walkInitializers(s)
			return
		}
		w.walk(s)
	}
}

// scope handles the nodes that open a scope or that name no variable, and
// reports whether it visited the children of n itself.
func (w *clientRefWalker) scope(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		w.enter(x)
		w.walk(x.X)
		w.leave()
	case *ast.BlockStmt:
		w.enter(x)
		w.list(x.List)
		w.leave()
	case *ast.CaseClause:
		w.enter(x)
		w.walkExprs(x.List)
		w.list(x.Body)
		w.leave()
	case *ast.CommClause:
		w.enter(x)
		if x.Comm != nil && clientStmtDeclares(x.Comm, w.name) {
			w.walkInitializers(x.Comm)
		} else {
			w.walk(x.Comm)
			w.list(x.Body)
		}
		w.leave()
	case *ast.IfStmt:
		w.enter(x)
		if x.Init != nil && clientStmtDeclares(x.Init, w.name) {
			w.walkInitializers(x.Init)
		} else {
			w.walk(x.Init)
			w.walk(x.Cond)
			w.walk(x.Body)
			w.walk(x.Else)
		}
		w.leave()
	case *ast.ForStmt:
		w.enter(x)
		if x.Init != nil && clientStmtDeclares(x.Init, w.name) {
			w.walkInitializers(x.Init)
		} else {
			w.walk(x.Init)
			w.walk(x.Cond)
			w.walk(x.Post)
			w.walk(x.Body)
		}
		w.leave()
	case *ast.RangeStmt:
		w.enter(x)
		w.walk(x.X)
		if x.Tok == token.DEFINE && (isIdentNamed(x.Key, w.name) || isIdentNamed(x.Value, w.name)) {
			w.leave()
			return true
		}
		w.walk(x.Key)
		w.walk(x.Value)
		w.walk(x.Body)
		w.leave()
	case *ast.SwitchStmt:
		w.enter(x)
		if x.Init != nil && clientStmtDeclares(x.Init, w.name) {
			w.walkInitializers(x.Init)
		} else {
			w.walk(x.Init)
			w.walk(x.Tag)
			w.walk(x.Body)
		}
		w.leave()
	case *ast.TypeSwitchStmt:
		w.enter(x)
		switch {
		case x.Init != nil && clientStmtDeclares(x.Init, w.name):
			w.walkInitializers(x.Init)
		case clientStmtDeclares(x.Assign, w.name):
			w.walk(x.Init)
			w.walkInitializers(x.Assign)
		default:
			w.walk(x.Init)
			w.walk(x.Assign)
			w.walk(x.Body)
		}
		w.leave()
	case *ast.FuncLit:
		w.enter(x)
		if !clientFuncTypeDeclares(x.Type, w.name) {
			w.walk(x.Type)
			w.walk(x.Body)
		}
		w.leave()
	default:
		return false
	}
	return true
}

// clientStmtDeclares reports whether s declares name in the scope it appears
// in: a short variable declaration, or a var, const or type declaration.
func clientStmtDeclares(s ast.Stmt, name string) bool {
	switch x := s.(type) {
	case *ast.AssignStmt:
		if x.Tok != token.DEFINE {
			return false
		}
		return slices.ContainsFunc(x.Lhs, func(e ast.Expr) bool { return isIdentNamed(e, name) })
	case *ast.DeclStmt:
		gd, ok := x.Decl.(*ast.GenDecl)
		if !ok {
			return false
		}
		for _, spec := range gd.Specs {
			switch sp := spec.(type) {
			case *ast.ValueSpec:
				if slices.ContainsFunc(sp.Names, func(id *ast.Ident) bool { return id.Name == name }) {
					return true
				}
			case *ast.TypeSpec:
				if sp.Name.Name == name {
					return true
				}
			}
		}
	case *ast.LabeledStmt:
		return clientStmtDeclares(x.Stmt, name)
	}
	return false
}

// clientFuncTypeDeclares reports whether the parameters or results of ft
// declare name.
func clientFuncTypeDeclares(ft *ast.FuncType, name string) bool {
	if ft == nil {
		return false
	}
	_, found := declInFields(name, ft)
	return found
}

// clientDeclaredBefore reports whether name is declared in the scope of the
// list before the statement at index idx.
func clientDeclaredBefore(l clientList, idx int, name string) bool {
	if slices.Contains(l.bound, name) {
		return true
	}
	for _, s := range l.stmts[:min(idx, len(l.stmts))] {
		if clientStmtDeclares(s, name) {
			return true
		}
	}
	return false
}

// clientTopLevelDefineNames returns the names a short variable declaration or
// var declaration introduces.
func clientDefinedNames(s ast.Stmt) []string {
	var names []string
	switch x := s.(type) {
	case *ast.AssignStmt:
		if x.Tok == token.DEFINE {
			for _, e := range x.Lhs {
				if id, ok := e.(*ast.Ident); ok {
					names = append(names, id.Name)
				}
			}
		}
	case *ast.DeclStmt:
		if gd, ok := x.Decl.(*ast.GenDecl); ok {
			for _, spec := range gd.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, id := range vs.Names {
						names = append(names, id.Name)
					}
				}
			}
		}
	}
	return names
}

// clientDefineStaysValid reports whether the statement s, which declares name,
// is still valid when name already exists in its scope: only a short variable
// declaration with another new variable on the left is. declared reports
// whether a name is declared in the scope before s.
func clientDefineStaysValid(s ast.Stmt, name string, declared func(string) bool) bool {
	as, ok := s.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE {
		return false
	}
	for _, n := range clientDefinedNames(as) {
		if n != name && n != "_" && !declared(n) {
			return true
		}
	}
	return false
}

// clientNewFunc collects the facts about a top level declaration that the
// naming decisions need.
func clientNewFunc(root ast.Node) *clientFunc {
	fn := &clientFunc{mentions: map[string]bool{}, closureNames: map[string]bool{}}

	var visit func(n ast.Node, inClosure bool)
	visit = func(n ast.Node, inClosure bool) {
		ast.Inspect(n, func(c ast.Node) bool {
			switch x := c.(type) {
			case *ast.SelectorExpr:
				visit(x.X, inClosure)
				return false
			case *ast.FuncLit:
				visit(x.Type, true)
				visit(x.Body, true)
				return false
			case *ast.DeferStmt:
				visit(x.Call, true)
				return false
			case *ast.GoStmt:
				visit(x.Call, true)
				return false
			case *ast.Ident:
				fn.mentions[x.Name] = true
				if inClosure {
					fn.closureNames[x.Name] = true
				}
			}
			return true
		})
	}
	visit(root, false)
	return fn
}

// forFunction returns a copy of fn that describes the function with the given
// type and body: its named results and its bare returns.
func (fn *clientFunc) forFunction(ft *ast.FuncType, body *ast.BlockStmt) *clientFunc {
	c := *fn
	c.results = nil
	c.bareReturns = nil
	if ft != nil && ft.Results != nil {
		for _, f := range ft.Results.List {
			for _, id := range f.Names {
				c.results = append(c.results, id.Name)
			}
		}
	}
	if body != nil {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt:
				if len(x.Results) == 0 {
					c.bareReturns = append(c.bareReturns, x.Pos())
				}
			}
			return true
		})
	}
	return &c
}

// fresh returns an identifier based on base that nothing in the function uses
// and records it as taken. The candidates are base, base2, base3 and so on.
func (fn *clientFunc) fresh(base string) string {
	name := base
	for i := 2; fn.mentions[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	fn.mentions[name] = true
	return name
}
