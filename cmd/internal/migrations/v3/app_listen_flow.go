package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
)

// A fiber.Config does not always go straight from the code that fills it in to the
// fiber.New call that makes the app: it is handed to a function as a parameter,
// returned by one, kept in the field of a struct or in a variable of the package, or
// changed by the functions of a functional option. The settings that v3 moved to
// fiber.ListenConfig travel with it, so the config and the call that starts the app
// have to be changed together, wherever they are.
//
// The flow follows the config through the places that hold it, which are the
// variables and parameters, the results of functions, and the fields of structs.
// Every place the config can come from or go to joins the same set: if one cannot
// be followed, none is changed. When all can, they hold the config of v2 instead,
// fibermigrate.Config of the registry (see app_listen_returned.go), the literals
// that make one wrap their settings in it, and the fiber.New calls that make apps
// from it are the registry's New. The code that reads and assigns the settings is
// not touched: the registry's Config has the fields.
//
//	func New(cfg fibermigrate.Config) *fiber.App { return fibermigrate.New(cfg) }
//
// A function that takes the config to change it (type Option func(*fiber.Config))
// is held by places too, and every function of the type takes the registry's
// Config then: the places of such a type are one more kind of thing the set follows
// (see app_listen_flow_func.go).
//
// The places are found by name and by the types the declarations name, as
// everywhere in the app migrations, so a place that cannot be told apart from the
// ones that hold the config fails the set rather than being skipped.

// appFlowKind says what kind of place holds a value of the set.
type appFlowKind uint8

const (
	flowVar    appFlowKind = iota + 1 // a variable or a parameter
	flowResult                        // a result of a function declaration
	flowField                         // a field of a struct type
	flowType                          // a named function type, with all its places
)

// appFlowClass says what the places of the set hold.
type appFlowClass uint8

const (
	flowConfig appFlowClass = iota // a fiber.Config, or a pointer to one
	flowFunc                       // a function that takes one
	flowFuncs                      // a slice of such functions, such as the options of a variadic parameter
)

// what names the value in a reason.
func (c appFlowClass) what() string {
	switch c {
	case flowFunc:
		return "a function that takes the config"
	case flowFuncs:
		return "functions that take the config"
	default:
		return "the config"
	}
}

// appFlowKey identifies a place that holds a value of the set.
type appFlowKey struct {
	kind  appFlowKind
	class appFlowClass
	// node is the declaration of the variable (see appBinding.node), of the function
	// or of the struct type.
	node ast.Node
	name string
	idx  int
}

// appFlowLimit bounds how many places one set may have: a set that large is not
// the config of an app.
const appFlowLimit = 2000

// appFlow is the set of places that hold one config.
type appFlow struct {
	pl   *appPlanner
	seed *appFile
	// why says why the config cannot be followed, empty while it can.
	why   string
	keys  map[appFlowKey]bool
	queue []func()

	// types are the type expressions that name the config in the declarations of
	// its places.
	types map[ast.Expr]*appFile
	// lits are the literals that make a config of the set.
	lits map[*ast.CompositeLit]*appFile
	// news are the fiber.New calls that make an app from it.
	news    []appNew
	seenNew map[*ast.CallExpr]bool
	// sels are the reads and assignments of the settings v3 moved.
	sels map[*ast.SelectorExpr]*appFile
	// funcs are the function literals and declarations that take the config.
	funcs map[ast.Node]bool
	// changed are the declarations whose type changes, for the note.
	changed []appFlowChange
}

// appFlowChange is a declaration of the project that holds the config.
type appFlowChange struct {
	// label names it: config.Fiber, server.New, config.Config.fiber.
	label string
	// how says what it does with the config: "return", "take" or "hold".
	how string
	// exposed is set when other modules can use it.
	exposed bool
}

func (pl *appPlanner) newFlow(seed *appFile) *appFlow {
	return &appFlow{
		pl:      pl,
		seed:    seed,
		keys:    map[appFlowKey]bool{},
		types:   map[ast.Expr]*appFile{},
		lits:    map[*ast.CompositeLit]*appFile{},
		seenNew: map[*ast.CallExpr]bool{},
		sels:    map[*ast.SelectorExpr]*appFile{},
		funcs:   map[ast.Node]bool{},
	}
}

// failf records why the config cannot be followed, for the first time it cannot.
func (fl *appFlow) failf(f *appFile, n ast.Node, format string, args ...any) {
	if fl.why != "" {
		return
	}
	fl.why = fmt.Sprintf(format, args...)
	if f != nil && n != nil {
		fl.why += fmt.Sprintf(", at line %d of %s", f.g.line(n.Pos()), appFileLabel(f))
	}
}

// run follows the config until every place it reaches is known, or one cannot be
// followed.
func (fl *appFlow) run() {
	for steps := 0; len(fl.queue) > 0 && fl.why == ""; steps++ {
		if steps > appFlowLimit {
			fl.failf(nil, nil, "the config reaches more places than the migration follows")
			return
		}
		next := fl.queue[0]
		fl.queue = fl.queue[1:]
		next()
	}
}

// claim reports whether the place is new to the set.
func (fl *appFlow) claim(key appFlowKey) bool {
	if fl.why != "" || fl.keys[key] {
		return false
	}
	fl.keys[key] = true
	return true
}

// claimExpr adds the place the value of e comes from to the set: the variable, the
// field or the function result it reads, or the literal that makes it. With usage,
// the use the value is put to is followed too, which the places do for themselves.
func (fl *appFlow) claimExpr(f *appFile, e ast.Expr, class appFlowClass, usage bool) {
	if fl.why != "" {
		return
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		fl.claimExpr(f, x.X, class, usage)
		return
	case *ast.StarExpr:
		if class == flowConfig {
			fl.claimExpr(f, x.X, class, usage)
			return
		}
	case *ast.UnaryExpr:
		if class == flowConfig && x.Op == token.AND {
			fl.claimExpr(f, x.X, class, usage)
			return
		}
	case *ast.CompositeLit:
		switch at, isSlice := x.Type.(*ast.ArrayType); {
		case class == flowConfig && x.Type != nil && f.fiberTypeName(x.Type) == appNameConfig:
			fl.lits[x] = f
			if usage {
				fl.use(f, x, class)
			}
			return
		case class == flowFuncs && isSlice && at.Len == nil:
			if fl.retype(f, at, class) {
				fl.sliceLiteral(f, x)
			}
			return
		case class == flowConfig && x.Type == nil:
			fl.failf(f, e, "the config is %s", f.useOf(x)) // an element of a slice or map
			return
		}
	case *ast.FuncLit:
		if class == flowFunc {
			fl.funcLit(f, x)
			if usage {
				fl.use(f, x, class)
			}
			return
		}
	case *ast.Ident:
		if x.Name == "nil" {
			if _, bound := f.binding(x); !bound {
				return
			}
		}
		if b, ok := f.binding(x); ok {
			fl.claimVar(b, class)
			return
		}
		if fn := f.declaredFunc(x); fn != nil && class == flowFunc {
			fl.funcDecl(fn.file, fn.decl)
			if usage {
				fl.use(f, x, class)
			}
			return
		}
		fl.failf(f, e, "%s cannot be traced to its declaration", x.Name)
		return
	case *ast.SelectorExpr:
		fl.claimSelector(f, x, class, usage)
		return
	case *ast.CallExpr:
		fl.claimCall(f, x, 0, class)
		return
	}
	fl.failf(f, e, "%s comes from %s, which the migration does not follow", class.what(), f.g.text(e))
}

// declaredFunc returns the function declaration the identifier names, when it is
// not hidden by a variable.
func (f *appFile) declaredFunc(id *ast.Ident) *appFunc {
	if _, bound := f.binding(id); bound {
		return nil
	}
	if pkg := f.ix.pkgOf(f); pkg != nil {
		return pkg.funcs[id.Name]
	}
	return nil
}

// claimSelector adds the field, package variable or function a.b names.
func (fl *appFlow) claimSelector(f *appFile, sel *ast.SelectorExpr, class appFlowClass, usage bool) {
	if b, _, ok := f.packageVar(sel); ok {
		fl.claimVar(b, class)
		return
	}
	if id, isIdent := sel.X.(*ast.Ident); isIdent && f.isImportName(id) {
		if pkg := f.packageNamed(id.Name); pkg != nil && class == flowFunc {
			if fn := pkg.funcs[sel.Sel.Name]; fn != nil {
				fl.funcDecl(fn.file, fn.decl)
				if usage {
					fl.use(f, sel, class)
				}
				return
			}
		}
		fl.failf(f, sel, "%s is not a variable of the project", f.g.text(sel))
		return
	}
	holder := f.fieldHolder(sel.X, sel.Sel.Name)
	if holder == nil {
		fl.failf(f, sel, "the struct that has the field %s could not be traced", f.g.text(sel))
		return
	}
	fl.claimField(holder, sel.Sel.Name, class)
}

// claimCall adds the idx-th result of the function the call invokes.
func (fl *appFlow) claimCall(f *appFile, call *ast.CallExpr, idx int, class appFlowClass) {
	if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "new" && len(call.Args) == 1 && class == flowConfig {
		if _, bound := f.binding(id); !bound {
			fl.retype(f, call.Args[0], class)
			return
		}
	}
	if _, _, isAppConfig := f.appConfigCall(call); isAppConfig {
		fl.failf(f, call, "the config of an app is handed on (%s)", f.g.text(call))
		return
	}
	if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "append" && class == flowFuncs && len(call.Args) > 0 {
		if _, bound := f.binding(id); !bound {
			fl.claimExpr(f, call.Args[0], flowFuncs, false)
			for i, arg := range call.Args[1:] {
				if call.Ellipsis.IsValid() && i == len(call.Args)-2 {
					fl.claimExpr(f, arg, flowFuncs, false)
				} else {
					fl.claimExpr(f, arg, flowFunc, false)
				}
			}
			return
		}
	}
	if class == flowFunc && len(call.Args) == 1 {
		// a conversion to the named type of the functions
		if td := f.resolveNamedType(call.Fun); td != nil {
			fl.claimFamily(td)
			fl.claimExpr(f, call.Args[0], class, false)
			return
		}
	}

	callee, found := f.calleeOf(call, 0)
	switch {
	case !found:
		fl.failf(f, call, "%s comes from %s, which the migration cannot trace", class.what(), f.g.text(call.Fun))
	case callee.isInterfaceMethod():
		fl.failf(f, call, "%s comes from %s, a method of an interface", class.what(), f.g.text(call.Fun))
	case callee.decl == nil:
		fl.failf(f, call, "%s comes from a function literal", class.what())
	default:
		fl.claimResult(callee.file, callee.decl, idx, class)
	}
}

// claimVar adds a variable or parameter.
func (fl *appFlow) claimVar(b appBinding, class appFlowClass) {
	if b.node == nil {
		fl.failf(b.file, nil, "%s has a declaration the migration does not follow", b.name)
		return
	}
	if fl.claim(appFlowKey{kind: flowVar, class: class, node: b.node, name: b.name}) {
		fl.queue = append(fl.queue, func() { fl.scanVar(b, class) })
	}
}

// claimResult adds the idx-th result of a function.
func (fl *appFlow) claimResult(f *appFile, fn *ast.FuncDecl, idx int, class appFlowClass) {
	if fl.claim(appFlowKey{kind: flowResult, class: class, node: fn, idx: idx}) {
		fl.queue = append(fl.queue, func() { fl.scanResult(f, fn, idx, class) })
	}
}

// claimField adds a field of a struct type.
func (fl *appFlow) claimField(td *appTypeDecl, name string, class appFlowClass) {
	if fl.claim(appFlowKey{kind: flowField, class: class, node: td.spec, name: name}) {
		fl.queue = append(fl.queue, func() { fl.scanField(td, name, class) })
	}
}

// retype records the type expression t, which a place is declared with, as one to
// change. It fails when t is not the type the class says.
func (fl *appFlow) retype(f *appFile, t ast.Expr, class appFlowClass) bool {
	switch class {
	case flowFunc:
		return fl.retypeFunc(f, t)
	case flowFuncs:
		switch x := t.(type) {
		case *ast.Ellipsis:
			return fl.retypeFunc(f, x.Elt)
		case *ast.ArrayType:
			if x.Len == nil {
				return fl.retypeFunc(f, x.Elt)
			}
		}
		fl.failf(f, t, "the functions that take the config are held by a value of type %s", f.g.text(t))
		return false
	}

	if _, variadic := t.(*ast.Ellipsis); variadic {
		fl.failf(f, t, "the config is held by a variadic parameter")
		return false
	}
	node := appStripType(t)
	if f.fiberTypeName(node) != appNameConfig {
		fl.failf(f, t, "the config is held by a value of type %s", f.g.text(t))
		return false
	}
	if td := f.resolveNamedType(node); td != nil && td.spec.Assign.IsValid() {
		if _, isPointer := ast.Unparen(td.spec.Type).(*ast.StarExpr); isPointer {
			fl.failf(f, t, "%s is an alias of a pointer type", f.g.text(t))
			return false
		}
	}
	fl.types[node] = f
	return true
}

// occurrence follows one use of a variable: a value it is given, or a place its
// value goes to.
func (fl *appFlow) occurrence(f *appFile, e ast.Expr, class appFlowClass) {
	switch p := f.parent[e].(type) {
	case *ast.AssignStmt:
		if i := slices.Index(p.Lhs, e); i >= 0 {
			fl.assignedFrom(f, p, i, class)
			return
		}
	case *ast.ValueSpec:
		for i, id := range p.Names {
			if ast.Expr(id) == e {
				fl.declaredFrom(f, p, i, class)
				return
			}
		}
	}
	fl.use(f, e, class)
}

// assignedFrom follows what is assigned to the i-th target of the statement.
func (fl *appFlow) assignedFrom(f *appFile, as *ast.AssignStmt, i int, class appFlowClass) {
	if as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
		fl.failf(f, as, "%s is changed by the operator %s", class.what(), as.Tok)
		return
	}
	switch {
	case len(as.Lhs) == len(as.Rhs):
		fl.claimExpr(f, as.Rhs[i], class, false)
	case len(as.Rhs) == 1:
		fl.produceResult(f, as.Rhs[0], i, class)
	default:
		fl.failf(f, as, "%s is assigned in a way the migration does not follow", class.what())
	}
}

// declaredFrom follows what the i-th name of a var declaration is given.
func (fl *appFlow) declaredFrom(f *appFile, spec *ast.ValueSpec, i int, class appFlowClass) {
	switch {
	case len(spec.Values) == 0:
	case len(spec.Values) == len(spec.Names):
		fl.claimExpr(f, spec.Values[i], class, false)
	case len(spec.Values) == 1:
		fl.produceResult(f, spec.Values[0], i, class)
	default:
		fl.failf(f, spec, "%s is declared in a way the migration does not follow", class.what())
	}
}

// produceResult follows the idx-th value of e, a call that returns several.
func (fl *appFlow) produceResult(f *appFile, e ast.Expr, idx int, class appFlowClass) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		fl.failf(f, e, "%s comes from %s, which the migration does not follow", class.what(), f.g.text(e))
		return
	}
	fl.claimCall(f, call, idx, class)
}

// use follows where the value of e goes: into another place of the set, into the
// fiber.New call that makes an app from it, into a call that applies it, or
// nowhere that matters.
func (fl *appFlow) use(f *appFile, e ast.Expr, class appFlowClass) {
	if fl.why != "" {
		return
	}
	for climbed := true; climbed; {
		climbed = false
		switch p := f.parent[e].(type) {
		case *ast.ParenExpr:
			e, climbed = p, true
		case *ast.StarExpr:
			if class == flowConfig {
				e, climbed = p, true
			}
		case *ast.UnaryExpr:
			if class == flowConfig && p.Op == token.AND {
				e, climbed = p, true
			}
		}
	}

	switch p := f.parent[e].(type) {
	case *ast.SelectorExpr:
		if p.X == e && class == flowConfig {
			if appListenerFieldV3(p.Sel.Name) != "" {
				fl.sels[p] = f
			}
			return
		}
	case *ast.CallExpr:
		if p.Fun == e {
			if class == flowFunc {
				fl.applied(f, p)
				return
			}
		} else {
			fl.argument(f, p, e, class)
			return
		}
	case *ast.AssignStmt:
		if slices.Contains(p.Lhs, e) {
			return // it is assigned, not read
		}
		i := slices.Index(p.Rhs, e)
		switch {
		case p.Tok != token.ASSIGN && p.Tok != token.DEFINE:
			fl.failf(f, p, "%s is changed by the operator %s", class.what(), p.Tok)
		case i < 0 || len(p.Lhs) != len(p.Rhs):
			fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
		default:
			fl.store(f, p, p.Lhs[i], class)
		}
		return
	case *ast.ValueSpec:
		if i := slices.Index(p.Values, e); i >= 0 && len(p.Values) == len(p.Names) {
			fl.claimVar(f.bindingOf(decl{typ: p.Type, init: e}, p.Names[i].Name), class)
			return
		}
	case *ast.ReturnStmt:
		fn, isDecl := f.enclosingFuncOf(p).(*ast.FuncDecl)
		if !isDecl {
			fl.failf(f, e, "%s is returned by a function literal", class.what())
			return
		}
		if i := slices.Index(p.Results, e); i >= 0 && len(p.Results) == appResultCount(fn.Type) {
			fl.claimResult(f, fn, i, class)
			return
		}
	case *ast.KeyValueExpr:
		if p.Value == e {
			fl.storeInLiteral(f, p, e, class)
			return
		}
	case *ast.CompositeLit:
		if at, isSlice := p.Type.(*ast.ArrayType); isSlice && at.Len == nil && class == flowFunc {
			if fl.retype(f, at, flowFuncs) {
				fl.sliceLiteral(f, p) // one of the functions in a slice of them
			}
			return
		}
		fl.storeUnkeyed(f, p, e, class)
		return
	case *ast.RangeStmt:
		if p.X == e && class == flowFuncs {
			fl.rangeValue(f, p)
			return
		}
	case *ast.IndexExpr:
		if p.X == e && class == flowFuncs {
			fl.use(f, p, flowFunc)
			return
		}
	case *ast.ExprStmt:
		return
	}
	fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
}

// argument follows the value handed to a call.
func (fl *appFlow) argument(f *appFile, call *ast.CallExpr, e ast.Expr, class appFlowClass) {
	if f.isNewCall(call) {
		switch {
		case class != flowConfig:
			fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
		case !f.isPlainNew(call):
			fl.failf(f, call, "the config is passed to %s, which the registry does not stand in for", f.g.text(call.Fun))
		case !fl.seenNew[call]:
			fl.seenNew[call] = true
			fl.news = append(fl.news, appNew{file: f, call: call})
		}
		return
	}
	if id, isIdent := call.Fun.(*ast.Ident); isIdent && (class == flowFuncs || class == flowFunc) {
		if _, bound := f.binding(id); !bound {
			switch id.Name {
			case "len", "cap":
				if class == flowFuncs {
					return
				}
			case "append":
				fl.use(f, call, flowFuncs) // the slice that append returns holds them
				return
			}
		}
	}

	callee, found := f.calleeOf(call, 0)
	switch {
	case found && callee.decl != nil:
	case found && callee.isInterfaceMethod():
		fl.failf(f, e, "%s is passed to %s, a method of an interface", class.what(), f.g.text(call.Fun))
		return
	default:
		// a function that a variable or a field holds
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.Ident:
			if _, bound := f.binding(fun); bound && class == flowConfig {
				fl.claimExpr(f, fun, flowFunc, false)
				return
			}
		case *ast.SelectorExpr:
			if id, isIdent := fun.X.(*ast.Ident); class == flowConfig && !(isIdent && f.isImportName(id)) && f.fieldHolder(fun.X, fun.Sel.Name) != nil {
				fl.claimExpr(f, fun, flowFunc, false) // a field that holds a function
				return
			}
		}
		fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
		return
	}

	idx := slices.Index(call.Args, e)
	b, variadic, ok := paramBindingAt(callee.file, callee.decl, idx)
	switch {
	case !ok:
		fl.failf(f, e, "%s is passed to %s, which takes it in a way the migration does not follow", class.what(), f.g.text(call.Fun))
	case variadic && class != flowFunc && class != flowFuncs:
		fl.failf(f, e, "%s is passed to %s, which takes it in a variadic parameter", class.what(), f.g.text(call.Fun))
	case variadic:
		fl.claimVar(b, flowFuncs)
	case call.Ellipsis.IsValid():
		fl.failf(f, call, "the arguments of the call are spread from a slice")
	default:
		fl.claimVar(b, class)
	}
}

// isPlainNew reports whether the call is fiber.New, which the registry's New stands
// in for, and not fiber.NewWithCustomCtx.
func (f *appFile) isPlainNew(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fun.Sel.Name == mw2New
	case *ast.Ident:
		return fun.Name == mw2New
	}
	return false
}

// paramBindingAt describes the parameter of fn that the argument idx goes to, and
// whether it is the variadic parameter, which takes every argument from its own on.
func paramBindingAt(f *appFile, fn *ast.FuncDecl, idx int) (b appBinding, variadic, ok bool) {
	if idx < 0 || fn.Type.Params == nil {
		return appBinding{}, false, false
	}
	n := 0
	for _, fld := range fn.Type.Params.List {
		count := max(len(fld.Names), 1)
		_, isVariadic := fld.Type.(*ast.Ellipsis)
		if idx >= n+count && !isVariadic {
			n += count
			continue
		}
		if len(fld.Names) == 0 {
			return appBinding{}, false, false // a parameter without a name: nothing in the body uses it
		}
		name := fld.Names[min(idx-n, len(fld.Names)-1)].Name
		return appBinding{file: f, typ: fld.Type, name: name, count: 1, node: fld.Type, scope: fn}, isVariadic, true
	}
	return appBinding{}, false, false
}

// store follows the value into the target of an assignment.
func (fl *appFlow) store(f *appFile, as *ast.AssignStmt, lhs ast.Expr, class appFlowClass) {
	switch x := lhs.(type) {
	case *ast.ParenExpr:
		fl.store(f, as, x.X, class)
	case *ast.Ident:
		if x.Name == "_" {
			return
		}
		if as.Tok == token.DEFINE {
			fl.claimVar(f.bindingOf(decl{init: f.assignedValue(as, x)}, x.Name), class)
			return
		}
		fl.claimExpr(f, x, class, false)
	case *ast.SelectorExpr:
		fl.claimSelector(f, x, class, false)
	case *ast.StarExpr:
		if class == flowConfig {
			fl.claimExpr(f, x.X, class, false) // *p = cfg: the config is what p points to
			return
		}
		fl.failf(f, lhs, "%s is assigned to %s", class.what(), f.g.text(lhs))
	default:
		fl.failf(f, lhs, "%s is assigned to %s", class.what(), f.g.text(lhs))
	}
}

// assignedValue returns the expression a short variable declaration gives to id.
func (f *appFile) assignedValue(as *ast.AssignStmt, id *ast.Ident) ast.Expr {
	i := slices.Index(as.Lhs, ast.Expr(id))
	switch {
	case i < 0:
		return nil
	case len(as.Rhs) == len(as.Lhs):
		return as.Rhs[i]
	case len(as.Rhs) == 1:
		return as.Rhs[0]
	}
	return nil
}

// storeInLiteral follows the value into the field a keyed element of a struct
// literal names.
func (fl *appFlow) storeInLiteral(f *appFile, kv *ast.KeyValueExpr, e ast.Expr, class appFlowClass) {
	lit, ok := f.parent[kv].(*ast.CompositeLit)
	key, isIdent := kv.Key.(*ast.Ident)
	if !ok || !isIdent || lit.Type == nil {
		fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
		return
	}
	td := f.resolveNamedType(lit.Type)
	if td == nil {
		fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
		return
	}
	holder := td.file.structHolding(td, key.Name, 0)
	if holder == nil {
		fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
		return
	}
	fl.claimField(holder, key.Name, class)
}

// storeUnkeyed follows the value into the field an element of a struct literal
// without keys stands for.
func (fl *appFlow) storeUnkeyed(f *appFile, lit *ast.CompositeLit, e ast.Expr, class appFlowClass) {
	i := slices.Index(lit.Elts, e)
	var td *appTypeDecl
	if lit.Type != nil {
		td = f.resolveNamedType(lit.Type)
	}
	if td == nil || i < 0 {
		fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
		return
	}
	st, ok := td.spec.Type.(*ast.StructType)
	if !ok {
		fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
		return
	}
	n := 0
	for _, fld := range st.Fields.List {
		count := max(len(fld.Names), 1)
		if i < n+count {
			name := appBaseTypeName(appSelectorTail(fld.Type))
			if len(fld.Names) > 0 {
				name = fld.Names[i-n].Name
			}
			fl.claimField(td, name, class)
			return
		}
		n += count
	}
	fl.failf(f, e, "%s is %s", class.what(), f.useOf(e))
}

// useResult follows the idx-th result of a call of a function that returns want
// values.
func (fl *appFlow) useResult(f *appFile, call *ast.CallExpr, idx, want int, class appFlowClass) {
	if want == 1 {
		fl.use(f, call, class)
		return
	}

	switch p := f.parent[call].(type) {
	case *ast.AssignStmt:
		if len(p.Rhs) == 1 && len(p.Lhs) == want && (p.Tok == token.ASSIGN || p.Tok == token.DEFINE) {
			fl.store(f, p, p.Lhs[idx], class)
			return
		}
	case *ast.ValueSpec:
		if len(p.Values) == 1 && len(p.Names) == want {
			fl.claimVar(f.bindingOf(decl{typ: p.Type, init: call}, p.Names[idx].Name), class)
			return
		}
	case *ast.ReturnStmt:
		fn, isDecl := f.enclosingFuncOf(p).(*ast.FuncDecl)
		if isDecl && len(p.Results) == 1 && appResultCount(fn.Type) == want {
			fl.claimResult(f, fn, idx, class)
			return
		}
	case *ast.ExprStmt:
		return
	}
	fl.failf(f, call, "%s is %s", class.what(), f.useOf(call))
}
