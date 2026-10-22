package v3

import (
	"go/ast"
	"go/token"
	"slices"
)

// mw3KindReader reads the kind of the expressions of a file.
type mw3KindReader struct {
	// facts are those of the package the file is in.
	facts *mw3SessionFacts
	env   *mw3Env
	depth int
	// noBind keeps the reader from asking which types stand in for the store, which
	// it does while that is being decided.
	noBind bool
}

// factsOf returns the facts of the package at the import path, which is empty for
// the package of the file. It is nil for a package that cannot be read.
func (r *mw3KindReader) factsOf(pkg string) *mw3SessionFacts {
	if pkg == "" || pkg == r.facts.path {
		return r.facts
	}
	peer := r.facts.owner.peer(pkg)
	if peer == nil {
		return nil
	}
	return peer.sessions()
}

// from makes a kind that the facts of a package gave refer to its types in a way
// that a file of any package can follow.
func (r *mw3KindReader) from(f *mw3SessionFacts, k mw3Kind) mw3Kind {
	if f == r.facts {
		return k
	}
	return mw3Qualify(k, f.path)
}

// resolve follows a type of the program to the kind it stands for.
func (r *mw3KindReader) resolve(k mw3Kind) mw3Kind {
	for range 4 {
		if k.base != mw3KindNamed {
			break
		}
		f := r.factsOf(k.pkg)
		if f == nil {
			break
		}
		next, ok := f.named[k.name]
		if !ok {
			break
		}
		k = r.from(f, next)
	}
	return k
}

// reaches reports whether a value of the kind is, holds or leads to a store, a
// session or a config: it is one, a collection of them, or a type with one in a
// field.
func (r *mw3KindReader) reaches(k mw3Kind, depth int) bool {
	for k.base == mw3KindList && k.elem != nil {
		k = *k.elem
	}
	k = r.resolve(k)
	if mw3IsSessionKind(k) {
		return true
	}
	if k.base != mw3KindNamed || depth > 3 {
		return false
	}
	f := r.factsOf(k.pkg)
	if f == nil {
		return false
	}
	for _, field := range f.structs[k.name].fields {
		if r.reaches(r.from(f, field), depth+1) {
			return true
		}
	}
	return false
}

// ofExpr returns the kind of the expression whose ancestors are stack (see
// goSource.walk); stack is nil for an expression of the package level.
func (r *mw3KindReader) ofExpr(e ast.Expr, stack []ast.Node) mw3Kind {
	if r.depth > mw3MaxKindDepth {
		return mw3Kind{}
	}
	r.depth++
	defer func() { r.depth-- }()

	switch x := e.(type) {
	case *ast.ParenExpr:
		return r.ofExpr(x.X, stack)
	case *ast.StarExpr:
		return r.ofExpr(x.X, stack)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return r.ofExpr(x.X, stack)
		}
	case *ast.Ident:
		return r.ofIdent(x, stack)
	case *ast.SelectorExpr:
		return r.ofSelector(x, stack)
	case *ast.IndexExpr:
		return r.element(r.ofExpr(x.X, stack))
	case *ast.SliceExpr:
		return r.ofExpr(x.X, stack)
	case *ast.CallExpr:
		return r.ofCall(x, stack)
	case *ast.CompositeLit:
		if x.Type != nil {
			return r.resolve(mw3KindOfType(x.Type, r.env))
		}
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			return r.resolve(mw3KindOfType(x.Type, r.env))
		}
	}
	return mw3Kind{}
}

// valueOf returns the kind of a variable that is initialized with the expression.
// A variable that is given the config of a store holds a copy of it, which has
// nothing to do with the store any more, unless it is a pointer to the config.
func (r *mw3KindReader) valueOf(init ast.Expr, stack []ast.Node) mw3Kind {
	k := r.ofExpr(init, stack)
	if k.base != mw3KindConfig {
		return k
	}
	if u, isAddr := ast.Unparen(init).(*ast.UnaryExpr); !isAddr || u.Op != token.AND {
		k.name = ""
	}
	return k
}

// element returns what a collection holds.
func (r *mw3KindReader) element(k mw3Kind) mw3Kind {
	if k = r.resolve(k); k.base != mw3KindList || k.elem == nil {
		return mw3Kind{}
	}
	return r.resolve(*k.elem)
}

// isPackage reports whether the identifier names the session package here and
// not a variable that has the same name.
func (r *mw3KindReader) isPackage(id *ast.Ident, stack []ast.Node) bool {
	return r.env.isSession(id.Name) && !mw3Shadowed(id.Name, id.Pos(), stack)
}

// modulePackage returns the import path of the package of the module that the
// identifier names here, and not a variable that has the same name.
func (r *mw3KindReader) modulePackage(id *ast.Ident, stack []ast.Node) (string, bool) {
	path, ok := r.env.module(id.Name)
	if !ok || mw3Shadowed(id.Name, id.Pos(), stack) {
		return "", false
	}
	return path, true
}

func (r *mw3KindReader) ofIdent(id *ast.Ident, stack []ast.Node) mw3Kind {
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	switch {
	case ok && d.typ != nil:
		return r.resolve(mw3KindOfType(d.typ, r.env))
	case ok && d.init != nil:
		return r.valueOf(d.init, stack)
	case ok:
		return mw3Kind{}
	}
	if kind, bound := r.bound(id, stack); bound {
		return kind
	}
	return r.facts.varKind(id.Name)
}

// bound returns the kind of a variable that a range clause or a type switch
// declares, and whether the identifier is one.
func (r *mw3KindReader) bound(id *ast.Ident, stack []ast.Node) (mw3Kind, bool) {
	for i, node := range slices.Backward(stack) {
		switch n := node.(type) {
		case *ast.RangeStmt:
			if n.Tok != token.DEFINE || n.Body == nil || id.Pos() < n.Body.Pos() {
				continue
			}
			if isIdentNamed(n.Value, id.Name) {
				return r.element(r.ofExpr(n.X, stack[:i])), true
			}
			if isIdentNamed(n.Key, id.Name) {
				return mw3Kind{}, true
			}
		case *ast.TypeSwitchStmt:
			bind, ok := n.Assign.(*ast.AssignStmt)
			if !ok || len(bind.Lhs) != 1 || !isIdentNamed(bind.Lhs[0], id.Name) {
				continue
			}
			// The variable has the type of the case it is in, when that is one.
			if i+2 < len(stack) {
				if clause, isClause := stack[i+2].(*ast.CaseClause); isClause && len(clause.List) == 1 {
					return r.resolve(mw3KindOfType(clause.List[0], r.env)), true
				}
			}
			return mw3Kind{}, true
		}
	}
	return mw3Kind{}, false
}

func (r *mw3KindReader) ofSelector(sel *ast.SelectorExpr, stack []ast.Node) mw3Kind {
	if id, ok := sel.X.(*ast.Ident); ok {
		if r.isPackage(id, stack) {
			if sel.Sel.Name == mw3ConfigDefault {
				return mw3Kind{base: mw3KindConfig}
			}
			return mw3Kind{}
		}
		if path, inModule := r.modulePackage(id, stack); inModule {
			f := r.factsOf(path)
			if f == nil {
				return mw3Kind{}
			}
			return r.from(f, f.varKind(sel.Sel.Name))
		}
	}
	return r.fieldOf(r.ofExpr(sel.X, stack), sel.Sel.Name)
}

// fieldOf returns the kind of the field of a value of the kind: one the program
// declares, or the Config that a store embeds.
func (r *mw3KindReader) fieldOf(owner mw3Kind, name string) mw3Kind {
	m := r.lookup(owner, name)
	switch {
	case m.user:
		f := r.factsOf(m.owner.pkg)
		if f == nil {
			return mw3Kind{}
		}
		return r.resolve(r.from(f, f.structs[m.owner.name].fields[name]))
	case m.base == mw3KindStore && name == mw3Config:
		return mw3Kind{base: mw3KindConfig, name: mw3StoreType}
	}
	return mw3Kind{}
}

// resultOf returns the kind of what a method of a value of the kind returns.
func (r *mw3KindReader) resultOf(owner mw3Kind, name string) mw3Kind {
	m := r.lookup(owner, name)
	switch {
	case m.user:
		f := r.factsOf(m.owner.pkg)
		if f == nil {
			return mw3Kind{}
		}
		return r.resolve(r.from(f, f.methods[m.owner.name][name]))
	case m.base == mw3KindStore && (name == appMethodGet || name == mw3GetByID):
		return mw3Kind{base: mw3KindSession}
	}
	return mw3Kind{}
}

// ofCall returns what a call gives back.
func (r *mw3KindReader) ofCall(call *ast.CallExpr, stack []ast.Node) mw3Kind {
	fun := ast.Unparen(call.Fun)
	for {
		// A generic function, instantiated.
		switch x := fun.(type) {
		case *ast.IndexExpr:
			fun = ast.Unparen(x.X)
			continue
		case *ast.IndexListExpr:
			fun = ast.Unparen(x.X)
			continue
		}
		break
	}

	switch fun := fun.(type) {
	case *ast.FuncLit:
		return r.resolve(mw3FirstResult(fun.Type, r.env))
	case *ast.Ident:
		return r.ofFuncCall(call, fun, stack)
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			if r.isPackage(id, stack) {
				if fun.Sel.Name == mw3SessionNew || fun.Sel.Name == mw3SessionNewStore {
					return mw3Kind{base: mw3KindStore}
				}
				return mw3Kind{}
			}
			if path, inModule := r.modulePackage(id, stack); inModule {
				f := r.factsOf(path)
				if f == nil {
					return mw3Kind{}
				}
				return r.resolve(r.from(f, f.funcs[fun.Sel.Name]))
			}
		}
		return r.resultOf(r.ofExpr(fun.X, stack), fun.Sel.Name)
	}
	return mw3Kind{}
}

// ofFuncCall returns what a call of a function by its name gives back: a
// function of the package, a function the code declares as a variable or takes as
// a parameter, or a builtin that makes a value.
func (r *mw3KindReader) ofFuncCall(call *ast.CallExpr, fun *ast.Ident, stack []ast.Node) mw3Kind {
	d, declared := resolveIdent(fun.Name, fun.Pos(), stack)
	if declared {
		switch {
		case d.typ != nil:
			if ft, ok := d.typ.(*ast.FuncType); ok {
				return r.resolve(mw3FirstResult(ft, r.env))
			}
		case d.init != nil:
			if lit, ok := d.init.(*ast.FuncLit); ok {
				return r.resolve(mw3FirstResult(lit.Type, r.env))
			}
		}
		return mw3Kind{}
	}

	switch fun.Name {
	case mw3BuiltinNew, mw3BuiltinMake:
		if len(call.Args) > 0 {
			return r.resolve(mw3KindOfType(call.Args[0], r.env))
		}
	case mw3BuiltinAppend:
		if len(call.Args) > 0 {
			return r.ofExpr(call.Args[0], stack)
		}
	}
	return r.resolve(r.facts.funcs[fun.Name])
}

// The names of the members of the session types: a type that embeds one has them
// as well.
const (
	mw3GetByID       = "GetByID"
	mw3Reset         = "Reset"
	mw3RegisterType  = "RegisterType"
	mw3KeysMethod    = "Keys"
	mw3SetExpiry     = "SetExpiry"
	mw3SetIdle       = "SetIdleTimeout"
	mw3ReleaseMethod = "Release"
)

// mw3StoreCalls are the methods of the store that a type standing in for it has.
var mw3StoreCalls = []string{appMethodGet, mw3Reset, nameDelete}

var (
	mw3StoreMembers = mw3Names(appMethodGet, mw3GetByID, mw3Reset, nameDelete, mw3RegisterType, mw3Config)

	mw3SessionMembers = mw3Names("Fresh", "ID", appMethodGet, "Set", nameDelete, "Destroy", "Regenerate", mw3Reset, "Save",
		mw3KeysMethod, mw3SetExpiry, mw3SetIdle, mw3ReleaseMethod)

	mw3ConfigMembers = mw3Names(cacheExpiration, "IdleTimeout", "AbsoluteTimeout", "Storage", mw3KeyLookup, mw3CookieName,
		"CookieDomain", "CookiePath", "CookieSecure", "CookieHTTPOnly", "CookieSameSite", "CookieSessionOnly", "KeyGenerator",
		mw3ExtractorField, "Next", "ErrorHandler", cacheStore)
)

func mw3Names(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}

// mw3MaxEmbedDepth bounds how deep the types a type embeds are followed.
const mw3MaxEmbedDepth = 6

// mw3Member says where a member of a value comes from.
type mw3Member struct {
	// owner is the type of the program that declares the member, when one does.
	owner mw3Kind
	// base is the session type that provides the member: a store, a session or a
	// config. It is none when the program's own type declares the member, or when
	// nothing is known to.
	base mw3Base
	// viaStore is set for a config that a store embeds.
	viaStore bool
	// unsure is set when a type the source does not show sits above the one that
	// provides the member, so that it may be that type's.
	unsure bool
	// user is set when a type of the program declares the member.
	user bool
}

// mw3Entity is a type among those a value has the members of.
type mw3Entity struct {
	kind     mw3Kind
	viaStore bool
}

// lookup finds where x.member comes from for a value x of the kind, the way Go
// does: the member at the smallest depth of embedding, the types that sit at the
// same depth being as good as each other (the program is valid, so only one of
// them has it).
func (r *mw3KindReader) lookup(k mw3Kind, member string) mw3Member {
	type seenType struct{ pkg, name string }
	seen := map[seenType]struct{}{}

	level := []mw3Entity{{kind: r.resolve(k)}}
	// A type that the source does not show may have the member too. above is set
	// when such a type sits at a smaller depth than the level looked at, here when
	// it sits at the depth of the level.
	var above, here bool
	for depth := 0; depth <= mw3MaxEmbedDepth && len(level) > 0; depth++ {
		var (
			found []mw3Member
			next  []mw3Entity
			below bool
		)
		for _, e := range level {
			switch e.kind.base {
			case mw3KindStore:
				if _, ok := mw3StoreMembers[member]; ok {
					found = append(found, mw3Member{base: mw3KindStore})
				}
				next = append(next, mw3Entity{kind: mw3Kind{base: mw3KindConfig}, viaStore: true})
			case mw3KindConfig:
				if _, ok := mw3ConfigMembers[member]; ok {
					found = append(found, mw3Member{base: mw3KindConfig, viaStore: e.viaStore || e.kind.name == mw3StoreType})
				}
			case mw3KindSession:
				if _, ok := mw3SessionMembers[member]; ok {
					found = append(found, mw3Member{base: mw3KindSession})
				}
			case mw3KindNamed:
				key := seenType{e.kind.pkg, e.kind.name}
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}

				f := r.factsOf(e.kind.pkg)
				if f == nil {
					here = true // a type that is not in the module
					continue
				}
				if f.declares(e.kind.name, member) {
					m := mw3Member{user: true, owner: e.kind}
					if slices.Contains(mw3StoreCalls, member) && r.storeBound(e.kind) {
						m.base = mw3KindStore // a method of a type that stands in for the store
					}
					found = append(found, m)
					continue
				}
				s := f.structs[e.kind.name]
				for _, embedded := range s.embedded {
					next = append(next, mw3Entity{kind: r.resolve(r.from(f, embedded))})
				}
				below = below || s.opaque
			case mw3KindNone, mw3KindList:
			}
		}

		switch len(found) {
		case 0:
		case 1:
			m := found[0]
			m.unsure = above
			return m
		default:
			return mw3Member{} // ambiguous, which valid code never is
		}
		above = above || here
		here = below
		level = next
	}
	return mw3Member{unsure: above || here}
}
