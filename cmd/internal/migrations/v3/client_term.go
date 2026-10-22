package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// clientErrsMode says what becomes of the []error a v2 request returned.
type clientErrsMode int

const (
	// clientErrsNone: the errors are not used.
	clientErrsNone clientErrsMode = iota
	// clientErrsRewrite: every use was rewritten to the error of the v3 call.
	clientErrsRewrite
	// clientErrsOwn: the slice is kept, declared by the migrated code.
	clientErrsOwn
	// clientErrsExisting: the slice is kept, it was declared before.
	clientErrsExisting
)

// clientTermSide is one of the three values a v2 request returns.
type clientTermSide struct {
	text  string
	blank bool
	isNew bool
}

// term writes the statement that sends the request and reads its response.
// call is the v3 call that sends it, pre the statements that come first.
func (e *clientEmit) term(it *clientItem, call, pre string) {
	f, u := e.f, e.u
	t := it.term
	l := it.list
	define := t.assign != nil && t.assign.Tok == token.DEFINE

	var sides [3]clientTermSide
	if t.assign != nil {
		for i, lhs := range t.assign.Lhs {
			id, isIdent := lhs.(*ast.Ident)
			sides[i] = clientTermSide{text: f.text(lhs), blank: isIdent && id.Name == "_"}
			sides[i].isNew = define && isIdent && !sides[i].blank &&
				(t.ifStmt != nil || !clientDeclaredBefore(l, it.idx, id.Name))
		}
	} else {
		for i := range sides {
			sides[i].blank = true
		}
	}
	status, body, errs := sides[0], sides[1], sides[2]
	respUsed := !status.blank || !body.blank || t.kind == clientTermStruct

	// The code that follows the statement sees the error through the name that
	// is picked here.
	var region []ast.Node
	if t.ifStmt != nil {
		region = append(region, t.ifStmt.Body)
		if t.ifStmt.Cond != nil {
			region = append(region, t.ifStmt.Cond)
		}
		if t.ifStmt.Else != nil {
			region = append(region, t.ifStmt.Else)
		}
	}
	avoid := map[string]bool{}
	for _, s := range sides {
		avoid[s.text] = true
	}
	errName, errExists := f.pickErr(l, it.idx, region, avoid)
	if h := u.parseHandler(it); h != nil && h.declared {
		// The variable that held the error of Parse lives on under its name.
		errName, errExists = h.errVar, false
	}

	respName := "_"
	if respUsed {
		respName = l.fn.fresh("resp")
	}

	mode := clientErrsNone
	var rewrites []clientEdit
	if !errs.blank {
		mode, rewrites = e.errsPlan(it, errs, define, errName)
	}
	for _, r := range rewrites {
		f.edits.replace(e.id, r.start, r.end, r.text)
	}

	var lines []string
	if pre != "" {
		lines = append(lines, pre)
	}
	switch {
	case !respUsed && mode == clientErrsNone:
		lines = append(lines, "_, _ = "+call)
	default:
		op := ":="
		if respName == "_" && errExists {
			op = "="
		}
		lines = append(lines, respName+", "+errName+" "+op+" "+call)
	}

	// The error of Parse and the errors of the terminal call are one error of
	// Send. When the code that handled the first leaves the block and the code
	// that handles the second is not the same code, Send's error only goes to
	// the first when it is what Parse reported: a URL that is not valid.
	var repeated *ast.IfStmt
	if h := u.parseHandler(it); h != nil {
		handler := e.handlerText(h, errName)
		cond := errName + " != nil"
		if f.blockEnds(h.handler) {
			repeated = e.repeatedCheck(it, handler, mode, rewrites, errName)
			if repeated == nil {
				cond = f.use("errors", "errors") + ".Is(" + errName + ", " + f.use(clientPkgPath, "client") + ".ErrURLFormat)"
			}
		}
		lines = append(lines, "if "+cond+" "+handler)
	}

	bodyType, bodyZero, bodyExpr := "[]byte", "nil", respName+".Body()"
	if t.kind == clientTermString {
		bodyType, bodyZero, bodyExpr = nameString, `""`, "string("+respName+".Body())"
	}
	var assigns []clientTermAssign
	if !status.blank {
		assigns = append(assigns, clientTermAssign{side: status, typ: "int", zero: "0", expr: respName + ".StatusCode()"})
	}
	if !body.blank {
		assigns = append(assigns, clientTermAssign{side: body, typ: bodyType, zero: bodyZero, expr: bodyExpr})
	}
	isStruct := t.kind == clientTermStruct

	switch check := e.followingCheck(it, assigns, mode, rewrites, errName); {
	case repeated != nil:
		// The handler of Parse leaves the block and does what the check behind
		// it does: the error is nil from here on.
		lines = append(lines, clientDirect(assigns)...)
		if isStruct {
			lines = append(lines, errName+" = "+respName+".JSON("+t.structArg+")")
		}
		lines = clientErrsSlice(lines, mode, errs.text, errName, isStruct)
		f.deleteStmt(e.id, repeated)
	case check != nil:
		// The check that follows leaves the block: read the response behind it.
		at := f.g.offset(check.End())
		if _, end := f.trailingComment(at); end > at {
			at = end
		}
		f.edits.replace(e.id, at, at, "\n"+strings.Join(clientDirect(assigns), "\n"))
	default:
		for _, a := range assigns {
			lines = append(lines, clientDeclareOrReset(a.side, a.typ, a.zero))
		}
		switch mode {
		case clientErrsOwn:
			lines = append(lines, "var "+errs.text+" []error")
		case clientErrsExisting:
			lines = append(lines, errs.text+" = nil")
		case clientErrsNone, clientErrsRewrite:
		}
		var guard []string
		for _, a := range assigns {
			guard = append(guard, a.side.text+" = "+a.expr)
		}
		if isStruct {
			guard = append(guard, errName+" = "+respName+".JSON("+t.structArg+")")
		}
		if len(guard) > 0 {
			lines = append(lines, "if "+errName+" == nil {")
			lines = append(lines, guard...)
			lines = append(lines, "}")
		}
		if mode == clientErrsOwn || mode == clientErrsExisting {
			lines = append(lines, "if "+errName+" != nil {", errs.text+" = []error{"+errName+"}", "}")
		}
	}

	block := strings.Join(lines, "\n")
	wrap := false
	if t.ifStmt != nil {
		names := []string{status.text, body.text, errs.text, respName}
		if !errExists {
			names = append(names, errName)
		}
		wrap = define && clientMentioned(l, t.ifStmt, names)
		e.hoist(t, block, wrap)
	} else {
		f.replaceStmt(e.id, it.stmt, block)
	}
	if !wrap {
		if f.intro[l.owner] == nil {
			f.intro[l.owner] = map[string]bool{}
		}
		f.intro[l.owner][errName] = true
	}
}

// clientTermAssign is one value of the response that the statement which sends
// the request stored in a variable.
type clientTermAssign struct {
	typ  string
	zero string
	expr string
	side clientTermSide
}

// clientDirect renders the assignments for a response that is known to be
// there: the new variables are declared with their value.
func clientDirect(assigns []clientTermAssign) []string {
	var lines, names, exprs []string
	for _, a := range assigns {
		if a.side.isNew {
			names = append(names, a.side.text)
			exprs = append(exprs, a.expr)
			continue
		}
		lines = append(lines, a.side.text+" = "+a.expr)
	}
	if len(names) > 0 {
		lines = append(lines, strings.Join(names, ", ")+" := "+strings.Join(exprs, ", "))
	}
	return lines
}

// clientErrsSlice keeps the []error variable of a request whose error is nil
// in the lines that follow. set says the error may still be set later.
func clientErrsSlice(lines []string, mode clientErrsMode, errs, errName string, set bool) []string {
	switch mode {
	case clientErrsOwn:
		lines = append(lines, "var "+errs+" []error")
	case clientErrsExisting:
		lines = append(lines, errs+" = nil")
	case clientErrsNone, clientErrsRewrite:
	}
	if set && (mode == clientErrsOwn || mode == clientErrsExisting) {
		lines = append(lines, "if "+errName+" != nil {", errs+" = []error{"+errName+"}", "}")
	}
	return lines
}

// followingCheck finds the if statement right behind the statement that sends
// the request when it only handles the error and leaves the block: the values
// of the response can then be read behind it, without a guard. It returns nil
// when the statement is not that simple.
func (e *clientEmit) followingCheck(it *clientItem, assigns []clientTermAssign, mode clientErrsMode, rewrites []clientEdit, errName string) *ast.IfStmt {
	f := e.f
	t := it.term
	l := it.list
	if t.ifStmt != nil || t.assign == nil || t.kind == clientTermStruct || mode != clientErrsRewrite ||
		len(assigns) == 0 || it.idx+1 >= len(l.stmts) {
		return nil
	}
	for _, a := range assigns {
		if !a.side.isNew {
			return nil
		}
	}
	next, ok := l.stmts[it.idx+1].(*ast.IfStmt)
	if !ok || next.Init != nil || next.Else != nil || next.Cond == nil || !f.blockEnds(next.Body) {
		return nil
	}
	start, end := f.g.offset(next.Cond.Pos()), f.g.offset(next.Cond.End())
	isCheck := slices.ContainsFunc(rewrites, func(r clientEdit) bool {
		return r.start == start && r.end == end && r.text == errName+" != nil"
	})
	if !isCheck {
		return nil
	}
	for _, a := range assigns {
		if refs, _ := clientRefsIn(a.side.text, []ast.Stmt{next}); len(refs) > 0 {
			return nil
		}
	}
	return next
}

// repeatedCheck finds the if statement behind the statement that sends the
// request that does what the handler of Parse, which runs right behind it, does
// already: the same code under the same condition on the same error.
func (e *clientEmit) repeatedCheck(it *clientItem, handler string, mode clientErrsMode, rewrites []clientEdit, errName string) *ast.IfStmt {
	f := e.f
	t := it.term
	l := it.list
	// The check of a request that decodes the body is also the check of the
	// decoding, which the handler of Parse knows nothing about.
	if t.ifStmt != nil || t.kind == clientTermStruct || mode != clientErrsRewrite || it.idx+1 >= len(l.stmts) {
		return nil
	}
	next, ok := l.stmts[it.idx+1].(*ast.IfStmt)
	if !ok || next.Init != nil || next.Else != nil || next.Cond == nil {
		return nil
	}
	start, end := f.g.offset(next.Cond.Pos()), f.g.offset(next.Cond.End())
	isCheck := slices.ContainsFunc(rewrites, func(r clientEdit) bool {
		return r.start == start && r.end == end && r.text == errName+" != nil"
	})
	if !isCheck || strings.Join(strings.Fields(f.text(next.Body)), " ") != strings.Join(strings.Fields(handler), " ") {
		return nil
	}
	return next
}

// blockEnds reports whether the code after the block can only run when the
// block was not entered: it ends with a return, a branch, a panic or a call
// that stops the program.
func (f *clientFile) blockEnds(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	switch s := b.List[len(b.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok != token.FALLTHROUGH
	case *ast.BlockStmt:
		return f.blockEnds(s)
	case *ast.IfStmt:
		if s.Else == nil || !f.blockEnds(s.Body) {
			return false
		}
		switch els := s.Else.(type) {
		case *ast.BlockStmt:
			return f.blockEnds(els)
		case *ast.IfStmt:
			return f.blockEnds(&ast.BlockStmt{List: []ast.Stmt{els}})
		}
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		if isIdentNamed(call.Fun, "panic") {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		switch {
		case !ok:
		case slices.Contains(f.log, pkg.Name):
			return strings.HasPrefix(sel.Sel.Name, "Fatal") || strings.HasPrefix(sel.Sel.Name, "Panic")
		case pkg.Name == "os":
			return sel.Sel.Name == "Exit"
		}
	}
	return false
}

// hoist writes the statements that replace the init statement of an if before
// the if statement, which loses its init statement.
func (e *clientEmit) hoist(t *clientTerm, block string, wrap bool) {
	f := e.f
	ifPos := f.g.offset(t.ifStmt.Pos())
	ifEnd := f.g.offset(t.ifStmt.End())
	if wrap {
		block = "{\n" + block
		f.edits.replace(e.id, ifEnd, ifEnd, "\n}")
	}
	f.edits.replace(e.id, ifPos, ifPos, block+"\n")
	f.edits.replace(e.id, f.g.offset(t.assign.Pos()), f.g.offset(t.ifStmt.Cond.Pos()), "")
}

// clientMentioned reports whether one of names appears in the list outside of the
// statement skip.
func clientMentioned(l clientList, skip ast.Stmt, names []string) bool {
	for _, n := range names {
		if slices.Contains(l.bound, n) {
			return true
		}
	}
	for _, s := range l.stmts {
		if s == skip {
			continue
		}
		found := false
		ast.Inspect(s, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && slices.Contains(names, id.Name) {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// clientDeclareOrReset declares a variable the statement introduced, or resets one
// that already existed to the value a failed v2 request returned.
func clientDeclareOrReset(s clientTermSide, typ, zero string) string {
	if s.isNew {
		return "var " + s.text + " " + typ
	}
	return s.text + " = " + zero
}

// parseHandler returns the code that handled the error of Parse and that has
// to move to the statement that sends the request.
func (u *clientUnit) parseHandler(it *clientItem) *clientParseCheck {
	var first *clientItem
	for _, other := range u.items {
		if other.kind == clientItemTerm {
			first = other
			break
		}
	}
	if first != it {
		return nil
	}
	for _, p := range u.items {
		if p.kind == clientItemParse && p.parse.handler != nil {
			return p.parse
		}
	}
	return nil
}

// handlerText returns the block that handled the error of Parse, with the
// variable that held the error renamed.
func (e *clientEmit) handlerText(h *clientParseCheck, errName string) string {
	f := e.f
	if h.errVar != errName {
		refs, _ := clientRefsIn(h.errVar, h.handler.List)
		for _, r := range refs {
			f.edits.replace(e.id, f.g.offset(r.id.Pos()), f.g.offset(r.id.End()), errName)
		}
	}
	return f.text(h.handler)
}

// pickErr chooses the name of the error variable a request is sent with. The
// name must not capture a use of another variable further down, and a variable
// the code already declared is only reused when nothing can observe the change.
// exists says the name is declared in the scope of the statement.
func (f *clientFile) pickErr(l clientList, idx int, extra []ast.Node, avoid map[string]bool) (name string, exists bool) {
	rest := l.stmts[min(idx+1, len(l.stmts)):]
	for _, cand := range []string{"err", "clientErr"} {
		if avoid[cand] {
			continue
		}
		refs, end := clientRefsInNodes(cand, extra, rest)
		if len(refs) > 0 {
			continue
		}
		if end < len(rest) && !clientDefineStaysValid(rest[end], cand, func(n string) bool {
			return clientDeclaredBefore(l, idx+1+end, n)
		}) {
			continue
		}
		ours := f.intro[l.owner][cand]
		declared := clientDeclaredBefore(l, idx, cand)
		if declared && !ours && !l.fn.reuseSafe(cand) {
			continue
		}
		return cand, declared || ours
	}
	return l.fn.fresh("clientErr"), false
}

// reuseSafe reports whether the variable name can be assigned to without a
// closure, a deferred call or a bare return observing the new value.
func (fn *clientFunc) reuseSafe(name string) bool {
	if fn.closureNames[name] {
		return false
	}
	return !slices.Contains(fn.results, name) || len(fn.bareReturns) == 0
}

// errsPlan decides what becomes of the []error variable of a terminal. The
// uses of a variable that the migrated code does not need any more are
// rewritten to the error of the v3 call; if one of them needs the slice, the
// slice stays.
func (e *clientEmit) errsPlan(it *clientItem, errs clientTermSide, define bool, errName string) (clientErrsMode, []clientEdit) {
	f := e.f
	l := it.list
	t := it.term

	own := errs.isNew
	if define && !own && f.errsGone[l.owner][errs.text] {
		own = true
	}
	if !own {
		return clientErrsExisting, nil
	}

	var refs []clientRef
	if t.ifStmt != nil {
		nodes := []ast.Node{t.ifStmt.Body}
		if t.ifStmt.Cond != nil {
			nodes = append(nodes, t.ifStmt.Cond)
		}
		if t.ifStmt.Else != nil {
			nodes = append(nodes, t.ifStmt.Else)
		}
		refs, _ = clientRefsInNodes(errs.text, nodes, nil)
	} else {
		refs, _ = clientRefsIn(errs.text, l.stmts[it.idx+1:])
	}

	var edits []clientEdit
	printedAt := 0
	for _, r := range refs {
		ed, printed, ok := f.errsRewrite(r, errName)
		if !ok {
			delete(f.errsGone[l.owner], errs.text)
			return clientErrsOwn, nil
		}
		if printed && printedAt == 0 {
			printedAt = f.line(r.id)
		}
		edits = append(edits, ed)
	}
	if printedAt > 0 {
		f.noteOncef(clientNoteErrs, printedAt, "a printed []error of a request is now the error itself, so fmt and log show it without the brackets "+
			"of the slice; reported once for the project")
	}
	if f.errsGone[l.owner] == nil {
		f.errsGone[l.owner] = map[string]bool{}
	}
	f.errsGone[l.owner][errs.text] = true
	return clientErrsRewrite, edits
}

// clientFormatSinks are the functions that only format the errors they get.
var clientFormatSinks = map[string][]string{
	"fmt": {"Errorf", "Sprintf", "Sprint", "Sprintln", "Printf", "Print", "Println", "Fprintf", "Fprint", "Fprintln"},
	"log": {"Printf", "Print", "Println", "Fatalf", "Fatal", "Fatalln", "Panicf", "Panic", "Panicln"},
}

// errsRewrite returns the edit that makes one use of the []error variable work
// with the single error of the v3 call, ok is false when the use needs a slice.
// printed says the use only formats the error, which looks different without the
// slice around it.
func (f *clientFile) errsRewrite(r clientRef, errName string) (ed clientEdit, printed, ok bool) {
	if len(r.stack) == 0 {
		return clientEdit{}, false, false
	}
	parent := r.stack[len(r.stack)-1]
	var grand ast.Node
	if len(r.stack) >= 2 {
		grand = r.stack[len(r.stack)-2]
	}
	edit := func(n ast.Node, text string) (clientEdit, bool, bool) {
		return clientEdit{start: f.g.offset(n.Pos()), end: f.g.offset(n.End()), text: text}, false, true
	}

	switch p := parent.(type) {
	case *ast.CallExpr:
		if isIdentNamed(p.Fun, "len") && len(p.Args) == 1 && p.Args[0] == r.id && !p.Ellipsis.IsValid() {
			if cmp, ok := grand.(*ast.BinaryExpr); ok {
				if text, ok := clientLenComparison(cmp, p, errName); ok {
					return edit(cmp, text)
				}
			}
			return clientEdit{}, false, false
		}
		if f.isErrorsJoin(p, r.id) {
			return edit(p, errName)
		}
		if f.isFormatSink(p, r.id) {
			ed, _, ok := edit(r.id, errName)
			return ed, true, ok
		}
	case *ast.BinaryExpr:
		if (p.Op == token.EQL || p.Op == token.NEQ) && (isIdentNamed(p.X, "nil") || isIdentNamed(p.Y, "nil")) {
			op := "!="
			if p.Op == token.EQL {
				op = "=="
			}
			return edit(p, errName+" "+op+" nil")
		}
	case *ast.IndexExpr:
		if lit, ok := p.Index.(*ast.BasicLit); ok && p.X == r.id && lit.Kind == token.INT && lit.Value == "0" {
			return edit(p, errName)
		}
	}
	return clientEdit{}, false, false
}

// clientLenComparison rewrites a comparison of len(errs) with a constant to a
// comparison of the error with nil.
func clientLenComparison(cmp *ast.BinaryExpr, call *ast.CallExpr, errName string) (string, bool) {
	other, op := cmp.Y, cmp.Op
	if cmp.Y == ast.Expr(call) {
		other = cmp.X
		op = clientFlipComparison(op)
	} else if cmp.X != ast.Expr(call) {
		return "", false
	}
	lit, ok := other.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return "", false
	}
	n, err := strconv.Atoi(lit.Value)
	if err != nil {
		return "", false
	}
	switch {
	case op == token.GTR && n == 0, op == token.NEQ && n == 0, op == token.GEQ && n == 1:
		return errName + " != nil", true
	case op == token.EQL && n == 0, op == token.LSS && n == 1, op == token.LEQ && n == 0:
		return errName + " == nil", true
	default:
		return "", false
	}
}

// clientFlipComparison returns the operator to use when the operands swap.
func clientFlipComparison(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GTR
	case token.GTR:
		return token.LSS
	case token.LEQ:
		return token.GEQ
	case token.GEQ:
		return token.LEQ
	default:
		return op
	}
}

// isErrorsJoin recognizes errors.Join(errs...).
func (f *clientFile) isErrorsJoin(call *ast.CallExpr, id *ast.Ident) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Join" || !call.Ellipsis.IsValid() || len(call.Args) != 1 || call.Args[0] != ast.Expr(id) {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains(f.errors, pkg.Name)
}

// isFormatSink recognizes a call of a printing function with the errors as
// one of the arguments.
func (f *clientFile) isFormatSink(call *ast.CallExpr, id *ast.Ident) bool {
	if call.Ellipsis.IsValid() || !slices.Contains(call.Args, ast.Expr(id)) {
		return false
	}
	if isIdentNamed(call.Fun, "panic") {
		return true
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch {
	case slices.Contains(f.fmt, pkg.Name):
		return slices.Contains(clientFormatSinks["fmt"], sel.Sel.Name)
	case slices.Contains(f.log, pkg.Name):
		return slices.Contains(clientFormatSinks["log"], sel.Sel.Name)
	}
	return false
}
