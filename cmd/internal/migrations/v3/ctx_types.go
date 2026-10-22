package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
)

// The environment (ctx_env.go) works on type expressions: it asks what type the
// receiver of a call has, and the answer is the expression the code declares it
// with. This file holds what that needs beyond a name and a field: the types
// that generic declarations give their results and fields once their type
// parameters are bound, the methods of interfaces and of the structs a type
// embeds, the several results of a call, and what a range statement hands out.

// ctxIterPackage is the standard package of the iterator types iter.Seq and iter.Seq2.
const ctxIterPackage = "iter"

// ctxTypeParams returns the names of the type parameters a declaration lists, in
// the order they are declared.
func ctxTypeParams(fl *ast.FieldList) []string {
	if fl == nil {
		return nil
	}

	var names []string
	for _, f := range fl.List {
		for _, id := range f.Names {
			names = append(names, id.Name)
		}
	}
	return names
}

// ctxTypeArgs returns the type arguments of an instantiated type, kbox[int] or
// *pair[string, int]; nil when the type is not instantiated.
func ctxTypeArgs(t ast.Expr) []ast.Expr {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.IndexExpr:
			return []ast.Expr{x.Index}
		case *ast.IndexListExpr:
			return x.Indices
		default:
			return nil
		}
	}
}

// ctxBindParams pairs the type parameters names with the type arguments args.
func ctxBindParams(names []string, args []ast.Expr) map[string]ast.Expr {
	if len(names) == 0 || len(args) == 0 {
		return nil
	}

	bind := make(map[string]ast.Expr, len(names))
	for i, name := range names {
		if i < len(args) && args[i] != nil {
			bind[name] = args[i]
		}
	}
	return bind
}

// ctxResultTypes returns the type of every value the function type returns, a
// field that declares several names counting once for each.
func ctxResultTypes(ft *ast.FuncType) []ast.Expr {
	if ft == nil || ft.Results == nil {
		return nil
	}

	var out []ast.Expr
	for _, f := range ft.Results.List {
		for range max(1, len(f.Names)) {
			out = append(out, f.Type)
		}
	}
	return out
}

// ctxParamTypes is ctxResultTypes for the parameters; the type of a variadic
// parameter is the ...T it is written with.
func ctxParamTypes(ft *ast.FuncType) []ast.Expr {
	if ft == nil || ft.Params == nil {
		return nil
	}

	var out []ast.Expr
	for _, f := range ft.Params.List {
		for range max(1, len(f.Names)) {
			out = append(out, f.Type)
		}
	}
	return out
}

// ctxSubst returns t with the type parameters in bind replaced by what they are
// bound to. The tree of t is not changed: the parts that differ are new.
func ctxSubst(t ast.Expr, bind map[string]ast.Expr) ast.Expr {
	return ctxSubstAt(t, bind, 0)
}

func ctxSubstAt(t ast.Expr, bind map[string]ast.Expr, depth int) ast.Expr {
	if t == nil || len(bind) == 0 || depth > ctxMaxDepth {
		return t
	}
	next := func(e ast.Expr) ast.Expr { return ctxSubstAt(e, bind, depth+1) }

	switch x := t.(type) {
	case *ast.Ident:
		if bound, ok := bind[x.Name]; ok {
			return bound
		}
	case *ast.StarExpr:
		if inner := next(x.X); inner != x.X {
			return &ast.StarExpr{Star: x.Star, X: inner}
		}
	case *ast.ParenExpr:
		if inner := next(x.X); inner != x.X {
			return &ast.ParenExpr{Lparen: x.Lparen, X: inner, Rparen: x.Rparen}
		}
	case *ast.ArrayType:
		if elt := next(x.Elt); elt != x.Elt {
			return &ast.ArrayType{Lbrack: x.Lbrack, Len: x.Len, Elt: elt}
		}
	case *ast.Ellipsis:
		if elt := next(x.Elt); elt != x.Elt {
			return &ast.Ellipsis{Ellipsis: x.Ellipsis, Elt: elt}
		}
	case *ast.MapType:
		key, value := next(x.Key), next(x.Value)
		if key != x.Key || value != x.Value {
			return &ast.MapType{Map: x.Map, Key: key, Value: value}
		}
	case *ast.ChanType:
		if value := next(x.Value); value != x.Value {
			return &ast.ChanType{Begin: x.Begin, Arrow: x.Arrow, Dir: x.Dir, Value: value}
		}
	case *ast.IndexExpr:
		base, index := next(x.X), next(x.Index)
		if base != x.X || index != x.Index {
			return &ast.IndexExpr{X: base, Lbrack: x.Lbrack, Index: index, Rbrack: x.Rbrack}
		}
	case *ast.IndexListExpr:
		base := next(x.X)
		indices, changed := make([]ast.Expr, len(x.Indices)), base != x.X
		for i, index := range x.Indices {
			indices[i] = next(index)
			changed = changed || indices[i] != index
		}
		if changed {
			return &ast.IndexListExpr{X: base, Lbrack: x.Lbrack, Indices: indices, Rbrack: x.Rbrack}
		}
	case *ast.FuncType:
		params, results := ctxSubstFields(x.Params, bind, depth+1), ctxSubstFields(x.Results, bind, depth+1)
		if params != x.Params || results != x.Results {
			return &ast.FuncType{Func: x.Func, TypeParams: x.TypeParams, Params: params, Results: results}
		}
	}
	return t
}

// ctxSubstFields is ctxSubst for the fields of a parameter or result list; it
// returns fl itself when nothing changes.
func ctxSubstFields(fl *ast.FieldList, bind map[string]ast.Expr, depth int) *ast.FieldList {
	if fl == nil {
		return nil
	}

	fields, changed := make([]*ast.Field, len(fl.List)), false
	for i, f := range fl.List {
		fields[i] = f
		if typ := ctxSubstAt(f.Type, bind, depth); typ != f.Type {
			fields[i], changed = &ast.Field{Doc: f.Doc, Names: f.Names, Type: typ, Tag: f.Tag, Comment: f.Comment}, true
		}
	}
	if !changed {
		return fl
	}
	return &ast.FieldList{Opening: fl.Opening, List: fields, Closing: fl.Closing}
}

// unify binds the type parameters of tparams that param mentions, from the type
// arg of the value passed for it, the way the inference of a generic call does.
func (env *ctxEnv) unify(param, arg ast.Expr, tparams []string, bind map[string]ast.Expr, depth int) {
	if param == nil || arg == nil || depth > ctxMaxDepth {
		return
	}

	switch p := param.(type) {
	case *ast.Ident:
		if _, bound := bind[p.Name]; !bound && slices.Contains(tparams, p.Name) {
			bind[p.Name] = arg
		}
		return
	case *ast.ParenExpr:
		env.unify(p.X, arg, tparams, bind, depth+1)
		return
	case *ast.StarExpr:
		// the type of &x is the type of x (see typeOf), so the pointer may be missing
		if a, ok := arg.(*ast.StarExpr); ok {
			arg = a.X
		}
		env.unify(p.X, arg, tparams, bind, depth+1)
		return
	}

	// a value of a named type is matched by what the type is made of: a
	// spans ([]fiber.RangeSet) passed for a []T
	if _, same := arg.(*ast.Ident); same {
		if under := env.underlying(arg, depth+1); under != nil {
			arg = under
		}
	}

	switch p := param.(type) {
	case *ast.ArrayType:
		if a, ok := arg.(*ast.ArrayType); ok {
			env.unify(p.Elt, a.Elt, tparams, bind, depth+1)
		}
	case *ast.MapType:
		if a, ok := arg.(*ast.MapType); ok {
			env.unify(p.Key, a.Key, tparams, bind, depth+1)
			env.unify(p.Value, a.Value, tparams, bind, depth+1)
		}
	case *ast.ChanType:
		if a, ok := arg.(*ast.ChanType); ok {
			env.unify(p.Value, a.Value, tparams, bind, depth+1)
		}
	case *ast.FuncType:
		if a, ok := arg.(*ast.FuncType); ok {
			env.unifyAll(ctxParamTypes(p), ctxParamTypes(a), tparams, bind, depth+1)
			env.unifyAll(ctxResultTypes(p), ctxResultTypes(a), tparams, bind, depth+1)
		}
	case *ast.IndexExpr, *ast.IndexListExpr:
		if key := env.typeKey(param); key != "" && key == env.typeKey(arg) {
			env.unifyAll(ctxTypeArgs(param), ctxTypeArgs(arg), tparams, bind, depth+1)
		}
	}
}

func (env *ctxEnv) unifyAll(params, args []ast.Expr, tparams []string, bind map[string]ast.Expr, depth int) {
	for i := range min(len(params), len(args)) {
		env.unify(params[i], args[i], tparams, bind, depth)
	}
}

// underlying returns what the type declared by name is made of, one step: the
// type of its declaration with the type arguments of t put in. It is nil for a
// type that is not declared in the file and for one that is not a plain type
// (a struct or an interface is returned as it is declared).
func (env *ctxEnv) underlying(t ast.Expr, depth int) ast.Expr {
	for ; depth <= ctxMaxDepth; depth++ {
		switch x := t.(type) {
		case *ast.ParenExpr:
			t = x.X
			continue
		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
			spec := env.typeSpec(env.typeKey(x))
			if spec == nil {
				return nil
			}
			return ctxSubst(spec.Type, ctxBindParams(ctxTypeParams(spec.TypeParams), ctxTypeArgs(x)))
		}
		return nil
	}
	return nil
}

// elementType returns the type of what indexing a value of type t gives, or what a
// receive from it does: the element of an array, slice, map or channel, also when
// the value is of a type declared in the file as one of those.
func (env *ctxEnv) elementType(t ast.Expr) ast.Expr {
	for range ctxMaxDepth {
		if elem := ctxElementType(t); elem != nil {
			return elem
		}
		under := env.underlying(t, 0)
		if under == nil {
			return nil
		}
		t = under
	}
	return nil
}

// rangeElemType returns the type of the variable of a range statement over a value
// of type t: the key variable when key is set, the value variable otherwise.
func (env *ctxEnv) rangeElemType(t ast.Expr, key bool) ast.Expr {
	for range ctxMaxDepth {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.ParenExpr:
			t = x.X
			continue
		case *ast.ArrayType:
			if key {
				return nil
			}
			return x.Elt
		case *ast.MapType:
			if key {
				return x.Key
			}
			return x.Value
		case *ast.ChanType:
			// a channel has no value variable, the key variable takes what it carries
			if key {
				return x.Value
			}
			return nil
		case *ast.FuncType:
			return ctxYielded(x, key)
		case *ast.IndexExpr, *ast.IndexListExpr:
			if elem, ok := env.sequenceElem(x, key); ok {
				return elem
			}
		}

		under := env.underlying(t, 0)
		if under == nil {
			return nil
		}
		t = under
	}
	return nil
}

// ctxYielded returns the type of the first (key) or second (value) value that the
// iterator function type ft, func(yield func(K, V) bool), hands to its loop.
func ctxYielded(ft *ast.FuncType, key bool) ast.Expr {
	params := ctxParamTypes(ft)
	if len(params) != 1 {
		return nil
	}
	yield, ok := ast.Unparen(params[0]).(*ast.FuncType)
	if !ok {
		return nil
	}

	values := ctxParamTypes(yield)
	switch {
	case key && len(values) > 0:
		return values[0]
	case !key && len(values) > 1:
		return values[1]
	}
	return nil
}

// sequenceElem is rangeElemType for iter.Seq[V] and iter.Seq2[K, V].
func (env *ctxEnv) sequenceElem(t ast.Expr, key bool) (ast.Expr, bool) {
	var base ast.Expr
	var args []ast.Expr
	switch x := t.(type) {
	case *ast.IndexExpr:
		base, args = x.X, []ast.Expr{x.Index}
	case *ast.IndexListExpr:
		base, args = x.X, x.Indices
	}

	sel, ok := base.(*ast.SelectorExpr)
	if !ok || !env.isIterPackage(sel.X) {
		return nil, false
	}
	switch {
	case sel.Sel.Name == "Seq" && len(args) == 1:
		return args[0], key
	case sel.Sel.Name == "Seq2" && len(args) == 2:
		if key {
			return args[0], true
		}
		return args[1], true
	}
	return nil, false
}

// isIterPackage reports whether e names the standard package iter in the file.
func (env *ctxEnv) isIterPackage(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}

	for _, spec := range env.g.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != ctxIterPackage {
			continue
		}
		name := ctxIterPackage
		if spec.Name != nil {
			name = spec.Name.Name
		}
		return id.Name == name
	}
	return false
}

// methodFuncType returns the type of the method name of a value of type recv, with
// the binding of the type parameters of the declaration the method is written in:
// a method of a type declared in the file, of an interface, or one that the
// structs and interfaces embedded in the type promote.
func (env *ctxEnv) methodFuncType(recv ast.Expr, name string, depth int) (*ast.FuncType, map[string]ast.Expr) {
	typeName := env.typeKey(recv)
	if typeName == "" || depth > ctxMaxDepth {
		return nil, nil
	}
	args := ctxTypeArgs(recv)

	if method := env.methodDecl(typeName, name); method != nil {
		return method.Type, ctxBindParams(ctxTypeNames(ctxTypeArgs(ctxFirstFieldType(method.Recv))), args)
	}
	spec := env.typeSpec(typeName)
	if spec == nil {
		return nil, nil
	}
	bind := ctxBindParams(ctxTypeParams(spec.TypeParams), args)

	var fields *ast.FieldList
	switch x := spec.Type.(type) {
	case *ast.InterfaceType:
		fields = x.Methods
	case *ast.StructType:
		fields = x.Fields
	}
	if fields == nil {
		return nil, nil
	}

	for _, f := range fields.List {
		if ft, ok := f.Type.(*ast.FuncType); ok && slices.ContainsFunc(f.Names, func(id *ast.Ident) bool { return id.Name == name }) {
			return ft, bind
		}
	}
	for _, f := range fields.List {
		if len(f.Names) > 0 {
			continue
		}
		// what an embedded type declares is promoted; the type it is written with may
		// mention the type parameters of this one
		if ft, inner := env.methodFuncType(ctxSubst(f.Type, bind), name, depth+1); ft != nil {
			return ft, inner
		}
	}
	return nil, nil
}

// ctxTypeNames returns the names of the identifiers in list; an expression that is
// no identifier gives an empty name.
func ctxTypeNames(list []ast.Expr) []string {
	names := make([]string, len(list))
	for i, e := range list {
		if id, ok := e.(*ast.Ident); ok {
			names[i] = id.Name
		}
	}
	return names
}

// funcResults returns the types of the values a call of the generic or plain
// function declared by fn returns. explicit are the type arguments written in
// the call, f[int](x); the others are inferred from the arguments.
func (env *ctxEnv) funcResults(fn *ast.FuncType, call *ast.CallExpr, explicit []ast.Expr, stack []ast.Node, depth int) []ast.Expr {
	results := ctxResultTypes(fn)
	tparams := ctxTypeParams(fn.TypeParams)
	if len(tparams) == 0 || len(results) == 0 {
		return results
	}

	bind := ctxBindParams(tparams, explicit)
	if bind == nil {
		bind = map[string]ast.Expr{}
	}
	params := ctxParamTypes(fn)
	for i, arg := range call.Args {
		if len(params) == 0 {
			break
		}
		param := params[min(i, len(params)-1)]
		if ell, variadic := param.(*ast.Ellipsis); variadic {
			// f(xs...) passes the slice itself for the ...T
			param = ell.Elt
			if call.Ellipsis.IsValid() {
				param = &ast.ArrayType{Elt: ell.Elt}
			}
		} else if i >= len(params) {
			break
		}
		env.unify(param, env.typeOfAt(arg, stack, depth+1), tparams, bind, 0)
	}

	out := make([]ast.Expr, len(results))
	for i, r := range results {
		out[i] = ctxSubst(r, bind)
	}
	return out
}

// resultTypeAt returns the type of the index-th value that the expression e
// yields: a result of a call, or the value of a comma-ok form (v, ok := m[k]).
func (env *ctxEnv) resultTypeAt(e ast.Expr, index int, stack []ast.Node, depth int) ast.Expr {
	if depth > ctxMaxDepth {
		return nil
	}

	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if results := env.callResults(x, stack, depth); index < len(results) {
			return results[index]
		}
	case *ast.TypeAssertExpr:
		if index == 0 {
			return x.Type
		}
	case *ast.IndexExpr:
		if index == 0 {
			return env.elementType(env.typeOfAt(x.X, stack, depth+1))
		}
	case *ast.UnaryExpr:
		if index == 0 && x.Op == token.ARROW {
			return env.elementType(env.typeOfAt(x.X, stack, depth+1))
		}
	}
	return nil
}
