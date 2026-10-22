package v3

import (
	"go/ast"
	"maps"
	"path/filepath"

	"github.com/gofiber/cli/cmd/internal"
)

// A program may keep its session store behind an interface of its own, to hand it
// to code that does not need to know the type or to replace it in tests:
//
//	type SessionStore interface {
//		Get(c *fiber.Ctx) (*session.Session, error)
//		Delete(id string) error
//		Reset() error
//	}
//
// The v2 store satisfies it. The store of v3 takes a context in Delete and Reset,
// so it satisfies the interface no more, and neither do the types that stand in
// for it. The migration gives the methods of such an interface and of such types
// the context, where the calls through them are written like the calls on the store.
//
// An interface is the store's when it asks for Get, which only a session store
// has in this shape (it takes the context of a request and gives a session), and
// for nothing else than methods of the v2 store, in the signatures it had. A type
// stands in for it when it has every method of such an interface, in these
// signatures: its own, or the ones it gets from a store that it embeds.

// mw3TypeKey identifies a type of a package of the module.
type mw3TypeKey struct {
	pkg  *mw3Pkg
	name string
}

// mw3StoreTypes are the types of a module that have the methods of the v2 session
// store: the interfaces that ask for them, and the types that have them, with the
// methods that an interface asks of them.
type mw3StoreTypes struct {
	ifaces map[mw3TypeKey]struct{}
	impls  map[mw3TypeKey]map[string]struct{}
}

// stands reports whether the method of the type is one that stands in for a
// method of the store: the interface asks for it, or an interface asks the type
// for it.
func (t *mw3StoreTypes) stands(k mw3TypeKey, method string) bool {
	if _, iface := t.ifaces[k]; iface {
		return true
	}
	_, asked := t.impls[k][method]
	return asked
}

// mw3ContextMethods are the methods of the v2 session store that take a context
// in v3.
var mw3ContextMethods = []string{mw3Reset, nameDelete}

// storeTypes returns the types of the module of the file that have the methods of
// the v2 session store. It is decided once, before any file of the module is
// rewritten, because the files have to agree.
func (p *mw3Pkgs) storeTypes(fi internal.FileInfo) *mw3StoreTypes {
	mod, ok := moduleOf(fi.Path, fi.Root)
	if !ok {
		return &mw3StoreTypes{}
	}
	if types, done := p.stores[mod.root]; done {
		return types
	}
	if p.stores == nil {
		p.stores = map[string]*mw3StoreTypes{}
	}

	types := &mw3StoreTypes{ifaces: map[mw3TypeKey]struct{}{}, impls: map[mw3TypeKey]map[string]struct{}{}}
	p.stores[mod.root] = types

	var pkgs []*mw3Pkg
	for _, pkg := range p.indexes.of(fi).allPackages() {
		own := p.of(internal.FileInfo{Root: fi.Root, Path: filepath.Join(pkg.dir, "_.go")}, pkg.name)
		if own.relevant() {
			pkgs = append(pkgs, own)
		}
	}

	var ifaces []map[string]mw3IfaceMethod
	for _, pkg := range pkgs {
		facts := pkg.sessions()
		r := facts.reader(nil)
		for name := range facts.ifaces {
			if methods, ok := r.flatMethods(mw3Kind{base: mw3KindNamed, name: name}, 0); ok && mw3IsStoreInterface(methods) {
				types.ifaces[mw3TypeKey{pkg, name}] = struct{}{}
				ifaces = append(ifaces, methods)
			}
		}
	}
	if len(ifaces) == 0 {
		return types
	}

	for _, pkg := range pkgs {
		facts := pkg.sessions()
		r := facts.reader(nil)
		r.noBind = true
		for name := range facts.methodDecls {
			if _, isIface := facts.ifaces[name]; isIface {
				continue
			}
			for _, methods := range ifaces {
				if !r.satisfies(mw3Kind{base: mw3KindNamed, name: name}, methods) {
					continue
				}
				key := mw3TypeKey{pkg, name}
				if types.impls[key] == nil {
					types.impls[key] = map[string]struct{}{}
				}
				for method := range methods {
					types.impls[key][method] = struct{}{}
				}
			}
		}
	}
	return types
}

// mw3IfaceMethod is a method of an interface, with what the file that declares it
// says about its names.
type mw3IfaceMethod struct {
	field *ast.Field
	env   *mw3Env
}

// flatMethods returns the methods of the interface, those of the interfaces it
// embeds included. ok is false when it embeds something that is not shown.
func (r *mw3KindReader) flatMethods(k mw3Kind, depth int) (map[string]mw3IfaceMethod, bool) {
	f := r.factsOf(k.pkg)
	if f == nil || depth > modMaxDepth {
		return nil, false
	}
	iface, ok := f.ifaces[k.name]
	if !ok || iface.opaque {
		return nil, false
	}

	methods := map[string]mw3IfaceMethod{}
	for name, field := range iface.methods {
		methods[name] = mw3IfaceMethod{field: field, env: iface.env}
	}
	for _, embedded := range iface.embedded {
		embedded = r.resolve(r.from(f, embedded))
		inner, ok := r.flatMethods(embedded, depth+1)
		if !ok {
			return nil, false
		}
		maps.Copy(methods, inner)
	}
	return methods, true
}

// mw3IsStoreInterface reports whether the methods are those of the v2 session
// store, in the signatures it had, and ask for Get.
func mw3IsStoreInterface(methods map[string]mw3IfaceMethod) bool {
	if _, hasGet := methods[appMethodGet]; !hasGet {
		return false
	}
	for name, m := range methods {
		ft, ok := m.field.Type.(*ast.FuncType)
		if !ok || !mw3IsStoreMethod(name, ft, m.env) {
			return false
		}
	}
	return true
}

// mw3IsStoreMethod reports whether the method of the name, declared with the
// signature, is the method of the v2 session store of that name.
func mw3IsStoreMethod(name string, ft *ast.FuncType, env *mw3Env) bool {
	params, results := mw3FieldTypes(ft.Params), mw3FieldTypes(ft.Results)
	switch name {
	case appMethodGet:
		return len(params) == 1 && mw3IsFiberCtx(params[0]) && len(results) == 2 &&
			mw3KindOfType(results[0], env).base == mw3KindSession && isIdentNamed(results[1], "error")
	case mw3Reset:
		return len(params) == 0 && len(results) == 1 && isIdentNamed(results[0], "error")
	case nameDelete:
		return len(params) == 1 && isIdentNamed(params[0], "string") && len(results) == 1 && isIdentNamed(results[0], "error")
	case mw3RegisterType:
		return len(params) == 1 && len(results) == 0
	}
	return false
}

// mw3FieldTypes returns the types of the parameters or results, one for each.
func mw3FieldTypes(list *ast.FieldList) []ast.Expr {
	if list == nil {
		return nil
	}
	var types []ast.Expr
	for _, field := range list.List {
		for range max(1, len(field.Names)) {
			types = append(types, field.Type)
		}
	}
	return types
}

// mw3IsFiberCtx reports whether the type is the context of a request, in either
// of the forms it has had: fiber.Ctx or *fiber.Ctx, whatever the package is called.
func mw3IsFiberCtx(t ast.Expr) bool {
	t = ast.Unparen(t)
	if star, ok := t.(*ast.StarExpr); ok {
		t = ast.Unparen(star.X)
	}
	sel, ok := t.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Ctx"
}

// satisfies reports whether the type has every method of the interface in the
// signature the store has it in: one it declares, or one it gets from a store.
func (r *mw3KindReader) satisfies(k mw3Kind, methods map[string]mw3IfaceMethod) bool {
	for name := range methods {
		m := r.lookup(k, name)
		switch {
		case m.base == mw3KindStore:
		case m.user:
			f := r.factsOf(m.owner.pkg)
			if f == nil {
				return false
			}
			decl, ok := f.methodDecls[m.owner.name][name]
			if !ok || !mw3IsStoreMethod(name, decl.decl.Type, decl.env) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// storeBound reports whether the method of the type is one that stands in for a
// method of the v2 session store, so that it is written like the one of the store.
func (r *mw3KindReader) storeBound(owner mw3Kind, method string) bool {
	if r.noBind {
		return false
	}
	f := r.factsOf(owner.pkg)
	if f == nil {
		return false
	}
	return f.owner.owner.storeTypes(f.owner.fi).stands(mw3TypeKey{f.owner, owner.name}, method)
}
