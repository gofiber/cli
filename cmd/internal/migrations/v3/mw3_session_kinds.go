package v3

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// The code is v2 and has no type information, so what a value is can only be
// read from what the source says about it: the type a variable or a field is
// declared with, the literal or the call that initialized it, the type a
// function returns. mw3Kind is the part of that which matters to the session
// migrations: a store, a session, a config, a collection of them, or a type of
// the program that may hold some. The types and the functions of the other
// packages of the module are read through the module index.

// mw3Base is what a value is, as far as the session middleware goes.
type mw3Base uint8

const (
	// mw3KindNone is anything the source does not say enough about, or that has
	// nothing to do with sessions.
	mw3KindNone mw3Base = iota
	// mw3KindStore is a *session.Store, mw3KindSession a *session.Session and
	// mw3KindConfig a session.Config.
	mw3KindStore
	mw3KindSession
	mw3KindConfig
	// mw3KindNamed is a type that the program declares, by name.
	mw3KindNamed
	// mw3KindList is an array, a slice or a map: its elem holds the kind of what it
	// holds (of the values, for a map).
	mw3KindList
)

// mw3Kind tells what a value is.
type mw3Kind struct {
	elem *mw3Kind
	// name is the name of the type of a mw3KindNamed. A mw3KindConfig that is the
	// config a store embeds has mw3StoreType here: a store always has a lookup.
	name string
	// pkg is the import path of the package that declares a mw3KindNamed, empty for
	// the package the kind is read in.
	pkg  string
	base mw3Base
}

// mw3MaxKindDepth bounds how far the source is followed to tell what a value is.
const mw3MaxKindDepth = 12

// The types of the session package that matter here, and the builtins that make a
// value.
const (
	mw3StoreType     = "Store"
	mw3SessionType   = "Session"
	mw3BuiltinAppend = "append"
	mw3BuiltinMake   = "make"
	mw3BuiltinNew    = "new"
)

// mw3ListOf is the kind of a collection of elem, which is nothing when the
// collection holds nothing of interest.
func mw3ListOf(elem mw3Kind) mw3Kind {
	if elem.base == mw3KindNone {
		return mw3Kind{}
	}
	return mw3Kind{base: mw3KindList, elem: &elem}
}

// mw3Qualify makes a kind that was read in the package at pkg refer to the types
// of that package by their path, so that it can be used from another package.
func mw3Qualify(k mw3Kind, pkg string) mw3Kind {
	switch {
	case pkg == "":
	case k.base == mw3KindNamed && k.pkg == "":
		k.pkg = pkg
	case k.base == mw3KindList && k.elem != nil:
		elem := mw3Qualify(*k.elem, pkg)
		k.elem = &elem
	}
	return k
}

// mw3Env is what a file says about the names it uses.
type mw3Env struct {
	ix *moduleIndex
	// session are the identifiers the file binds to the session package.
	session []string
	// modules are the imports of the file that name packages of the module.
	modules []mw3ModuleImport
}

// mw3ModuleImport is an import of a package of the module.
type mw3ModuleImport struct {
	// alias is the name written in the import, empty when there is none.
	alias string
	path  string
}

// isSession reports whether the identifier is one the file binds to the session
// package. Whether a variable of the same name hides it is for the caller to say.
func (e *mw3Env) isSession(name string) bool {
	return e != nil && slices.Contains(e.session, name)
}

// module returns the import path of the package of the module that the file binds
// to the identifier.
func (e *mw3Env) module(name string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, imp := range e.modules {
		bound := imp.alias
		if bound == "" {
			// The package may be called otherwise than the last element of its
			// path, which only reading it tells.
			if defaultImportName(imp.path) == name {
				return imp.path, true
			}
			if pkg := e.ix.packageAt(imp.path); pkg != nil {
				bound = pkg.name
			}
		}
		if bound == name {
			return imp.path, true
		}
	}
	return "", false
}

// mw3KindOfType reads the kind from a type expression written in a file whose
// names are env.
func mw3KindOfType(t ast.Expr, env *mw3Env) mw3Kind {
	switch x := t.(type) {
	case *ast.ParenExpr:
		return mw3KindOfType(x.X, env)
	case *ast.StarExpr:
		return mw3KindOfType(x.X, env)
	case *ast.IndexExpr:
		return mw3KindOfType(x.X, env) // a generic type, instantiated
	case *ast.IndexListExpr:
		return mw3KindOfType(x.X, env)
	case *ast.Ellipsis:
		return mw3ListOf(mw3KindOfType(x.Elt, env))
	case *ast.ArrayType:
		return mw3ListOf(mw3KindOfType(x.Elt, env))
	case *ast.MapType:
		return mw3ListOf(mw3KindOfType(x.Value, env))
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return mw3Kind{}
		}
		if env.isSession(id.Name) {
			switch x.Sel.Name {
			case mw3StoreType:
				return mw3Kind{base: mw3KindStore}
			case mw3SessionType:
				return mw3Kind{base: mw3KindSession}
			case mw3Config:
				return mw3Kind{base: mw3KindConfig}
			}
			return mw3Kind{}
		}
		if path, inModule := env.module(id.Name); inModule {
			return mw3Kind{base: mw3KindNamed, name: x.Sel.Name, pkg: path}
		}
	case *ast.Ident:
		if types.Universe.Lookup(x.Name) == nil {
			return mw3Kind{base: mw3KindNamed, name: x.Name}
		}
	}
	return mw3Kind{}
}

// mw3PkgVar is a variable of the package level, with what says what it holds.
type mw3PkgVar struct {
	typ  ast.Expr
	init ast.Expr
	// env is what the file that declares it says about its names.
	env *mw3Env
}

// mw3Struct is a struct or interface type of the package: the kinds of its
// fields, and of the types it embeds, whose fields it has as well.
type mw3Struct struct {
	fields map[string]mw3Kind
	// own are the names of all the fields of a struct and the methods of an
	// interface, those of the embedded fields included.
	own      map[string]struct{}
	embedded []mw3Kind
	// opaque is set when the type embeds one that the source does not show: a type
	// of a package outside of the module.
	opaque bool
}

// mw3SessionFacts is what the files of a package say about what holds sessions,
// which a file of the package may use without declaring it.
type mw3SessionFacts struct {
	owner *mw3Pkg
	vars  map[string]mw3PkgVar
	// varKinds are the kinds of the variables, read when they are first asked for.
	varKinds map[string]mw3Kind
	// funcs and methods (by the name of the type) are what the first result of a
	// function is.
	funcs   map[string]mw3Kind
	methods map[string]map[string]mw3Kind
	// methodNames are the names of all the methods of a type, whatever they return.
	methodNames map[string]map[string]struct{}
	structs     map[string]mw3Struct
	// named are the types that are not structs and stand for something of interest.
	named map[string]mw3Kind
	// funcDecls are the functions of the package that return a config, and
	// methodDecls its methods, by the name of the type.
	funcDecls   map[string]mw3FuncDecl
	methodDecls map[string]map[string]mw3FuncDecl
	// ifaces are the interface types of the package.
	ifaces map[string]mw3Iface
	// mutated tells, by the name of a variable of the package level, whether the
	// package assigns to it or to its lookup, read when first asked for.
	mutated map[string]bool
	// path is the import path of the package, empty outside of a module.
	path string
}

// mw3FuncDecl is the declaration of a function, with what the file that declares
// it says about its names.
type mw3FuncDecl struct {
	decl *ast.FuncDecl
	env  *mw3Env
}

func newMw3SessionFacts(owner *mw3Pkg) mw3SessionFacts {
	return mw3SessionFacts{
		owner:       owner,
		vars:        map[string]mw3PkgVar{},
		varKinds:    map[string]mw3Kind{},
		funcs:       map[string]mw3Kind{},
		methods:     map[string]map[string]mw3Kind{},
		methodNames: map[string]map[string]struct{}{},
		structs:     map[string]mw3Struct{},
		named:       map[string]mw3Kind{},
		funcDecls:   map[string]mw3FuncDecl{},
		methodDecls: map[string]map[string]mw3FuncDecl{},
		ifaces:      map[string]mw3Iface{},
		mutated:     map[string]bool{},
	}
}

// scan reads the package level declarations of a file of the package.
func (f *mw3SessionFacts) scan(file *ast.File, env *mw3Env) {
	if f.path == "" && !strings.HasSuffix(f.owner.name, "_test") {
		// The external tests of a package are in its directory, and nobody imports them.
		f.path, _ = f.owner.index().importPathOf(f.owner.dir)
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			f.scanFunc(d, env)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					if d.Tok == token.VAR {
						f.scanVar(s, env)
					}
				case *ast.TypeSpec:
					f.scanType(s, env)
				}
			}
		}
	}
}

// addMethod records the name of a method of a type.
func (f *mw3SessionFacts) addMethod(typ, name string) {
	if f.methodNames[typ] == nil {
		f.methodNames[typ] = map[string]struct{}{}
	}
	f.methodNames[typ][name] = struct{}{}
}

// scanFunc records what a function or a method returns.
func (f *mw3SessionFacts) scanFunc(d *ast.FuncDecl, env *mw3Env) {
	recv := ""
	if d.Recv != nil && len(d.Recv.List) == 1 {
		if k := mw3KindOfType(d.Recv.List[0].Type, nil); k.base == mw3KindNamed {
			recv = k.name
			f.addMethod(recv, d.Name.Name)
			if f.methodDecls[recv] == nil {
				f.methodDecls[recv] = map[string]mw3FuncDecl{}
			}
			f.methodDecls[recv][d.Name.Name] = mw3FuncDecl{decl: d, env: env}
		}
	}

	if d.Type.Results == nil {
		return
	}
	kind := mw3FirstResult(d.Type, env)
	if kind.base == mw3KindNone {
		return
	}

	if d.Recv == nil {
		f.funcs[d.Name.Name] = kind
		if kind.base == mw3KindConfig {
			f.funcDecls[d.Name.Name] = mw3FuncDecl{decl: d, env: env}
		}
		return
	}
	if recv == "" {
		return
	}
	if f.methods[recv] == nil {
		f.methods[recv] = map[string]mw3Kind{}
	}
	f.methods[recv][d.Name.Name] = kind
}

// mw3FirstResult returns the kind of what a function returns: its first result
// that is something of interest, or else its first result.
func mw3FirstResult(ft *ast.FuncType, env *mw3Env) mw3Kind {
	if ft.Results == nil {
		return mw3Kind{}
	}
	var first mw3Kind
	for i, res := range ft.Results.List {
		kind := mw3KindOfType(res.Type, env)
		if i == 0 {
			first = kind
		}
		if mw3IsSessionKind(kind) {
			return kind
		}
	}
	return first
}

// mw3IsSessionKind reports whether the kind is a store, a session or a config.
func mw3IsSessionKind(k mw3Kind) bool {
	return k.base == mw3KindStore || k.base == mw3KindSession || k.base == mw3KindConfig
}

// scanVar records what a package level variable is declared with.
func (f *mw3SessionFacts) scanVar(s *ast.ValueSpec, env *mw3Env) {
	for i, id := range s.Names {
		v := mw3PkgVar{typ: s.Type, env: env}
		switch {
		case len(s.Values) == len(s.Names):
			v.init = s.Values[i]
		case len(s.Values) == 1:
			v.init = s.Values[0]
		}
		f.vars[id.Name] = v
	}
}

// scanType records a struct or interface type, or another type that stands for
// something of interest.
func (f *mw3SessionFacts) scanType(spec *ast.TypeSpec, env *mw3Env) {
	switch t := spec.Type.(type) {
	case *ast.StructType:
		f.scanStruct(spec.Name.Name, t, env)
	case *ast.InterfaceType:
		f.scanInterface(spec.Name.Name, t, env)
	default:
		// An alias is the type it stands for. A type that is defined from another
		// has the fields of it and none of its methods, which is nothing the
		// migrations could tell, unless it is a collection: Stores []*session.Store.
		kind := mw3KindOfType(spec.Type, env)
		if kind.base == mw3KindList || (kind.base != mw3KindNone && spec.Assign != token.NoPos) {
			f.named[spec.Name.Name] = kind
		}
	}
}

// scanStruct records the fields of a struct type.
func (f *mw3SessionFacts) scanStruct(name string, st *ast.StructType, env *mw3Env) {
	s := mw3Struct{fields: map[string]mw3Kind{}, own: map[string]struct{}{}}
	for _, field := range st.Fields.List {
		kind := mw3KindOfType(field.Type, env)
		if len(field.Names) > 0 {
			for _, id := range field.Names {
				s.own[id.Name] = struct{}{}
				if kind.base != mw3KindNone {
					s.fields[id.Name] = kind
				}
				f.scanFuncField(name, id.Name, field.Type, env)
			}
			continue
		}

		// An embedded field is called after its type, and has the fields of it.
		embedded := mw3EmbeddedName(field.Type)
		s.own[embedded] = struct{}{}
		if kind.base == mw3KindNone {
			s.opaque = s.opaque || !mw3IsLock(field.Type)
			continue
		}
		s.embedded = append(s.embedded, kind)
		s.fields[embedded] = kind
	}
	f.structs[name] = s
}

// scanFuncField records what a field that holds a function returns, which is
// called like a method.
func (f *mw3SessionFacts) scanFuncField(typ, field string, t ast.Expr, env *mw3Env) {
	ft, ok := ast.Unparen(t).(*ast.FuncType)
	if !ok {
		return
	}
	if kind := mw3FirstResult(ft, env); kind.base != mw3KindNone {
		if f.methods[typ] == nil {
			f.methods[typ] = map[string]mw3Kind{}
		}
		f.methods[typ][field] = kind
	}
}

// scanInterface records the methods of an interface type, and the interfaces it
// embeds.
func (f *mw3SessionFacts) scanInterface(name string, it *ast.InterfaceType, env *mw3Env) {
	s := mw3Struct{fields: map[string]mw3Kind{}, own: map[string]struct{}{}}
	iface := mw3Iface{methods: map[string]*ast.Field{}, env: env}
	for _, m := range it.Methods.List {
		if len(m.Names) == 0 {
			// An embedded interface, or a constraint such as ~string | int.
			kind := mw3KindOfType(m.Type, env)
			if kind.base == mw3KindNone {
				s.opaque = true
				continue
			}
			s.embedded = append(s.embedded, kind)
			iface.embedded = append(iface.embedded, kind)
			continue
		}
		for _, id := range m.Names {
			s.own[id.Name] = struct{}{}
			f.addMethod(name, id.Name)
			iface.methods[id.Name] = m
			ft, ok := m.Type.(*ast.FuncType)
			if !ok {
				continue
			}
			if kind := mw3FirstResult(ft, env); kind.base != mw3KindNone {
				if f.methods[name] == nil {
					f.methods[name] = map[string]mw3Kind{}
				}
				f.methods[name][id.Name] = kind
			}
		}
	}
	iface.opaque = s.opaque
	f.ifaces[name] = iface
	f.structs[name] = s
}

// mw3Iface is an interface type of the package: its own methods, as the fields
// of the syntax tree that declare them, and the interfaces it embeds.
type mw3Iface struct {
	methods  map[string]*ast.Field
	env      *mw3Env
	embedded []mw3Kind
	// opaque is set when it embeds something the source does not show.
	opaque bool
}

// mw3IsLock reports whether an embedded type of another package is a lock of the
// sync package, which has none of the names the session types have.
func mw3IsLock(t ast.Expr) bool {
	t = ast.Unparen(t)
	if star, ok := t.(*ast.StarExpr); ok {
		t = ast.Unparen(star.X)
	}
	sel, ok := t.(*ast.SelectorExpr)
	if !ok || !isIdentNamed(sel.X, "sync") {
		return false
	}
	switch sel.Sel.Name {
	case "Mutex", "RWMutex", "Once", "WaitGroup":
		return true
	}
	return false
}

// mw3EmbeddedName is the name of an embedded field: the name of its type.
func mw3EmbeddedName(t ast.Expr) string {
	switch x := t.(type) {
	case *ast.StarExpr:
		return mw3EmbeddedName(x.X)
	case *ast.ParenExpr:
		return mw3EmbeddedName(x.X)
	case *ast.IndexExpr:
		return mw3EmbeddedName(x.X)
	case *ast.IndexListExpr:
		return mw3EmbeddedName(x.X)
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// declares reports whether the type of the package has a field or a method of the
// name.
func (f *mw3SessionFacts) declares(typ, name string) bool {
	if _, ok := f.structs[typ].own[name]; ok {
		return true
	}
	_, ok := f.methodNames[typ][name]
	return ok
}

// reader returns a reader of the kinds of expressions of a file of the package
// whose names are env.
func (f *mw3SessionFacts) reader(env *mw3Env) *mw3KindReader {
	return &mw3KindReader{facts: f, env: env}
}

// varKind returns the kind of a variable of the package level.
func (f *mw3SessionFacts) varKind(name string) mw3Kind {
	if kind, ok := f.varKinds[name]; ok {
		return kind
	}
	v, ok := f.vars[name]
	if !ok {
		return mw3Kind{}
	}

	f.varKinds[name] = mw3Kind{} // what refers to itself is nothing
	r := f.reader(v.env)
	switch {
	case v.typ != nil:
		f.varKinds[name] = r.resolve(mw3KindOfType(v.typ, v.env))
	case v.init != nil:
		f.varKinds[name] = r.valueOf(v.init, nil)
	}
	return f.varKinds[name]
}

// holdsSessions reports whether something the package declares at its level is a
// store, a session or a config, or has one in a field, however deep.
func (f *mw3SessionFacts) holdsSessions() bool {
	r := f.reader(nil)
	holds := func(k mw3Kind) bool { return r.reaches(k, 0) }

	for name := range f.vars {
		if holds(f.varKind(name)) {
			return true
		}
	}
	for _, kind := range f.funcs {
		if holds(kind) {
			return true
		}
	}
	for _, methods := range f.methods {
		for _, kind := range methods {
			if holds(kind) {
				return true
			}
		}
	}
	for _, s := range f.structs {
		for _, kind := range s.fields {
			if holds(kind) {
				return true
			}
		}
	}
	return false
}

// mw3ImportPaths returns the import paths of a file.
func mw3ImportPaths(file *ast.File) []string {
	var paths []string
	for _, spec := range file.Imports {
		if path, err := strconv.Unquote(spec.Path.Value); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}
