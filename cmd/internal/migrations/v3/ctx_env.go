package v3

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"sync"
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
	// names are the identifiers the file binds to the core fiber package.
	names []string
}

// newCtxEnv indexes the declarations of g. names are the identifiers the file
// binds to the fiber package (see fiberNames).
func newCtxEnv(g *goSource, names []string) *ctxEnv {
	env := &ctxEnv{
		g:        g,
		names:    names,
		typeDecl: map[string]*ast.TypeSpec{},
		funcDecl: map[string]*ast.FuncDecl{},
		methods:  map[string]*ast.FuncDecl{},
		fields:   map[string][]ast.Expr{},
	}

	for _, decl := range g.file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					env.typeDecl[ts.Name.Name] = ts
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil {
				env.funcDecl[d.Name.Name] = d
			} else if recv := ctxTypeName(ctxFirstFieldType(d.Recv)); recv != "" {
				env.methods[recv+"."+d.Name.Name] = d
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

	return env
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

// qual returns the file's spelling of the fiber identifier name.
func (env *ctxEnv) qual(name string) string {
	return ctxQualifier(env.names) + name
}

// fiberType returns the AST of the fiber type name, as the file spells it.
func (env *ctxEnv) fiberType(name string) ast.Expr {
	prefix := ctxQualifier(env.names)
	if prefix == "" {
		return ast.NewIdent(name)
	}
	return &ast.SelectorExpr{X: ast.NewIdent(prefix[:len(prefix)-1]), Sel: ast.NewIdent(name)}
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
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return ctxUnknown
		}
		if slices.Contains(env.names, id.Name) && slices.Contains(wanted, x.Sel.Name) {
			return ctxYes
		}
		return ctxNo
	case *ast.Ident:
		if spec := env.typeDecl[x.Name]; spec != nil {
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
		if x.Op == token.AND {
			return env.typeOfAt(x.X, stack, depth+1)
		}
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		switch {
		case !ok:
		case d.typ != nil:
			return d.typ
		case d.init != nil:
			return env.typeOfAt(d.init, stack, depth+1)
		}
	case *ast.CompositeLit:
		return x.Type
	case *ast.TypeAssertExpr:
		return x.Type
	case *ast.IndexExpr:
		return ctxElementType(env.typeOfAt(x.X, stack, depth+1))
	case *ast.SelectorExpr:
		return env.fieldTypeOf(x, stack, depth)
	case *ast.CallExpr:
		return env.callTypeOf(x, stack, depth)
	}
	return nil
}

// ctxElementType returns the element type of an array, slice or map type.
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
		default:
			return nil
		}
	}
}

// fieldTypeOf returns the type of the field or method value sel selects.
func (env *ctxEnv) fieldTypeOf(sel *ast.SelectorExpr, stack []ast.Node, depth int) ast.Expr {
	base := env.typeOfAt(sel.X, stack, depth+1)
	if base == nil {
		return env.uniqueFieldType(sel.Sel.Name)
	}

	name := ctxTypeName(base)
	spec := env.typeDecl[name]
	if spec == nil {
		return nil
	}
	return env.structFieldType(spec.Type, sel.Sel.Name, depth+1)
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
	case *ast.Ident:
		if spec := env.typeDecl[x.Name]; spec != nil {
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
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return env.methodCallType(call, fun, stack, depth)
	case *ast.Ident:
		switch {
		case env.funcDecl[fun.Name] != nil:
			return ctxFirstFieldType(env.funcDecl[fun.Name].Type.Results)
		case env.typeDecl[fun.Name] != nil:
			return fun
		case fun.Name == "new" && len(call.Args) == 1:
			return call.Args[0]
		}
	case *ast.ParenExpr, *ast.StarExpr, *ast.ArrayType, *ast.MapType:
		return fun
	}
	return nil
}

// methodCallType is callTypeOf for a call of a selector.
func (env *ctxEnv) methodCallType(call *ast.CallExpr, fun *ast.SelectorExpr, stack []ast.Node, depth int) ast.Expr {
	switch name := fun.Sel.Name; {
	case name == "AcquireCtx":
		return env.fiberType(ctxTypeCtx)
	case name == "Group" && len(call.Args) > 0:
		return env.fiberType(ctxTypeGroup)
	case name == "New" && env.isFiberPackage(fun.X):
		return env.fiberType(ctxTypeApp)
	case slices.Contains(ctxChainMethods, name):
		if env.typeIs(env.typeOfAt(fun.X, stack, depth+1), ctxTypeCtx) == ctxYes {
			return env.fiberType(ctxTypeCtx)
		}
	}

	if recv := ctxTypeName(env.typeOfAt(fun.X, stack, depth+1)); recv != "" {
		if method := env.methods[recv+"."+fun.Sel.Name]; method != nil {
			return ctxFirstFieldType(method.Type.Results)
		}
	}
	return nil
}

// isFiberPackage reports whether e names the fiber package.
func (env *ctxEnv) isFiberPackage(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && slices.Contains(env.names, id.Name)
}

// ctxKindOf says whether e is a fiber request context.
func (env *ctxEnv) ctxKindOf(e ast.Expr, stack []ast.Node) ctxKind {
	return env.typeIs(env.typeOf(e, stack), ctxTypeCtx)
}

// declsNamed returns the types and the initializing expressions of every
// declaration of an identifier called name in the file, in any scope, for
// parameters, struct fields, var declarations and assignments.
func (env *ctxEnv) declsNamed(name string) (typs, inits []ast.Expr) {
	ast.Inspect(env.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			for _, id := range x.Names {
				if id.Name == name {
					typs = append(typs, x.Type)
				}
			}
			if len(x.Names) == 0 && ctxEmbeddedName(x.Type) == name {
				typs = append(typs, x.Type)
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if id.Name != name {
					continue
				}
				if x.Type != nil {
					typs = append(typs, x.Type)
				}
				inits = ctxAppendRHS(inits, x.Values, len(x.Names), i)
			}
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				if ctxAssignedName(lhs) == name {
					inits = ctxAppendRHS(inits, x.Rhs, len(x.Lhs), i)
				}
			}
		}
		return true
	})
	return typs, inits
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

// ctxEnvCache remembers the environment of the file the helpers of common.go
// looked at last: they are called with the same source for every candidate of
// a file.
var ctxEnvCache struct {
	env *ctxEnv
	src string
	sync.Mutex
}

// ctxEnvFor returns the environment of src, nil when src does not parse. A file
// without a fiber import is treated as if it imported the package as "fiber",
// so that the snippets the helpers are given without an import clause keep
// working.
func ctxEnvFor(src string) *ctxEnv {
	ctxEnvCache.Lock()
	defer ctxEnvCache.Unlock()

	if ctxEnvCache.env != nil && ctxEnvCache.src == src {
		return ctxEnvCache.env
	}

	g, ok := parseSource(src)
	if !ok {
		// A snippet that is only a part of a file still has declarations.
		g, ok = parseSource("package p\n" + src)
		if !ok {
			return nil
		}
	}
	names := fiberNames(src)
	if len(names) == 0 {
		names = []string{"fiber"}
	}

	ctxEnvCache.env = newCtxEnv(g, names)
	ctxEnvCache.src = src
	return ctxEnvCache.env
}

// ctxFileDeclares reports whether some declaration of ident in src has one of
// the wanted fiber types, or is initialized with a value of one.
func ctxFileDeclares(src, ident string, wanted ...string) bool {
	env := ctxEnvFor(src)
	if env == nil {
		return false
	}

	typs, inits := env.declsNamed(ident)
	for _, t := range typs {
		if env.typeIs(t, wanted...) == ctxYes {
			return true
		}
	}
	for _, init := range inits {
		if env.typeIs(env.typeOf(init, nil), wanted...) == ctxYes {
			return true
		}
	}
	return false
}

// ctxTypeString spells the type t without its pointers.
func ctxTypeString(t ast.Expr) string {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		default:
			return types.ExprString(t)
		}
	}
}

// ctxSpellings returns the ways the source can spell the type typ. A type of the
// fiber package, given as "fiber.X", is spelled with the names the source has for
// the package.
func ctxSpellings(env *ctxEnv, typ string) []string {
	typ = strings.TrimLeft(typ, "*")
	rest, ok := strings.CutPrefix(typ, "fiber.")
	if !ok {
		return []string{typ}
	}

	spellings := make([]string, 0, len(env.names))
	for _, name := range env.names {
		if name == "." {
			spellings = append(spellings, rest)
			continue
		}
		spellings = append(spellings, name+"."+rest)
	}
	return spellings
}
