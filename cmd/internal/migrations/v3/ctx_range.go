package v3

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strconv"
)

// The types and the field of the fiber package that carry the ranges of a request.
const (
	ctxTypeRange    = "Range"
	ctxTypeRangeSet = "RangeSet"
	ctxFieldRanges  = "Ranges"
)

// ctxRangeNote is the note for a place where the Start and End of the ranges can
// not be followed any more.
const ctxRangeNote = "c.Range takes an int64 size in Fiber v3, and the Start and End of the returned ranges are int64: convert them where they are used as int"

// ctxRangeUnknownNote is the note for a read of a Start or End of a value whose
// type is not known in a file that handles ranges.
const ctxRangeUnknownNote = "the Start and End of a fiber.RangeSet are int64 in Fiber v3: the type of this value is not known here, so if it is a range, convert it where it is used as int"

// ctxRangeTracker follows the ranges a request context returns through a file.
// In v2 the Start and End of a range are int, in v3 they are int64: the places
// that read them as int need a conversion, and so do the places that set them.
// There are no types to ask, so the tracker goes by the declarations of the
// file and of its package (see rshape): the variable that takes the result of
// c.Range, the Ranges of it, the elements of those, and whatever is declared
// with one of the types of the fiber package, in a slice, a map, a channel, a
// function, a struct, a generic type or behind a function value. A range that is
// handed to a place the tracker can not follow is reported, and so is a read of a
// Start or End of a value whose type is not known.
type ctxRangeTracker struct {
	env   *ctxEnv
	noter *ctxNoter
	plan  *ctxPlan
	// keep are the reads that are int64 where they are used, and stay as they are.
	keep map[*ast.SelectorExpr]bool
	// harmless are the names the file binds to packages that take a range as an
	// argument without looking into it: the standard library and testify.
	harmless map[string]bool
	// imports are the import paths of the packages of the file, by local name.
	imports map[string]string
	memo    map[ast.Expr]*rshape
	busy    map[ast.Expr]bool
	// active is set when the file handles ranges.
	active bool
}

// ctxQueueRangeReads queues the conversions of the Start and End of the ranges
// that c.Range returns, in the file env describes, and reports the places that
// can not be followed.
func ctxQueueRangeReads(plan *ctxPlan, noter *ctxNoter, env *ctxEnv) {
	env.g.walk(newCtxRangeTracker(plan, noter, env).visit)
}

// newCtxRangeTracker prepares the tracker for the file env describes.
func newCtxRangeTracker(plan *ctxPlan, noter *ctxNoter, env *ctxEnv) *ctxRangeTracker {
	t := &ctxRangeTracker{
		env: env, noter: noter, plan: plan,
		keep: map[*ast.SelectorExpr]bool{}, harmless: map[string]bool{}, imports: map[string]string{},
		memo: map[ast.Expr]*rshape{}, busy: map[ast.Expr]bool{},
	}
	for _, imp := range env.g.file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := defaultImportName(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		t.imports[name] = path
		if ctxIsHarmlessImport(path) {
			t.harmless[name] = true
		}
	}

	t.active = t.handlesRanges()
	return t
}

// handlesRanges reports whether the file uses the ranges of a request: it names
// one of the types, reads the Ranges of a value or calls Range of a context.
func (t *ctxRangeTracker) handlesRanges() bool {
	found := t.env.dotImported()
	t.env.g.walk(func(n ast.Node, stack []ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case ctxTypeRange, ctxTypeRangeSet:
			if id, isIdent := sel.X.(*ast.Ident); isIdent && slices.Contains(t.env.pkgNames, id.Name) {
				found = true
			} else if call, isCall := ctxParentCall(sel, stack); isCall && sel.Sel.Name == ctxTypeRange && len(call.Args) == 1 {
				found = true
			}
		case ctxFieldRanges:
			found = true
		}
		return !found
	})
	return found
}

// ctxReTestify matches the import paths of testify.
var ctxReTestify = regexp.MustCompile(`^github\.com/stretchr/testify(/|$)`)

// ctxIsHarmlessImport reports whether a package takes the values it is given
// without caring for their fields: the standard library, which looks at them
// as an interface, and testify, which compares them.
func ctxIsHarmlessImport(path string) bool {
	return contribIsStdPath(path) || ctxReTestify.MatchString(path)
}

// visit looks at one node of the file.
func (t *ctxRangeTracker) visit(n ast.Node, stack []ast.Node) bool {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		t.checkRead(x, stack)
	case *ast.CallExpr:
		t.checkConversion(x, stack)
	case *ast.AssignStmt:
		t.checkAssign(x, stack)
	case *ast.CompositeLit:
		t.checkLiteral(x, stack)
	}

	if expr, ok := n.(ast.Expr); ok && ctxIsValueNode(expr) && t.flowsOut(expr, stack) {
		t.noter.oncef("range", n.Pos(), ctxRangeNote)
	}
	return true
}

// ctxIsValueNode reports whether e can be a value of its own that the tracker
// looks at: the parts of an expression that wrap or select one are looked at
// through the expression they belong to.
func ctxIsValueNode(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.SliceExpr, *ast.CompositeLit, *ast.CallExpr:
		return true
	}
	return false
}

// ctxParent returns the node that holds the node whose ancestors are stack.
func ctxParent(stack []ast.Node) ast.Node {
	if len(stack) == 0 {
		return nil
	}
	return stack[len(stack)-1]
}

// ctxFieldTypes returns the type of each name of a parameter or result list, one
// for each name, or one for a field without a name.
func ctxFieldTypes(fields *ast.FieldList) []ast.Expr {
	if fields == nil {
		return nil
	}

	var out []ast.Expr
	for _, f := range fields.List {
		for range max(len(f.Names), 1) {
			out = append(out, f.Type)
		}
	}
	return out
}

// ctxEnclosingFuncType returns the signature of the function that holds the
// node whose ancestors are stack.
func ctxEnclosingFuncType(stack []ast.Node) *ast.FuncType {
	for _, node := range slices.Backward(stack) {
		switch fn := node.(type) {
		case *ast.FuncLit:
			return fn.Type
		case *ast.FuncDecl:
			return fn.Type
		}
	}
	return nil
}

// ctxIsNumericType reports whether name is a predeclared numeric type, from
// which every conversion to another numeric type is as good as it is from int.
func ctxIsNumericType(name string) bool {
	obj, ok := types.Universe.Lookup(name).(*types.TypeName)
	if !ok {
		return false
	}
	basic, ok := obj.Type().Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsNumeric != 0
}

// isRangeCall reports whether call is c.Range(size) on a fiber context.
func (t *ctxRangeTracker) isRangeCall(call *ast.CallExpr, stack []ast.Node) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != ctxRange || len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return false
	}
	if t.env.isCtxType(sel.X) {
		return false
	}
	kind, _ := t.env.receiverKindVia(sel, stack, "")
	return kind == ctxYes
}

// isSet reports whether the shape has the Start and End of a fiber.RangeSet: it is
// one, or it is a struct that embeds one and does not declare the fields itself.
func (t *ctxRangeTracker) isSet(s *rshape) bool {
	return t.promotesSet(s, 0)
}

func (t *ctxRangeTracker) promotesSet(s *rshape, depth int) bool {
	if s == nil || depth > rMaxDepth {
		return false
	}
	switch s.kind {
	case rkSet:
		return true
	case rkStruct:
		if s.fields == nil {
			return false
		}
		for _, f := range s.fields.List {
			for _, id := range f.Names {
				if id.Name == "Start" || id.Name == "End" {
					return false
				}
			}
		}
		for _, f := range s.fields.List {
			if len(f.Names) == 0 && t.promotesSet(t.typeShape(f.Type, s.subst, depth+1), depth+1) {
				return true
			}
		}
	case rkNone, rkRange, rkSlice, rkMap, rkChan, rkFunc, rkIface, rkParam:
	}
	return false
}

// isRangeField reports whether sel reads or writes the Start or the End of a range.
func (t *ctxRangeTracker) isRangeField(sel *ast.SelectorExpr, stack []ast.Node) bool {
	return (sel.Sel.Name == "Start" || sel.Sel.Name == "End") && t.isSet(t.shapeOf(sel.X, stack))
}

// checkRead converts the Start or the End of a range, which is an int64 now,
// where it is read: v2 gave an int, and so does the conversion. A Start or End of
// a value whose type is not known is reported when the file handles ranges.
func (t *ctxRangeTracker) checkRead(sel *ast.SelectorExpr, stack []ast.Node) {
	if sel.Sel.Name != "Start" && sel.Sel.Name != "End" {
		return
	}
	if _, isCall := ctxParentCall(sel, stack); isCall {
		return // a method of that name
	}

	shape := t.shapeOf(sel.X, stack)
	if shape == nil && t.active {
		t.noter.oncef("range-unknown", sel.Pos(), "%s", ctxRangeUnknownNote)
	}
	if t.keep[sel] || !t.isSet(shape) {
		return
	}

	switch p := ctxParent(stack).(type) {
	case *ast.AssignStmt:
		if slices.Contains(p.Lhs, ast.Expr(sel)) {
			return // a target, which checkAssign dealt with
		}
	case *ast.IncDecStmt:
		return
	case *ast.UnaryExpr:
		if p.Op == token.AND {
			t.noter.oncef("range", sel.Pos(), "%s", ctxRangeNote)
			return
		}
	case *ast.CallExpr:
		// int(rs.Start) or float64(rs.End) are as good for an int64 as for an int.
		if id, ok := ast.Unparen(p.Fun).(*ast.Ident); ok && len(p.Args) == 1 && ctxIsNumericType(id.Name) {
			return
		}
	}

	g := t.env.g
	t.plan.add(func(e *editSet) {
		e.replaceNode(g, sel, "int("+ctxRenderNode(e, g, sel)+")")
	})
}

// checkConversion rewrites the conversion of a range to a struct of the code, and
// of such a struct to a range. In v2 span(rs) was valid when span had the
// fields Start and End of type int; the fields of a RangeSet are int64 now, so
// the struct is made from them.
func (t *ctxRangeTracker) checkConversion(call *ast.CallExpr, stack []ast.Node) {
	if len(call.Args) != 1 || call.Ellipsis.IsValid() || !t.isTypeExpr(ast.Unparen(call.Fun), stack) {
		return
	}

	to, from := t.typeShape(call.Fun, nil, 0), t.shapeOf(call.Args[0], stack)
	var elem string
	switch {
	case from != nil && from.kind == rkSet && t.isStartEndStruct(to):
		elem = "int"
	case to != nil && to.kind == rkSet && t.isStartEndStruct(from):
		elem = "int64"
	default:
		return
	}

	g := t.env.g
	target := g.text(call.Fun)
	arg := call.Args[0]
	pure := ctxIsPure(arg)
	t.plan.add(func(e *editSet) {
		operand := ctxRenderNode(e, g, arg)
		if pure {
			e.replaceNode(g, call, target+"{Start: "+elem+"("+operand+".Start), End: "+elem+"("+operand+".End)}")
			return
		}
		e.replaceNode(g, call, "func(v "+t.env.qual(ctxTypeRangeSet)+") "+target+" {\nreturn "+target+"{Start: "+elem+"(v.Start), End: "+elem+"(v.End)}\n}("+operand+")")
	})
}

// isStartEndStruct reports whether the shape is a struct with the two fields
// Start and End of type int, the shape of the RangeSet of v2.
func (t *ctxRangeTracker) isStartEndStruct(s *rshape) bool {
	if s == nil || s.kind != rkStruct || s.fields == nil {
		return false
	}

	var names []string
	for _, f := range s.fields.List {
		if !isIdentNamed(f.Type, "int") {
			return false
		}
		for _, id := range f.Names {
			names = append(names, id.Name)
		}
	}
	return slices.Equal(names, []string{"Start", "End"})
}

// checkAssign converts what is assigned to the Start or the End of a range to
// int64.
func (t *ctxRangeTracker) checkAssign(as *ast.AssignStmt, stack []ast.Node) {
	if as.Tok == token.DEFINE {
		return
	}

	for i, lhs := range as.Lhs {
		sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
		if !ok || !t.isRangeField(sel, stack) {
			continue
		}
		if len(as.Lhs) != len(as.Rhs) {
			t.noter.oncef("range", sel.Pos(), "%s", ctxRangeNote)
			return
		}
		t.checkInt64(as.Rhs[i], stack)
	}
}

// checkLiteral converts the values in a literal of a range to int64.
func (t *ctxRangeTracker) checkLiteral(lit *ast.CompositeLit, stack []ast.Node) {
	shape := t.shapeOf(lit, stack)
	if shape == nil || shape.kind != rkSet {
		return
	}

	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if isIdentNamed(kv.Key, "Start") || isIdentNamed(kv.Key, "End") {
				t.checkInt64(kv.Value, stack)
			}
			continue
		}
		t.checkInt64(elt, stack)
	}
}

// checkInt64 converts the expression e, which is assigned to a place that holds
// an int64, unless it is that already or a constant that fits.
func (t *ctxRangeTracker) checkInt64(e ast.Expr, stack []ast.Node) {
	switch x := ast.Unparen(e).(type) {
	case *ast.BasicLit:
		return
	case *ast.UnaryExpr:
		if _, isLit := x.X.(*ast.BasicLit); isLit && (x.Op == token.SUB || x.Op == token.ADD) {
			return
		}
	case *ast.SelectorExpr:
		if t.isRangeField(x, stack) {
			t.keep[x] = true
			return
		}
	case *ast.CallExpr:
		if isIdentNamed(x.Fun, "int64") && len(x.Args) == 1 {
			return
		}
	}

	g := t.env.g
	t.plan.add(func(ed *editSet) {
		ed.replaceNode(g, e, "int64("+ctxRenderNode(ed, g, e)+")")
	})
}

// flowsOut reports whether the range e, whose ancestors are stack, is handed to
// a place that the tracker does not follow.
func (t *ctxRangeTracker) flowsOut(e ast.Expr, stack []ast.Node) bool {
	switch p := ctxParent(stack).(type) {
	case *ast.AssignStmt:
		return t.holdsRange(e, stack) && t.assignFlowsOut(p, e, stack)
	case *ast.ValueSpec:
		return slices.Index(p.Values, e) >= 0 && p.Type != nil && t.holdsRange(e, stack) && t.typeShape(p.Type, nil, 0) == nil
	case *ast.ReturnStmt:
		return t.holdsRange(e, stack) && t.returnFlowsOut(p, e, stack)
	case *ast.CallExpr:
		return p.Fun != e && t.holdsRange(e, stack) && t.argFlowsOut(p, e, stack)
	case *ast.CompositeLit:
		return t.holdsRange(e, stack) && t.shapeOf(p, stack[:len(stack)-1]) == nil
	case *ast.KeyValueExpr:
		return t.holdsRange(e, stack) && t.valueFlowsOut(p, e, stack)
	case *ast.SendStmt:
		return p.Value == e && t.holdsRange(e, stack) && t.shapeOf(p.Chan, stack) == nil
	}
	return false
}

// holdsRange reports whether the value e is, or holds, a range.
func (t *ctxRangeTracker) holdsRange(e ast.Expr, stack []ast.Node) bool {
	return t.shapeOf(e, stack).holdsRange()
}

// assignFlowsOut is flowsOut for a value on the right side of an assignment.
func (t *ctxRangeTracker) assignFlowsOut(as *ast.AssignStmt, e ast.Expr, stack []ast.Node) bool {
	i := slices.Index(as.Rhs, e)
	switch {
	case i < 0:
		return false // a target
	case len(as.Rhs) == 1:
		i = 0 // the first of the results of a call
	case len(as.Lhs) != len(as.Rhs):
		return false
	}

	target := as.Lhs[i]
	if id, ok := target.(*ast.Ident); ok && (id.Name == "_" || as.Tok == token.DEFINE) {
		return false // the new variable is what it is initialized with
	}
	return t.shapeOf(target, stack) == nil
}

// returnFlowsOut is flowsOut for a value that is returned.
func (t *ctxRangeTracker) returnFlowsOut(ret *ast.ReturnStmt, e ast.Expr, stack []ast.Node) bool {
	ft := ctxEnclosingFuncType(stack)
	i := slices.Index(ret.Results, e)
	if ft == nil || i < 0 {
		return false
	}
	results := ctxFieldTypes(ft.Results)
	if i >= len(results) {
		return false
	}
	return t.typeShape(results[i], nil, 0) == nil
}

// valueFlowsOut is flowsOut for the value of a field in a composite literal.
func (t *ctxRangeTracker) valueFlowsOut(kv *ast.KeyValueExpr, e ast.Expr, stack []ast.Node) bool {
	if kv.Value != e || len(stack) < 2 {
		return false
	}

	lit, ok := stack[len(stack)-2].(*ast.CompositeLit)
	if !ok {
		return false
	}
	shape := t.shapeOf(lit, stack[:len(stack)-2])
	if shape == nil {
		return true
	}
	if shape.kind != rkStruct {
		return false // the value of a map or an element of a slice: typed by the literal
	}
	key, isIdent := kv.Key.(*ast.Ident)
	return !isIdent || t.member(shape, key.Name, 0) == nil
}

// argFlowsOut is flowsOut for an argument of a call.
func (t *ctxRangeTracker) argFlowsOut(call *ast.CallExpr, e ast.Expr, stack []ast.Node) bool {
	fun := ast.Unparen(call.Fun)
	if id, ok := fun.(*ast.Ident); ok {
		if _, builtin := types.Universe.Lookup(id.Name).(*types.Builtin); builtin {
			return false
		}
	}
	if t.isTypeExpr(fun, stack) {
		return false // a conversion
	}
	if root := ctxRootIdent(fun); root != nil && (t.harmless[root.Name] || slices.Contains(t.env.pkgNames, root.Name)) && t.isPackageName(root, stack) {
		return false
	}
	if sel, ok := fun.(*ast.SelectorExpr); ok && t.env.receiverKind(sel, stack) == ctxYes {
		return false // c.JSON(r)
	}

	fn := t.shapeOf(fun, stack)
	if fn == nil || fn.kind != rkFunc {
		return true
	}
	i := slices.Index(call.Args, e)
	switch {
	case i < 0 || len(fn.params) == 0:
		return true
	case i >= len(fn.params):
		i = len(fn.params) - 1
	}
	return fn.params[i] == nil
}

// ctxRootIdent returns the identifier an expression of selectors, calls and
// indexes starts from: json in json.NewEncoder(w).Encode.
func ctxRootIdent(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			e = x.X
		case *ast.CallExpr:
			e = x.Fun
		case *ast.IndexExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return nil
		}
	}
}
