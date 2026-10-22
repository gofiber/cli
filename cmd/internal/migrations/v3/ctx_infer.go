package v3

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
)

// rNoneResults are the results of a call of a function that knows nothing of
// ranges: any of them holds no range.
var rNoneResults = []*rshape{rNone, rNone, rNone, rNone}

// typeShape returns the shape of the type expression e. subst binds the type
// parameters of the declaration e is part of.
func (t *ctxRangeTracker) typeShape(e ast.Expr, subst map[string]*rshape, depth int) *rshape {
	if e == nil || depth > rMaxDepth {
		return nil
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return t.typeShape(x.X, subst, depth+1)
	case *ast.StarExpr:
		return t.typeShape(x.X, subst, depth+1)
	case *ast.Ellipsis:
		return rSliceOf(t.typeShape(x.Elt, subst, depth+1))
	case *ast.ArrayType:
		return rSliceOf(t.typeShape(x.Elt, subst, depth+1))
	case *ast.MapType:
		return rMapOf(t.typeShape(x.Key, subst, depth+1), t.typeShape(x.Value, subst, depth+1))
	case *ast.ChanType:
		return rChanOf(t.typeShape(x.Value, subst, depth+1))
	case *ast.FuncType:
		return t.funcShape(x, subst, depth+1)
	case *ast.StructType:
		return &rshape{kind: rkStruct, fields: x.Fields, subst: subst}
	case *ast.InterfaceType:
		if x.Methods == nil || len(x.Methods.List) == 0 {
			return rNone
		}
		return &rshape{kind: rkIface, fields: x.Methods, subst: subst}
	case *ast.Ident:
		return t.identType(x, subst, depth)
	case *ast.SelectorExpr:
		return t.selectorType(x)
	case *ast.IndexExpr:
		return t.instantiated(x.X, []ast.Expr{x.Index}, subst, depth)
	case *ast.IndexListExpr:
		return t.instantiated(x.X, x.Indices, subst, depth)
	}
	return nil
}

// identType is typeShape for a type named by an identifier.
func (t *ctxRangeTracker) identType(id *ast.Ident, subst map[string]*rshape, depth int) *rshape {
	if bound, ok := subst[id.Name]; ok {
		return bound
	}
	if spec := t.env.typeDecl[id.Name]; spec != nil {
		return t.declared(spec, nil, depth+1)
	}
	if t.env.dotImported() {
		switch id.Name {
		case ctxTypeRange:
			return rRange
		case ctxTypeRangeSet:
			return rSet
		}
	}
	if ctxIsPredeclaredType(id.Name) {
		return rNone
	}
	return nil
}

// selectorType is typeShape for a type of a package: fiber.Range and
// fiber.RangeSet are the ranges, any other type of the fiber package or of a
// package that knows nothing of ranges holds none.
func (t *ctxRangeTracker) selectorType(sel *ast.SelectorExpr) *rshape {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil
	}

	switch {
	case slices.Contains(t.env.pkgNames, id.Name):
		switch sel.Sel.Name {
		case ctxTypeRange:
			return rRange
		case ctxTypeRangeSet:
			return rSet
		}
		return rNone
	case t.harmless[id.Name]:
		return rNone
	}
	return nil
}

// instantiated is typeShape for a generic type with its type arguments, base[args].
func (t *ctxRangeTracker) instantiated(base ast.Expr, args []ast.Expr, subst map[string]*rshape, depth int) *rshape {
	shapes := make([]*rshape, len(args))
	for i, arg := range args {
		shapes[i] = t.typeShape(arg, subst, depth+1)
	}

	switch b := base.(type) {
	case *ast.Ident:
		if spec := t.env.typeDecl[b.Name]; spec != nil {
			return t.declared(spec, shapes, depth+1)
		}
	case *ast.SelectorExpr:
		// iter.Seq[V] and iter.Seq2[K, V] are functions that call a function with the values
		if pkg, ok := b.X.(*ast.Ident); ok && t.imports[pkg.Name] == "iter" && (b.Sel.Name == "Seq" || b.Sel.Name == "Seq2") {
			return t.sequence(shapes)
		}
		if t.harmless[ctxRootName(b)] && !slices.ContainsFunc(shapes, func(s *rshape) bool { return s != nil && s.holdsRange() }) {
			return rNone
		}
	}
	return nil
}

// sequence is the shape of an iterator that yields values of the given shapes.
func (t *ctxRangeTracker) sequence(yielded []*rshape) *rshape {
	return rFuncOf([]*rshape{rFuncOf(yielded, []*rshape{rNone})}, nil)
}

// ctxRootName returns the name of the package a qualified identifier is in.
func ctxRootName(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// declared returns the shape of a type declared in the package, instantiated with
// the type arguments given, which may be fewer than its type parameters.
func (t *ctxRangeTracker) declared(spec *ast.TypeSpec, args []*rshape, depth int) *rshape {
	subst := map[string]*rshape{}
	if spec.TypeParams != nil {
		i := 0
		for _, f := range spec.TypeParams.List {
			for _, id := range f.Names {
				if i < len(args) {
					subst[id.Name] = args[i]
				} else {
					subst[id.Name] = &rshape{kind: rkParam, name: id.Name}
				}
				i++
			}
		}
	}

	under := t.typeShape(spec.Type, subst, depth+1)
	if under == nil {
		return nil
	}
	named := *under
	named.name = spec.Name.Name
	named.args = args
	return &named
}

// funcShape returns the shape of a function with the signature ft. A generic
// function lists its type parameters, which its parameters and results refer to.
func (t *ctxRangeTracker) funcShape(ft *ast.FuncType, subst map[string]*rshape, depth int) *rshape {
	if ft == nil || depth > rMaxDepth {
		return nil
	}

	shape := &rshape{kind: rkFunc}
	if ft.TypeParams != nil {
		subst = maps.Clone(subst)
		if subst == nil {
			subst = map[string]*rshape{}
		}
		for _, f := range ft.TypeParams.List {
			for _, id := range f.Names {
				shape.tparams = append(shape.tparams, id.Name)
				subst[id.Name] = &rshape{kind: rkParam, name: id.Name}
			}
		}
	}

	collect := func(list *ast.FieldList) (out []*rshape) {
		if list == nil {
			return nil
		}
		for _, f := range list.List {
			s := t.typeShape(f.Type, subst, depth+1)
			for range max(len(f.Names), 1) {
				out = append(out, s)
			}
		}
		return out
	}
	shape.params, shape.results = collect(ft.Params), collect(ft.Results)

	if ft.Params != nil && len(ft.Params.List) > 0 {
		_, shape.variadic = ft.Params.List[len(ft.Params.List)-1].Type.(*ast.Ellipsis)
	}
	return shape
}

// member returns the shape of the field or the method name of a value of the
// shape s: the Ranges of a Range, a field of a struct (promoted ones included),
// a method of a declared type or of an interface. It is nil when it is not known.
func (t *ctxRangeTracker) member(s *rshape, name string, depth int) *rshape {
	if s == nil || depth > rMaxDepth {
		return nil
	}

	switch s.kind {
	case rkRange:
		switch name {
		case ctxFieldRanges:
			return rSliceOf(rSet)
		case "Type":
			return rNone
		}
	case rkSet:
		if name == "Start" || name == "End" {
			return rNone
		}
	case rkStruct:
		if field := t.fieldOf(s, name, depth); field != nil {
			return field
		}
	case rkIface:
		if method := t.interfaceMethod(s, name, depth); method != nil {
			return method
		}
	case rkNone, rkSlice, rkMap, rkChan, rkFunc, rkParam:
	}

	if s.name != "" {
		if method := t.methodOf(s, name, depth); method != nil {
			return method
		}
	}
	return nil
}

// fieldOf returns the shape of the field name of a struct, looking through the
// structs it embeds.
func (t *ctxRangeTracker) fieldOf(s *rshape, name string, depth int) *rshape {
	if s.fields == nil {
		return nil
	}

	for _, f := range s.fields.List {
		if len(f.Names) == 0 && ctxEmbeddedName(f.Type) == name {
			return t.typeShape(f.Type, s.subst, depth+1)
		}
		for _, id := range f.Names {
			if id.Name == name {
				return t.typeShape(f.Type, s.subst, depth+1)
			}
		}
	}
	for _, f := range s.fields.List {
		if len(f.Names) > 0 {
			continue
		}
		if promoted := t.member(t.typeShape(f.Type, s.subst, depth+1), name, depth+1); promoted != nil {
			return promoted
		}
	}
	return nil
}

// interfaceMethod returns the shape of a method of an interface.
func (t *ctxRangeTracker) interfaceMethod(s *rshape, name string, depth int) *rshape {
	if s.fields == nil {
		return nil
	}
	for _, f := range s.fields.List {
		ft, isFunc := f.Type.(*ast.FuncType)
		if isFunc && slices.ContainsFunc(f.Names, func(id *ast.Ident) bool { return id.Name == name }) {
			return t.funcShape(ft, s.subst, depth+1)
		}
	}
	return nil
}

// methodOf returns the shape of the method name of a declared type, a function
// whose type parameters are the ones of the receiver, bound to the type arguments.
func (t *ctxRangeTracker) methodOf(s *rshape, name string, depth int) *rshape {
	decl := t.env.methods[s.name+"."+name]
	if decl == nil {
		return nil
	}

	subst := map[string]*rshape{}
	var names []ast.Expr
	switch recv := ast.Unparen(ctxFirstFieldType(decl.Recv)).(type) {
	case *ast.StarExpr:
		switch inner := ast.Unparen(recv.X).(type) {
		case *ast.IndexExpr:
			names = []ast.Expr{inner.Index}
		case *ast.IndexListExpr:
			names = inner.Indices
		}
	case *ast.IndexExpr:
		names = []ast.Expr{recv.Index}
	case *ast.IndexListExpr:
		names = recv.Indices
	}
	for i, n := range names {
		if id, ok := n.(*ast.Ident); ok && id.Name != "_" {
			if i < len(s.args) && s.args[i] != nil {
				subst[id.Name] = s.args[i]
			} else {
				subst[id.Name] = &rshape{kind: rkParam, name: id.Name}
			}
		}
	}
	return t.funcShape(decl.Type, subst, depth+1)
}

// shapeOf returns the shape of the expression e, whose ancestors are stack.
func (t *ctxRangeTracker) shapeOf(e ast.Expr, stack []ast.Node) *rshape {
	return t.shapeAt(e, stack, 0)
}

func (t *ctxRangeTracker) shapeAt(e ast.Expr, stack []ast.Node, depth int) *rshape {
	if e == nil || depth > rMaxDepth {
		return nil
	}
	if s, known := t.memo[e]; known {
		return s
	}
	if t.busy[e] {
		return nil
	}

	t.busy[e] = true
	s := t.computeShape(e, stack, depth)
	delete(t.busy, e)
	if s != nil || depth == 0 {
		t.memo[e] = s
	}
	return s
}

func (t *ctxRangeTracker) computeShape(e ast.Expr, stack []ast.Node, depth int) *rshape {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return t.shapeAt(x.X, stack, depth+1)
	case *ast.StarExpr:
		return t.shapeAt(x.X, stack, depth+1)
	case *ast.BasicLit, *ast.BinaryExpr:
		return rNone
	case *ast.FuncLit:
		return t.funcShape(x.Type, nil, depth+1)
	case *ast.CompositeLit:
		return t.litShape(x, stack, depth)
	case *ast.UnaryExpr:
		switch x.Op {
		case token.AND:
			return t.shapeAt(x.X, stack, depth+1)
		case token.ARROW:
			if ch := t.shapeAt(x.X, stack, depth+1); ch != nil && ch.kind == rkChan {
				return ch.elem
			}
			return nil
		}
		return rNone
	case *ast.Ident:
		return t.identShape(x, stack, depth)
	case *ast.SelectorExpr:
		return t.selectorShape(x, stack, depth)
	case *ast.IndexExpr:
		return t.indexShape(x, stack, depth)
	case *ast.SliceExpr:
		return t.shapeAt(x.X, stack, depth+1)
	case *ast.TypeAssertExpr:
		return t.typeShape(x.Type, nil, depth+1)
	case *ast.CallExpr:
		if results := t.callResults(x, stack, depth); len(results) > 0 {
			return results[0]
		}
	}
	return nil
}

// identShape returns the shape of the variable, parameter or function an
// identifier names.
func (t *ctxRangeTracker) identShape(id *ast.Ident, stack []ast.Node, depth int) *rshape {
	switch id.Name {
	case "nil", "true", "false", "iota":
		return rNone
	case "_":
		return nil
	}

	if site, ok := ctxResolveSite(id.Name, id.Pos(), stack); ok {
		return t.siteShape(site, depth)
	}
	if pv, ok := t.env.pkgVars[id.Name]; ok {
		return t.valueSpecShape(pv.spec, pv.index, depth)
	}
	if decl := t.env.funcDecl[id.Name]; decl != nil {
		return t.funcShape(decl.Type, nil, depth+1)
	}
	return nil
}

// valueSpecShape returns the shape of the index-th name of a var declaration.
func (t *ctxRangeTracker) valueSpecShape(spec *ast.ValueSpec, index, depth int) *rshape {
	if spec.Type != nil {
		return t.typeShape(spec.Type, nil, depth+1)
	}
	switch {
	case len(spec.Values) == len(spec.Names):
		return t.shapeAt(spec.Values[index], nil, depth+1)
	case len(spec.Values) == 1:
		return t.multiShape(spec.Values[0], index, nil, depth)
	}
	return nil
}

// siteShape returns the shape of the variable a declaration makes.
func (t *ctxRangeTracker) siteShape(site ctxSite, depth int) *rshape {
	switch {
	case site.typ != nil:
		return t.typeShape(site.typ, nil, depth+1)
	case site.over != nil:
		return t.rangeVariable(t.shapeAt(site.over, site.stack, depth+1), site.key)
	case site.init != nil && site.index < 0:
		return t.shapeAt(site.init, site.stack, depth+1)
	case site.init != nil:
		return t.multiShape(site.init, site.index, site.stack, depth)
	}
	return nil
}

// rangeVariable returns the shape of the key or of the value variable of a range
// statement over a value of the shape over.
func (t *ctxRangeTracker) rangeVariable(over *rshape, key bool) *rshape {
	if over == nil {
		return nil
	}

	switch over.kind {
	case rkSlice:
		if key {
			return rNone
		}
		return over.elem
	case rkMap:
		if key {
			return over.key
		}
		return over.elem
	case rkChan:
		return over.elem
	case rkFunc:
		// an iterator: the function it calls takes the values
		if len(over.params) == 1 && over.params[0] != nil && over.params[0].kind == rkFunc {
			yield := over.params[0].params
			switch {
			case key && len(yield) > 0:
				return yield[0]
			case !key && len(yield) > 1:
				return yield[1]
			}
		}
		return nil
	case rkNone:
		return rNone // a string, or a number to count to
	case rkRange, rkSet, rkStruct, rkIface, rkParam:
	}
	return nil
}

// multiShape returns the shape of the index-th value of an expression that gives
// several: the results of a call, or the value and the boolean of an index into a
// map, a type assertion or a receive.
func (t *ctxRangeTracker) multiShape(init ast.Expr, index int, stack []ast.Node, depth int) *rshape {
	if call, ok := ast.Unparen(init).(*ast.CallExpr); ok {
		if results := t.callResults(call, stack, depth+1); index < len(results) {
			return results[index]
		}
		return nil
	}
	if index == 0 {
		return t.shapeAt(init, stack, depth+1)
	}
	return rNone
}

// selectorShape returns the shape of x.name: a member of a value, or a name of a
// package.
func (t *ctxRangeTracker) selectorShape(x *ast.SelectorExpr, stack []ast.Node, depth int) *rshape {
	if t.isPackageName(x.X, stack) {
		switch id, _ := x.X.(*ast.Ident); {
		case slices.Contains(t.env.pkgNames, id.Name) || t.harmless[id.Name]:
			return rNone
		}
		return nil
	}

	if x.Sel.Name == ctxRange {
		if _, isCall := ctxParentCall(x, stack); !isCall {
			if kind, _ := t.env.receiverKindVia(x, stack, ""); kind == ctxYes {
				// c.Range as a value
				return rFuncOf([]*rshape{rNone}, []*rshape{rRange, rNone})
			}
		}
	}

	if member := t.member(t.shapeAt(x.X, stack, depth+1), x.Sel.Name, depth+1); member != nil {
		return member
	}
	if guess := t.env.uniqueFieldType(x.Sel.Name); guess != nil {
		return t.typeShape(guess, nil, depth+1)
	}
	return nil
}

// isPackageName reports whether e names an imported package of the file, and not
// a variable that hides it.
func (t *ctxRangeTracker) isPackageName(e ast.Expr, stack []ast.Node) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	if _, isImport := t.imports[id.Name]; !isImport && !slices.Contains(t.env.pkgNames, id.Name) {
		return false
	}
	_, shadowed := ctxResolveSite(id.Name, id.Pos(), stack)
	return !shadowed
}

// indexShape returns the shape of x[i]: an element, a value of a map, or a
// generic function that is instantiated.
func (t *ctxRangeTracker) indexShape(x *ast.IndexExpr, stack []ast.Node, depth int) *rshape {
	base := t.shapeAt(x.X, stack, depth+1)
	if base == nil {
		return nil
	}

	switch base.kind {
	case rkSlice, rkMap:
		return base.elem
	case rkNone:
		return rNone
	case rkFunc:
		if len(base.tparams) > 0 {
			bind := map[string]*rshape{}
			if len(base.tparams) > 0 {
				bind[base.tparams[0]] = t.typeShape(x.Index, nil, depth+1)
			}
			return base.with(bind)
		}
	case rkRange, rkSet, rkChan, rkStruct, rkIface, rkParam:
	}
	return nil
}

// litShape returns the shape of a composite literal. The type of a literal inside
// another one may be left out.
func (t *ctxRangeTracker) litShape(lit *ast.CompositeLit, stack []ast.Node, depth int) *rshape {
	if lit.Type != nil {
		return t.typeShape(lit.Type, nil, depth+1)
	}
	if len(stack) == 0 {
		return nil
	}

	parent, outer := stack[len(stack)-1], stack[:len(stack)-1]
	if kv, ok := parent.(*ast.KeyValueExpr); ok && len(outer) > 0 {
		container, isLit := outer[len(outer)-1].(*ast.CompositeLit)
		if !isLit {
			return nil
		}
		shape := t.litShape(container, outer[:len(outer)-1], depth+1)
		if shape != nil && shape.kind == rkMap {
			if kv.Key == ast.Expr(lit) {
				return shape.key
			}
			return shape.elem
		}
		return nil
	}

	container, isLit := parent.(*ast.CompositeLit)
	if !isLit {
		return nil
	}
	if shape := t.litShape(container, outer, depth+1); shape != nil && (shape.kind == rkSlice || shape.kind == rkMap) {
		return shape.elem
	}
	return nil
}

// callResults returns the shapes of the results of a call, nil when they are not
// known.
func (t *ctxRangeTracker) callResults(call *ast.CallExpr, stack []ast.Node, depth int) []*rshape {
	if depth > rMaxDepth {
		return nil
	}

	fun := ast.Unparen(call.Fun)

	if id, ok := fun.(*ast.Ident); ok {
		if results, isBuiltin := t.builtinResults(id, call, stack, depth); isBuiltin {
			return results
		}
	}
	if t.isTypeExpr(fun, stack) {
		return []*rshape{t.typeShape(fun, nil, depth+1)}
	}

	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if t.isRangeCall(call, stack) {
			return []*rshape{rRange, rNone}
		}
		if t.isPackageName(sel.X, stack) {
			return t.packageResults(sel, call, stack, depth)
		}
	}

	var explicit []*rshape
	switch f := fun.(type) {
	case *ast.IndexExpr:
		explicit, fun = []*rshape{t.typeShape(f.Index, nil, depth+1)}, ast.Unparen(f.X)
	case *ast.IndexListExpr:
		for _, index := range f.Indices {
			explicit = append(explicit, t.typeShape(index, nil, depth+1))
		}
		fun = ast.Unparen(f.X)
	}

	fn := t.shapeAt(fun, stack, depth+1)
	if fn == nil || fn.kind != rkFunc {
		return nil
	}
	return t.applyCall(fn, call, stack, explicit, depth)
}

// applyCall returns the results of calling a function of shape fn, with the type
// parameters it is generic in bound by the type arguments given or inferred from
// the arguments.
func (t *ctxRangeTracker) applyCall(fn *rshape, call *ast.CallExpr, stack []ast.Node, explicit []*rshape, depth int) []*rshape {
	if len(fn.tparams) == 0 {
		return fn.results
	}

	bind := map[string]*rshape{}
	for i, name := range fn.tparams {
		if i < len(explicit) && explicit[i] != nil {
			bind[name] = explicit[i]
		}
	}
	for i, arg := range call.Args {
		var param *rshape
		switch {
		case fn.variadic && i >= len(fn.params)-1 && len(fn.params) > 0:
			param = fn.params[len(fn.params)-1]
			if !call.Ellipsis.IsValid() && param != nil {
				param = param.elem
			}
		case i < len(fn.params):
			param = fn.params[i]
		}
		unify(param, t.shapeAt(arg, stack, depth+1), bind, 0)
	}
	return withAll(fn.results, bind, 0)
}

// builtinResults returns the results of a call of a builtin function; isBuiltin
// is false when the identifier names something else.
func (t *ctxRangeTracker) builtinResults(id *ast.Ident, call *ast.CallExpr, stack []ast.Node, depth int) (results []*rshape, isBuiltin bool) {
	if _, builtin := types.Universe.Lookup(id.Name).(*types.Builtin); !builtin {
		return nil, false
	}
	if _, local := ctxResolveSite(id.Name, id.Pos(), stack); local ||
		t.env.funcDecl[id.Name] != nil || t.env.pkgVars[id.Name].spec != nil || t.env.typeDecl[id.Name] != nil {
		return nil, false
	}

	switch id.Name {
	case "append", "min", "max":
		if len(call.Args) > 0 {
			return []*rshape{t.shapeAt(call.Args[0], stack, depth+1)}, true
		}
	case "make", "new":
		if len(call.Args) > 0 {
			return []*rshape{t.typeShape(call.Args[0], nil, depth+1)}, true
		}
	case "len", "cap", "copy", "real", "imag", "complex", "recover":
		return []*rshape{rNone}, true
	}
	return nil, true
}

// isTypeExpr reports whether e is a type, which makes a call of it a conversion.
func (t *ctxRangeTracker) isTypeExpr(e ast.Expr, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.StructType, *ast.InterfaceType:
		return true
	case *ast.StarExpr:
		return t.isTypeExpr(ast.Unparen(x.X), stack)
	case *ast.Ident:
		if _, local := ctxResolveSite(x.Name, x.Pos(), stack); local {
			return false
		}
		return t.env.typeDecl[x.Name] != nil || (ctxIsPredeclaredType(x.Name) && t.env.funcDecl[x.Name] == nil) ||
			(t.env.dotImported() && (x.Name == ctxTypeRange || x.Name == ctxTypeRangeSet))
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && slices.Contains(t.env.pkgNames, id.Name) && (x.Sel.Name == ctxTypeRange || x.Sel.Name == ctxTypeRangeSet)
	case *ast.IndexExpr:
		return t.isGenericType(x.X)
	case *ast.IndexListExpr:
		return t.isGenericType(x.X)
	}
	return false
}

// isGenericType reports whether e names a generic type of the package.
func (t *ctxRangeTracker) isGenericType(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && t.env.typeDecl[id.Name] != nil && t.env.typeDecl[id.Name].TypeParams != nil
}

// packageResults returns the results of a call of a function of an imported
// package. Those of the packages that know nothing of ranges hold none, except for
// the generic functions of the standard library that hand a value on.
func (t *ctxRangeTracker) packageResults(sel *ast.SelectorExpr, call *ast.CallExpr, stack []ast.Node, depth int) []*rshape {
	id, _ := sel.X.(*ast.Ident)
	if slices.Contains(t.env.pkgNames, id.Name) {
		// fiber.Range(x) is a conversion, any other function of the package returns no range
		return rNoneResults
	}
	if !t.harmless[id.Name] {
		return nil
	}

	arg := func(i int) *rshape {
		if i < len(call.Args) {
			return t.shapeAt(call.Args[i], stack, depth+1)
		}
		return nil
	}
	elementOf := func(s *rshape) *rshape {
		if s != nil && s.kind == rkSlice {
			return s.elem
		}
		return nil
	}

	switch t.imports[id.Name] + "." + sel.Sel.Name {
	case "slices.Clone", "slices.Compact", "slices.CompactFunc", "slices.Concat", "slices.Delete", "slices.DeleteFunc",
		"slices.Insert", "slices.Replace", "slices.Grow", "slices.Clip", "maps.Clone", "cmp.Or":
		return []*rshape{arg(0)}
	case "slices.Max", "slices.Min", "slices.MaxFunc", "slices.MinFunc":
		return []*rshape{elementOf(arg(0))}
	case "slices.Values":
		return []*rshape{t.sequence([]*rshape{elementOf(arg(0))})}
	case "slices.All", "slices.Backward":
		return []*rshape{t.sequence([]*rshape{rNone, elementOf(arg(0))})}
	case "slices.Collect", "slices.Sorted":
		if seq := arg(0); seq != nil && seq.kind == rkFunc && len(seq.params) == 1 && seq.params[0] != nil && len(seq.params[0].params) == 1 {
			return []*rshape{rSliceOf(seq.params[0].params[0])}
		}
		return nil
	case "maps.Keys":
		if m := arg(0); m != nil && m.kind == rkMap {
			return []*rshape{t.sequence([]*rshape{m.key})}
		}
		return nil
	case "maps.Values":
		if m := arg(0); m != nil && m.kind == rkMap {
			return []*rshape{t.sequence([]*rshape{m.elem})}
		}
		return nil
	case "maps.All":
		if m := arg(0); m != nil && m.kind == rkMap {
			return []*rshape{t.sequence([]*rshape{m.key, m.elem})}
		}
		return nil
	}
	return rNoneResults
}
