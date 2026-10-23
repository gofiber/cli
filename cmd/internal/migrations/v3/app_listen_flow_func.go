package v3

import (
	"go/ast"
	"go/token"
)

// Functions that take the config to change it, the options of a constructor, are
// values the flow follows like the config: a literal or a declared function is made,
// handed to a variadic parameter, and applied to the config by a call.
//
//	type Option func(*fiber.Config)
//
//	func Quiet() Option { return func(c *fiber.Config) { c.DisableStartupMessage = true } }
//
//	func Build(opts ...Option) fiber.Config {
//		cfg := fiber.Config{}
//		for _, o := range opts {
//			o(&cfg)
//		}
//		return cfg
//	}
//
// The call o(&cfg) puts cfg in the set, and every function that o may be takes the
// registry's Config instead of the fiber.Config: the literal that Quiet returns, the
// declared functions that are handed to Build, and the type Option itself, with all
// the places that have it for a type.

// funcLit adds the function literal to the set: every parameter of it that is a
// fiber.Config is a place of the set.
func (fl *appFlow) funcLit(f *appFile, lit *ast.FuncLit) {
	if fl.why != "" || fl.funcs[lit] {
		return
	}
	fl.funcs[lit] = true
	fl.funcSignature(f, lit.Type, lit)
}

// funcDecl adds a declared function that is used as a value to the set.
func (fl *appFlow) funcDecl(f *appFile, fn *ast.FuncDecl) {
	if fl.why != "" || fl.funcs[fn] {
		return
	}
	fl.funcs[fn] = true
	if fn.Recv != nil {
		fl.failf(f, fn, "the method %s is used as a function that takes the config", f.funcLabel(fn))
		return
	}
	fl.funcSignature(f, fn.Type, fn)
}

// funcSignature adds the parameters of a function that take the config to the set.
// owner is the function declaration or literal.
func (fl *appFlow) funcSignature(f *appFile, ft *ast.FuncType, owner ast.Node) {
	if ft.Results != nil {
		for _, fld := range ft.Results.List {
			if f.typeKind(fld.Type, 0) == appKindConfig {
				fl.failf(f, fld, "a function that takes the config returns one")
				return
			}
		}
	}

	taken := 0
	if ft.Params != nil {
		for _, fld := range ft.Params.List {
			if f.typeKind(fld.Type, 0) != appKindConfig {
				continue
			}
			taken++
			if len(fld.Names) == 0 {
				fl.retype(f, fld.Type, flowConfig)
				continue
			}
			for _, id := range fld.Names {
				fl.claimVar(appBinding{file: f, typ: fld.Type, name: id.Name, count: 1, node: fld.Type, scope: owner}, flowConfig)
			}
		}
	}
	if taken == 0 {
		fl.failf(f, ft, "a function that is used as one that takes the config does not take it")
	}
}

// applied follows a call of a function that takes the config: the arguments that
// are one go to the functions of the set.
func (fl *appFlow) applied(f *appFile, call *ast.CallExpr) {
	for _, arg := range call.Args {
		if f.kind(arg) == appKindConfig {
			fl.claimExpr(f, arg, flowConfig, false)
		}
	}
}

// rangeValue adds the variable that a range statement over the functions assigns
// each of them to.
func (fl *appFlow) rangeValue(f *appFile, rng *ast.RangeStmt) {
	id, ok := rng.Value.(*ast.Ident)
	switch {
	case rng.Value == nil:
		return
	case !ok || rng.Tok != token.DEFINE:
		fl.failf(f, rng, "the functions that take the config are assigned in a way the migration does not follow")
	case id.Name == "_":
	default:
		fl.claimVar(appBinding{file: f, name: id.Name, count: 1, node: rng.Value, scope: rng, rangeX: rng.X, loop: true}, flowFunc)
	}
}

// retypeFunc records the type of a place that holds a function which takes the
// config: the parameters of a function type literal change, and a named function
// type changes with all the places that have it.
func (fl *appFlow) retypeFunc(f *appFile, t ast.Expr) bool {
	switch x := ast.Unparen(t).(type) {
	case *ast.FuncType:
		return fl.retypeFuncType(f, x)
	case *ast.Ident, *ast.SelectorExpr:
		td := f.resolveNamedType(x)
		if td == nil {
			fl.failf(f, t, "the functions that take the config have the type %s, which the project does not declare", f.g.text(t))
			return false
		}
		fl.claimFamily(td)
		return fl.why == ""
	}
	fl.failf(f, t, "the functions that take the config have the type %s, which the migration does not follow", f.g.text(t))
	return false
}

// retypeFuncType records the parameters of the function type that are a
// fiber.Config as types to change.
func (fl *appFlow) retypeFuncType(f *appFile, ft *ast.FuncType) bool {
	taken := 0
	if ft.Params != nil {
		for _, fld := range ft.Params.List {
			if f.typeKind(fld.Type, 0) == appKindConfig {
				taken++
				if !fl.retype(f, fld.Type, flowConfig) {
					return false
				}
			}
		}
	}
	if ft.Results != nil {
		for _, fld := range ft.Results.List {
			if f.typeKind(fld.Type, 0) == appKindConfig {
				fl.failf(f, fld, "a function that takes the config returns one")
				return false
			}
		}
	}
	if taken == 0 {
		fl.failf(f, ft, "the function type does not take a fiber.Config")
		return false
	}
	return true
}

// claimFamily adds the named function type, and with it every place that has it.
func (fl *appFlow) claimFamily(td *appTypeDecl) {
	if fl.claim(appFlowKey{kind: flowType, node: td.spec}) {
		fl.queue = append(fl.queue, func() { fl.scanFamily(td) })
	}
}

// scanFamily changes the named function type, and follows every place of the
// project that has it for a type: the parameters, results, fields and variables
// that hold such functions, which receive and hand on the functions of the set.
func (fl *appFlow) scanFamily(td *appTypeDecl) {
	name := td.spec.Name.Name
	ft, ok := td.spec.Type.(*ast.FuncType)
	if !ok {
		fl.failf(td.file, td.spec, "%s is not declared as a function type", name)
		return
	}
	if !fl.retypeFuncType(td.file, ft) {
		return
	}
	fl.addChange(td.file, appFlowChange{label: td.file.ix.pkgLabel(td.file) + name, how: changeTake}, appIsExported(name))

	for _, pf := range fl.pl.ix.moduleFiles() {
		if isShimFile(pf.path, pf.g.src) {
			continue
		}
		ast.Inspect(pf.g.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if sel, isSel := pf.parent[x].(*ast.SelectorExpr); isSel && sel.Sel == x {
					return true // the selector is the mention
				}
				if x.Name == name && x != td.spec.Name && pf.resolveNamedType(x) == td {
					fl.mention(pf, x)
				}
			case *ast.SelectorExpr:
				if x.Sel.Name == name && pf.resolveNamedType(x) == td {
					fl.mention(pf, x)
				}
			}
			return fl.why == ""
		})
	}
}

// mention follows one place of the project that names the function type.
func (fl *appFlow) mention(f *appFile, m ast.Expr) {
	top, class := m, flowFunc
	for climbed := true; climbed; {
		climbed = false
		switch p := f.parent[top].(type) {
		case *ast.ParenExpr:
			top, climbed = p, true
		case *ast.Ellipsis:
			top, class, climbed = p, flowFuncs, true
		case *ast.ArrayType:
			if p.Len != nil || p.Elt != top {
				fl.failf(f, m, "%s is the type of the elements of an array or a map", f.g.text(m))
				return
			}
			top, class, climbed = p, flowFuncs, true
		}
	}

	handled := false
	switch p := f.parent[top].(type) {
	case *ast.Field:
		fl.mentionField(f, p, class)
		handled = true
	case *ast.ValueSpec:
		if p.Type != top {
			break
		}
		for i, id := range p.Names {
			d := decl{typ: p.Type}
			switch {
			case len(p.Values) == len(p.Names):
				d.init = p.Values[i]
			case len(p.Values) == 1:
				d.init = p.Values[0]
			}
			fl.claimVar(f.bindingOf(d, id.Name), class)
		}
		handled = true
	case *ast.CallExpr:
		switch {
		case p.Fun == top && class == flowFunc && len(p.Args) == 1:
			// a conversion of a function to the type
			fl.claimExpr(f, p.Args[0], flowFunc, false)
			handled = true
		case class == flowFuncs && len(p.Args) > 0 && p.Args[0] == top && f.isBuiltin(p.Fun, "make"):
			fl.use(f, p, flowFuncs)
			handled = true
		}
	case *ast.CompositeLit:
		if p.Type == top && class == flowFuncs {
			fl.sliceLiteral(f, p)
			handled = true
		}
	}
	if !handled {
		fl.failf(f, m, "%s is used in a way the migration does not follow", f.g.text(m))
	}
}

// mentionField follows a parameter, result, receiver or field that has the function
// type.
func (fl *appFlow) mentionField(f *appFile, fld *ast.Field, class appFlowClass) {
	list := f.fieldList(fld)
	if list == nil {
		fl.failf(f, fld, "the functions that take the config are held in a way the migration does not follow")
		return
	}

	switch holder := f.parent[list].(type) {
	case *ast.FuncDecl: // the receiver
		for _, id := range fld.Names {
			fl.claimVar(appBinding{file: f, typ: fld.Type, name: id.Name, count: 1, node: fld.Type, scope: holder}, class)
		}
	case *ast.FuncType:
		fn, isDecl := f.parent[holder].(*ast.FuncDecl)
		if !isDecl || fn.Type != holder {
			fl.failf(f, fld, "a function literal or a function type takes or returns the functions that take the config")
			return
		}
		if list == holder.Results {
			idx := 0
			for _, rf := range list.List {
				if rf == fld {
					break
				}
				idx += max(len(rf.Names), 1)
			}
			fl.claimResult(f, fn, idx, class)
			return
		}
		for _, id := range fld.Names {
			fl.claimVar(appBinding{file: f, typ: fld.Type, name: id.Name, count: 1, node: fld.Type, scope: fn}, class)
		}
	case *ast.StructType:
		spec, isSpec := f.parent[holder].(*ast.TypeSpec)
		if !isSpec || len(fld.Names) == 0 {
			fl.failf(f, fld, "the functions that take the config are held by a field the migration does not follow")
			return
		}
		pkg := f.ix.pkgOf(f)
		if pkg == nil || pkg.types[spec.Name.Name] == nil {
			fl.failf(f, fld, "the struct type %s could not be found", spec.Name.Name)
			return
		}
		for _, id := range fld.Names {
			fl.claimField(pkg.types[spec.Name.Name], id.Name, class)
		}
	default:
		fl.failf(f, fld, "the functions that take the config are held by an interface or a type the migration does not follow")
	}
}

// sliceLiteral follows a literal of a slice of the functions: its elements are some
// of them, and the slice goes where the code puts it.
func (fl *appFlow) sliceLiteral(f *appFile, lit *ast.CompositeLit) {
	for _, el := range lit.Elts {
		fl.claimExpr(f, el, flowFunc, false)
	}
	fl.use(f, lit, flowFuncs)
}

// isBuiltin reports whether e names the builtin function name, and is not hidden
// by a variable.
func (f *appFile) isBuiltin(e ast.Expr, name string) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok || id.Name != name {
		return false
	}
	_, bound := f.binding(id)
	return !bound
}
