package v3

import (
	"go/ast"
	"go/token"
)

// The accessors of the environment for declarations. A name the file's package
// declares is a plain name, a name of another package of the module is a
// ctxForeignKey, and the declarations of the other files of the package are
// translated (ctx_module.go) before the environment reads them, because the
// imports of the file that declares them are not the ones of this file.

// attach connects the environment to the module the file belongs to.
func (env *ctxEnv) attach(mod *ctxModule) {
	if mod == nil {
		return
	}
	env.mod = mod
	env.imports = mod.importsOf(env.g.file)
}

// typeKey returns the key of the declaration of the type that t names, looking
// through pointers, parentheses and type arguments: the name for a type of the
// package of the file, a ctxForeignKey for one of another package of the module.
// An alias is the type it stands for: the key is the one of the type behind a chain
// of aliases of the module. It is empty for anything else.
func (env *ctxEnv) typeKey(t ast.Expr) string {
	return env.resolveAlias(env.declKey(t))
}

// resolveAlias follows the aliases of the module from the key of a type to the key
// of the type they stand for. An alias of a type that is no declaration of the
// module (type Ctx = fiber.Ctx), of an instantiation or a generic alias is where
// the chain ends.
func (env *ctxEnv) resolveAlias(key string) string {
	for range ctxMaxDepth {
		spec := env.typeSpec(key)
		if spec == nil || !spec.Assign.IsValid() || spec.TypeParams != nil {
			return key
		}

		var next string
		switch target := ast.Unparen(ctxDerefType(spec.Type)).(type) {
		case *ast.Ident, *ast.SelectorExpr:
			next = env.declKey(target)
		}
		if next == "" || next == key {
			return key
		}
		key = next
	}
	return key
}

// declKey is typeKey without the aliases being followed.
func (env *ctxEnv) declKey(t ast.Expr) string {
	switch x := t.(type) {
	case *ast.Ident:
		if env.typeDecl[x.Name] == nil && env.mod != nil && appIsExported(x.Name) {
			for _, importPath := range env.imports.dotsOrNil() {
				if mem := env.mod.member(importPath, x.Name); mem != nil && mem.spec != nil {
					return mem.key
				}
			}
		}
		return x.Name
	case *ast.SelectorExpr:
		return env.qualifiedKey(x)
	case *ast.StarExpr:
		return env.declKey(x.X)
	case *ast.ParenExpr:
		return env.declKey(x.X)
	case *ast.IndexExpr:
		return env.declKey(x.X)
	case *ast.IndexListExpr:
		return env.declKey(x.X)
	}
	return ""
}

// isCtxAlias reports whether t names an alias of the fiber context, type Ctx =
// fiber.Ctx, declared in the package of the file or in another package of the
// module, or an alias of such an alias. stack are the ancestors of t, which tell
// whether a local declaration hides the name; nil when t is not a use.
func (env *ctxEnv) isCtxAlias(t ast.Expr, stack []ast.Node) bool {
	return env.isCtxAliasAt(t, stack, 0)
}

func (env *ctxEnv) isCtxAliasAt(t ast.Expr, stack []ast.Node, depth int) bool {
	if depth > ctxMaxDepth {
		return false
	}

	switch x := t.(type) {
	case *ast.Ident:
		if depth == 0 && modDeclaredLocally(x.Name, x.Pos(), stack) {
			return false
		}
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && depth == 0 && modDeclaredLocally(id.Name, id.Pos(), stack) {
			return false
		}
	default:
		return false
	}

	spec := env.typeSpec(env.typeKey(t))
	if spec == nil || !spec.Assign.IsValid() || spec.TypeParams != nil {
		return false
	}
	// the alias must name the context itself, not a pointer to it or a wrapper
	target := ast.Unparen(spec.Type)
	if sel, ok := target.(*ast.SelectorExpr); ok && isFiberType(sel, env.pkgNames, ctxTypeCtx) {
		return true
	}
	return env.isCtxAliasAt(target, nil, depth+1)
}

// isInterfaceValue reports whether x is a value of an interface type: any value that
// satisfies the interface can be in it.
func (env *ctxEnv) isInterfaceValue(x ast.Expr, stack []ast.Node) bool {
	t := env.typeOf(x, stack)
	if _, ok := ast.Unparen(t).(*ast.InterfaceType); ok {
		return true
	}
	spec := env.typeSpec(env.typeKey(t))
	if spec == nil {
		return false
	}
	_, ok := spec.Type.(*ast.InterfaceType)
	return ok
}

// dotsOrNil returns the dot imports of the file.
func (i *ctxImports) dotsOrNil() []string {
	if i == nil {
		return nil
	}
	return i.dots
}

// qualifiedKey returns the key of pkg.Name when pkg is a package of the module.
func (env *ctxEnv) qualifiedKey(sel *ast.SelectorExpr) string {
	if importPath, name, ok := ctxAbsParts(sel); ok {
		return ctxForeignKey(importPath, name)
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || env.mod == nil {
		return ""
	}
	importPath := env.imports.pathOf(id.Name)
	if importPath == "" || !env.mod.inModule(importPath) {
		return ""
	}
	return ctxForeignKey(importPath, sel.Sel.Name)
}

// declaredElsewhere reports whether the declaration is in another file of the
// package, whose imports are not the ones of this file.
func (env *ctxEnv) declaredElsewhere(node ast.Node) (*ast.File, bool) {
	if env.mod == nil {
		return nil, false
	}
	file := env.declFile[node]
	return file, file != nil && file != env.g.file
}

// typeSpec returns the declaration of the type with the key, nil when the
// environment does not know it.
func (env *ctxEnv) typeSpec(key string) *ast.TypeSpec {
	if key == "" {
		return nil
	}
	if ctxIsForeignKey(key) {
		importPath, name, ok := ctxSplitKey(key)
		if !ok {
			return nil
		}
		if mem := env.mod.member(importPath, name); mem != nil {
			return mem.spec
		}
		return nil
	}

	spec := env.typeDecl[key]
	if file, other := env.declaredElsewhere(spec); other {
		return env.mod.translatedSpec(spec, file, "", false)
	}
	return spec
}

// funcDeclOf returns the declaration of the function of the package with the
// name, without its body.
func (env *ctxEnv) funcDeclOf(name string) *ast.FuncDecl {
	fn := env.funcDecl[name]
	if file, other := env.declaredElsewhere(fn); other {
		return env.mod.translatedFunc(fn, file, "", false)
	}
	return fn
}

// methodDecl returns the declaration of the method of the type with the key.
func (env *ctxEnv) methodDecl(typeKey, name string) *ast.FuncDecl {
	if ctxIsForeignKey(typeKey) {
		return env.mod.method(typeKey, name)
	}

	fn := env.methods[typeKey+"."+name]
	if file, other := env.declaredElsewhere(fn); other {
		return env.mod.translatedFunc(fn, file, "", false)
	}
	return fn
}

// pkgMember returns what the qualified name sel, pkg.Name, is when pkg is a
// package of the module that the file imports and no declaration of the code
// hides the name of the package. It is nil for anything else.
func (env *ctxEnv) pkgMember(sel *ast.SelectorExpr, stack []ast.Node) *ctxMember {
	id, ok := sel.X.(*ast.Ident)
	if !ok || env.mod == nil {
		return nil
	}
	importPath := env.imports.pathOf(id.Name)
	if importPath == "" || !env.mod.inModule(importPath) || modDeclaredLocally(id.Name, id.Pos(), stack) {
		return nil
	}
	return env.mod.member(importPath, sel.Sel.Name)
}

// dotMember returns what the bare name is when a package of the module that the
// file dot-imports declares it and the package of the file does not.
func (env *ctxEnv) dotMember(name string) *ctxMember {
	if env.mod == nil || !appIsExported(name) || env.typeDecl[name] != nil || env.funcDecl[name] != nil || env.pkgVars[name].spec != nil {
		return nil
	}
	for _, importPath := range env.imports.dotsOrNil() {
		if mem := env.mod.member(importPath, name); mem != nil {
			return mem
		}
	}
	return nil
}

// valueType returns the type of the value the member is: the variable's type, or
// the signature of a function used as a value.
func (mem *ctxMember) valueType() ast.Expr {
	switch {
	case mem == nil:
		return nil
	case mem.fn != nil:
		return mem.fn.Type
	}
	return mem.typ
}

// pkgVarType returns the type of a variable of the package that the file
// declares: its declared type, or the type of a composite literal or of a call that
// initializes it. A variable of another file of the package gets the type
// translated; the code that initializes it is not read there.
func (env *ctxEnv) pkgVarType(name string, depth int) ast.Expr {
	pv, ok := env.pkgVars[name]
	if !ok || depth > ctxMaxDepth {
		return nil
	}

	spec := pv.spec
	file, elsewhere := env.declaredElsewhere(spec)
	switch {
	case spec.Type != nil:
		if elsewhere {
			return env.mod.translatedType(spec.Type, file, "", false)
		}
		return spec.Type
	case !elsewhere:
		switch {
		case len(spec.Values) == len(spec.Names):
			return env.typeOfAt(spec.Values[pv.index], nil, depth+1)
		case len(spec.Values) == 1:
			return env.resultTypeAt(spec.Values[0], pv.index, nil, depth+1)
		}
	default:
		return env.elsewhereInitType(spec, pv.index, file, depth+1)
	}
	return nil
}

// elsewhereInitType is the type of the variable of another file of the package
// from its initializer, as far as the type of a composite literal, the results of a
// function of the package or of the module and the type of another variable say.
// The initializer is written with the imports of the file that declares the
// variable, so it is read with those.
func (env *ctxEnv) elsewhereInitType(spec *ast.ValueSpec, index int, file *ast.File, depth int) ast.Expr {
	var e ast.Expr
	idx := 0
	switch {
	case len(spec.Values) == len(spec.Names):
		e = spec.Values[index]
	case len(spec.Values) == 1:
		e, idx = spec.Values[0], index
	default:
		return nil
	}
	return env.elsewhereExprType(e, idx, file, depth)
}

// elsewhereExprType is the type of the idx-th value of the expression e, which is
// written in a file of the package other than the one of the environment.
func (env *ctxEnv) elsewhereExprType(e ast.Expr, idx int, file *ast.File, depth int) ast.Expr {
	if depth > ctxMaxDepth {
		return nil
	}

	switch x := ast.Unparen(e).(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return env.elsewhereExprType(x.X, idx, file, depth+1)
		}
	case *ast.CompositeLit:
		if x.Type != nil && idx == 0 {
			return env.mod.translatedType(x.Type, file, "", false)
		}
	case *ast.TypeAssertExpr:
		if x.Type != nil && idx == 0 {
			return env.mod.translatedType(x.Type, file, "", false)
		}
	case *ast.Ident:
		// another variable of the package
		if idx == 0 {
			return env.pkgVarType(x.Name, depth+1)
		}
	case *ast.SelectorExpr:
		// a variable of a package of the module
		if mem := env.fileMember(x, file); mem != nil && idx == 0 {
			return mem.valueType()
		}
	case *ast.CallExpr:
		return env.elsewhereCallType(x, idx, file)
	}
	return nil
}

// elsewhereCallType is the type of the idx-th result of a call written in a file
// of the package other than the one of the environment: a function of the package,
// a function of a package of the module, or a conversion to a type of the module.
func (env *ctxEnv) elsewhereCallType(call *ast.CallExpr, idx int, file *ast.File) ast.Expr {
	var results []ast.Expr
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		switch fn := env.funcDeclOf(fun.Name); {
		case fn != nil:
			results = ctxResultTypes(fn.Type)
		case env.typeDecl[fun.Name] != nil && idx == 0:
			// a conversion to a type of the package
			return fun
		case (fun.Name == mw3BuiltinNew && len(call.Args) == 1 || fun.Name == mw3BuiltinMake && len(call.Args) > 0) &&
			env.typeDecl[fun.Name] == nil && env.pkgVars[fun.Name].spec == nil:
			// new(T) and make(T, ...) make a value of the type of their first argument
			if idx == 0 {
				return env.mod.translatedType(call.Args[0], file, "", false)
			}
		}
	case *ast.SelectorExpr:
		mem := env.fileMember(fun, file)
		switch {
		case mem == nil:
		case mem.fn != nil:
			results = ctxResultTypes(mem.fn.Type)
		case mem.spec != nil && idx == 0:
			// a conversion to a type of the module
			importPath, name, _ := ctxSplitKey(mem.key)
			return ctxAbsRef(importPath, name)
		}
	}
	if idx < len(results) {
		return results[idx]
	}
	return nil
}

// fileMember is pkgMember for a selector written in another file of the package,
// whose imports are not the ones of the file of the environment. The scope of the
// declarations inside functions does not matter at the package level.
func (env *ctxEnv) fileMember(sel *ast.SelectorExpr, file *ast.File) *ctxMember {
	id, ok := sel.X.(*ast.Ident)
	if !ok || env.mod == nil {
		return nil
	}
	importPath := env.mod.importsOf(file).pathOf(id.Name)
	if importPath == "" || !env.mod.inModule(importPath) {
		return nil
	}
	return env.mod.member(importPath, sel.Sel.Name)
}
