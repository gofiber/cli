package v3

import (
	"go/ast"
	"go/token"
)

// isAgentPtr reports whether t is *fiber.Agent.
func (f *clientFile) isAgentPtr(t ast.Expr) (*ast.SelectorExpr, bool) {
	star, ok := t.(*ast.StarExpr)
	if !ok {
		return nil, false
	}
	sel, ok := f.fiberSel(star.X)
	if !ok || sel.Sel.Name != nameAgent {
		return nil, false
	}
	return sel, true
}

// renameAgentTypes points the *fiber.Agent parameters and results of functions
// at *client.Request, the v3 type that holds a request that was not sent yet.
// stack holds the ancestors of the function type.
//
// The agents a function returns reach the code that calls it, which the
// migration only follows for the functions of the package with a single result
// (agentFromHelper): the results of every other function keep their type, and with
// it the package keeps its agents on the copy of the v2 agent.
func (f *clientFile) renameAgentTypes(ft *ast.FuncType, stack []ast.Node) {
	lists := []*ast.FieldList{ft.Params}
	if f.agentResultsFollowed(ft, stack) {
		lists = append(lists, ft.Results)
	}
	for _, fl := range lists {
		if fl == nil {
			continue
		}
		for _, field := range fl.List {
			sel, ok := f.isAgentPtr(field.Type)
			if !ok {
				continue
			}
			pkg := f.use(clientPkgPath, "client")
			f.edits.replace(f.edits.begin(), f.g.offset(sel.Pos()), f.g.offset(sel.End()), pkg+".Request")
			f.handled[sel] = true
		}
	}
}

// agentResultsFollowed reports whether the agent a function returns is followed
// to the code that calls it.
func (f *clientFile) agentResultsFollowed(ft *ast.FuncType, stack []ast.Node) bool {
	if len(stack) == 0 || !clientReturnsAgent(ft, f.fiber) {
		return false
	}
	decl, ok := stack[len(stack)-1].(*ast.FuncDecl)
	return ok && decl.Recv == nil
}

// agentParams migrates the uses of the parameters and named results of a
// function that are *fiber.Agent: the request was made by the caller.
func (f *clientFile) agentParams(ft *ast.FuncType, l clientList) {
	for _, fl := range []*ast.FieldList{ft.Params, ft.Results} {
		if fl == nil {
			continue
		}
		for _, field := range fl.List {
			if _, ok := f.isAgentPtr(field.Type); !ok {
				continue
			}
			for _, name := range field.Names {
				if name.Name == "_" {
					continue
				}
				f.migrate(&clientUnit{
					f: f, list: l, idx: -1, kind: clientExternal, varName: name.Name, anchor: name,
				})
			}
		}
	}
}

// agentFromHelper recognizes v := helper(...) where helper is a function of the
// package that returns a *fiber.Agent.
func (f *clientFile) agentFromHelper(l clientList, idx int) *clientUnit {
	s, ok := l.stmts[idx].(*ast.AssignStmt)
	if !ok || s.Tok != token.DEFINE || len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return nil
	}
	id, ok := s.Lhs[0].(*ast.Ident)
	call, isCall := s.Rhs[0].(*ast.CallExpr)
	if !ok || id.Name == "_" || !isCall {
		return nil
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || !f.pkgs.agentFunc(f.dir(), f.g.file.Name.Name, fn.Name) {
		return nil
	}
	f.helperCalls[call] = true
	return &clientUnit{f: f, list: l, idx: idx, kind: clientExternal, varName: id.Name, anchor: s}
}

// returnedAgents migrates the requests a return statement creates.
func (f *clientFile) returnedAgents(l clientList, s *ast.ReturnStmt) {
	for _, res := range s.Results {
		ch, ok := f.decompose(res)
		if !ok || ch.root.kind == clientVar || ch.terminal() != nil {
			continue
		}
		f.migrate(&clientUnit{
			f: f, list: l, kind: ch.root.kind, root: ch.root, url: ch.root.url, client: ch.root.client,
			method: ch.root.method, creationCalls: ch.calls, retExpr: res, anchor: s,
		})
	}
}
