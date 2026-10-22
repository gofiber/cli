package v3

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

// The code is v2 and has no type information, so what a value is can only be
// read from what the source says about it: the type a variable or a field is
// declared with, the literal or the call that initialized it, the type a
// function returns. mw3Kind is the part of that which matters to the session
// migrations: a store, a session, a config, a collection of them, or a type of
// the package that may hold some.

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
	// mw3KindNamed is a type that the package declares, by name.
	mw3KindNamed
	// mw3KindList is an array, a slice or a map: its elem holds the kind of what it
	// holds (of the values, for a map).
	mw3KindList
)

// mw3Kind tells what a value is.
type mw3Kind struct {
	elem *mw3Kind
	name string
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

// mw3KindOfType reads the kind from a type expression. names are the identifiers
// the file binds to the session package.
func mw3KindOfType(t ast.Expr, names []string) mw3Kind {
	switch x := t.(type) {
	case *ast.ParenExpr:
		return mw3KindOfType(x.X, names)
	case *ast.StarExpr:
		return mw3KindOfType(x.X, names)
	case *ast.Ellipsis:
		return mw3ListOf(mw3KindOfType(x.Elt, names))
	case *ast.ArrayType:
		return mw3ListOf(mw3KindOfType(x.Elt, names))
	case *ast.MapType:
		return mw3ListOf(mw3KindOfType(x.Value, names))
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok || !slices.Contains(names, id.Name) {
			return mw3Kind{}
		}
		switch x.Sel.Name {
		case mw3StoreType:
			return mw3Kind{base: mw3KindStore}
		case mw3SessionType:
			return mw3Kind{base: mw3KindSession}
		case mw3Config:
			return mw3Kind{base: mw3KindConfig}
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
	// names are the identifiers the file that declares it binds to the session
	// package.
	names []string
}

// mw3Struct is a struct type of the package: the kinds of its fields, and of the
// types it embeds, whose fields it has as well.
type mw3Struct struct {
	fields   map[string]mw3Kind
	embedded []mw3Kind
}

// mw3SessionFacts is what the files of a package say about what holds sessions,
// which a file of the package may use without declaring it.
type mw3SessionFacts struct {
	vars map[string]mw3PkgVar
	// varKinds are the kinds of the variables, read when they are first asked for.
	varKinds map[string]mw3Kind
	// funcs and methods (by the name of the type) are what the first result of a
	// function is.
	funcs   map[string]mw3Kind
	methods map[string]map[string]mw3Kind
	structs map[string]mw3Struct
	// named are the types that are not structs and stand for something of interest.
	named map[string]mw3Kind
}

func newMw3SessionFacts() mw3SessionFacts {
	return mw3SessionFacts{
		vars:     map[string]mw3PkgVar{},
		varKinds: map[string]mw3Kind{},
		funcs:    map[string]mw3Kind{},
		methods:  map[string]map[string]mw3Kind{},
		structs:  map[string]mw3Struct{},
		named:    map[string]mw3Kind{},
	}
}

// scan reads the package level declarations of a file of the package.
func (f *mw3SessionFacts) scan(file *ast.File, src string) {
	var names []string
	if strings.Contains(src, "middleware/session") {
		names = importNames(src, mw3SessionPath, "session")
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			f.scanFunc(d, names)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					if d.Tok == token.VAR {
						f.scanVar(s, names)
					}
				case *ast.TypeSpec:
					f.scanType(s, names)
				}
			}
		}
	}
}

// scanFunc records what a function or a method returns.
func (f *mw3SessionFacts) scanFunc(d *ast.FuncDecl, names []string) {
	if d.Type.Results == nil {
		return
	}
	kind := mw3FirstResult(d.Type, names)
	if kind.base == mw3KindNone {
		return
	}

	if d.Recv == nil {
		f.funcs[d.Name.Name] = kind
		return
	}
	if len(d.Recv.List) != 1 {
		return
	}
	recv := mw3KindOfType(d.Recv.List[0].Type, nil)
	if recv.base != mw3KindNamed {
		return
	}
	if f.methods[recv.name] == nil {
		f.methods[recv.name] = map[string]mw3Kind{}
	}
	f.methods[recv.name][d.Name.Name] = kind
}

// mw3FirstResult returns the kind of what a function returns: its first result
// that is something of interest, or else its first result.
func mw3FirstResult(ft *ast.FuncType, names []string) mw3Kind {
	if ft.Results == nil {
		return mw3Kind{}
	}
	var first mw3Kind
	for i, res := range ft.Results.List {
		kind := mw3KindOfType(res.Type, names)
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
func (f *mw3SessionFacts) scanVar(s *ast.ValueSpec, names []string) {
	for i, id := range s.Names {
		v := mw3PkgVar{typ: s.Type, names: names}
		switch {
		case len(s.Values) == len(s.Names):
			v.init = s.Values[i]
		case len(s.Values) == 1:
			v.init = s.Values[0]
		}
		f.vars[id.Name] = v
	}
}

// scanType records a struct type, or another type that stands for something of
// interest.
func (f *mw3SessionFacts) scanType(spec *ast.TypeSpec, names []string) {
	st, isStruct := spec.Type.(*ast.StructType)
	if !isStruct {
		// An alias is the type it stands for. A type that is defined from another
		// has the fields of it and none of its methods, which is nothing the
		// migrations could tell, unless it is a collection: Stores []*session.Store.
		kind := mw3KindOfType(spec.Type, names)
		if kind.base == mw3KindList || (kind.base != mw3KindNone && spec.Assign != token.NoPos) {
			f.named[spec.Name.Name] = kind
		}
		return
	}

	s := mw3Struct{fields: map[string]mw3Kind{}}
	for _, field := range st.Fields.List {
		kind := mw3KindOfType(field.Type, names)
		if kind.base == mw3KindNone {
			continue
		}
		if len(field.Names) > 0 {
			for _, id := range field.Names {
				s.fields[id.Name] = kind
			}
			continue
		}
		// An embedded field is called after its type, and has the fields of it.
		s.embedded = append(s.embedded, kind)
		s.fields[mw3EmbeddedName(field.Type)] = kind
	}
	f.structs[spec.Name.Name] = s
}

// mw3EmbeddedName is the name of an embedded field: the name of its type.
func mw3EmbeddedName(t ast.Expr) string {
	switch x := t.(type) {
	case *ast.StarExpr:
		return mw3EmbeddedName(x.X)
	case *ast.ParenExpr:
		return mw3EmbeddedName(x.X)
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// resolve follows a type of the package to the kind it stands for.
func (f *mw3SessionFacts) resolve(k mw3Kind) mw3Kind {
	for range 4 {
		if k.base != mw3KindNamed {
			break
		}
		next, ok := f.named[k.name]
		if !ok {
			break
		}
		k = next
	}
	return k
}

// field returns the kind of a field of a struct type of the package, which may
// be one of an embedded type.
func (f *mw3SessionFacts) field(structName, name string, depth int) mw3Kind {
	s, ok := f.structs[structName]
	if !ok || depth > 4 {
		return mw3Kind{}
	}
	if kind, found := s.fields[name]; found {
		return f.resolve(kind)
	}
	for _, embedded := range s.embedded {
		if embedded = f.resolve(embedded); embedded.base != mw3KindNamed {
			continue
		}
		if kind := f.field(embedded.name, name, depth+1); kind.base != mw3KindNone {
			return kind
		}
	}
	return mw3Kind{}
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
	r := mw3KindReader{facts: f, names: v.names}
	switch {
	case v.typ != nil:
		f.varKinds[name] = f.resolve(mw3KindOfType(v.typ, v.names))
	case v.init != nil:
		f.varKinds[name] = r.ofExpr(v.init, nil)
	}
	return f.varKinds[name]
}

// holdsSessions reports whether something the package declares at its level is a
// store, a session or a config, or has one in a field.
func (f *mw3SessionFacts) holdsSessions() bool {
	interesting := func(k mw3Kind) bool {
		for k.base == mw3KindList && k.elem != nil {
			k = *k.elem
		}
		return mw3IsSessionKind(f.resolve(k))
	}

	for name := range f.vars {
		if interesting(f.varKind(name)) {
			return true
		}
	}
	for _, kind := range f.funcs {
		if interesting(kind) {
			return true
		}
	}
	for _, methods := range f.methods {
		for _, kind := range methods {
			if interesting(kind) {
				return true
			}
		}
	}
	for _, s := range f.structs {
		for _, kind := range s.fields {
			if interesting(kind) {
				return true
			}
		}
	}
	return false
}

// mw3KindReader reads the kind of the expressions of a file.
type mw3KindReader struct {
	facts *mw3SessionFacts
	// names are the identifiers the file binds to the session package.
	names []string
	depth int
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
			return r.facts.resolve(mw3KindOfType(x.Type, r.names))
		}
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			return r.facts.resolve(mw3KindOfType(x.Type, r.names))
		}
	}
	return mw3Kind{}
}

// element returns what a collection holds.
func (r *mw3KindReader) element(k mw3Kind) mw3Kind {
	if k = r.facts.resolve(k); k.base != mw3KindList || k.elem == nil {
		return mw3Kind{}
	}
	return r.facts.resolve(*k.elem)
}

// isPackage reports whether the identifier names the session package here and
// not a variable that has the same name.
func (r *mw3KindReader) isPackage(id *ast.Ident, stack []ast.Node) bool {
	if !slices.Contains(r.names, id.Name) {
		return false
	}
	_, shadowed := resolveIdent(id.Name, id.Pos(), stack)
	return !shadowed
}

func (r *mw3KindReader) ofIdent(id *ast.Ident, stack []ast.Node) mw3Kind {
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	switch {
	case ok && d.typ != nil:
		return r.facts.resolve(mw3KindOfType(d.typ, r.names))
	case ok && d.init != nil:
		return r.ofExpr(d.init, stack)
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
					return r.facts.resolve(mw3KindOfType(clause.List[0], r.names)), true
				}
			}
			return mw3Kind{}, true
		}
	}
	return mw3Kind{}, false
}

func (r *mw3KindReader) ofSelector(sel *ast.SelectorExpr, stack []ast.Node) mw3Kind {
	if id, ok := sel.X.(*ast.Ident); ok && r.isPackage(id, stack) {
		if sel.Sel.Name == mw3ConfigDefault {
			return mw3Kind{base: mw3KindConfig}
		}
		return mw3Kind{}
	}

	owner := r.ofExpr(sel.X, stack)
	if owner.base == mw3KindStore && sel.Sel.Name == mw3Config {
		return mw3Kind{base: mw3KindConfig}
	}
	if owner.base == mw3KindNamed {
		return r.facts.field(owner.name, sel.Sel.Name, 0)
	}
	return mw3Kind{}
}

// ofCall returns what a call gives back.
func (r *mw3KindReader) ofCall(call *ast.CallExpr, stack []ast.Node) mw3Kind {
	switch fun := call.Fun.(type) {
	case *ast.FuncLit:
		return r.facts.resolve(mw3FirstResult(fun.Type, r.names))
	case *ast.Ident:
		return r.ofFuncCall(call, fun, stack)
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok && r.isPackage(id, stack) {
			if fun.Sel.Name == mw3SessionNew || fun.Sel.Name == mw3SessionNewStore {
				return mw3Kind{base: mw3KindStore}
			}
			return mw3Kind{}
		}

		owner := r.ofExpr(fun.X, stack)
		if owner.base == mw3KindStore && (fun.Sel.Name == appMethodGet || fun.Sel.Name == "GetByID") {
			return mw3Kind{base: mw3KindSession}
		}
		if owner.base == mw3KindNamed {
			return r.facts.resolve(r.facts.methods[owner.name][fun.Sel.Name])
		}
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
				return r.facts.resolve(mw3FirstResult(ft, r.names))
			}
		case d.init != nil:
			if lit, ok := d.init.(*ast.FuncLit); ok {
				return r.facts.resolve(mw3FirstResult(lit.Type, r.names))
			}
		}
		return mw3Kind{}
	}

	switch fun.Name {
	case mw3BuiltinNew, mw3BuiltinMake:
		if len(call.Args) > 0 {
			return r.facts.resolve(mw3KindOfType(call.Args[0], r.names))
		}
	case mw3BuiltinAppend:
		if len(call.Args) > 0 {
			return r.ofExpr(call.Args[0], stack)
		}
	}
	return r.facts.resolve(r.facts.funcs[fun.Name])
}
