package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// mw2LocalsPlan is what the migration is going to do to one file. It is made for
// every file before any is rewritten, because whether a config key can be
// dropped depends on the reads of that key in all the other files.
type mw2LocalsPlan struct {
	// edits are the reads that can be migrated.
	edits editSet
	// removals are the config keys that go away once no read of them is left.
	removals []mw2Removal
	// imports are the imports the edits need.
	imports []mw2Import
	// notes are the things the user has to look at.
	notes []mw2PlanNote
}

// mw2PlanNote is a note for a line of a file.
type mw2PlanNote struct {
	msg  string
	line int
}

// mw2Removal drops a key field of a middleware config: v3 has no such field.
type mw2Removal struct {
	role *mw2Role
	// what is the removed text, for the notes.
	what  string
	edits editSet
	key   mw2Key
	line  int
}

// mw2Planner plans one file.
type mw2Planner struct {
	m     *mw2LocalsModule
	f     *mw2LocalsFile
	plan  *mw2LocalsPlan
	quals map[*mw2Role]string
}

// plan works out what to do to a file.
func (m *mw2LocalsModule) plan(f *mw2LocalsFile) *mw2LocalsPlan {
	p := &mw2Planner{m: m, f: f, plan: &mw2LocalsPlan{}, quals: map[*mw2Role]string{}}
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if call, sel, ok := mw2IsLocalsCall(x); ok && len(call.Args) == 1 {
				p.read(call, sel, stack)
			}
		case *ast.CompositeLit:
			p.literal(x)
		case *ast.AssignStmt:
			p.assignment(x, stack)
		}
		return true
	})
	return p.plan
}

// notef records something to tell the user about the node.
func (p *mw2Planner) notef(n ast.Node, format string, args ...any) {
	p.plan.notes = append(p.plan.notes, mw2PlanNote{line: p.f.g.line(n.Pos()), msg: fmt.Sprintf(format, args...)})
}

// skipf leaves a read as it is, and remembers that the key still has a read.
func (p *mw2Planner) skipf(key mw2Key, call *ast.CallExpr, format string, args ...any) {
	p.m.blocked[key]++
	p.notef(call, format, args...)
}

// qualifier returns what precedes the reader of a role in the output, importing
// the package when the file does not.
func (p *mw2Planner) qualifier(role *mw2Role) string {
	if q, ok := p.quals[role]; ok {
		return q
	}

	for alias, roles := range p.f.names {
		if slices.Contains(roles, role) {
			p.quals[role] = alias + "."
			return alias + "."
		}
	}

	name, alias := role.pkg, role.alias
	if alias != "" {
		name = alias
	}
	if mw2NameTaken(p.f.g, p.f.kr.pkg, name) {
		name = "fiber" + role.pkg
		if role.alias != "" {
			name = role.alias + "v3"
		}
		alias = name
	}
	p.plan.imports = append(p.plan.imports, mw2Import{alias: alias, path: role.addPath})
	p.quals[role] = name + "."
	return name + "."
}

// pick chooses the middleware that stored the value a read is after. Several can
// store under the same key; the one the file imports is taken then.
func (p *mw2Planner) pick(candidates []*mw2Role) *mw2Role {
	if len(candidates) == 1 {
		return candidates[0]
	}
	var here []*mw2Role
	for _, role := range candidates {
		if p.f.imports(role) {
			here = append(here, role)
		}
	}
	if len(here) == 1 {
		return here[0]
	}
	return nil
}

// read plans the migration of one c.Locals(key) read.
func (p *mw2Planner) read(call *ast.CallExpr, sel *ast.SelectorExpr, stack []ast.Node) {
	key, ok := p.f.kr.key(call.Args[0])
	if !ok {
		return
	}
	candidates := p.m.candidates(key)
	if len(candidates) == 0 {
		return
	}

	// The receiver decides whether it is the locals of the request: a file that
	// imports fiber reads them from a value it does not know the type of too (that
	// is where the handlers declare their context), but one that does not imports
	// the package for a reason, and a Locals call of a value that is no context
	// is none of the middleware's.
	switch kind := p.receiver(sel, stack); {
	case kind == ctxNo:
		return
	case kind == ctxUnknown && len(p.f.fiber) == 0:
		p.skipf(key, call, "%s(%s) was left as it is: %s is not known to be a fiber context here, so it may not read what %s stored. If it does, use %s.",
			p.f.g.text(sel), p.f.g.text(call.Args[0]), p.f.g.text(sel.X), mw2Names(candidates), mw2Readers(candidates))
		return
	}

	key0 := p.f.g.text(call.Args[0])
	written, isWritten := p.m.setters[key]
	switch role := p.pick(candidates); {
	case isWritten:
		p.skipf(key, call, "%s(%s) was left as it is: your own code stores a value under that key too (%s), so it is not clear whether it reads %s's. If it does, use %s.",
			p.f.g.text(sel), key0, written, mw2Names(candidates), mw2Readers(candidates))
	case role == nil:
		p.skipf(key, call, "%s(%s) was left as it is: %s store a value under that key. Use %s for the one you mean.",
			p.f.g.text(sel), key0, mw2Names(candidates), mw2Readers(candidates))
	case role.reader == "":
		p.skipf(key, call, "%s(%s) was left as it is: basicauth no longer keeps the password in v3. Read the credentials from the Authorization header, or use basicauth.UsernameFromContext for the user name.",
			p.f.g.text(sel), key0)
	default:
		p.rewrite(call, sel, stack, key, role)
	}
}

// receiver says whether the receiver of the Locals call is a fiber context. The
// file types it the way the other migrations of the request context do: a value of
// a type another package of the module declares, a wrapper or an alias of the
// context, is one.
func (p *mw2Planner) receiver(sel *ast.SelectorExpr, stack []ast.Node) ctxKind {
	env := p.f.typing()
	if env == nil {
		return ctxNo
	}

	// A fiber context satisfies an interface that declares Locals: what it holds
	// is not known.
	if kind := env.receiverKind(sel, stack); kind != ctxNo || !env.isInterfaceValue(sel.X, stack) {
		return kind
	}
	return ctxUnknown
}

// readerArg returns the text of the argument of the reader that replaces
// recv.Locals(...): the receiver, or the context it embeds where it is a struct
// that wraps one, because that is a fiber.Ctx whatever the wrapper declares.
func (p *mw2Planner) readerArg(sel *ast.SelectorExpr, stack []ast.Node) string {
	text := p.f.g.text(sel.X)
	if env := p.f.typing(); env != nil {
		if path := env.embeddedCtxOf(sel.X, stack); path != "" {
			return text + "." + path
		}
	}
	return text
}

// mw2Names lists the middleware of the roles for a note.
func mw2Names(roles []*mw2Role) string {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		if !slices.Contains(names, role.pkg) {
			names = append(names, role.pkg)
		}
	}
	slices.Sort(names)
	return mw2List(names, "and")
}

// mw2Readers lists the v3 readers of the roles for a note.
func mw2Readers(roles []*mw2Role) string {
	var readers []string
	for _, role := range roles {
		if role.reader != "" {
			readers = append(readers, role.pkg+"."+role.reader+"(c)")
		}
	}
	slices.Sort(readers)
	return mw2List(readers, "or")
}

// mw2List joins words the way a sentence does: "a", "a and b", "a, b and c".
func mw2List(words []string, conjunction string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " " + conjunction + " " + words[len(words)-1]
}

// mw2ReadContext is where a read sits: the expression (the call, or the
// parentheses around it) and the node it is part of.
type mw2ReadContext struct {
	expr   ast.Expr
	parent ast.Node
	// idx is the index of the parent in the stack.
	idx int
}

// mw2ContextOf finds where node sits, looking through parentheses. stack holds
// the ancestors of node.
func mw2ContextOf(node ast.Expr, stack []ast.Node) mw2ReadContext {
	expr := node
	for i, n := range slices.Backward(stack) {
		if paren, ok := n.(*ast.ParenExpr); ok {
			expr = paren
			continue
		}
		return mw2ReadContext{expr: expr, parent: n, idx: i}
	}
	return mw2ReadContext{expr: expr, idx: -1}
}

// rewrite replaces a read with the call of the v3 reader. Where the value is
// used decides what else has to change: v3 returns a string or a pointer where
// c.Locals returned an any.
func (p *mw2Planner) rewrite(call *ast.CallExpr, sel *ast.SelectorExpr, stack []ast.Node, key mw2Key, role *mw2Role) {
	reader := p.qualifier(role) + role.reader + "(" + p.readerArg(sel, stack) + ")"
	ctx := mw2ContextOf(call, stack)
	edits, g := &p.plan.edits, p.f.g

	// An any stays an any: the reader returns one, whatever surrounds the call stays valid.
	if role.kind == mw2KindAny {
		edits.replaceNode(g, ctx.expr, reader)
		return
	}

	switch parent := ctx.parent.(type) {
	case *ast.TypeAssertExpr:
		if parent.X == ctx.expr {
			if problem := p.assertion(parent, stack[:ctx.idx], role, reader); problem != "" {
				p.skipf(key, call, "c.Locals(%s) was left as it is: %s. Use %s.", p.f.g.text(call.Args[0]), problem, reader)
			}
			return
		}
	case *ast.BinaryExpr:
		if p.nilComparison(parent, ctx.expr, role, reader) {
			return
		}
	case *ast.AssignStmt:
		if p.variable(parent, ctx, stack, key, call, role, reader) {
			return
		}
	case *ast.ValueSpec:
		if parent.Type == nil && p.variableSpec(parent, ctx, stack, key, call, role, reader) {
			return
		}
	}
	edits.replaceNode(g, ctx.expr, reader)
}

// assertion migrates value.(T), where value is a read or a variable that holds
// one: the reader returns the T already. It returns what stops it from doing so.
func (p *mw2Planner) assertion(assert *ast.TypeAssertExpr, above []ast.Node, role *mw2Role, value string) string {
	switch {
	case assert.Type == nil:
		return "a type switch on it would see " + mw2ReturnType(role)
	case !mw2Asserts(assert.Type, role):
		return "it is asserted to " + p.f.g.text(assert.Type) + " but v3 returns " + mw2ReturnType(role)
	}

	outer := mw2ContextOf(assert, above)
	switch parent := outer.parent.(type) {
	case *ast.AssignStmt:
		if len(parent.Lhs) == 2 && len(parent.Rhs) == 1 && parent.Rhs[0] == outer.expr {
			p.commaOK(parent, assert, role, value)
			return ""
		}
	case *ast.ValueSpec:
		if len(parent.Names) == 2 && len(parent.Values) == 1 {
			return "a two-value var declaration cannot test for absence the way v3 does" + mw2PresenceHint(role)
		}
	}
	p.plan.edits.replaceNode(p.f.g, assert, value)
	return ""
}

// mw2PresenceHint tells how to find out that a value is absent.
func mw2PresenceHint(role *mw2Role) string {
	if role.kind == mw2KindString {
		return ` (compare with "")`
	}
	return " (compare with nil)"
}

// mw2ReturnType names what the reader of a role returns.
func mw2ReturnType(role *mw2Role) string {
	switch role.kind {
	case mw2KindString:
		return "a string"
	case mw2KindPointer:
		return "a pointer"
	case mw2KindAny:
		return "an any"
	}
	return "a value"
}

// mw2Asserts reports whether typ is the type the reader of the role returns.
func mw2Asserts(typ ast.Expr, role *mw2Role) bool {
	if role.kind == mw2KindString {
		return isIdentNamed(typ, "string")
	}

	star, ok := typ.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch x := star.X.(type) {
	case *ast.Ident:
		return slices.Contains(role.pointers, x.Name)
	case *ast.SelectorExpr:
		return slices.Contains(role.pointers, x.Sel.Name)
	}
	return false
}

// commaOK migrates v, ok := value.(T), whose value is replaced by the expression
// with the text expr. The reader returns the zero value when the middleware
// stored nothing, which is what ok reports.
func (p *mw2Planner) commaOK(as *ast.AssignStmt, value ast.Node, role *mw2Role, expr string) {
	g, edits := p.f.g, &p.plan.edits
	absent := ` != ""`
	if role.kind != mw2KindString {
		absent = " != nil"
	}

	switch {
	case isIdentNamed(as.Lhs[1], "_"):
		edits.replace(g.offset(as.Lhs[0].End()), g.offset(as.Lhs[1].End()), "")
		edits.replaceNode(g, value, expr)
	case isIdentNamed(as.Lhs[0], "_"):
		edits.replace(g.offset(as.Lhs[0].Pos()), g.offset(as.Lhs[1].Pos()), "")
		edits.replaceNode(g, value, expr+absent)
	default:
		edits.replaceNode(g, value, expr+", "+expr+absent)
	}
}

// nilComparison migrates c.Locals(key) == nil and != nil: a string is never nil.
func (p *mw2Planner) nilComparison(bin *ast.BinaryExpr, read ast.Expr, role *mw2Role, reader string) bool {
	if bin.Op != token.EQL && bin.Op != token.NEQ {
		return false
	}
	other := bin.X
	switch read {
	case bin.X:
		other = bin.Y
	case bin.Y:
	default:
		return false
	}
	if !isIdentNamed(other, "nil") {
		return false
	}

	p.plan.edits.replaceNode(p.f.g, read, reader)
	if role.kind == mw2KindString {
		p.plan.edits.replaceNode(p.f.g, other, `""`)
	}
	return true
}

// variable migrates x := c.Locals(key): the type of x changes with the reader, so
// the uses of x have to follow. It reports whether it dealt with the read.
func (p *mw2Planner) variable(as *ast.AssignStmt, ctx mw2ReadContext, stack []ast.Node, key mw2Key, call *ast.CallExpr, role *mw2Role, reader string) bool {
	if as.Tok != token.DEFINE || len(as.Lhs) != len(as.Rhs) {
		return false
	}
	i := slices.Index(as.Rhs, ctx.expr)
	if i < 0 {
		return false
	}
	name, ok := as.Lhs[i].(*ast.Ident)
	if !ok || name.Name == "_" {
		return false
	}

	scope, known := mw2DeclScope(as, stack[:ctx.idx])
	return p.trackVariable(name, scope, known, ctx.expr, key, call, role, reader)
}

// variableSpec is variable for var x = c.Locals(key).
func (p *mw2Planner) variableSpec(spec *ast.ValueSpec, ctx mw2ReadContext, stack []ast.Node, key mw2Key, call *ast.CallExpr, role *mw2Role, reader string) bool {
	if len(spec.Names) != len(spec.Values) {
		return false
	}
	i := slices.Index(spec.Values, ctx.expr)
	if i < 0 || spec.Names[i].Name == "_" {
		return false
	}

	// The spec sits in a var declaration, which is a statement of a statement list.
	var scope []ast.Node
	known := false
	if ctx.idx >= 2 {
		if decl, ok := stack[ctx.idx-1].(*ast.GenDecl); ok && decl.Tok == token.VAR {
			if ds, ok := stack[ctx.idx-2].(*ast.DeclStmt); ok {
				scope, known = mw2DeclScope(ds, stack[:ctx.idx-2])
			}
		}
	}
	return p.trackVariable(spec.Names[i], scope, known, ctx.expr, key, call, role, reader)
}

// mw2DeclScope returns the nodes in which the name declared by stmt can be used:
// the statements that follow it in its list, or the whole statement an init
// clause belongs to. above holds the ancestors of stmt. known is false when stmt
// sits somewhere this does not understand.
func mw2DeclScope(stmt ast.Stmt, above []ast.Node) (scope []ast.Node, known bool) {
	if len(above) == 0 {
		return nil, false
	}

	var list []ast.Stmt
	switch parent := above[len(above)-1].(type) {
	case *ast.BlockStmt:
		list = parent.List
	case *ast.CaseClause:
		list = parent.Body
	case *ast.CommClause:
		list = parent.Body
	case *ast.IfStmt, *ast.ForStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt:
		return []ast.Node{parent}, true
	default:
		return nil, false
	}

	i := slices.Index(list, stmt)
	if i < 0 {
		return nil, false
	}
	for _, s := range list[i+1:] {
		scope = append(scope, s)
	}
	return scope, true
}

// trackVariable rewrites the declaration of a variable that holds a read, and the
// uses of the variable, or leaves them all as they are when one use cannot follow.
func (p *mw2Planner) trackVariable(name *ast.Ident, scope []ast.Node, known bool, read ast.Expr, key mw2Key, call *ast.CallExpr, role *mw2Role, reader string) bool {
	hint := fmt.Sprintf("%s := c.Locals(%s) was left as it is: ", name.Name, p.f.g.text(call.Args[0]))
	advice := fmt.Sprintf(" Declare it with %s%s instead.", reader, mw2PresenceHint(role))

	uses, ok := mw2UsesOf(name, scope)
	if !known || !ok {
		p.skipf(key, call, "%sthe uses of %s are too varied to follow.%s", hint, name.Name, advice)
		return true
	}

	saved := p.plan
	trial := &mw2LocalsPlan{}
	p.plan = trial
	defer func() { p.plan = saved }()

	for _, u := range uses {
		if problem := p.followUse(u, role, name.Name); problem != "" {
			p.plan = saved
			p.skipf(key, call, "%s%s.%s", hint, problem, advice)
			return true
		}
	}
	saved.edits.edits = append(saved.edits.edits, trial.edits.edits...)
	saved.edits.replaceNode(p.f.g, read, reader)
	return true
}

// mw2Use is a use of a variable and the nodes around it.
type mw2Use struct {
	id *ast.Ident
	// stack are the ancestors of the identifier.
	stack []ast.Node
}

// mw2UsesOf collects the uses of the variable name in scope. ok is false when
// the name is declared again there, or used as a key of a literal, which may be
// a field name.
func mw2UsesOf(name *ast.Ident, scope []ast.Node) (uses []mw2Use, ok bool) {
	ok = true
	for _, root := range scope {
		mw2Inspect(root, func(n ast.Node, stack []ast.Node) bool {
			if !ok {
				return false
			}
			if mw2Redeclares(n, name) {
				ok = false
				return false
			}
			id, isIdent := n.(*ast.Ident)
			if !isIdent || id.Name != name.Name || id == name || len(stack) == 0 {
				return true
			}

			switch parent := stack[len(stack)-1].(type) {
			case *ast.SelectorExpr:
				if parent.Sel == id {
					return true // a field or method that is called like the variable
				}
			case *ast.KeyValueExpr:
				if parent.Key == id {
					ok = false // a struct field or a map key: cannot tell
					return false
				}
			}
			uses = append(uses, mw2Use{id: id, stack: slices.Clone(stack)})
			return true
		})
	}
	return uses, ok
}

// mw2Redeclares reports whether n declares another variable called like name,
// which would make the uses below it uses of that variable.
func mw2Redeclares(n ast.Node, name *ast.Ident) bool {
	declares := func(id ast.Expr) bool {
		other, ok := id.(*ast.Ident)
		return ok && other.Name == name.Name && other != name
	}

	switch x := n.(type) {
	case *ast.AssignStmt:
		return x.Tok == token.DEFINE && slices.ContainsFunc(x.Lhs, declares)
	case *ast.ValueSpec:
		for _, id := range x.Names {
			if declares(id) {
				return true
			}
		}
	case *ast.Field:
		for _, id := range x.Names {
			if declares(id) {
				return true
			}
		}
	case *ast.RangeStmt:
		return x.Tok == token.DEFINE && (x.Key != nil && declares(x.Key) || x.Value != nil && declares(x.Value))
	}
	return false
}

// followUse plans how one use of the variable follows the change of its type. It
// returns what makes it impossible.
func (p *mw2Planner) followUse(u mw2Use, role *mw2Role, name string) string {
	ctx := mw2ContextOf(u.id, u.stack)

	switch parent := ctx.parent.(type) {
	case *ast.BinaryExpr:
		p.followComparison(parent, ctx.expr, role)
	case *ast.TypeAssertExpr:
		if parent.X != ctx.expr {
			return ""
		}
		return p.assertion(parent, u.stack[:ctx.idx], role, name)
	case *ast.AssignStmt:
		if slices.ContainsFunc(parent.Lhs, func(e ast.Expr) bool { return e == ctx.expr }) {
			return name + " is assigned to, and would have to be " + mw2ReturnType(role)
		}
	case *ast.UnaryExpr:
		if parent.Op == token.AND {
			return "its address is taken"
		}
	case *ast.IncDecStmt:
		return name + " is incremented"
	}
	return ""
}

// followComparison follows a comparison of the variable with nil.
func (p *mw2Planner) followComparison(bin *ast.BinaryExpr, read ast.Expr, role *mw2Role) {
	if bin.Op != token.EQL && bin.Op != token.NEQ {
		return
	}
	other := bin.X
	switch read {
	case bin.X:
		other = bin.Y
	case bin.Y:
	default:
		return
	}
	if isIdentNamed(other, "nil") && role.kind == mw2KindString {
		p.plan.edits.replaceNode(p.f.g, other, `""`)
	}
}

// literal plans the removal of the key fields of a middleware config literal.
func (p *mw2Planner) literal(lit *ast.CompositeLit) {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if lit.Type == nil || !ok || sel.Sel.Name != mw2ConfigType || !mw2IsKeyed(lit) {
		return
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}

	for _, role := range p.f.names[id.Name] {
		idx, kv := mw2Field(lit, role.field)
		if kv == nil {
			continue
		}
		key, ok := p.f.kr.key(kv.Value)
		if !ok {
			continue
		}
		removal := mw2Removal{key: key, role: role, what: p.f.g.text(kv), line: p.f.g.line(kv.Pos())}
		mw2RemoveElement(p.f.g, &removal.edits, lit, idx)
		p.plan.removals = append(p.plan.removals, removal)
	}
}

// assignment plans the removal of cfg.ContextKey = key for a middleware config variable.
func (p *mw2Planner) assignment(as *ast.AssignStmt, stack []ast.Node) {
	role, ok := p.f.configAssignment(as, stack)
	if !ok || !slices.Contains(mw2StmtList(stack), ast.Stmt(as)) {
		return
	}
	key, ok := p.f.kr.key(as.Rhs[0])
	if !ok {
		return
	}

	removal := mw2Removal{key: key, role: role, what: p.f.g.text(as), line: p.f.g.line(as.Pos())}
	mw2RemoveStmt(p.f.g, &removal.edits, as)
	p.plan.removals = append(p.plan.removals, removal)
}
