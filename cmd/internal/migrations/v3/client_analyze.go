package v3

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// Reasons a request is left alone.
const (
	clientErrUse     clientUnsupported = "it is used in a way the migration does not handle"
	clientErrClosure clientUnsupported = "it is used inside a function literal"
)

// migrate rewrites the request of u, or leaves it on the copy of the v2 agent and
// says why. A request the migration fails on is left alone too: one request must
// not stop the others, nor the migrations that run after this one.
func (f *clientFile) migrate(u *clientUnit) {
	edits := len(f.edits.list)
	defer func() {
		if r := recover(); r != nil {
			f.edits.list = f.edits.list[:edits]
			f.bail(u, clientUnsupported(fmt.Sprintf("the migration failed on this request (%v), please report this", r)))
		}
	}()
	if u.viaClient() && u.root.sel != nil {
		f.viaClient[u.root.sel] = true
	}
	if reason := f.analyze(u); reason != "" {
		f.bail(u, reason)
		return
	}
	if reason := f.sharedWithCopy(u); reason != "" {
		f.bailShared(u, reason)
		return
	}
	if f.holdsBailed(u) && f.shimOK() {
		// The code that is replaced holds a request that stays on the copy: its
		// names cannot be changed inside the new code.
		if f.dry {
			f.bail(u, "it contains a request that stays on the copy")
		} else {
			f.bailShared(u, "it contains a request that stays on the copy of the Fiber v2 agent in "+clientShimFile+", so this request does too")
		}
		return
	}
	f.emit(u)
}

// bail leaves the request as it is. It keeps running on the copy of the v2 agent,
// which the code of the request is renamed to with the other leftovers, or it is
// reported where the package cannot have the copy. A scan only records what the
// request means for the rest of the package.
func (f *clientFile) bail(u *clientUnit, reason clientUnsupported) {
	if f.dry {
		if u.connected() {
			f.taint(clientDomainAgent)
		}
		if u.viaClient() {
			f.taint(clientDomainClient)
		}
		f.markBailed(u)
		return
	}

	line := f.line(u.rootNode())
	if f.shimOK() {
		f.shimNoted = true
		f.markCopied(u)
		f.notef(line, "%s; this request still runs on the copy of the Fiber v2 agent in %s", reason, clientShimFile)
		return
	}
	f.notef(line, "left as Fiber v2 code: %s%s. Convert this request by hand to the client package (client.Get/client.Post with a client.Config, or client.C().R())",
		reason, f.copyMissing(true, nil))
	f.markBailed(u)
}

// bailShared keeps a request that could be migrated on the copy of the v2 agent
// because it shares its agent or its client with requests that cannot.
func (f *clientFile) bailShared(u *clientUnit, note string) {
	f.shimNoted = true
	f.markCopied(u)
	f.notef(f.line(u.rootNode()), "%s", note)
}

// markCopied records the fiber members of a request that stays on the copy.
func (f *clientFile) markCopied(u *clientUnit) {
	for _, n := range u.nodes() {
		ast.Inspect(n, func(c ast.Node) bool {
			if sel, ok := c.(*ast.SelectorExpr); ok && f.isFiberName(sel.X) {
				f.copied[sel] = true
			}
			return true
		})
	}
}

// holdsBailed reports whether the code the migration would write anew for the
// request holds a request that was kept as it is.
func (f *clientFile) holdsBailed(u *clientUnit) bool {
	found := false
	for _, n := range u.nodes() {
		ast.Inspect(n, func(c ast.Node) bool {
			if sel, ok := c.(*ast.SelectorExpr); ok && (f.copied[sel] || f.reported[sel]) {
				found = true
			}
			return !found
		})
	}
	return found
}

// nodes returns the code of the request that the migration writes anew.
func (u *clientUnit) nodes() []ast.Node {
	var nodes []ast.Node
	if u.root.sel != nil {
		nodes = append(nodes, u.root.sel)
	}
	for _, it := range u.items {
		nodes = append(nodes, it.replaced())
	}
	if u.create != nil {
		nodes = append(nodes, u.create)
	}
	if u.retExpr != nil {
		nodes = append(nodes, u.retExpr)
	}
	return nodes
}

// replaced returns the code of the statement that the migration writes anew: the
// init statement of an if that sends the request, and otherwise the statement.
func (it *clientItem) replaced() ast.Node {
	if it.term != nil && it.term.ifStmt != nil && it.term.assign != nil {
		return it.term.assign
	}
	return it.stmt
}

// markBailed records that the fiber members of the request are left to the
// leftover pass.
func (f *clientFile) markBailed(u *clientUnit) {
	f.markReported(u.rootNode())
	for _, it := range u.items {
		f.markReported(it.stmt)
	}
	if u.create != nil {
		f.markReported(u.create)
	}
	if u.retExpr != nil {
		f.markReported(u.retExpr)
	}
}

// markReported records that the fiber members below n were reported.
func (f *clientFile) markReported(n ast.Node) {
	ast.Inspect(n, func(c ast.Node) bool {
		if sel, ok := c.(*ast.SelectorExpr); ok && f.isFiberName(sel.X) {
			f.reported[sel] = true
		}
		return true
	})
}

// rootNode is the node the request is reported at.
func (u *clientUnit) rootNode() ast.Node {
	switch {
	case u.root.sel != nil:
		return u.root.sel
	case u.anchor != nil:
		return u.anchor
	case u.create != nil:
		return u.create
	case len(u.items) > 0:
		return u.items[0].stmt
	}
	return u.list.owner
}

// analyze finds every use of the request and checks that each one is
// understood.
func (f *clientFile) analyze(u *clientUnit) clientUnsupported {
	u.flows = f.agentFlows(u)
	for _, c := range u.creationCalls {
		op, reason := f.agentOp(c.name, c.call, nil)
		if reason != "" {
			return reason
		}
		u.ops = append(u.ops, op)
	}
	if u.varName != "" {
		if reason := f.analyzeUses(u); reason != "" {
			return reason
		}
	}
	slices.SortStableFunc(u.items, func(a, b *clientItem) int { return cmp.Compare(a.stmt.Pos(), b.stmt.Pos()) })
	return clientValidate(u)
}

// analyzeUses classifies the statements that use the agent variable and the
// variables that hold its request.
func (f *clientFile) analyzeUses(u *clientUnit) clientUnsupported {
	refs, _ := clientRefsIn(u.varName, u.list.stmts[u.idx+1:])
	if reason := f.classifyRefs(u, u.list, refs, u.varName, false); reason != "" {
		return reason
	}
	for i := 0; i < len(u.aliases); i++ {
		a := u.aliases[i]
		refs, _ := clientRefsIn(a.name, a.list.stmts[a.idx+1:])
		if reason := f.classifyRefs(u, a.list, refs, a.name, true); reason != "" {
			return reason
		}
	}
	return ""
}

// classifyRefs classifies the statements that hold refs, the references to
// the variable name declared in the list base.
func (f *clientFile) classifyRefs(u *clientUnit, base clientList, refs []clientRef, name string, isAlias bool) clientUnsupported {
	groups, ok := clientGroupRefs(base, refs)
	if !ok {
		return clientErrUse
	}
	for _, g := range groups {
		if reason := f.classify(u, g, name, isAlias); reason != "" {
			return reason
		}
	}
	return ""
}

// classify decides what a statement that uses the request does with it.
func (f *clientFile) classify(u *clientUnit, g clientRefGroup, name string, isAlias bool) clientUnsupported {
	for _, r := range g.refs {
		if slices.ContainsFunc(r.stack, func(n ast.Node) bool { _, ok := n.(*ast.FuncLit); return ok }) {
			return clientErrClosure
		}
	}

	it := &clientItem{stmt: g.stmt, list: g.list, idx: g.idx, nested: g.nested}
	var consumed int
	var reason clientUnsupported
	switch s := g.stmt.(type) {
	case *ast.ExprStmt:
		consumed, reason = f.classifyExprStmt(u, it, s, name, isAlias)
	case *ast.AssignStmt:
		consumed, reason = f.classifyAssign(u, it, s, name, isAlias)
	case *ast.IfStmt:
		consumed, reason = f.classifyIf(it, s, name, isAlias)
	case *ast.DeferStmt:
		consumed, reason = f.classifyDefer(it, s, name)
	case *ast.ReturnStmt:
		consumed, reason = clientClassifyReturn(it, s, name)
	default:
		return f.useReason(g.stmt)
	}
	if reason == "" && consumed != len(g.refs) {
		reason = clientErrUse
	}
	if reason == clientErrUse {
		return f.useReason(g.stmt)
	}
	if reason != "" {
		return reason
	}
	u.items = append(u.items, it)
	return ""
}

// useReason explains that a statement could not be understood.
func (f *clientFile) useReason(s ast.Stmt) clientUnsupported {
	snippet, _, _ := strings.Cut(f.g.text(s), "\n")
	snippet = strings.TrimSpace(snippet)
	if len(snippet) > 60 {
		snippet = snippet[:57] + "..."
	}
	return clientUnsupported("`" + snippet + "` is not understood by the migration")
}

// isRelease recognizes fiber.ReleaseAgent(name).
func (f *clientFile) isRelease(call *ast.CallExpr, name string) bool {
	sel, ok := f.fiberSel(call.Fun)
	return ok && sel.Sel.Name == nameReleaseAgent && len(call.Args) == 1 && isIdentNamed(call.Args[0], name)
}

// clientReqPath is a call on the fasthttp request of an agent, such as
// req.Header.Set(k, v) or a.Request().URI().QueryArgs().Add(k, v).
type clientReqPath struct {
	// root is the variable the chain starts at.
	root *ast.Ident
	// path is the chain of selectors below the request: "Header.Set".
	path string
	// viaRequest says the request was fetched with Request() in the chain.
	viaRequest bool
}

// clientPeelReq takes apart a call on the fasthttp request of an agent.
func clientPeelReq(call *ast.CallExpr) (clientReqPath, bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return clientReqPath{}, false
	}
	path := sel.Sel.Name
	cur := sel.X
	for {
		switch x := cur.(type) {
		case *ast.SelectorExpr:
			path = x.Sel.Name + "." + path
			cur = x.X
		case *ast.CallExpr:
			s, isSel := x.Fun.(*ast.SelectorExpr)
			if !isSel || len(x.Args) != 0 {
				return clientReqPath{}, false
			}
			if id, isIdent := s.X.(*ast.Ident); isIdent && s.Sel.Name == clientMethodRequest {
				return clientReqPath{root: id, path: path, viaRequest: true}, true
			}
			path = s.Sel.Name + "()." + path
			cur = s.X
		case *ast.Ident:
			return clientReqPath{root: x, path: path}, true
		default:
			return clientReqPath{}, false
		}
	}
}

// classifyExprStmt handles a call used as a statement.
func (f *clientFile) classifyExprStmt(u *clientUnit, it *clientItem, s *ast.ExprStmt, name string, isAlias bool) (int, clientUnsupported) {
	call, ok := s.X.(*ast.CallExpr)
	if !ok {
		return 0, clientErrUse
	}
	if f.isRelease(call, name) {
		it.kind = clientItemDrop
		return 1, ""
	}
	if rp, ok := clientPeelReq(call); ok && rp.root.Name == name && rp.viaRequest == !isAlias {
		op, reason := f.reqOp(rp.path, call, u)
		if reason != "" {
			return 0, reason
		}
		it.kind = clientItemSetter
		it.ops = []clientOp{op}
		return 1, ""
	}
	if isAlias {
		return 0, clientErrUse
	}
	ch, ok := f.decompose(call)
	if !ok || ch.root.kind != clientVar || ch.root.ident.Name != name || len(ch.calls) == 0 {
		return 0, clientErrUse
	}
	if len(ch.calls) == 1 && ch.calls[0].name == clientMethodParse {
		it.kind = clientItemParse
		it.parse = &clientParseCheck{}
		return 1, ""
	}
	return f.chainItem(it, ch, &clientTerm{})
}

// chainItem fills the item for a chain of agent calls that starts at a
// variable and may end in a terminal.
func (f *clientFile) chainItem(it *clientItem, ch clientChain, term *clientTerm) (int, clientUnsupported) {
	calls := ch.calls
	last := ch.terminal()
	if last != nil {
		calls = calls[:len(calls)-1]
		term.kind = clientTerminalKind(last.name)
		if term.kind == clientTermStruct {
			if len(last.call.Args) != 1 || last.call.Ellipsis.IsValid() {
				return 0, clientErrUse
			}
			term.structArg = f.text(last.call.Args[0])
		}
	}
	for _, c := range calls {
		op, reason := f.agentOp(c.name, c.call, it)
		if reason != "" {
			return 0, reason
		}
		it.ops = append(it.ops, op)
	}
	if last == nil {
		it.kind = clientItemSetter
		return 1, ""
	}
	it.kind = clientItemTerm
	it.term = term
	return 1, ""
}

// parsedBefore reports whether the statements classified so far call Parse before s.
func (u *clientUnit) parsedBefore(s ast.Stmt) bool {
	return slices.ContainsFunc(u.items, func(it *clientItem) bool {
		return it.kind == clientItemParse && it.stmt.Pos() < s.Pos()
	})
}

// classifyAssign handles an assignment that uses the request.
func (f *clientFile) classifyAssign(u *clientUnit, it *clientItem, s *ast.AssignStmt, name string, isAlias bool) (int, clientUnsupported) {
	if len(s.Rhs) != 1 || isAlias {
		return 0, clientErrUse
	}

	// The Name field of an agent that was just acquired is its User-Agent, as long
	// as Parse has not read it yet.
	if sel, ok := s.Lhs[0].(*ast.SelectorExpr); ok && len(s.Lhs) == 1 && s.Tok == token.ASSIGN && isIdentNamed(sel.X, name) && sel.Sel.Name == clientMemberName {
		if !u.allowsURI() {
			return 0, "setting Name of an agent that fiber.Get made, which has no effect in v2"
		}
		if u.parsedBefore(s) {
			return 0, "setting Name after Parse(), which has no effect in v2"
		}
		it.kind = clientItemSetter
		it.ops = []clientOp{{method: clientMemberName, req: "SetUserAgent(" + f.text(s.Rhs[0]) + ")", line: f.line(s)}}
		return 1, ""
	}

	call, isCall := s.Rhs[0].(*ast.CallExpr)
	if !isCall {
		return 0, clientErrUse
	}

	// The variable that holds the fasthttp request of the agent.
	if s.Tok == token.DEFINE && len(s.Lhs) == 1 && len(call.Args) == 0 {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == clientMethodRequest && isIdentNamed(sel.X, name) {
			id, ok := s.Lhs[0].(*ast.Ident)
			if !ok || id.Name == "_" {
				return 0, clientErrUse
			}
			u.aliases = append(u.aliases, clientAlias{name: id.Name, list: it.list, idx: it.idx})
			it.kind = clientItemDrop
			return 1, ""
		}
	}

	ch, ok := f.decompose(call)
	if !ok || ch.root.kind != clientVar || ch.root.ident.Name != name || len(ch.calls) == 0 {
		return 0, clientErrUse
	}
	isParse := len(ch.calls) == 1 && ch.calls[0].name == clientMethodParse

	// The error of Parse is ignored.
	if isParse && len(s.Lhs) == 1 && s.Tok == token.ASSIGN && isIdentNamed(s.Lhs[0], "_") {
		it.kind = clientItemParse
		it.parse = &clientParseCheck{}
		return 1, ""
	}

	// The error of Parse is declared, then checked by the next statement.
	if isParse && len(s.Lhs) == 1 && s.Tok == token.DEFINE {
		id, isIdent := s.Lhs[0].(*ast.Ident)
		if !isIdent || id.Name == "_" || it.idx+1 >= len(it.list.stmts) {
			return 0, clientErrUse
		}
		check, isIf := it.list.stmts[it.idx+1].(*ast.IfStmt)
		if !isIf || check.Init != nil || check.Else != nil || !clientIsNilCheck(check.Cond, id.Name) {
			return 0, clientErrUse
		}
		it.kind = clientItemParse
		it.parse = &clientParseCheck{handler: check.Body, check: check, errVar: id.Name, declared: true}
		return 1, ""
	}

	if len(s.Lhs) == 3 && ch.terminal() != nil {
		return f.chainItem(it, ch, &clientTerm{assign: s})
	}
	return 0, clientErrUse
}

// classifyIf handles an if statement whose init statement uses the request.
func (f *clientFile) classifyIf(it *clientItem, s *ast.IfStmt, name string, isAlias bool) (int, clientUnsupported) {
	init, ok := s.Init.(*ast.AssignStmt)
	if !ok || len(init.Rhs) != 1 || isAlias {
		return 0, clientErrUse
	}
	call, isCall := init.Rhs[0].(*ast.CallExpr)
	if !isCall {
		return 0, clientErrUse
	}
	ch, ok := f.decompose(call)
	if !ok || ch.root.kind != clientVar || ch.root.ident.Name != name || len(ch.calls) == 0 {
		return 0, clientErrUse
	}

	if len(init.Lhs) == 3 && ch.terminal() != nil {
		return f.chainItem(it, ch, &clientTerm{assign: init, ifStmt: s})
	}

	// The error of Parse is declared and checked by the if statement.
	if len(init.Lhs) == 1 && init.Tok == token.DEFINE && len(ch.calls) == 1 && ch.calls[0].name == clientMethodParse && s.Else == nil {
		id, isIdent := init.Lhs[0].(*ast.Ident)
		if isIdent && id.Name != "_" && clientIsNilCheck(s.Cond, id.Name) {
			it.kind = clientItemParse
			it.parse = &clientParseCheck{handler: s.Body, errVar: id.Name}
			return 1, ""
		}
	}
	return 0, clientErrUse
}

// clientOverwritten reports whether the variable name is not read before the
// statements stmts assign to it, or not used at all: its value at their start
// does not matter.
func clientOverwritten(name string, stmts []ast.Stmt) bool {
	refs, end := clientRefsIn(name, stmts)
	if len(refs) == 0 {
		return true
	}
	// The first statement that mentions it has to assign it before anything
	// else happens, without reading it.
	for _, s := range stmts[:min(end+1, len(stmts))] {
		found, _ := clientRefsIn(name, []ast.Stmt{s})
		if len(found) == 0 {
			continue
		}
		as, ok := s.(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN || len(found) != 1 {
			return false
		}
		return slices.ContainsFunc(as.Lhs, func(e ast.Expr) bool { return e == ast.Expr(found[0].id) })
	}
	return true
}

// clientIsNilCheck reports whether cond is name != nil.
func clientIsNilCheck(cond ast.Expr, name string) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return false
	}
	return (isIdentNamed(bin.X, name) && isIdentNamed(bin.Y, "nil")) || (isIdentNamed(bin.Y, name) && isIdentNamed(bin.X, "nil"))
}

// classifyDefer handles defer fiber.ReleaseAgent(a).
func (f *clientFile) classifyDefer(it *clientItem, s *ast.DeferStmt, name string) (int, clientUnsupported) {
	if !f.isRelease(s.Call, name) {
		return 0, clientErrUse
	}
	it.kind = clientItemDrop
	return 1, ""
}

// clientClassifyReturn handles a return statement that hands the request on.
func clientClassifyReturn(it *clientItem, s *ast.ReturnStmt, name string) (int, clientUnsupported) {
	n := 0
	for _, r := range s.Results {
		if isIdentNamed(r, name) {
			n++
		}
	}
	if n == 0 {
		return 0, clientErrUse
	}
	it.kind = clientItemEscape
	return n, ""
}

// clientValidate checks the request as a whole.
func clientValidate(u *clientUnit) clientUnsupported {
	all := slices.Clone(u.ops)
	for _, it := range u.items {
		all = append(all, it.ops...)
	}
	for _, op := range all {
		if op.cli != "" {
			u.dedicated = true
		}
	}
	if reason := clientValidateForms(u); reason != "" {
		return reason
	}

	if u.retExpr != nil && slices.ContainsFunc(u.ops, func(op clientOp) bool { return op.stmt != nil }) {
		return clientUnsupported(u.ops[slices.IndexFunc(u.ops, func(op clientOp) bool { return op.stmt != nil })].method +
			"() needs a statement of its own, which cannot be part of a returned expression")
	}
	if u.dedicated && (u.kind == clientViaClient || u.kind == clientExternal) {
		return "it changes the TLS or JSON settings of a client that other requests share"
	}

	switch u.kind {
	case clientFiberFunc, clientViaClient, clientFluent:
		// The request is parsed when it is created, a later Parse never fails.
		for _, it := range u.items {
			if it.kind == clientItemParse {
				it.kind = clientItemDrop
			}
		}
	default:
	}

	var firstTerm *clientItem
	for _, it := range u.items {
		if it.kind == clientItemTerm {
			firstTerm = it
			break
		}
	}
	for _, it := range u.items {
		if it.kind != clientItemParse || it.parse.handler == nil {
			continue
		}
		if firstTerm == nil || firstTerm.list.owner != it.list.owner || firstTerm.idx < it.idx {
			return "Parse() is checked, but no request is sent after it in the same block"
		}
		if it.parse.declared {
			between := it.list.stmts[min(it.idx+2, firstTerm.idx):firstTerm.idx]
			if refs, _ := clientRefsIn(it.parse.errVar, between); len(refs) > 0 {
				return "the error of Parse() is used before the request is sent"
			}
			if !clientOverwritten(it.parse.errVar, firstTerm.list.stmts[firstTerm.idx+1:]) {
				return "the error of Parse() is read after the request is sent, where it was nil in v2"
			}
		}
	}
	return ""
}
