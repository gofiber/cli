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

// ctxRangeKind says which of the types that carry the ranges of a request a value
// has.
type ctxRangeKind int

const (
	// ctxRangeNone: anything else.
	ctxRangeNone ctxRangeKind = iota
	// ctxRangeValue: a fiber.Range, what c.Range returns.
	ctxRangeValue
	// ctxRangeSlice: a []fiber.RangeSet, the Ranges of a fiber.Range.
	ctxRangeSlice
	// ctxRangeSet: a fiber.RangeSet, one of the ranges.
	ctxRangeSet
)

// ctxRangeTracker follows the ranges a request context returns through a file.
// In v2 the Start and End of a range are int, in v3 they are int64: the places
// that read them as int need a conversion, and so do the places that set them.
// There are no types to ask, so the tracker goes by the declarations of the
// file: the variable that takes the result of c.Range, the Ranges of it, the
// elements of those, and whatever is declared as one of the types of the fiber
// package. A range that is handed to a place the tracker can not follow is
// reported.
type ctxRangeTracker struct {
	env   *ctxEnv
	noter *ctxNoter
	plan  *ctxPlan
	// keep are the reads that are int64 where they are used, and stay as they are.
	keep map[*ast.SelectorExpr]bool
	// harmless are the names the file binds to packages that take a range as an
	// argument without looking into it: the standard library and testify.
	harmless map[string]bool
}

// ctxQueueRangeReads queues the conversions of the Start and End of the ranges
// that c.Range returns, in the file env describes, and reports the places that
// can not be followed.
func ctxQueueRangeReads(plan *ctxPlan, noter *ctxNoter, env *ctxEnv) {
	t := &ctxRangeTracker{env: env, noter: noter, plan: plan, keep: map[*ast.SelectorExpr]bool{}, harmless: map[string]bool{}}
	for _, imp := range env.g.file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !ctxIsHarmlessImport(path) {
			continue
		}
		name := defaultImportName(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		t.harmless[name] = true
	}

	env.g.walk(t.visit)
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
	case *ast.CallExpr:
		t.checkSource(x, stack)
	case *ast.SelectorExpr:
		t.checkRead(x, stack)
	case *ast.AssignStmt:
		t.checkAssign(x, stack)
	case *ast.CompositeLit:
		t.checkLiteral(x, stack)
	}

	if expr, ok := n.(ast.Expr); ok && ctxIsValueNode(expr) && t.kindOf(expr, stack, 0) != ctxRangeNone && t.flowsOut(expr, stack) {
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

// ctxSite is a place that declares a name, with what is known about its type.
type ctxSite struct {
	// typ is the type it is declared with.
	typ ast.Expr
	// init is the value it starts with, or the call it is one of the results of.
	init ast.Expr
	// over is the value a range statement loops over, for the variable that
	// takes its elements.
	over ast.Expr
	// stack holds the ancestors of init and over.
	stack []ast.Node
	// index is the position of the name among the results of init when that is
	// a call with several, -1 otherwise.
	index int
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
			if as, isAssign := n.Assign.(*ast.AssignStmt); isAssign && len(as.Lhs) == 1 && isIdentNamed(as.Lhs[0], name) && pos > n.Body.Lbrace {
				return ctxSite{index: -1}, true
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE && pos > n.Body.Lbrace {
				switch {
				case isIdentNamed(n.Value, name):
					return ctxSite{over: n.X, stack: scope, index: -1}, true
				case isIdentNamed(n.Key, name):
					return ctxSite{index: -1}, true
				}
			}
		case *ast.FuncLit:
			if site, ok = ctxParamSite(name, pos, n.Body, n.Type, nil); ok {
				return site, true
			}
		case *ast.FuncDecl:
			if site, ok = ctxParamSite(name, pos, n.Body, n.Type, n.Recv); ok {
				return site, true
			}
		}

		if site, ok = ctxStmtSite(name, pos, stmts, scope); ok {
			return site, true
		}
	}
	return ctxSite{}, false
}

// ctxParamSite looks name up among the receiver, parameters and results of a
// function whose body holds pos.
func ctxParamSite(name string, pos token.Pos, body *ast.BlockStmt, ft *ast.FuncType, recv *ast.FieldList) (ctxSite, bool) {
	if body == nil || pos <= body.Lbrace {
		return ctxSite{}, false
	}

	for _, fields := range []*ast.FieldList{recv, ft.Params, ft.Results} {
		if fields == nil {
			continue
		}
		for _, f := range fields.List {
			if slices.ContainsFunc(f.Names, func(id *ast.Ident) bool { return id.Name == name }) {
				return ctxSite{typ: f.Type, index: -1}, true
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
				site := ctxSite{stack: slices.Concat(scope, []ast.Node{s}), index: -1}
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

// kindOf says what the expression e is, given the ancestors of it.
func (t *ctxRangeTracker) kindOf(e ast.Expr, stack []ast.Node, depth int) ctxRangeKind {
	if depth > ctxMaxDepth {
		return ctxRangeNone
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return t.kindOf(x.X, stack, depth+1)
	case *ast.StarExpr:
		return t.kindOf(x.X, stack, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return t.kindOf(x.X, stack, depth+1)
		}
	case *ast.Ident:
		if site, ok := ctxResolveSite(x.Name, x.Pos(), stack); ok {
			return t.siteKind(site, depth+1)
		}
	case *ast.SelectorExpr:
		switch base := t.kindOf(x.X, stack, depth+1); {
		case base == ctxRangeValue && x.Sel.Name == ctxFieldRanges:
			return ctxRangeSlice
		case base == ctxRangeNone:
			return t.typeKind(t.env.typeOf(x, stack), depth+1)
		}
	case *ast.IndexExpr:
		if t.kindOf(x.X, stack, depth+1) == ctxRangeSlice {
			return ctxRangeSet
		}
	case *ast.SliceExpr:
		if t.kindOf(x.X, stack, depth+1) == ctxRangeSlice {
			return ctxRangeSlice
		}
	case *ast.CompositeLit:
		return t.litKind(x, stack, depth+1)
	case *ast.CallExpr:
		if !t.isRangeCall(x, stack) {
			return t.callKind(x, 0, stack, depth+1)
		}
	}
	return ctxRangeNone
}

// siteKind says what the name declared at the site is.
func (t *ctxRangeTracker) siteKind(site ctxSite, depth int) ctxRangeKind {
	switch {
	case site.typ != nil:
		return t.typeKind(site.typ, depth)
	case site.over != nil:
		if t.kindOf(site.over, site.stack, depth) == ctxRangeSlice {
			return ctxRangeSet
		}
	case site.init != nil && site.index < 0:
		return t.kindOf(site.init, site.stack, depth)
	case site.init != nil:
		if call, ok := site.init.(*ast.CallExpr); ok {
			return t.callKind(call, site.index, site.stack, depth)
		}
	}
	return ctxRangeNone
}

// typeKind says which kind the type expression typ spells.
func (t *ctxRangeTracker) typeKind(typ ast.Expr, depth int) ctxRangeKind {
	if typ == nil || depth > ctxMaxDepth {
		return ctxRangeNone
	}

	switch x := typ.(type) {
	case *ast.ParenExpr:
		return t.typeKind(x.X, depth+1)
	case *ast.StarExpr:
		return t.typeKind(x.X, depth+1)
	case *ast.Ellipsis:
		if t.typeKind(x.Elt, depth+1) == ctxRangeSet {
			return ctxRangeSlice
		}
		return ctxRangeNone
	case *ast.ArrayType:
		if t.typeKind(x.Elt, depth+1) == ctxRangeSet {
			return ctxRangeSlice
		}
		return ctxRangeNone
	case *ast.Ident:
		// A type of the file is what it is declared as, whatever its name.
		if spec := t.env.typeDecl[x.Name]; spec != nil {
			return t.typeKind(spec.Type, depth+1)
		}
	}

	// The type may be declared in another file of the package, which can bind the
	// fiber package to another name.
	switch {
	case ctxIsFiberType(typ, t.env.pkgNames, ctxTypeRange):
		return ctxRangeValue
	case ctxIsFiberType(typ, t.env.pkgNames, ctxTypeRangeSet):
		return ctxRangeSet
	}
	return ctxRangeNone
}

// litKind says which kind the composite literal lit is. The type of a literal
// inside another one may be left out.
func (t *ctxRangeTracker) litKind(lit *ast.CompositeLit, stack []ast.Node, depth int) ctxRangeKind {
	if lit.Type != nil {
		return t.typeKind(lit.Type, depth)
	}
	if outer, ok := ctxParent(stack).(*ast.CompositeLit); ok && t.litKind(outer, stack[:len(stack)-1], depth+1) == ctxRangeSlice {
		return ctxRangeSet
	}
	return ctxRangeNone
}

// callKind says what the idx-th result of the call is.
func (t *ctxRangeTracker) callKind(call *ast.CallExpr, idx int, stack []ast.Node, depth int) ctxRangeKind {
	if t.isRangeCall(call, stack) {
		if idx == 0 {
			return ctxRangeValue
		}
		return ctxRangeNone
	}

	if isIdentNamed(call.Fun, "append") && len(call.Args) > 0 {
		return t.kindOf(call.Args[0], stack, depth+1)
	}

	if ft := t.calleeType(call, stack); ft != nil {
		if results := ctxFieldTypes(ft.Results); idx < len(results) {
			return t.typeKind(results[idx], depth+1)
		}
	}
	return ctxRangeNone
}

// calleeType returns the signature of the function or method of the file that
// call calls, nil when that is not known.
func (t *ctxRangeTracker) calleeType(call *ast.CallExpr, stack []ast.Node) *ast.FuncType {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.FuncLit:
		return fun.Type
	case *ast.Ident:
		if decl := t.env.funcDecl[fun.Name]; decl != nil {
			return decl.Type
		}
	case *ast.SelectorExpr:
		if recv := ctxTypeName(t.env.typeOf(fun.X, stack)); recv != "" {
			if decl := t.env.methods[recv+"."+fun.Sel.Name]; decl != nil {
				return decl.Type
			}
		}
	}
	return nil
}

// isRangeCall reports whether call is c.Range(size) on a fiber context.
func (t *ctxRangeTracker) isRangeCall(call *ast.CallExpr, stack []ast.Node) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != ctxRange || len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return false
	}
	kind, _ := t.env.receiverKindVia(sel, stack, "")
	return kind == ctxYes
}

// checkSource looks at where the two results of c.Range(size) go. They are
// followed when they are assigned to variables or returned as the results of
// a function that is declared to return them.
func (t *ctxRangeTracker) checkSource(call *ast.CallExpr, stack []ast.Node) {
	if !t.isRangeCall(call, stack) {
		return
	}

	followed := false
	switch p := ctxParent(stack).(type) {
	case *ast.ExprStmt:
		followed = true
	case *ast.AssignStmt:
		followed = len(p.Lhs) == 2 && len(p.Rhs) == 1 && t.holdsValue(p.Lhs[0], p.Tok == token.DEFINE, stack)
	case *ast.ValueSpec:
		followed = len(p.Names) == 2 && len(p.Values) == 1 && (p.Type == nil || t.typeKind(p.Type, 0) == ctxRangeValue)
	case *ast.ReturnStmt:
		if ft := ctxEnclosingFuncType(stack); ft != nil {
			results := ctxFieldTypes(ft.Results)
			followed = len(p.Results) == 1 && len(results) == 2 && t.typeKind(results[0], 0) == ctxRangeValue
		}
	}
	if !followed {
		t.noter.oncef("range", call.Pos(), ctxRangeNote)
	}
}

// holdsValue reports whether the target of an assignment is a place that holds
// a fiber.Range: a variable the assignment declares, the blank identifier, or
// anything that is declared with that type.
func (t *ctxRangeTracker) holdsValue(target ast.Expr, declares bool, stack []ast.Node) bool {
	if id, ok := target.(*ast.Ident); ok && (id.Name == "_" || declares) {
		return true
	}
	return t.kindOf(target, stack, 0) == ctxRangeValue
}

// isRangeField reports whether sel reads or writes the Start or the End of a range.
func (t *ctxRangeTracker) isRangeField(sel *ast.SelectorExpr, stack []ast.Node) bool {
	return (sel.Sel.Name == "Start" || sel.Sel.Name == "End") && t.kindOf(sel.X, stack, 0) == ctxRangeSet
}

// checkRead converts the Start or the End of a range, which is an int64 now,
// where it is read: v2 gave an int, and so does the conversion.
func (t *ctxRangeTracker) checkRead(sel *ast.SelectorExpr, stack []ast.Node) {
	if t.keep[sel] || !t.isRangeField(sel, stack) {
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
			t.noter.oncef("range", sel.Pos(), ctxRangeNote)
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
			t.noter.oncef("range", sel.Pos(), ctxRangeNote)
			return
		}
		t.checkInt64(as.Rhs[i], stack)
	}
}

// checkLiteral converts the values in a literal of a range to int64.
func (t *ctxRangeTracker) checkLiteral(lit *ast.CompositeLit, stack []ast.Node) {
	if t.litKind(lit, stack, 0) != ctxRangeSet {
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
	case *ast.SelectorExpr, *ast.ParenExpr, *ast.StarExpr, *ast.BinaryExpr, *ast.RangeStmt, *ast.IndexExpr, *ast.SliceExpr:
		return false // the range is looked at through the expression it is part of
	case *ast.Field, *ast.TypeSpec, *ast.FuncDecl, *ast.LabeledStmt, *ast.BranchStmt, *ast.CaseClause:
		return false // the name of a declaration, or a comparison
	case *ast.UnaryExpr:
		return p.Op != token.AND
	case *ast.AssignStmt:
		return t.assignFlowsOut(p, e, stack)
	case *ast.ValueSpec:
		return slices.Index(p.Values, e) >= 0 && p.Type != nil && t.typeKind(p.Type, 0) == ctxRangeNone
	case *ast.ReturnStmt:
		return t.returnFlowsOut(p, e, stack)
	case *ast.CallExpr:
		return p.Fun != e && t.argFlowsOut(p, e, stack)
	case *ast.CompositeLit:
		return t.litKind(p, stack[:len(stack)-1], 0) == ctxRangeNone
	case *ast.KeyValueExpr:
		return t.valueFlowsOut(p, e, stack)
	}
	return true
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
		return true
	}

	target := as.Lhs[i]
	if id, ok := target.(*ast.Ident); ok && (id.Name == "_" || as.Tok == token.DEFINE) {
		return false // the new variable is what it is initialized with
	}
	return t.kindOf(target, stack, 0) == ctxRangeNone
}

// returnFlowsOut is flowsOut for a value that is returned.
func (t *ctxRangeTracker) returnFlowsOut(ret *ast.ReturnStmt, e ast.Expr, stack []ast.Node) bool {
	ft := ctxEnclosingFuncType(stack)
	i := slices.Index(ret.Results, e)
	if ft == nil || i < 0 {
		return true
	}
	results := ctxFieldTypes(ft.Results)
	return len(results) != len(ret.Results) || t.typeKind(results[i], 0) == ctxRangeNone
}

// valueFlowsOut is flowsOut for the value of a field in a composite literal.
func (t *ctxRangeTracker) valueFlowsOut(kv *ast.KeyValueExpr, e ast.Expr, stack []ast.Node) bool {
	if kv.Value != e || len(stack) < 2 {
		return false
	}

	lit, ok := stack[len(stack)-2].(*ast.CompositeLit)
	if !ok {
		return true
	}
	if t.litKind(lit, stack[:len(stack)-2], 0) != ctxRangeNone {
		return false
	}
	key, isIdent := kv.Key.(*ast.Ident)
	if !isIdent || lit.Type == nil {
		return true
	}
	return t.typeKind(t.env.structFieldType(lit.Type, key.Name, 0), 0) == ctxRangeNone
}

// argFlowsOut is flowsOut for an argument of a call.
func (t *ctxRangeTracker) argFlowsOut(call *ast.CallExpr, e ast.Expr, stack []ast.Node) bool {
	fun := ast.Unparen(call.Fun)
	if id, ok := fun.(*ast.Ident); ok {
		if _, builtin := types.Universe.Lookup(id.Name).(*types.Builtin); builtin {
			return false
		}
	}
	if root := ctxRootIdent(fun); root != nil && t.harmless[root.Name] {
		return false
	}
	if sel, ok := fun.(*ast.SelectorExpr); ok && t.env.receiverKind(sel, stack) == ctxYes {
		return false // c.JSON(r)
	}

	ft := t.calleeType(call, stack)
	if ft == nil {
		return true
	}
	params := ctxFieldTypes(ft.Params)
	i := slices.Index(call.Args, e)
	switch {
	case i < 0 || len(params) == 0:
		return true
	case i >= len(params):
		i = len(params) - 1
	}
	return t.typeKind(params[i], 0) == ctxRangeNone
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
