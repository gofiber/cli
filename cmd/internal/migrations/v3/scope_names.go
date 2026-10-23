package v3

import (
	"go/ast"
	"go/token"
	"slices"
)

// A rewrite that writes the name of a package (fiber.Query, strconv.Atoi, ...) has
// to know that the name means the package where the code goes: a parameter, a
// variable, a constant or a type of the function can hide the name of an import
// (func start(fiber *fiber.App) { fiber.Listen(...) }), and the package cannot be
// named there. localScopes answers that question from the AST, by position, with
// the scoping rules of the language: where the scope of a name declared inside a
// function begins and ends. It knows nothing about package level declarations,
// which cannot share a name with an import of the file.

// scopeRange is the part [from, to) of a file in which a declaration hides a name.
type scopeRange struct {
	from, to token.Pos
}

// localScopes holds, for every name that a function of the file declares, the
// parts of the file in which the declaration hides the name.
type localScopes struct {
	ranges map[string][]scopeRange
}

// newLocalScopes reads the declarations inside the functions of the file.
func newLocalScopes(file *ast.File) *localScopes {
	s := &localScopes{ranges: map[string][]scopeRange{}}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			s.funcDecl(x)
		case *ast.FuncLit:
			if x.Body != nil {
				s.fields(x.Type.Params, x.Body.Lbrace, x.Body.End())
				s.fields(x.Type.Results, x.Body.Lbrace, x.Body.End())
			}
		case *ast.BlockStmt:
			s.list(x.List, x.Rbrace)
		case *ast.IfStmt:
			s.initStmt(x.Init, x.End())
		case *ast.ForStmt:
			s.initStmt(x.Init, x.End())
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE && x.Body != nil {
				s.add(x.Key, x.Body.Lbrace, x.Body.End())
				s.add(x.Value, x.Body.Lbrace, x.Body.End())
			}
		case *ast.SwitchStmt:
			s.initStmt(x.Init, x.End())
			s.clauses(x.Body, nil)
		case *ast.TypeSwitchStmt:
			s.initStmt(x.Init, x.End())
			s.clauses(x.Body, typeSwitchBinding(x))
		case *ast.SelectStmt:
			s.clauses(x.Body, nil)
		}
		return true
	})
	return s
}

// hiddenAt reports whether a declaration inside a function hides name at pos.
func (s *localScopes) hiddenAt(name string, pos token.Pos) bool {
	return slices.ContainsFunc(s.ranges[name], func(r scopeRange) bool { return r.from <= pos && pos < r.to })
}

// declared reports whether a function of the file declares name anywhere.
func (s *localScopes) declared(name string) bool {
	return len(s.ranges[name]) > 0
}

// scopeTable returns the local scopes of the file, read when first asked for.
func (g *goSource) scopeTable() *localScopes {
	if g.scopes == nil {
		g.scopes = newLocalScopes(g.file)
	}
	return g.scopes
}

// hiddenAt reports whether name means a declaration inside a function at pos, and
// not what it means at the level of the file or the package.
func (g *goSource) hiddenAt(name string, pos token.Pos) bool {
	return g.scopeTable().hiddenAt(name, pos)
}

// declaresLocally reports whether a function of the file declares name anywhere.
func (g *goSource) declaresLocally(name string) bool {
	return g.scopeTable().declared(name)
}

// hide records that name is hidden in [from, to).
func (s *localScopes) hide(name string, from, to token.Pos) {
	if name == "" || name == "_" || from >= to {
		return
	}
	s.ranges[name] = append(s.ranges[name], scopeRange{from: from, to: to})
}

// add records the name of the identifier e, when it is one.
func (s *localScopes) add(e ast.Expr, from, to token.Pos) {
	if id, ok := e.(*ast.Ident); ok {
		s.hide(id.Name, from, to)
	}
}

// fields records the names a list of parameters or results declares.
func (s *localScopes) fields(fl *ast.FieldList, from, to token.Pos) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		for _, id := range f.Names {
			s.hide(id.Name, from, to)
		}
	}
}

// funcDecl records the receiver, the parameters, the results and the type
// parameters of a function. The first three are in scope in the body, the type
// parameters from the name of the function on.
func (s *localScopes) funcDecl(fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
	}
	s.fields(fn.Recv, fn.Body.Lbrace, fn.Body.End())
	s.fields(fn.Type.Params, fn.Body.Lbrace, fn.Body.End())
	s.fields(fn.Type.Results, fn.Body.Lbrace, fn.Body.End())
	s.fields(fn.Type.TypeParams, fn.Name.End(), fn.Body.End())
	if fn.Recv != nil {
		for _, f := range fn.Recv.List {
			for _, param := range receiverTypeParams(f.Type) {
				s.hide(param.Name, fn.Name.End(), fn.Body.End())
			}
		}
	}
}

// receiverTypeParams returns the type parameters a method receiver declares:
// func (r *Box[T, U]) ...
func receiverTypeParams(t ast.Expr) []*ast.Ident {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.IndexExpr:
			if id, ok := x.Index.(*ast.Ident); ok {
				return []*ast.Ident{id}
			}
			return nil
		case *ast.IndexListExpr:
			var out []*ast.Ident
			for _, index := range x.Indices {
				if id, ok := index.(*ast.Ident); ok {
					out = append(out, id)
				}
			}
			return out
		default:
			return nil
		}
	}
}

// initStmt records the variables an if, for or switch statement declares in its
// initialization; they are in scope from the end of that statement to the end of
// the whole statement.
func (s *localScopes) initStmt(init ast.Stmt, end token.Pos) {
	if init != nil {
		s.stmt(init, init.End(), end)
	}
}

// list records the declarations of a statement list that ends at end (the closing
// brace of a block, the start of the next clause).
func (s *localScopes) list(stmts []ast.Stmt, end token.Pos) {
	for _, stmt := range stmts {
		s.stmt(stmt, stmt.End(), end)
	}
}

// stmt records what one statement declares for the rest of its block. A constant
// or a variable is in scope from the end of its spec, a type from its name.
func (s *localScopes) stmt(stmt ast.Stmt, after, end token.Pos) {
	switch x := stmt.(type) {
	case *ast.AssignStmt:
		if x.Tok == token.DEFINE {
			for _, lhs := range x.Lhs {
				s.add(lhs, after, end)
			}
		}
	case *ast.DeclStmt:
		gd, ok := x.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, spec := range gd.Specs {
			switch sp := spec.(type) {
			case *ast.ValueSpec:
				for _, id := range sp.Names {
					s.hide(id.Name, sp.End(), end)
				}
			case *ast.TypeSpec:
				s.hide(sp.Name.Name, sp.Name.Pos(), end)
			}
		}
	case *ast.LabeledStmt:
		s.stmt(x.Stmt, after, end)
	}
}

// clauses records the declarations of the clauses of a switch or select. Each
// clause is a block of its own that ends where the next one starts. binding is
// the name a type switch declares in every clause, when it does.
func (s *localScopes) clauses(body *ast.BlockStmt, binding *ast.Ident) {
	if body == nil {
		return
	}
	for i, c := range body.List {
		end := body.Rbrace
		if i+1 < len(body.List) {
			end = body.List[i+1].Pos()
		}

		switch cl := c.(type) {
		case *ast.CaseClause:
			if binding != nil {
				s.hide(binding.Name, cl.Colon, end)
			}
			s.list(cl.Body, end)
		case *ast.CommClause:
			if as, ok := cl.Comm.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
				for _, lhs := range as.Lhs {
					s.add(lhs, cl.Colon, end)
				}
			}
			s.list(cl.Body, end)
		}
	}
}

// typeSwitchBinding returns the identifier a type switch declares in its clauses:
// the x of switch x := v.(type).
func typeSwitchBinding(sw *ast.TypeSwitchStmt) *ast.Ident {
	as, ok := sw.Assign.(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 1 {
		return nil
	}
	if id, isIdent := as.Lhs[0].(*ast.Ident); isIdent {
		return id
	}
	return nil
}

// posAt returns the position of the byte at offset off of the source.
func (g *goSource) posAt(off int) token.Pos {
	return g.fset.File(g.file.Pos()).Pos(off)
}
