package v3

import (
	"go/ast"
	"slices"
)

// The code of a package that makes requests may hand the values of the v2
// client from one function to another: an agent a function takes or returns, a
// client kept in a variable. The migration follows them where it understands
// the code, and moves their types to the client package. Where it does not, the
// request keeps running on the copy of the v2 agent, and so does everything the
// request shares its agent or its client with: the types of one kind are either
// all the client package's or all the copy's, or the values would not fit where
// they meet. A package whose values are the copy's is tainted by a scan before
// the files are rewritten.

// pkgName returns the name of the package of the file.
func (f *clientFile) pkgName() string {
	return f.g.file.Name.Name
}

// taint records that the code of the package hands the values of a kind around in
// ways the migration does not follow. Only a scan does.
func (f *clientFile) taint(domain clientShimDomain) {
	if f.dry && (domain == clientDomainAgent || domain == clientDomainClient) {
		f.pkgs.taint(f.dir(), f.pkgName(), domain)
	}
}

// shimmed reports whether the values of a kind are the copy's in the package of
// the file. The migration then keeps their types and everything that makes or
// uses one on the copy.
func (f *clientFile) shimmed(domain clientShimDomain) bool {
	return !f.dry && f.pkgs.tainted(f.dir(), f.pkgName(), domain) && f.shimOK()
}

// connected reports whether the agent of the request is handed to or taken from
// other code: a function that receives it, returns it, or uses it in a way the
// migration does not follow.
func (u *clientUnit) connected() bool {
	return u.kind == clientExternal || u.retExpr != nil || u.flows
}

// viaClient reports whether the request is made by a fiber.Client.
func (u *clientUnit) viaClient() bool {
	return u.kind == clientViaClient || (u.root.call != nil && u.root.kind == clientViaClient)
}

// agentFlows reports whether the variable of the request is used for more than
// calling its methods: any other use may hand the agent to code the migration
// does not see.
func (f *clientFile) agentFlows(u *clientUnit) bool {
	if u.varName == "" || u.idx < 0 {
		return false
	}
	refs, _ := clientRefsIn(u.varName, u.list.stmts[u.idx+1:])
	return slices.ContainsFunc(refs, func(r clientRef) bool {
		if len(r.stack) == 0 {
			return true
		}
		switch parent := r.stack[len(r.stack)-1].(type) {
		case *ast.SelectorExpr:
			return parent.X != ast.Expr(r.id)
		case *ast.CallExpr:
			return !f.isRelease(parent, u.varName)
		}
		return true
	})
}

// sharedWithCopy returns the note for a request that could be migrated but has to
// stay on the copy of the v2 agent: it shares its agent or its client with code
// that does.
func (f *clientFile) sharedWithCopy(u *clientUnit) string {
	switch {
	case u.connected() && f.shimmed(clientDomainAgent):
		return "its agent is shared with code that stays on the copy of the Fiber v2 agent in " + clientShimWhere + ", so this request does too"
	case u.viaClient() && f.shimmed(clientDomainClient):
		return "its client is shared with requests that stay on the copy of the Fiber v2 agent in " + clientShimWhere + ", so this request does too"
	}
	return ""
}

// leftovers deals with the members of the v2 client that are still used once the
// requests were migrated. A scan taints the package with them; the migration
// renames them to the names of the copy, or reports them where the package
// cannot have the copy.
func (f *clientFile) leftovers() {
	f.g.walk(func(n ast.Node, _ []ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !f.isFiberName(sel.X) || !slices.Contains(clientAPIMembers, sel.Sel.Name) || f.handled[sel] || f.reported[sel] {
			return true
		}

		domain := clientShimMembers[sel.Sel.Name]
		switch {
		case f.dry:
			f.taint(domain)
		case domain != 0 && f.shimOK():
			f.renameToCopy(sel)
		default:
			f.reported[sel] = true
			f.notef(f.line(sel), "`%s.%s` does not exist in Fiber v3 and was not migrated; use the client package (client.Get, client.Post, client.New().R(), ...)%s",
				f.g.text(sel.X), sel.Sel.Name, f.copyMissing(domain != 0))
		}
		return true
	})
}

// scanReferences taints the package with the uses of agents and clients that no
// request was made of: a call on a fiber.Client that is not part of a request, and
// a use of a function that returns an agent where the result is not taken by a
// variable the migration follows.
func (f *clientFile) scanReferences() {
	dir, pkg := f.dir(), f.pkgName()
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && clientIsMethod(sel.Sel.Name) && len(x.Args) == 1 && !f.viaClient[sel] && f.isFiberClientExpr(sel.X) {
				f.taint(clientDomainClient)
			}
		case *ast.Ident:
			if f.pkgs.agentFunc(dir, pkg, x.Name) && !f.isFollowedRef(x, stack) {
				f.taint(clientDomainAgent)
			}
		}
		return true
	})
}

// isFollowedRef reports whether the identifier of a function that returns an
// agent is a use of the function the migration follows: a call whose result a
// request was made of, one that goes to a parameter of a function of the package
// that takes an agent, or one that is returned by a function of the package that
// returns an agent; or it only names the function (its declaration, a field or a
// method).
func (f *clientFile) isFollowedRef(id *ast.Ident, stack []ast.Node) bool {
	if clientIsName(id, stack) {
		return true
	}
	if len(stack) == 0 {
		return false
	}
	call, ok := stack[len(stack)-1].(*ast.CallExpr)
	if !ok || call.Fun != ast.Expr(id) {
		return false
	}
	return f.helperCalls[call] || f.pkgs.get(f.dir(), f.pkgName()).agentResultFollowed(id, stack) || f.returnedByAgentFunc(call, stack[:len(stack)-1])
}

// returnedByAgentFunc reports whether the call is the result a function of the
// package returns, and the function returns an agent.
func (f *clientFile) returnedByAgentFunc(call *ast.CallExpr, ancestors []ast.Node) bool {
	if len(ancestors) == 0 {
		return false
	}
	ret, ok := ancestors[len(ancestors)-1].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 || ret.Results[0] != ast.Expr(call) {
		return false
	}
	for _, n := range slices.Backward(ancestors) {
		switch fn := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.FuncDecl:
			return fn.Recv == nil && clientReturnsAgent(fn.Type, f.fiber)
		}
	}
	return false
}

// renameToCopy points a member of the fiber package at the copy of the v2 agent,
// which declares it under the same name.
func (f *clientFile) renameToCopy(sel *ast.SelectorExpr) {
	pkg := f.use(f.shim.path, moduleShimPkg)
	f.edits.replace(f.edits.begin(), f.g.offset(sel.X.Pos()), f.g.offset(sel.X.End()), pkg)
	f.handled[sel] = true
	if f.shimNoted {
		return
	}
	f.shimNoted = true
	f.notef(f.line(sel), "`%s.%s` has no counterpart in the v3 client package; it now uses the copy of the Fiber v2 agent in %s",
		f.g.text(sel.X), sel.Sel.Name, clientShimWhere)
}

// copyMissing explains, for a member the copy stands in for, why it was not used.
func (f *clientFile) copyMissing(standsIn bool) string {
	if !standsIn || f.shim.why == "" {
		return ""
	}
	return " (the copy of the Fiber v2 agent could not be added: " + f.shim.why + ")"
}
