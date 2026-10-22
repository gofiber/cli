package v3

import (
	"go/ast"
	"go/token"
	"slices"
)

// clientKind says how a v2 request comes into existence.
type clientKind int

const (
	// clientFiberFunc is fiber.Get(url), fiber.Post(url), ...
	clientFiberFunc clientKind = iota
	// clientAcquire is fiber.AcquireAgent(): a request without URL or method.
	clientAcquire
	// clientViaClient is c.Get(url) on a fiber.Client.
	clientViaClient
	// clientFluent is a chain that is created and sent in one statement.
	clientFluent
	// clientExternal is an agent received from somewhere else.
	clientExternal
	// clientVar is not a creation: the chain starts at a variable.
	clientVar
)

// clientItemKind classifies a statement that uses the request.
type clientItemKind int

const (
	// clientItemSetter configures the request.
	clientItemSetter clientItemKind = iota
	// clientItemTerm sends the request and reads the response.
	clientItemTerm
	// clientItemDrop is a statement the v3 client does not need.
	clientItemDrop
	// clientItemParse is a call of Parse, which v3 has no counterpart for.
	clientItemParse
	// clientItemEscape hands the request to other code, unchanged.
	clientItemEscape
)

// clientTermKind says what a terminal call returns as the body.
type clientTermKind int

const (
	clientTermNone clientTermKind = iota
	clientTermBytes
	clientTermString
	clientTermStruct
)

// Names of the Agent methods the migration treats specially.
const (
	clientMethodRequest = "Request"
	clientMethodParse   = "Parse"
	clientMethodGet     = "Get"
)

// clientTerminals maps the Agent methods that send the request to the kind of
// body they return.
var clientTerminals = map[string]clientTermKind{
	"Bytes":  clientTermBytes,
	"String": clientTermString, //nolint:usestdlibvars // the String method of an Agent, not a go/constant kind
	"Struct": clientTermStruct,
}

// clientTerminalKind returns the kind of the terminal call called name.
func clientTerminalKind(name string) clientTermKind {
	return clientTerminals[name]
}

// clientRoot describes where a chain of agent calls starts.
type clientRoot struct {
	sel    *ast.SelectorExpr
	call   *ast.CallExpr
	url    ast.Expr
	client ast.Expr
	ident  *ast.Ident
	method string
	kind   clientKind
}

// clientCall is one method call of a chain.
type clientCall struct {
	call *ast.CallExpr
	name string
}

// clientChain is an expression like fiber.Get(url).Set(k, v).Bytes(), taken
// apart.
type clientChain struct {
	root  clientRoot
	calls []clientCall
}

// terminal returns the call that sends the request, nil when the chain does
// not end in one.
func (c clientChain) terminal() *clientCall {
	if len(c.calls) == 0 {
		return nil
	}
	last := &c.calls[len(c.calls)-1]
	if clientTerminalKind(last.name) == clientTermNone {
		return nil
	}
	return last
}

// clientTerm is a statement that sends the request and reads its response.
type clientTerm struct {
	assign    *ast.AssignStmt
	ifStmt    *ast.IfStmt
	structArg string
	kind      clientTermKind
}

// clientParseCheck is a call of Parse together with the code that handles its
// error.
type clientParseCheck struct {
	handler *ast.BlockStmt
	// check is the if statement that follows a Parse whose error was declared
	// before the check.
	check  *ast.IfStmt
	errVar string
	// declared says the error variable lives on after the check.
	declared bool
}

// clientItem is a statement that uses the request.
type clientItem struct {
	stmt  ast.Stmt
	list  clientList
	term  *clientTerm
	parse *clientParseCheck
	// calls are the calls that precede the terminal in the same chain.
	ops    []clientOp
	idx    int
	kind   clientItemKind
	nested bool
}

// clientAlias is a variable that holds the fasthttp request of an agent.
type clientAlias struct {
	name string
	list clientList
	idx  int
}

// clientUnit is a request the migration rewrites as a whole: its creation, the
// statements that configure it, and the statements that send it.
type clientUnit struct {
	f      *clientFile
	create ast.Stmt
	// anchor is where the request is reported when it has no root selector.
	anchor ast.Node
	// retExpr is the expression of a return statement that creates the request.
	retExpr ast.Expr
	// creationCalls are the calls chained to the creation expression.
	creationCalls []clientCall
	url           ast.Expr
	client        ast.Expr
	varName       string
	method        string
	root          clientRoot
	list          clientList
	items         []*clientItem
	ops           []clientOp
	aliases       []clientAlias
	idx           int
	kind          clientKind
	// dedicated says the request needs a client of its own, because it changes
	// the settings of the client.
	dedicated bool
}

// decompose takes a chain of agent calls apart.
func (f *clientFile) decompose(e ast.Expr) (clientChain, bool) {
	var ch clientChain
	var calls []clientCall
	for {
		call, isCall := e.(*ast.CallExpr)
		if !isCall {
			break
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return ch, false
		}
		if f.isFiberName(sel.X) {
			root, ok := clientFiberRoot(sel, call)
			if !ok {
				return ch, false
			}
			slices.Reverse(calls)
			return clientChain{root: root, calls: calls}, true
		}
		if clientIsMethod(sel.Sel.Name) && len(call.Args) == 1 && f.isFiberClientExpr(sel.X) {
			root := clientRoot{kind: clientViaClient, sel: sel, call: call, url: call.Args[0], client: sel.X, method: sel.Sel.Name}
			slices.Reverse(calls)
			return clientChain{root: root, calls: calls}, true
		}
		calls = append(calls, clientCall{call: call, name: sel.Sel.Name})
		e = sel.X
	}
	id, isIdent := e.(*ast.Ident)
	if !isIdent {
		return ch, false
	}
	slices.Reverse(calls)
	return clientChain{root: clientRoot{kind: clientVar, ident: id}, calls: calls}, true
}

// clientFiberRoot recognizes fiber.Get(url) and fiber.AcquireAgent().
func clientFiberRoot(sel *ast.SelectorExpr, call *ast.CallExpr) (clientRoot, bool) {
	switch {
	case clientIsMethod(sel.Sel.Name) && len(call.Args) == 1 && !call.Ellipsis.IsValid():
		return clientRoot{kind: clientFiberFunc, sel: sel, call: call, url: call.Args[0], method: sel.Sel.Name}, true
	case sel.Sel.Name == nameAcquireAgent && len(call.Args) == 0:
		return clientRoot{kind: clientAcquire, sel: sel, call: call}, true
	}
	return clientRoot{}, false
}

// clientIsMethod reports whether name is one of the verbs of the v2 client.
func clientIsMethod(name string) bool {
	switch name {
	case clientMethodGet, nameHead, namePost, namePut, namePatch, nameDelete:
		return true
	}
	return false
}

// creationAt recognizes a statement that creates a request, or that creates
// and sends one at once.
func (f *clientFile) creationAt(l clientList, idx int) *clientUnit {
	stmt := l.stmts[idx]
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Rhs) != 1 {
			return nil
		}
		if s.Tok == token.DEFINE && len(s.Lhs) == 1 {
			id, ok := s.Lhs[0].(*ast.Ident)
			if !ok || id.Name == "_" {
				return nil
			}
			return f.variableUnit(l, idx, stmt, id.Name, s.Rhs[0])
		}
		if len(s.Lhs) == 3 {
			return f.fluentUnit(l, idx, stmt, s.Rhs[0], &clientTerm{assign: s})
		}
	case *ast.ExprStmt:
		return f.fluentUnit(l, idx, stmt, s.X, &clientTerm{})
	case *ast.IfStmt:
		init, ok := s.Init.(*ast.AssignStmt)
		if ok && len(init.Lhs) == 3 && len(init.Rhs) == 1 {
			return f.fluentUnit(l, idx, stmt, init.Rhs[0], &clientTerm{assign: init, ifStmt: s})
		}
	}
	return nil
}

// variableUnit recognizes v := fiber.Get(url)... and v := fiber.AcquireAgent().
func (f *clientFile) variableUnit(l clientList, idx int, stmt ast.Stmt, name string, rhs ast.Expr) *clientUnit {
	ch, ok := f.decompose(rhs)
	if !ok || ch.root.kind == clientVar || ch.terminal() != nil {
		return nil
	}
	return &clientUnit{
		f: f, list: l, idx: idx, create: stmt, varName: name,
		kind: ch.root.kind, root: ch.root, url: ch.root.url, client: ch.root.client,
		method: ch.root.method, creationCalls: ch.calls,
	}
}

// fluentUnit recognizes a chain that is sent in the statement that creates it.
func (f *clientFile) fluentUnit(l clientList, idx int, stmt ast.Stmt, rhs ast.Expr, term *clientTerm) *clientUnit {
	ch, ok := f.decompose(rhs)
	if !ok || ch.root.kind == clientVar || ch.root.kind == clientAcquire {
		return nil
	}
	last := ch.terminal()
	if last == nil {
		return nil
	}
	term.kind = clientTerminalKind(last.name)
	if term.kind == clientTermStruct {
		if len(last.call.Args) != 1 || last.call.Ellipsis.IsValid() {
			return nil
		}
		term.structArg = f.text(last.call.Args[0])
	}
	it := &clientItem{stmt: stmt, list: l, idx: idx, kind: clientItemTerm, term: term}
	return &clientUnit{
		f: f, list: l, idx: idx, kind: clientFluent, root: ch.root, url: ch.root.url,
		client: ch.root.client, method: ch.root.method,
		creationCalls: ch.calls[:len(ch.calls)-1], items: []*clientItem{it},
	}
}

// clientRefGroup is the references to a variable made by one statement.
type clientRefGroup struct {
	stmt   ast.Stmt
	list   clientList
	refs   []clientRef
	idx    int
	nested bool
}

// clientGroupRefs sorts the references by the statement that holds them. base is
// the list the variable was declared in.
func clientGroupRefs(base clientList, refs []clientRef) ([]clientRefGroup, bool) {
	var groups []clientRefGroup
	byStmt := map[ast.Stmt]int{}
	for _, r := range refs {
		m, ok := clientMemberOf(base, r)
		if !ok {
			return nil, false
		}
		if i, seen := byStmt[m.stmt]; seen {
			groups[i].refs = append(groups[i].refs, r)
			continue
		}
		byStmt[m.stmt] = len(groups)
		groups = append(groups, clientRefGroup{stmt: m.stmt, list: m.list, idx: m.idx, refs: []clientRef{r}, nested: m.list.owner != base.owner})
	}
	return groups, true
}

// clientMember is a statement of a statement list.
type clientMember struct {
	stmt ast.Stmt
	list clientList
	idx  int
}

// clientMemberOf finds the statement of a statement list that holds the reference.
func clientMemberOf(base clientList, r clientRef) (clientMember, bool) {
	st := r.stack
	for k, n := range slices.Backward(st) {
		s, isStmt := n.(ast.Stmt)
		if !isStmt {
			continue
		}
		if k == 0 {
			idx := slices.Index(base.stmts, s)
			return clientMember{stmt: s, list: base, idx: idx}, idx >= 0
		}
		var stmts []ast.Stmt
		switch owner := st[k-1].(type) {
		case *ast.BlockStmt:
			stmts = owner.List
		case *ast.CaseClause:
			stmts = owner.Body
		case *ast.CommClause:
			stmts = owner.Body
		default:
			continue
		}
		idx := slices.Index(stmts, s)
		return clientMember{stmt: s, list: clientList{owner: st[k-1], fn: base.fn, stmts: stmts}, idx: idx}, idx >= 0
	}
	return clientMember{}, false
}

// allowsURI reports whether the URL of the request may be set after the unit
// was created: agents made by fiber.Get already carry their URL.
func (u *clientUnit) allowsURI() bool {
	return u.kind == clientAcquire || u.kind == clientExternal
}
