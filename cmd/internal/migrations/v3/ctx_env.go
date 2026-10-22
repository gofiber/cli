package v3

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// ctxKind says what is known about the type of an expression.
type ctxKind int

const (
	// ctxUnknown: the type cannot be told from this file.
	ctxUnknown ctxKind = iota
	// ctxNo: the type is known and it is not the one asked about.
	ctxNo
	// ctxYes: the type is the one asked about.
	ctxYes
)

// ctxMaxDepth bounds how far the environment follows variables and types, so a
// cycle in the code can not make it loop.
const ctxMaxDepth = 8

// Names of the fiber types the environment looks for.
const (
	ctxTypeCtx   = "Ctx"
	ctxTypeApp   = "App"
	ctxTypeGroup = "Group"
)

// ctxIsPredeclaredType reports whether name is a predeclared type such as int
// or error, none of which is a fiber type.
func ctxIsPredeclaredType(name string) bool {
	_, ok := types.Universe.Lookup(name).(*types.TypeName)
	return ok
}

// ctxEnv knows the declarations of one file and answers what type an
// expression has, as far as the file tells. It is built for the migrations that
// rewrite methods of the request context: unlike a search of the text for
// "c *fiber.Ctx", it follows the declaration that is in scope at the use, the
// fields of the file's structs, the results of its functions and methods, and
// it knows the file's names for the fiber package.
type ctxEnv struct {
	g        *goSource
	typeDecl map[string]*ast.TypeSpec
	funcDecl map[string]*ast.FuncDecl
	// methods are keyed by "Type.method".
	methods map[string]*ast.FuncDecl
	// fields are the declared types of every struct field of the file, by name.
	fields map[string][]ast.Expr
	// pkgVars are the package-level variables of the package, by name.
	pkgVars map[string]ctxPkgVar
	// declTypes and declInits are the types and the initializing expressions of
	// the declarations of the file by the name they declare, in any scope.
	declTypes map[string][]ast.Expr
	declInits map[string][]ast.Expr
	// mod reaches the other packages of the module, nil outside of one (see attach).
	mod *ctxModule
	// imports are the imports of the file, known with a module.
	imports *ctxImports
	// speller names the other packages the rewrites write (strconv, fmt), read when
	// first asked for (see pkgAt).
	speller *pkgSpeller
	// pkgPath is the import path of the package of the file, known with a module.
	pkgPath string
	// declFile is the file that declares each type, function, method and variable
	// of the package that the environment knows.
	declFile map[ast.Node]*ast.File
	// alias is the name the fiber package gets in a file that imports none: fiber,
	// unless the file or its package gives that name to something else.
	alias string
	// extra is the name of the second import of the fiber package, empty until a
	// rewrite needs one (see extraName).
	extra string
	// names are the identifiers the file binds to the core fiber package.
	names []string
	// pkgNames are the names the file and the other files of its package bind
	// to the fiber package: the declarations of those files are part of the
	// environment, and they spell the fiber types their own way.
	pkgNames []string
	// files are the other files of the package.
	files []*ast.File
	// usedFiber is set once a rewrite spelled the qualifier of the fiber package
	// (see finish).
	usedFiber bool
}

// ctxPkgVar is a package-level variable: its declaration and the position of the
// name in it.
type ctxPkgVar struct {
	spec  *ast.ValueSpec
	index int
}

// ctxSibling is another file of the package of the file that is migrated. Its
// type, function and method declarations are known to the environment.
type ctxSibling struct {
	file  *ast.File
	names []string
}

// newCtxEnv indexes the declarations of g and of the sibling files of its
// package. names are the identifiers g binds to the fiber package (see
// fiberNames).
func newCtxEnv(g *goSource, names []string, siblings ...ctxSibling) *ctxEnv {
	env := &ctxEnv{
		g:     g,
		names: names,
		// the types the environment makes up are spelled with the qualifier that
		// stands for the fiber package in every file (see ctxFiberRef)
		pkgNames: append(slices.Clone(names), ctxFiberQualifier),
		typeDecl: map[string]*ast.TypeSpec{},
		funcDecl: map[string]*ast.FuncDecl{},
		methods:  map[string]*ast.FuncDecl{},
		fields:   map[string][]ast.Expr{},
		pkgVars:  map[string]ctxPkgVar{},
		declFile: map[ast.Node]*ast.File{},

		declTypes: map[string][]ast.Expr{},
		declInits: map[string][]ast.Expr{},
	}

	env.index(g.file)
	for _, sibling := range siblings {
		env.files = append(env.files, sibling.file)
		env.index(sibling.file)
		for _, name := range sibling.names {
			if name != "." && !slices.Contains(env.pkgNames, name) {
				env.pkgNames = append(env.pkgNames, name)
			}
		}
	}

	ast.Inspect(g.file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, f := range st.Fields.List {
			if len(f.Names) == 0 {
				name := ctxEmbeddedName(f.Type)
				env.fields[name] = append(env.fields[name], f.Type)
				continue
			}
			for _, id := range f.Names {
				env.fields[id.Name] = append(env.fields[id.Name], f.Type)
			}
		}
		return true
	})
	env.indexDecls()

	return env
}

// index adds the type, function and method declarations of file to the
// environment. A declaration that is known already stays: the file that is
// migrated comes first.
func (env *ctxEnv) index(file *ast.File) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					if env.typeDecl[sp.Name.Name] == nil {
						env.typeDecl[sp.Name.Name] = sp
						env.declFile[sp] = file
					}
				case *ast.ValueSpec:
					if d.Tok != token.VAR {
						continue
					}
					for i, id := range sp.Names {
						if _, known := env.pkgVars[id.Name]; !known {
							env.pkgVars[id.Name] = ctxPkgVar{spec: sp, index: i}
							env.declFile[sp] = file
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv != nil {
				recv := ctxTypeName(ctxFirstFieldType(d.Recv))
				if key := recv + "." + d.Name.Name; recv != "" && env.methods[key] == nil {
					env.methods[key] = d
					env.declFile[d] = file
				}
			} else if env.funcDecl[d.Name.Name] == nil {
				env.funcDecl[d.Name.Name] = d
				env.declFile[d] = file
			}
		}
	}
}

// ctxDerefType returns the type a pointer type points to, the type itself for any
// other.
func ctxDerefType(t ast.Expr) ast.Expr {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		default:
			return t
		}
	}
}

// ctxFirstFieldType returns the type of the first field of fl, nil if there is none.
func ctxFirstFieldType(fl *ast.FieldList) ast.Expr {
	if fl == nil || len(fl.List) == 0 {
		return nil
	}
	return fl.List[0].Type
}

// ctxTypeName returns the name of a type declared by name, looking through
// pointers, parentheses and type arguments. It is empty for anything else,
// including types of other packages.
func ctxTypeName(t ast.Expr) string {
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return ctxTypeName(x.X)
	case *ast.ParenExpr:
		return ctxTypeName(x.X)
	case *ast.IndexExpr:
		return ctxTypeName(x.X)
	case *ast.IndexListExpr:
		return ctxTypeName(x.X)
	}
	return ""
}

// ctxEmbeddedName returns the field name an embedded field of type t gets.
func ctxEmbeddedName(t ast.Expr) string {
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.StarExpr:
		return ctxEmbeddedName(x.X)
	case *ast.IndexExpr:
		return ctxEmbeddedName(x.X)
	case *ast.IndexListExpr:
		return ctxEmbeddedName(x.X)
	}
	return ""
}

// ctxIsFiberType reports whether e names the type typeName of the fiber package,
// behind any pointers: fiber.Ctx, *f.Ctx or, in a file that dot-imports fiber,
// Ctx. names are the identifiers the file binds to the package.
func ctxIsFiberType(e ast.Expr, names []string, typeName string) bool {
	if isFiberType(e, names, typeName) {
		return true
	}
	if !slices.Contains(names, ".") {
		return false
	}

	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.Ident:
			return x.Name == typeName
		default:
			return false
		}
	}
}

// ctxQualifier returns the prefix that spells a fiber identifier in a file that
// binds the fiber package to names: "fiber.", "f.", or "" for a dot import.
func ctxQualifier(names []string) string {
	for _, name := range names {
		if name != "." {
			return name + "."
		}
	}
	if len(names) > 0 {
		return ""
	}
	return "fiber."
}

// qual returns the spelling of the fiber identifier name for the code of a
// rewrite that goes in at pos: a file that imports no fiber package gets the
// import added, and so does a file whose own name for the package means something
// else at pos (see finish).
func (env *ctxEnv) qual(pos token.Pos, name string) string {
	env.usedFiber = true
	return env.prefixAt(pos, name) + name
}

// spell returns the file's spelling of the fiber identifier name for a message,
// which asks for no import.
func (env *ctxEnv) spell(name string) string {
	return env.prefix() + name
}

// prefix is the qualifier of the fiber package in the file: the name the file
// binds it to or, when it imports none, the name the import will get.
func (env *ctxEnv) prefix() string {
	if len(env.names) == 0 && env.alias != "" {
		return env.alias + "."
	}
	return ctxQualifier(env.names)
}

// prefixAt is the qualifier of the fiber package for code at pos that spells
// the member: a name of the file for the package that no declaration hides there,
// else the second import that the file gets (see extraName).
func (env *ctxEnv) prefixAt(pos token.Pos, member string) string {
	if len(env.names) == 0 {
		return env.prefix()
	}
	for _, name := range env.names {
		if name != "." && !env.g.hiddenAt(name, pos) {
			return name + "."
		}
	}
	// a dot import names the package by its members
	if env.dotImported() && !env.g.hiddenAt(member, pos) {
		return ""
	}
	return env.extraName() + "."
}

// pkgAt returns the name that code written at pos uses for the standard package at
// path, which is also its name: the file's own name unless a declaration hides it
// there, and then a second import. The import goes in with the migrated source
// (see finish) when the code uses it.
func (env *ctxEnv) pkgAt(pos token.Pos, path string) string {
	return env.pkgAs(pos, path, path, "std"+path)
}

// pkgAs is pkgAt for a package whose name is not its path: def is its name, aliases
// are the names a second import may take.
func (env *ctxEnv) pkgAs(pos token.Pos, path, def string, aliases ...string) string {
	if env.speller == nil {
		env.speller = newPkgSpeller(env.g, env.nameTaken)
	}
	return env.speller.at(pos, path, def, aliases...)
}

// extraName is the name of a second import of the fiber package, for the code
// that goes where the names the file has for it are hidden by a declaration. It
// is not used anywhere in the file, so nothing hides it.
func (env *ctxEnv) extraName() string {
	if env.extra == "" {
		name := ctxFiberDefaultName + "v3"
		for i := 2; env.nameTaken(name); i++ {
			name = ctxFiberDefaultName + "v3_" + strconv.Itoa(i)
		}
		env.extra = name
	}
	return env.extra
}

// dotImported reports whether the file dot-imports fiber.
func (env *ctxEnv) dotImported() bool {
	return slices.Contains(env.names, ".")
}

// typeIs reports whether the type expression t is one of the named types of the
// fiber package, behind any number of pointers. A struct that embeds such a
// type counts too, because it has all its methods.
func (env *ctxEnv) typeIs(t ast.Expr, wanted ...string) ctxKind {
	return env.typeIsAt(t, wanted, 0)
}

func (env *ctxEnv) typeIsAt(t ast.Expr, wanted []string, depth int) ctxKind {
	if t == nil || depth > ctxMaxDepth {
		return ctxUnknown
	}

	switch x := t.(type) {
	case *ast.ParenExpr:
		return env.typeIsAt(x.X, wanted, depth+1)
	case *ast.StarExpr:
		return env.typeIsAt(x.X, wanted, depth+1)
	case *ast.IndexExpr:
		return env.typeIsAt(x.X, wanted, depth+1)
	case *ast.IndexListExpr:
		return env.typeIsAt(x.X, wanted, depth+1)
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return ctxUnknown
		}
		if slices.Contains(env.pkgNames, id.Name) {
			if slices.Contains(wanted, x.Sel.Name) {
				return ctxYes
			}
			return ctxNo
		}
		if key := env.qualifiedKey(x); key != "" {
			// a type of another package of the module: its declaration tells
			if spec := env.typeSpec(key); spec != nil {
				return env.specIs(spec, wanted, depth+1)
			}
			return ctxUnknown
		}
		return ctxNo
	case *ast.Ident:
		if spec := env.typeSpec(env.typeKey(x)); spec != nil {
			return env.specIs(spec, wanted, depth+1)
		}
		switch {
		case env.dotImported() && slices.Contains(wanted, x.Name):
			return ctxYes
		case ctxIsPredeclaredType(x.Name):
			return ctxNo
		}
		return ctxUnknown
	case *ast.StructType:
		return env.embedsIs(x.Fields, wanted, depth)
	case *ast.InterfaceType:
		return env.embedsIs(x.Methods, wanted, depth)
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType:
		return ctxNo
	}
	return ctxUnknown
}

// specIs is typeIs for a type declared in the file.
func (env *ctxEnv) specIs(spec *ast.TypeSpec, wanted []string, depth int) ctxKind {
	switch spec.Type.(type) {
	case *ast.StructType, *ast.InterfaceType:
		return env.typeIsAt(spec.Type, wanted, depth)
	}
	if spec.Assign.IsValid() {
		return env.typeIsAt(spec.Type, wanted, depth)
	}
	return ctxNo
}

// embedsIs reports whether a struct or interface embeds one of the wanted
// fiber types.
func (env *ctxEnv) embedsIs(fields *ast.FieldList, wanted []string, depth int) ctxKind {
	if fields == nil {
		return ctxNo
	}
	result := ctxNo
	for _, f := range fields.List {
		if len(f.Names) > 0 {
			continue
		}
		switch env.typeIsAt(f.Type, wanted, depth+1) {
		case ctxYes:
			return ctxYes
		case ctxUnknown:
			result = ctxUnknown
		case ctxNo:
		}
	}
	return result
}

// typeOf returns the type expression of e, nil when the file does not tell.
// stack are the ancestors of e (see goSource.walk) and decide which
// declaration of a name is in scope; pass nil to resolve only what needs no
// scope. Pointers are not tracked: the type of &x and of *p is the type of x
// and p's element, spelled without the pointer or with it, whichever the
// declaration used.
func (env *ctxEnv) typeOf(e ast.Expr, stack []ast.Node) ast.Expr {
	return env.typeOfAt(e, stack, 0)
}

func (env *ctxEnv) typeOfAt(e ast.Expr, stack []ast.Node, depth int) ast.Expr {
	if depth > ctxMaxDepth {
		return nil
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return env.typeOfAt(x.X, stack, depth+1)
	case *ast.StarExpr:
		return env.typeOfAt(x.X, stack, depth+1)
	case *ast.UnaryExpr:
		switch x.Op {
		case token.AND:
			return env.typeOfAt(x.X, stack, depth+1)
		case token.ARROW:
			return env.elementType(env.typeOfAt(x.X, stack, depth+1))
		default:
		}
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		switch {
		case !ok:
			if t := env.siteTypeOf(x, stack, depth); t != nil {
				return t
			}
			if t := env.pkgVarType(x.Name, depth); t != nil {
				return t
			}
			return env.dotMember(x.Name).valueType()
		case d.typ != nil:
			return d.typ
		case d.init != nil:
			// n, c := two(): c is the second result of the call
			if site, found := ctxResolveSite(x.Name, x.Pos(), stack); found && site.index >= 0 && site.init == d.init {
				return env.resultTypeAt(d.init, site.index, stack, depth+1)
			}
			return env.typeOfAt(d.init, stack, depth+1)
		}
	case *ast.CompositeLit:
		return x.Type
	case *ast.FuncLit:
		return x.Type
	case *ast.TypeAssertExpr:
		return x.Type
	case *ast.IndexExpr:
		return env.elementType(env.typeOfAt(x.X, stack, depth+1))
	case *ast.SelectorExpr:
		return env.fieldTypeOf(x, stack, depth)
	case *ast.CallExpr:
		return env.callTypeOf(x, stack, depth)
	}
	return nil
}

// ctxElementType returns the element type of an array, slice, map or channel type.
func ctxElementType(t ast.Expr) ast.Expr {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.ArrayType:
			return x.Elt
		case *ast.MapType:
			return x.Value
		case *ast.ChanType:
			return x.Value
		case *ast.Ellipsis:
			return x.Elt
		default:
			return nil
		}
	}
}

// siteTypeOf returns the type of a variable that resolveIdent does not follow: the
// value of a range statement, the binding of a type switch.
func (env *ctxEnv) siteTypeOf(id *ast.Ident, stack []ast.Node, depth int) ast.Expr {
	site, ok := ctxResolveSite(id.Name, id.Pos(), stack)
	switch {
	case !ok:
		return nil
	case site.typ != nil:
		return site.typ
	case site.over != nil:
		return env.rangeElemType(env.typeOfAt(site.over, site.stack, depth+1), site.key)
	case site.init != nil && site.index >= 0:
		return env.resultTypeAt(site.init, site.index, site.stack, depth+1)
	case site.init != nil:
		return env.typeOfAt(site.init, site.stack, depth+1)
	}
	return nil
}

// fieldTypeOf returns the type of the field or method value sel selects.
func (env *ctxEnv) fieldTypeOf(sel *ast.SelectorExpr, stack []ast.Node, depth int) ast.Expr {
	// a variable or a function of another package of the module: web.Default
	if mem := env.pkgMember(sel, stack); mem != nil {
		return mem.valueType()
	}

	base := env.typeOfAt(sel.X, stack, depth+1)
	if base == nil {
		// a method expression: holder.get, (*holder).get
		if ft := env.methodExprType(sel); ft != nil {
			return ft
		}
		return env.uniqueFieldType(sel.Sel.Name)
	}

	var found ast.Expr
	if spec := env.typeSpec(env.typeKey(base)); spec == nil {
		// a struct type written where it is used: s *struct{ c *fiber.Ctx }
		found = env.structFieldType(base, sel.Sel.Name, depth+1)
	} else if found = env.structFieldType(spec.Type, sel.Sel.Name, depth+1); found != nil {
		// the fields of kbox[*fiber.Ctx] are those of kbox[T] with the T put in
		found = ctxSubst(found, ctxBindParams(ctxTypeParams(spec.TypeParams), ctxTypeArgs(base)))
	}
	if found != nil {
		return found
	}

	// a method used as a value: get := h.get
	if ft, bind := env.methodFuncType(base, sel.Sel.Name, 0); ft != nil {
		return ctxSubst(ft, bind)
	}
	return nil
}

// methodExprType returns the type of the method expression sel, T.method or
// (*T).method with T a type declared in the file; the receiver is not among the
// parameters of the type, which only serves for the results.
func (env *ctxEnv) methodExprType(sel *ast.SelectorExpr) ast.Expr {
	x := ast.Unparen(sel.X)
	if star, ok := x.(*ast.StarExpr); ok {
		x = ast.Unparen(star.X)
	}
	if env.typeSpec(env.typeKey(x)) != nil {
		if ft, _ := env.methodFuncType(x, sel.Sel.Name, 0); ft != nil {
			return ft
		}
	}
	return nil
}

// uniqueFieldType is the guess for a selector whose base has no known type: the
// type that every struct field of that name in the file is declared with.
func (env *ctxEnv) uniqueFieldType(name string) ast.Expr {
	candidates := env.fields[name]
	if len(candidates) == 0 {
		return nil
	}
	for _, c := range candidates[1:] {
		if env.g.text(c) != env.g.text(candidates[0]) {
			return nil
		}
	}
	return candidates[0]
}

// structFieldType returns the declared type of the field name of the struct
// type t, looking through the fields of embedded structs.
func (env *ctxEnv) structFieldType(t ast.Expr, name string, depth int) ast.Expr {
	if depth > ctxMaxDepth {
		return nil
	}

	switch x := t.(type) {
	case *ast.ParenExpr:
		return env.structFieldType(x.X, name, depth+1)
	case *ast.StarExpr:
		return env.structFieldType(x.X, name, depth+1)
	case *ast.IndexExpr:
		return env.structFieldType(x.X, name, depth+1)
	case *ast.IndexListExpr:
		return env.structFieldType(x.X, name, depth+1)
	case *ast.Ident, *ast.SelectorExpr:
		if spec := env.typeSpec(env.typeKey(x)); spec != nil {
			return env.structFieldType(spec.Type, name, depth+1)
		}
	case *ast.StructType:
		if x.Fields == nil {
			return nil
		}
		for _, f := range x.Fields.List {
			if len(f.Names) == 0 && ctxEmbeddedName(f.Type) == name {
				return f.Type
			}
			for _, id := range f.Names {
				if id.Name == name {
					return f.Type
				}
			}
		}
		for _, f := range x.Fields.List {
			if len(f.Names) > 0 {
				continue
			}
			if found := env.structFieldType(f.Type, name, depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}

// callTypeOf returns the type of the (first) value a call returns.
func (env *ctxEnv) callTypeOf(call *ast.CallExpr, stack []ast.Node, depth int) ast.Expr {
	if results := env.callResults(call, stack, depth); len(results) > 0 {
		return results[0]
	}
	return nil
}

// callResults returns the types of the values a call returns. A conversion
// returns the type it converts to.
func (env *ctxEnv) callResults(call *ast.CallExpr, stack []ast.Node, depth int) []ast.Expr {
	if depth > ctxMaxDepth {
		return nil
	}

	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return env.methodCallResults(call, fun, stack, depth)
	case *ast.Ident:
		return env.identCallResults(call, fun, nil, stack, depth)
	case *ast.IndexExpr:
		// f[int](x), or the conversion box[int](x)
		if results := env.instantiatedCallResults(call, fun.X, []ast.Expr{fun.Index}, stack, depth); results != nil {
			return results
		}
	case *ast.IndexListExpr:
		return env.instantiatedCallResults(call, fun.X, fun.Indices, stack, depth)
	case *ast.FuncLit:
		return ctxResultTypes(fun.Type)
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType:
		return []ast.Expr{fun}
	}

	// the call of a value of a function type that is no name: what another call
	// returns (get()()), an element of a slice or a map (handlers[i](c)), the same in
	// parentheses
	if held := env.funcTypeOf(env.typeOfAt(call.Fun, stack, depth+1), depth+1); held != nil {
		return ctxResultTypes(held)
	}
	if paren, ok := call.Fun.(*ast.ParenExpr); ok {
		// the conversion (*Ctx)(x)
		return []ast.Expr{paren}
	}
	return nil
}

// instantiatedCallResults is callResults for the call of the generic function or
// the conversion to the generic type base, with the type arguments given.
func (env *ctxEnv) instantiatedCallResults(call *ast.CallExpr, base ast.Expr, explicit []ast.Expr, stack []ast.Node, depth int) []ast.Expr {
	switch x := base.(type) {
	case *ast.Ident:
		return env.identCallResults(call, x, explicit, stack, depth)
	case *ast.SelectorExpr:
		return env.memberResults(env.pkgMember(x, stack), call, explicit, stack, depth)
	}
	return nil
}

// identCallResults is callResults for the call of the name id. explicit are the
// type arguments written after it.
func (env *ctxEnv) identCallResults(call *ast.CallExpr, id *ast.Ident, explicit []ast.Expr, stack []ast.Node, depth int) []ast.Expr {
	// a variable that holds a function: get := func() *fiber.Ctx { ... }
	if explicit == nil {
		if held := env.funcTypeOf(env.typeOfAt(id, stack, depth+1), depth+1); held != nil {
			return ctxResultTypes(held)
		}
	}

	switch {
	case env.funcDecl[id.Name] != nil:
		return env.funcResults(env.funcDeclOf(id.Name).Type, call, explicit, stack, depth)
	case env.typeDecl[id.Name] != nil:
		return []ast.Expr{call.Fun}
	case id.Name == mw3BuiltinNew && len(call.Args) == 1:
		return []ast.Expr{call.Args[0]}
	case id.Name == mw3BuiltinMake && len(call.Args) > 0:
		return []ast.Expr{call.Args[0]}
	case id.Name == mw3BuiltinAppend && len(call.Args) > 0:
		return []ast.Expr{env.typeOfAt(call.Args[0], stack, depth+1)}
	}
	return env.memberResults(env.dotMember(id.Name), call, explicit, stack, depth)
}

// memberResults is callResults for the call of a function of another package of
// the module, or the conversion to one of its types. It is nil when the member is
// no function or type.
func (env *ctxEnv) memberResults(mem *ctxMember, call *ast.CallExpr, explicit []ast.Expr, stack []ast.Node, depth int) []ast.Expr {
	switch {
	case mem == nil:
		return nil
	case mem.fn != nil:
		return env.funcResults(mem.fn.Type, call, explicit, stack, depth)
	case mem.spec != nil:
		return []ast.Expr{call.Fun}
	}
	return nil
}

// methodCallResults is callResults for a call of a selector.
func (env *ctxEnv) methodCallResults(call *ast.CallExpr, fun *ast.SelectorExpr, stack []ast.Node, depth int) []ast.Expr {
	// a function of another package of the module: ctxutil.From(c)
	if mem := env.pkgMember(fun, stack); mem != nil {
		if results := env.memberResults(mem, call, nil, stack, depth); results != nil {
			return results
		}
		if mem.typ != nil {
			// a variable that holds a function
			if held := env.funcTypeOf(mem.typ, depth+1); held != nil {
				return ctxResultTypes(held)
			}
		}
		return nil
	}

	// a method of a type of the file, of an interface, or one that a type embeds: what
	// it declares comes before what the name of one of Fiber's methods suggests
	if recv := env.typeOfAt(fun.X, stack, depth+1); recv != nil {
		if ft, bind := env.methodFuncType(recv, fun.Sel.Name, 0); ft != nil {
			results := ctxResultTypes(ft)
			for i, r := range results {
				results[i] = ctxSubst(r, bind)
			}
			return results
		}
	}

	switch name := fun.Sel.Name; {
	case name == "AcquireCtx":
		return []ast.Expr{ctxFiberRef(ctxTypeCtx)}
	case name == appMethodGroup && len(call.Args) > 0:
		return []ast.Expr{ctxFiberRef(ctxTypeGroup)}
	case name == mw2New && env.isFiberPackage(fun.X):
		return []ast.Expr{ctxFiberRef(ctxTypeApp)}
	case slices.Contains(ctxChainMethods, name):
		if env.typeIs(env.typeOfAt(fun.X, stack, depth+1), ctxTypeCtx) == ctxYes {
			return []ast.Expr{ctxFiberRef(ctxTypeCtx)}
		}
	}

	// a field that holds a function: s.getCtx()
	if held := env.funcTypeOf(env.fieldTypeOf(fun, stack, depth), depth+1); held != nil {
		return ctxResultTypes(held)
	}
	return nil
}

// funcTypeOf returns the function type that t stands for: one written out, or a
// type declared by name as one. It is nil for any other type.
func (env *ctxEnv) funcTypeOf(t ast.Expr, depth int) *ast.FuncType {
	for ; depth <= ctxMaxDepth; depth++ {
		switch x := t.(type) {
		case *ast.FuncType:
			return x
		case *ast.ParenExpr:
			t = x.X
		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
			spec := env.typeSpec(env.typeKey(x))
			if spec == nil {
				return nil
			}
			t = ctxSubst(spec.Type, ctxBindParams(ctxTypeParams(spec.TypeParams), ctxTypeArgs(x)))
		default:
			return nil
		}
	}
	return nil
}

// isFiberPackage reports whether e names the fiber package.
func (env *ctxEnv) isFiberPackage(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && slices.Contains(env.names, id.Name)
}

// receiverKind says whether the method sel selects is a method of the fiber
// request context. A struct that embeds the context but declares a method or a
// field of the same name hides the one of the context: its calls are its own.
// The same goes for the names the rewrite of the call introduces.
func (env *ctxEnv) receiverKind(sel *ast.SelectorExpr, stack []ast.Node, introduced ...string) ctxKind {
	t := env.typeOf(sel.X, stack)
	kind := env.typeIs(t, ctxTypeCtx)
	if kind != ctxYes {
		return kind
	}

	if name := env.typeKey(t); name != "" {
		if env.declares(name, sel.Sel.Name) || slices.ContainsFunc(introduced, func(n string) bool { return env.declares(name, n) }) {
			return ctxNo
		}
	}
	return ctxYes
}

// receiverKindVia is receiverKind for a rewrite that can go through the embedded
// context. A type that embeds the context and declares a member of the name the
// rewrite introduces hides it from calls on the type itself (a wrapper with a
// Redirect(to string) of its own hides the Redirect() of v3), but the context it
// embeds still has it: for such a call the kind is ctxYes and via is the name of
// the embedded field, which the rewrite puts after the receiver (c.Ctx.Redirect()).
// via is empty when the call needs no such detour.
func (env *ctxEnv) receiverKindVia(sel *ast.SelectorExpr, stack []ast.Node, introduced ...string) (kind ctxKind, via string) {
	kind = env.receiverKind(sel, stack, introduced...)
	if kind != ctxNo {
		return kind, ""
	}

	if name := env.typeKey(env.typeOf(sel.X, stack)); name == "" || env.declares(name, sel.Sel.Name) {
		return ctxNo, ""
	}
	if via = env.embeddedCtxOf(sel.X, stack); via == "" {
		return ctxNo, ""
	}
	return ctxYes, via
}

// embeddedCtxOf returns the path of embedded fields (Ctx, or Base.Ctx) through
// which x, a value of a struct type declared in the file, reaches the fiber
// context it embeds. It is empty when x is no such value.
func (env *ctxEnv) embeddedCtxOf(x ast.Expr, stack []ast.Node) string {
	t := env.typeOf(x, stack)
	name := env.typeKey(t)
	if name == "" || env.typeIs(t, ctxTypeCtx) != ctxYes {
		return ""
	}

	path := env.embeddedCtxPath(name, 0)
	if ctxIsForeignKey(name) && slices.ContainsFunc(path, func(field string) bool { return !appIsExported(field) }) {
		// a field of a type of another package that this one can not name
		return ""
	}
	return strings.Join(path, ".")
}

// embeddedCtxVia is embeddedCtxOf for code that calls members of the context
// through x: the path is empty unless the type of x declares one of the members
// itself, because only then x is not the context for that code. ctxMembersAll
// asks for the path whatever the type declares.
func (env *ctxEnv) embeddedCtxVia(x ast.Expr, stack []ast.Node, members []string) string {
	if slices.Equal(members, ctxMembersAll) {
		return env.embeddedCtxOf(x, stack)
	}

	name := env.typeKey(env.typeOf(x, stack))
	if name == "" || !slices.ContainsFunc(members, func(m string) bool { return env.declares(name, m) }) {
		return ""
	}
	return env.embeddedCtxOf(x, stack)
}

// embeddedCtxPath returns the names of the embedded fields through which the
// struct type declared in the file reaches the fiber context ({"Ctx"} for a type
// that embeds it, {"Base", "Ctx"} for a type that embeds one that does), nil when
// it does not embed it.
func (env *ctxEnv) embeddedCtxPath(typeName string, depth int) []string {
	spec := env.typeSpec(typeName)
	if spec == nil || depth > ctxMaxDepth {
		return nil
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return nil
	}

	for _, f := range st.Fields.List {
		if len(f.Names) > 0 {
			continue
		}
		if ctxIsFiberType(f.Type, env.pkgNames, ctxTypeCtx) || env.isCtxAlias(ast.Unparen(ctxDerefType(f.Type)), nil) {
			return []string{ctxEmbeddedName(f.Type)}
		}
	}
	for _, f := range st.Fields.List {
		inner := env.typeKey(f.Type)
		if len(f.Names) > 0 || inner == "" || inner == typeName {
			continue
		}
		if path := env.embeddedCtxPath(inner, depth+1); path != nil {
			return append([]string{ctxEmbeddedName(f.Type)}, path...)
		}
	}
	return nil
}

// declares reports whether the type declared in the file has a method or a field
// called name, its own or one it gets from a struct of the file it embeds.
func (env *ctxEnv) declares(typeName, name string) bool {
	return env.declaresAt(typeName, name, 0)
}

func (env *ctxEnv) declaresAt(typeName, name string, depth int) bool {
	if depth > ctxMaxDepth {
		return false
	}
	if env.methodDecl(typeName, name) != nil {
		return true
	}
	spec := env.typeSpec(typeName)
	if spec == nil {
		return false
	}
	st, ok := spec.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return false
	}

	return slices.ContainsFunc(st.Fields.List, func(f *ast.Field) bool {
		if len(f.Names) > 0 {
			return slices.ContainsFunc(f.Names, func(id *ast.Ident) bool { return id.Name == name })
		}
		// An embedded field is called like its type, and promotes what the type declares.
		if ctxEmbeddedName(f.Type) == name {
			return true
		}
		inner := env.typeKey(f.Type)
		return inner != "" && inner != typeName && env.declaresAt(inner, name, depth+1)
	})
}

// indexDecls records the types and initializing expressions of the declarations
// of the file by the name they declare.
func (env *ctxEnv) indexDecls() {
	ast.Inspect(env.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			for _, id := range x.Names {
				env.declTypes[id.Name] = append(env.declTypes[id.Name], x.Type)
			}
			if len(x.Names) == 0 {
				name := ctxEmbeddedName(x.Type)
				env.declTypes[name] = append(env.declTypes[name], x.Type)
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if x.Type != nil {
					env.declTypes[id.Name] = append(env.declTypes[id.Name], x.Type)
				}
				env.declInits[id.Name] = ctxAppendRHS(env.declInits[id.Name], x.Values, len(x.Names), i)
			}
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				if name := ctxAssignedName(lhs); name != "" {
					env.declInits[name] = ctxAppendRHS(env.declInits[name], x.Rhs, len(x.Lhs), i)
				}
			}
		}
		return true
	})
}

// ctxAssignedName returns the name an assignment target assigns: x for x and
// for the field in s.x.
func ctxAssignedName(lhs ast.Expr) string {
	switch x := lhs.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

// ctxAppendRHS appends the expression assigned to the i-th of n targets.
func ctxAppendRHS(dst, values []ast.Expr, n, i int) []ast.Expr {
	switch {
	case len(values) == n:
		return append(dst, values[i])
	case len(values) == 1:
		return append(dst, values[0])
	}
	return dst
}
