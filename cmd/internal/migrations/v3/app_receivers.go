package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"unicode"
)

// appKind says what a value is, as far as the app migrations care.
type appKind uint8

const (
	appKindNone   appKind = iota
	appKindApp            // *fiber.App
	appKindRouter         // fiber.Router, *fiber.Group
	appKindHooks          // *fiber.Hooks
	appKindConfig         // fiber.Config
	appKindCtx            // fiber.Ctx, *fiber.Ctx
)

// isRouter reports whether values of the kind have the routing methods (Get,
// Use, Group, ...).
func (k appKind) isRouter() bool { return k == appKindApp || k == appKindRouter }

// Names of the identifiers of the fiber package the migrations look for.
const (
	appNameConfig       = "Config"
	appNameListen       = "Listen"
	appNameListener     = "Listener"
	appNameListenConfig = "ListenConfig"
	appNameStatic       = "Static"
	appMethodGet        = "Get"
	appMethodAdd        = "Add"
	appMethodGroup      = "Group"
)

// appMaxDepth bounds how far the resolvers follow declarations, which also
// keeps them from looping on cyclic ones.
const appMaxDepth = 24

// appRouterMethods are the methods of an app or router that return the router
// again, so that app.Group("/a").Get(...) is still a router.
var appRouterMethods = []string{
	"Use", appMethodGet, nameHead, namePost, namePut, nameDelete, "Connect", "Options", "Trace", namePatch, "Query",
	appMethodAdd, "All", appNameStatic, appMethodGroup, "Route", "RouteChain", nameMount, "Name", "Domain",
}

// isFiberQualifier reports whether name is how f refers to the fiber package.
func (f *appFile) isFiberQualifier(name string) bool {
	return name != "." && slices.Contains(f.names, name)
}

// dotImportsFiber reports whether f dot-imports the fiber package.
func (f *appFile) dotImportsFiber() bool { return slices.Contains(f.names, ".") }

// fiberQualifier returns the prefix that names the fiber package in f: "fiber.",
// "f." for an alias or "" for a dot import. ok is false when f does not import
// fiber.
func (f *appFile) fiberQualifier() (prefix string, ok bool) {
	if f.dotImportsFiber() {
		return "", true
	}
	if len(f.names) == 0 {
		return "", false
	}
	return f.names[0] + ".", true
}

// fiberTypeName returns the name of the fiber package type that e refers to
// (App for *fiber.App, f.App, or App in a dot-importing file), empty when e is
// not a type of the fiber package. An alias of such a type, in this package or in
// another one of the project (type Config = fiber.Config), is that type.
func (f *appFile) fiberTypeName(e ast.Expr) string { return f.fiberTypeNameAt(e, 0) }

func (f *appFile) fiberTypeNameAt(e ast.Expr, depth int) string {
	e = appStripType(e)
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if id, isIdent := sel.X.(*ast.Ident); isIdent && f.isFiberQualifier(id.Name) {
			return sel.Sel.Name
		}
	}
	if depth < appMaxDepth {
		if td := f.resolveNamedType(e); td != nil && td.spec.Assign.IsValid() {
			return td.file.fiberTypeNameAt(td.spec.Type, depth+1)
		}
	}
	if id, ok := e.(*ast.Ident); ok && f.dotImportsFiber() {
		return id.Name
	}
	return ""
}

// appStripType removes pointers and parentheses from a type expression.
func appStripType(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return e
		}
	}
}

// isNewCall reports whether call creates an app: fiber.New(...).
func (f *appFile) isNewCall(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		id, ok := fun.X.(*ast.Ident)
		return ok && f.isFiberQualifier(id.Name) && (fun.Sel.Name == mw2New || fun.Sel.Name == "NewWithCustomCtx")
	case *ast.Ident:
		return f.dotImportsFiber() && fun.Name == "New"
	}
	return false
}

// kind returns what e is.
func (f *appFile) kind(e ast.Expr) appKind { return f.kindOf(e, 0) }

func (f *appFile) kindOf(e ast.Expr, depth int) appKind {
	if depth > appMaxDepth {
		return appKindNone
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.kindOf(x.X, depth+1)
	case *ast.StarExpr:
		return f.kindOf(x.X, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return f.kindOf(x.X, depth+1)
		}
	case *ast.CompositeLit:
		if x.Type != nil {
			return f.typeKind(x.Type, depth+1)
		}
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			return f.typeKind(x.Type, depth+1)
		}
	case *ast.Ident:
		if b, ok := f.binding(x); ok {
			return b.file.bindingKind(b, depth+1)
		}
	case *ast.CallExpr:
		return f.callKind(x, 0, depth+1)
	case *ast.SelectorExpr:
		return f.selectorKind(x, depth+1)
	case *ast.IndexExpr:
		if elem, ef := f.elemOfValue(x.X, depth+1); elem != nil {
			return ef.typeKind(elem, depth+1)
		}
	case *ast.SliceExpr:
		return f.kindOf(x.X, depth+1)
	}
	return appKindNone
}

// receiverKind returns what the receiver of the method call is, appKindNone
// when the method is one the project declares itself: a type that embeds an app
// can have a method of its own with the name of one of the app's, and that call
// is not fiber's.
func (f *appFile) receiverKind(call *ast.CallExpr, sel *ast.SelectorExpr) appKind {
	if callee, ok := f.calleeOf(call, 0); ok && callee.decl != nil && callee.decl.Recv != nil {
		return appKindNone
	}
	return f.kind(sel.X)
}

// bindingKind returns what the declaration b declares.
func (f *appFile) bindingKind(b appBinding, depth int) appKind {
	switch {
	case b.rangeX != nil || b.caseType != nil:
		if t, tf := b.file.bindingType(b, depth); t != nil {
			return tf.typeKind(t, depth+1)
		}
		return appKindNone
	case b.typ != nil:
		return f.typeKind(b.typ, depth)
	case b.init == nil:
		return appKindNone
	case b.count > 1:
		switch init := b.init.(type) {
		case *ast.CallExpr:
			return f.callKind(init, b.index, depth)
		case *ast.TypeAssertExpr:
			if b.index == 0 && init.Type != nil {
				return f.typeKind(init.Type, depth)
			}
		}
		return appKindNone
	default:
		return f.kindOf(b.init, depth)
	}
}

// typeKind returns what values of the type expression t are.
func (f *appFile) typeKind(t ast.Expr, depth int) appKind {
	if depth > appMaxDepth {
		return appKindNone
	}
	t = appStripType(t)

	if name := f.fiberTypeName(t); name != "" {
		switch name {
		case "App":
			return appKindApp
		case "Router", appMethodGroup:
			return appKindRouter
		case nameHooks:
			return appKindHooks
		case appNameConfig:
			return appKindConfig
		case ctxTypeCtx:
			return appKindCtx
		}
		return appKindNone
	}

	if td := f.resolveNamedType(t); td != nil {
		return td.file.typeSpecKind(td.spec, depth+1)
	}
	return appKindNone
}

// typeSpecKind returns what values of a declared type are: an alias is what it
// aliases, and a struct or interface that embeds an app or router has its
// methods.
func (f *appFile) typeSpecKind(spec *ast.TypeSpec, depth int) appKind {
	if spec.Assign.IsValid() {
		return f.typeKind(spec.Type, depth)
	}

	switch t := spec.Type.(type) {
	case *ast.StructType:
		for _, fld := range t.Fields.List {
			if len(fld.Names) == 0 {
				if k := f.typeKind(fld.Type, depth); k == appKindApp || k == appKindRouter {
					return k
				}
			}
		}
	case *ast.InterfaceType:
		for _, m := range t.Methods.List {
			if k := f.typeKind(m.Type, depth); len(m.Names) == 0 && (k == appKindApp || k == appKindRouter) {
				return appKindRouter
			}
		}
	}
	return appKindNone
}

// resolveNamedType finds the declaration of the named type t refers to, in the
// package of f or in one of the project's other packages.
func (f *appFile) resolveNamedType(t ast.Expr) *appTypeDecl {
	switch x := appStripType(t).(type) {
	case *ast.Ident:
		if pkg := f.ix.pkgOf(f); pkg != nil {
			return pkg.types[x.Name]
		}
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && !f.isFiberQualifier(id.Name) {
			if pkg := f.packageNamed(id.Name); pkg != nil {
				return pkg.types[x.Sel.Name]
			}
		}
	}
	return nil
}

// callKind returns what the idx-th result of call is.
func (f *appFile) callKind(call *ast.CallExpr, idx, depth int) appKind {
	if depth > appMaxDepth {
		return appKindNone
	}
	if f.isNewCall(call) {
		return appKindApp
	}

	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.StarExpr, *ast.ArrayType, *ast.InterfaceType:
		// a conversion such as (*fiber.App)(x)
		return f.typeKind(fun, depth+1)
	case *ast.SelectorExpr:
		switch recv := f.kindOf(fun.X, depth+1); {
		case recv.isRouter() && slices.Contains(appRouterMethods, fun.Sel.Name):
			return appKindRouter
		case recv == appKindApp && fun.Sel.Name == "Hooks":
			return appKindHooks
		case recv == appKindApp && fun.Sel.Name == appNameConfig:
			return appKindConfig
		case recv == appKindCtx && fun.Sel.Name == ctxTypeApp:
			return appKindApp
		}
	}

	if callee, ok := f.calleeOf(call, depth+1); ok {
		return callee.file.resultKind(callee.typ, idx, depth+1)
	}
	return appKindNone
}

// resultKind returns what the idx-th result of a function with the type ft is.
func (f *appFile) resultKind(ft *ast.FuncType, idx, depth int) appKind {
	t := appResultType(ft, idx)
	if t == nil {
		return appKindNone
	}
	return f.typeKind(t, depth)
}

// appResultType returns the type of the idx-th result of ft, nil when there is
// no such result.
func appResultType(ft *ast.FuncType, idx int) ast.Expr {
	if ft == nil || ft.Results == nil {
		return nil
	}
	n := 0
	for _, fld := range ft.Results.List {
		count := max(len(fld.Names), 1)
		if idx < n+count {
			return fld.Type
		}
		n += count
	}
	return nil
}

// appResultCount returns how many values a function with the type ft returns.
func appResultCount(ft *ast.FuncType) int {
	if ft == nil || ft.Results == nil {
		return 0
	}
	n := 0
	for _, fld := range ft.Results.List {
		n += max(len(fld.Names), 1)
	}
	return n
}

// packageVar finds the variable pkg.Name that a selector refers to, when pkg is
// another package of the project.
func (f *appFile) packageVar(sel *ast.SelectorExpr) (appBinding, *appPackage, bool) {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return appBinding{}, nil, false
	}
	if _, bound := f.binding(id); bound {
		return appBinding{}, nil, false
	}
	pkg := f.packageNamed(id.Name)
	if pkg == nil {
		return appBinding{}, nil, false
	}
	v := pkg.vars[sel.Sel.Name]
	if v == nil {
		return appBinding{}, nil, false
	}
	return v.binding(), pkg, true
}

// selectorKind returns what the field a.b is.
func (f *appFile) selectorKind(sel *ast.SelectorExpr, depth int) appKind {
	if b, _, ok := f.packageVar(sel); ok {
		return b.file.bindingKind(b, depth+1)
	}
	t, tf := f.valueType(sel.X, depth+1)
	if t == nil {
		return appKindNone
	}
	st, sf := tf.structOf(t, depth+1)
	if st == nil {
		return appKindNone
	}
	if fld := appStructField(st, sel.Sel.Name); fld != nil {
		return sf.typeKind(fld.Type, depth+1)
	}
	return appKindNone
}

// appStructField returns the field of st called name. An embedded field is
// called like its type.
func appStructField(st *ast.StructType, name string) *ast.Field {
	for _, fld := range st.Fields.List {
		if len(fld.Names) == 0 {
			if appBaseTypeName(appSelectorTail(fld.Type)) == name {
				return fld
			}
			continue
		}
		for _, id := range fld.Names {
			if id.Name == name {
				return fld
			}
		}
	}
	return nil
}

// appSelectorTail turns pkg.T into T and leaves every other expression alone.
func appSelectorTail(e ast.Expr) ast.Expr {
	e = appStripType(e)
	if sel, ok := e.(*ast.SelectorExpr); ok {
		return sel.Sel
	}
	return e
}

// structOf returns the struct type behind the type expression t.
func (f *appFile) structOf(t ast.Expr, depth int) (*ast.StructType, *appFile) {
	if depth > appMaxDepth {
		return nil, nil
	}
	if st, ok := appStripType(t).(*ast.StructType); ok {
		return st, f
	}
	td := f.resolveNamedType(t)
	if td == nil {
		return nil, nil
	}
	if st, ok := td.spec.Type.(*ast.StructType); ok {
		return st, td.file
	}
	if _, ok := td.spec.Type.(*ast.Ident); ok || td.spec.Assign.IsValid() {
		return td.file.structOf(td.spec.Type, depth+1)
	}
	return nil, nil
}

// valueType returns the declared or evident type of the value e and the file
// the type expression belongs to; nil when it cannot be told.
func (f *appFile) valueType(e ast.Expr, depth int) (ast.Expr, *appFile) {
	if depth > appMaxDepth {
		return nil, nil
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.valueType(x.X, depth+1)
	case *ast.StarExpr:
		return f.valueType(x.X, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return f.valueType(x.X, depth+1)
		}
	case *ast.CompositeLit:
		if x.Type != nil {
			return x.Type, f
		}
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			return x.Type, f
		}
	case *ast.Ident:
		b, ok := f.binding(x)
		if !ok {
			return nil, nil
		}
		return b.file.bindingType(b, depth+1)
	case *ast.CallExpr:
		return f.callType(x, 0, depth+1)
	case *ast.IndexExpr:
		return f.elemOfValue(x.X, depth+1)
	case *ast.SliceExpr:
		return f.valueType(x.X, depth+1)
	case *ast.SelectorExpr:
		if b, _, ok := f.packageVar(x); ok {
			return b.file.bindingType(b, depth+1)
		}
		t, tf := f.valueType(x.X, depth+1)
		if t == nil {
			return nil, nil
		}
		st, sf := tf.structOf(t, depth+1)
		if st == nil {
			return nil, nil
		}
		if fld := appStructField(st, x.Sel.Name); fld != nil {
			return fld.Type, sf
		}
	}
	return nil, nil
}

// bindingType returns the type of the variable that b declares.
func (f *appFile) bindingType(b appBinding, depth int) (ast.Expr, *appFile) {
	switch {
	case b.caseType != nil:
		return b.caseType, b.file
	case b.rangeX != nil:
		if b.rangeKey {
			return b.file.keyOfValue(b.rangeX, depth+1)
		}
		return b.file.elemOfValue(b.rangeX, depth+1)
	case b.typ != nil:
		return b.typ, f
	case b.init == nil:
		return nil, nil
	case b.count > 1:
		if call, ok := b.init.(*ast.CallExpr); ok {
			return f.callType(call, b.index, depth)
		}
		if ta, ok := b.init.(*ast.TypeAssertExpr); ok && b.index == 0 && ta.Type != nil {
			return ta.Type, f
		}
		return nil, nil
	default:
		return f.valueType(b.init, depth)
	}
}

// callType returns the type of the idx-th result of call.
func (f *appFile) callType(call *ast.CallExpr, idx, depth int) (ast.Expr, *appFile) {
	if f.isNewCall(call) {
		return nil, nil
	}
	if id, ok := call.Fun.(*ast.Ident); ok && len(call.Args) > 0 {
		if _, bound := f.binding(id); !bound {
			switch id.Name {
			case "new", mw3BuiltinMake: // the type is the first argument
				return call.Args[0], f
			case mw3BuiltinAppend: // a slice of the type of its first argument
				return f.valueType(call.Args[0], depth+1)
			}
		}
	}
	callee, ok := f.calleeOf(call, depth+1)
	if !ok {
		return nil, nil
	}
	if t := appResultType(callee.typ, idx); t != nil {
		return t, callee.file
	}
	return nil, nil
}

// appCallee is the function a call invokes.
type appCallee struct {
	file *appFile
	typ  *ast.FuncType
	body *ast.BlockStmt
	// decl is the declaration of a function or method, nil for a literal.
	decl *ast.FuncDecl
}

// isInterfaceMethod reports whether the callee is a method of an interface: the
// signature is known, the function that answers is not.
func (c appCallee) isInterfaceMethod() bool { return c.decl == nil && c.body == nil && c.typ != nil }

// calleeOf finds the function a call invokes: a function of the package or of
// another package of the project, a method of a type declared there, or a
// function literal bound to a variable.
func (f *appFile) calleeOf(call *ast.CallExpr, depth int) (appCallee, bool) {
	if depth > appMaxDepth {
		return appCallee{}, false
	}

	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.FuncLit:
		return appCallee{file: f, typ: fun.Type, body: fun.Body}, true
	case *ast.Ident:
		if b, ok := f.binding(fun); ok {
			if lit, isLit := b.init.(*ast.FuncLit); isLit && b.count <= 1 {
				return appCallee{file: b.file, typ: lit.Type, body: lit.Body}, true
			}
			return appCallee{}, false
		}
		if pkg := f.ix.pkgOf(f); pkg != nil {
			if fn := pkg.funcs[fun.Name]; fn != nil {
				return appCallee{file: fn.file, typ: fn.decl.Type, body: fn.decl.Body, decl: fn.decl}, true
			}
		}
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			if _, bound := f.binding(id); !bound {
				if pkg := f.packageNamed(id.Name); pkg != nil {
					if fn := pkg.funcs[fun.Sel.Name]; fn != nil {
						return appCallee{file: fn.file, typ: fn.decl.Type, body: fn.decl.Body, decl: fn.decl}, true
					}
					return appCallee{}, false
				}
			}
		}
		t, tf := f.valueType(fun.X, depth+1)
		if t == nil {
			return appCallee{}, false
		}
		td := tf.resolveNamedType(t)
		if td == nil {
			return appCallee{}, false
		}
		if pkg := td.file.ix.pkgOf(td.file); pkg != nil {
			if fn := pkg.methods[td.spec.Name.Name+"."+fun.Sel.Name]; fn != nil {
				return appCallee{file: fn.file, typ: fn.decl.Type, body: fn.decl.Body, decl: fn.decl}, true
			}
		}
		// a method of an interface the project declares: its result is known, the
		// function that answers is not
		if ft, ff := td.file.interfaceMethod(td.spec.Type, fun.Sel.Name, depth+1); ft != nil {
			return appCallee{file: ff, typ: ft}, true
		}
	}
	return appCallee{}, false
}

// interfaceMethod returns the signature of the method called name of the interface
// type t (a type literal, or the name of a type of the project), and the file it is
// written in; nil when the interface has none, also through the interfaces it embeds.
func (f *appFile) interfaceMethod(t ast.Expr, name string, depth int) (*ast.FuncType, *appFile) {
	if depth > appMaxDepth {
		return nil, nil
	}

	switch x := appStripType(t).(type) {
	case *ast.InterfaceType:
		for _, m := range x.Methods.List {
			if ft, ok := m.Type.(*ast.FuncType); ok && slices.ContainsFunc(m.Names, func(id *ast.Ident) bool { return id.Name == name }) {
				return ft, f
			}
		}
		for _, m := range x.Methods.List {
			if len(m.Names) == 0 {
				if ft, ff := f.interfaceMethod(m.Type, name, depth+1); ft != nil {
					return ft, ff
				}
			}
		}
	case *ast.Ident, *ast.SelectorExpr:
		if td := f.resolveNamedType(x); td != nil {
			return td.file.interfaceMethod(td.spec.Type, name, depth+1)
		}
	}
	return nil, nil
}

// appBinding is the declaration an identifier refers to.
type appBinding struct {
	file *appFile
	typ  ast.Expr
	init ast.Expr
	// node identifies the declaration: the initializer, or the type of a
	// variable that has none.
	node ast.Node
	// scope is the block or function the declaration is visible in, nil for a
	// package-level variable.
	scope ast.Node
	// rangeX is the expression a range statement ranges over, when the binding is
	// its key or value variable (see rangeKey).
	rangeX ast.Expr
	// caseType is the type of a type switch variable in a clause with one type.
	caseType ast.Expr
	name     string
	// index is the position of the name among count names declared together
	// from a single call.
	index int
	count int
	// rangeKey is set for the key variable of a range statement.
	rangeKey bool
	// loop is set for the variables of range statements and type switches, which
	// hold a different value each time.
	loop bool
}

// binding finds the declaration id refers to: a parameter, a local variable or
// a package-level variable of the package.
func (f *appFile) binding(id *ast.Ident) (appBinding, bool) {
	if d, ok := resolveIdent(id.Name, id.Pos(), f.stack(id)); ok {
		return f.bindingOf(d, id.Name), true
	}
	if b, ok := f.loopBinding(id); ok {
		return b, true
	}

	// a package-level variable that another file declares
	pkg := f.ix.pkgOf(f)
	if pkg == nil {
		return appBinding{}, false
	}
	v := pkg.vars[id.Name]
	if v == nil || v.file == f {
		return appBinding{}, false
	}
	return v.binding(), true
}

// loopBinding finds the range or type switch variable id refers to, which
// resolveIdent knows to exist but says nothing about.
func (f *appFile) loopBinding(id *ast.Ident) (appBinding, bool) {
	for _, node := range slices.Backward(f.stack(id)) {
		switch n := node.(type) {
		case *ast.RangeStmt:
			if n.Tok != token.DEFINE || id.Pos() < n.Body.Pos() {
				continue
			}
			for i, v := range []ast.Expr{n.Key, n.Value} {
				if isIdentNamed(v, id.Name) {
					return appBinding{file: f, name: id.Name, count: 1, node: v, scope: n, rangeX: n.X, rangeKey: i == 0, loop: true}, true
				}
			}
		case *ast.CaseClause:
			ts, ok := f.parent[f.parent[n]].(*ast.TypeSwitchStmt)
			if !ok {
				continue
			}
			as, isAssign := ts.Assign.(*ast.AssignStmt)
			if !isAssign || len(as.Lhs) != 1 || !isIdentNamed(as.Lhs[0], id.Name) {
				continue
			}
			b := appBinding{file: f, name: id.Name, count: 1, node: n, scope: n, loop: true}
			if len(n.List) == 1 {
				b.caseType = n.List[0]
			}
			return b, true
		}
	}
	return appBinding{}, false
}

// elemOf returns the type of the values of the slice, array, map or channel type
// t, and the file it is written in.
func (f *appFile) elemOf(t ast.Expr, depth int) (ast.Expr, *appFile) {
	if depth > appMaxDepth {
		return nil, nil
	}
	switch x := appStripType(t).(type) {
	case *ast.ArrayType:
		return x.Elt, f
	case *ast.MapType:
		return x.Value, f
	case *ast.ChanType:
		return x.Value, f
	case *ast.Ident, *ast.SelectorExpr:
		if td := f.resolveNamedType(x); td != nil {
			return td.file.elemOf(td.spec.Type, depth+1)
		}
	}
	return nil, nil
}

// elemOfValue returns the type of the values e holds when it is a slice, array,
// map or channel, and the file it is written in.
func (f *appFile) elemOfValue(e ast.Expr, depth int) (ast.Expr, *appFile) {
	t, tf := f.valueType(e, depth+1)
	if t == nil {
		return nil, nil
	}
	return tf.elemOf(t, depth+1)
}

// keyOfValue returns the type of the first variable of a range statement over e
// when it is not the index of a slice: the key of a map, and the value of a channel.
func (f *appFile) keyOfValue(e ast.Expr, depth int) (ast.Expr, *appFile) {
	t, tf := f.valueType(e, depth+1)
	for range appMaxDepth {
		if t == nil {
			return nil, nil
		}
		switch x := appStripType(t).(type) {
		case *ast.MapType:
			return x.Key, tf
		case *ast.ChanType:
			return x.Value, tf
		case *ast.Ident, *ast.SelectorExpr:
			td := tf.resolveNamedType(x)
			if td == nil {
				return nil, nil
			}
			t, tf = td.spec.Type, td.file
		default:
			return nil, nil
		}
	}
	return nil, nil
}

// binding describes the package-level variable.
func (v *appVarDecl) binding() appBinding {
	b := appBinding{file: v.file, typ: v.spec.Type, name: v.spec.Names[v.index].Name, count: len(v.spec.Names), index: v.index}
	switch {
	case len(v.spec.Values) == len(v.spec.Names):
		b.init, b.count, b.index = v.spec.Values[v.index], 1, 0
	case len(v.spec.Values) == 1:
		b.init = v.spec.Values[0]
	}
	b.node = appDeclNode(b.init, b.typ)
	return b
}

// bindingOf describes the declaration d of the variable called name.
func (f *appFile) bindingOf(d decl, name string) appBinding {
	b := appBinding{file: f, typ: d.typ, init: d.init, name: name, count: 1}
	b.node = appDeclNode(d.init, d.typ)

	switch {
	case d.init != nil:
		switch p := f.parent[d.init].(type) {
		case *ast.AssignStmt:
			b.scope = f.parent[p]
			if len(p.Lhs) != len(p.Rhs) {
				b.count, b.index = len(p.Lhs), appNameIndex(p.Lhs, name)
			}
		case *ast.ValueSpec:
			b.scope = f.specScope(p)
			if len(p.Names) != len(p.Values) {
				b.count, b.index = len(p.Names), appNameIndexIdents(p.Names, name)
			}
		}
	case d.typ != nil:
		switch p := f.parent[d.typ].(type) {
		case *ast.Field:
			b.scope = f.enclosingFuncOfField(p)
		case *ast.ValueSpec:
			b.scope = f.specScope(p)
		}
	}
	return b
}

// specScope returns the block a variable declaration is visible in, nil for a
// package-level one.
func (f *appFile) specScope(spec *ast.ValueSpec) ast.Node {
	decl := f.parent[f.parent[spec]]
	if _, isStmt := decl.(*ast.DeclStmt); isStmt {
		return f.parent[decl]
	}
	return nil
}

// enclosingFuncOfField returns the function a parameter or result belongs to.
func (f *appFile) enclosingFuncOfField(fld *ast.Field) ast.Node {
	for p := f.parent[fld]; p != nil; p = f.parent[p] {
		switch p.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return p
		}
	}
	return nil
}

func appDeclNode(init, typ ast.Expr) ast.Node {
	if init != nil {
		return init
	}
	if typ != nil {
		return typ
	}
	return nil
}

func appNameIndex(exprs []ast.Expr, name string) int {
	for i, e := range exprs {
		if isIdentNamed(e, name) {
			return i
		}
	}
	return 0
}

func appNameIndexIdents(ids []*ast.Ident, name string) int {
	for i, id := range ids {
		if id.Name == name {
			return i
		}
	}
	return 0
}

// isParam reports whether the binding is a parameter, receiver or named result.
func (b appBinding) isParam() bool {
	if b.init != nil || b.typ == nil {
		return false
	}
	_, isField := b.file.parent[b.typ].(*ast.Field)
	return isField
}

// appNew is a call that creates an app.
type appNew struct {
	file *appFile
	call *ast.CallExpr
}

// appOrigins is where the value of an expression can come from.
type appOrigins struct {
	news []appNew
	// complete is false when some origin could not be determined, which means
	// news is not the whole story.
	complete bool
}

func (o *appOrigins) merge(other appOrigins) {
	for _, n := range other.news {
		if !slices.Contains(o.news, n) {
			o.news = append(o.news, n)
		}
	}
	o.complete = o.complete && other.complete
}

// originsOf follows the value of the idx-th result of e back to the fiber.New
// calls it can come from.
func (f *appFile) originsOf(e ast.Expr, idx, depth int) appOrigins {
	if depth > appMaxDepth {
		return appOrigins{}
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.originsOf(x.X, idx, depth+1)
	case *ast.StarExpr:
		return f.originsOf(x.X, idx, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return f.originsOf(x.X, idx, depth+1)
		}
	case *ast.CallExpr:
		if f.isNewCall(x) {
			return appOrigins{news: []appNew{{file: f, call: x}}, complete: true}
		}
		if callee, ok := f.calleeOf(x, depth+1); ok {
			return callee.returnOrigins(idx, depth+1)
		}
	case *ast.Ident:
		return f.identOrigins(x, depth+1)
	case *ast.SelectorExpr:
		if o, ok := f.packageVarOrigins(x, depth+1); ok {
			return o
		}
		return f.fieldOrigins(x, depth+1)
	}
	return appOrigins{}
}

// returnOrigins merges the origins of what the function returns as its idx-th
// result.
func (c appCallee) returnOrigins(idx, depth int) appOrigins {
	if c.body == nil {
		return appOrigins{}
	}

	out := appOrigins{complete: true}
	seen := false
	want := appResultCount(c.typ)
	ast.Inspect(c.body, func(n ast.Node) bool {
		switch r := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			seen = true
			switch {
			case len(r.Results) == want && idx < len(r.Results):
				out.merge(c.file.originsOf(r.Results[idx], 0, depth))
			case len(r.Results) == 1 && want > 1:
				out.merge(c.file.originsOf(r.Results[0], idx, depth))
			default: // a bare return of named results
				out.complete = false
			}
		}
		return true
	})
	if !seen {
		out.complete = false
	}
	return out
}

// identOrigins follows a variable: its initializer and everything assigned to
// it later.
func (f *appFile) identOrigins(id *ast.Ident, depth int) appOrigins {
	b, ok := f.binding(id)
	if !ok || b.isParam() || b.loop {
		return appOrigins{}
	}

	out := appOrigins{complete: true}
	if b.init != nil {
		idx := 0
		if b.count > 1 {
			idx = b.index
		}
		out.merge(b.file.originsOf(b.init, idx, depth))
	}
	out.merge(b.file.assignedOrigins(b, id.Name, depth))
	if b.init == nil && len(out.news) == 0 {
		out.complete = false
	}
	return out
}

// assignedOrigins merges the origins of everything assigned to the variable
// that b declares after its declaration.
func (f *appFile) assignedOrigins(b appBinding, name string, depth int) appOrigins {
	out := appOrigins{complete: true}

	var regions []struct {
		file *appFile
		root ast.Node
	}
	if b.scope != nil {
		regions = append(regions, struct {
			file *appFile
			root ast.Node
		}{b.file, b.scope})
	} else if pkg := f.ix.pkgOf(b.file); pkg != nil {
		for _, pf := range pkg.files {
			regions = append(regions, struct {
				file *appFile
				root ast.Node
			}{pf, pf.g.file})
		}
	}

	for _, r := range regions {
		ast.Inspect(r.root, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || as.Tok != token.ASSIGN {
				return true
			}
			for i, lhs := range as.Lhs {
				id, isIdent := lhs.(*ast.Ident)
				if !isIdent || id.Name != name {
					continue
				}
				other, bound := r.file.binding(id)
				if !bound || other.node != b.node || other.file != b.file {
					continue
				}
				switch {
				case len(as.Lhs) == len(as.Rhs):
					out.merge(r.file.originsOf(as.Rhs[i], 0, depth))
				case len(as.Rhs) == 1:
					out.merge(r.file.originsOf(as.Rhs[0], i, depth))
				default:
					out.complete = false
				}
			}
			return true
		})
	}
	return out
}

// packageVarOrigins follows pkg.Var, a variable of another package of the
// project, to its initializer and to every assignment to it in the project.
func (f *appFile) packageVarOrigins(sel *ast.SelectorExpr, depth int) (appOrigins, bool) {
	b, pkg, ok := f.packageVar(sel)
	if !ok {
		return appOrigins{}, false
	}

	out := appOrigins{complete: true}
	if b.init != nil {
		out.merge(b.file.originsOf(b.init, 0, depth))
	}
	out.merge(b.file.assignedOrigins(b, sel.Sel.Name, depth))

	// from other packages the variable is assigned as pkg.Var = ...
	for _, pf := range f.ix.moduleFiles() {
		if pf.ix.pkgOf(pf) == pkg {
			continue
		}
		ast.Inspect(pf.g.file, func(n ast.Node) bool {
			as, isAssign := n.(*ast.AssignStmt)
			if !isAssign || as.Tok != token.ASSIGN {
				return true
			}
			for i, lhs := range as.Lhs {
				s, isSel := lhs.(*ast.SelectorExpr)
				if !isSel || s.Sel.Name != sel.Sel.Name {
					continue
				}
				if other, _, same := pf.packageVar(s); !same || other.node != b.node {
					continue
				}
				switch {
				case len(as.Lhs) == len(as.Rhs):
					out.merge(pf.originsOf(as.Rhs[i], 0, depth))
				case len(as.Rhs) == 1:
					out.merge(pf.originsOf(as.Rhs[0], i, depth))
				default:
					out.complete = false
				}
			}
			return true
		})
	}
	if b.init == nil && len(out.news) == 0 {
		out.complete = false
	}
	return out, true
}

// fieldOrigins follows a struct field to everything that sets it: composite
// literals that name it and assignments to it, within the package that
// declares the struct. An exported field may be set from anywhere, so its
// origins are never complete.
func (f *appFile) fieldOrigins(sel *ast.SelectorExpr, depth int) appOrigins {
	t, tf := f.valueType(sel.X, depth+1)
	if t == nil {
		return appOrigins{}
	}
	td := tf.resolveNamedType(t)
	if td == nil {
		return appOrigins{}
	}
	return td.file.setsOfField(td, sel.Sel.Name, depth+1)
}

func (f *appFile) setsOfField(td *appTypeDecl, field string, depth int) appOrigins {
	key := appFieldKey{td: td, field: field}
	if known, ok := f.ix.fields[key]; ok {
		return known
	}
	// a field that is its own origin through a cycle is not complete
	f.ix.fields[key] = appOrigins{}
	out := f.scanFieldSets(td, field, depth)
	f.ix.fields[key] = out
	return out
}

func (f *appFile) scanFieldSets(td *appTypeDecl, field string, depth int) appOrigins {
	out := appOrigins{complete: true}
	pkg := f.ix.pkgOf(f)
	if pkg == nil {
		return appOrigins{}
	}
	unique := pkg.fieldOwners(field) <= 1

	// an exported field can be set from every package of the project
	files := pkg.files
	if appIsExported(field) {
		files = f.ix.moduleFiles()
	}

	found := false
	for _, pf := range files {
		otherPkg := f.ix.pkgOf(pf) != pkg
		ast.Inspect(pf.g.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				if x.Type == nil || pf.resolveNamedType(x.Type) != td {
					return true
				}
				for _, el := range x.Elts {
					kv, isKV := el.(*ast.KeyValueExpr)
					if !isKV {
						out.complete = false
						continue
					}
					if isIdentNamed(kv.Key, field) {
						found = true
						out.merge(pf.originsOf(kv.Value, 0, depth))
					}
				}
			case *ast.AssignStmt:
				if x.Tok != token.ASSIGN {
					return true
				}
				for i, lhs := range x.Lhs {
					s, isSel := lhs.(*ast.SelectorExpr)
					if !isSel || s.Sel.Name != field {
						continue
					}
					bt, btf := pf.valueType(s.X, depth+1)
					same := false
					switch {
					case bt != nil:
						same = btf.resolveNamedType(bt) == td
					default:
						same = unique && !otherPkg
						if !same {
							out.complete = false
						}
					}
					if !same {
						continue
					}
					found = true
					switch {
					case len(x.Lhs) == len(x.Rhs):
						out.merge(pf.originsOf(x.Rhs[i], 0, depth))
					case len(x.Rhs) == 1:
						out.merge(pf.originsOf(x.Rhs[0], i, depth))
					default:
						out.complete = false
					}
				}
			}
			return true
		})
	}
	if !found {
		out.complete = false
	}
	return out
}

// fieldOwners counts the struct types of the package that have a field called
// name.
func (p *appPackage) fieldOwners(name string) int {
	n := 0
	for _, td := range p.types {
		st, ok := td.spec.Type.(*ast.StructType)
		if !ok {
			continue
		}
		if appStructField(st, name) != nil {
			n++
		}
	}
	return n
}

func appIsExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}
