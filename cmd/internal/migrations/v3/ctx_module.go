package v3

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// The environment (ctx_env.go) answers what the type of a receiver is from type
// expressions. A value of a type that another package of the module declares
// (web.Context, ctxutil.From(c), req.Request{Ctx: c}) is typed from the
// declarations of that package, which the module index reads, and a type
// expression written in another file means something else in this file: its
// qualifiers are the imports of that file, its bare names the package of that
// file. So a declaration is translated into terms that mean the same everywhere
// before the environment looks at it:
//
//   - the fiber package becomes the qualifier ctxFiberQualifier,
//   - a package of the module becomes a qualifier that carries its import path
//     (ctxAbsRef), and a bare name of the package of the declaration is qualified
//     the same way,
//   - any other package becomes ctxExtQualifier, the type of a package that is
//     none of the above.
//
// The translated declarations are copies; the trees of the files stay as they are.

const (
	// ctxFiberQualifier stands for the core fiber package in a translated type.
	ctxFiberQualifier = "\x00fiber"
	// ctxExtQualifier stands for a package outside of the module, and
	// ctxExtHarmlessQualifier for one that takes the ranges it is given without
	// looking into them (ctxIsHarmlessImport).
	ctxExtQualifier         = "\x00ext"
	ctxExtHarmlessQualifier = "\x00ext:harmless"
	// ctxAbsQualifier starts the qualifier of a package of the module, followed by
	// its import path.
	ctxAbsQualifier = "\x00pkg:"
)

// ctxFiberRef returns fiber.name, as a translated type spells it.
func ctxFiberRef(name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: &ast.Ident{Name: ctxFiberQualifier}, Sel: &ast.Ident{Name: name}}
}

// ctxExtRef returns pkg.name for a package outside of the module.
func ctxExtRef(importPath, name string) *ast.SelectorExpr {
	qualifier := ctxExtQualifier
	if ctxIsHarmlessImport(importPath) {
		qualifier = ctxExtHarmlessQualifier
	}
	return &ast.SelectorExpr{X: &ast.Ident{Name: qualifier}, Sel: &ast.Ident{Name: name}}
}

// ctxAbsRef returns the reference to the type name of the package of the module
// with the import path.
func ctxAbsRef(importPath, name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: &ast.Ident{Name: ctxAbsQualifier + importPath}, Sel: &ast.Ident{Name: name}}
}

// ctxAbsParts splits a reference made by ctxAbsRef.
func ctxAbsParts(sel *ast.SelectorExpr) (importPath, name string, ok bool) {
	id, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	importPath, ok = strings.CutPrefix(id.Name, ctxAbsQualifier)
	return importPath, sel.Sel.Name, ok
}

// ctxForeignKey is the key under which the environment knows a declaration of
// another package of the module: the import path and the name. A name of the
// package of the file has no dot in its key, so the two never meet.
func ctxForeignKey(importPath, name string) string { return importPath + "." + name }

// ctxIsForeignKey reports whether the key is one of ctxForeignKey.
func ctxIsForeignKey(key string) bool { return strings.Contains(key, ".") }

// ctxSplitKey splits a key made by ctxForeignKey.
func ctxSplitKey(key string) (importPath, name string, ok bool) {
	i := strings.LastIndex(key, ".")
	if i <= 0 || i == len(key)-1 {
		return "", "", false
	}
	return key[:i], key[i+1:], true
}

// ctxImports are the imports of a file: the packages by the identifier the file
// refers to them by.
type ctxImports struct {
	paths map[string]string
	dots  []string
}

// pathOf returns the import path the identifier is bound to, "" when it is none.
func (i *ctxImports) pathOf(name string) string {
	if i == nil {
		return ""
	}
	return i.paths[name]
}

// ctxModule is the view of the packages of the module for the environment. It is
// made once for the run of a migration and shared by the environments of all its
// files; what it translates does not depend on the file that asks.
type ctxModule struct {
	ix      *moduleIndex
	imports map[*ast.File]*ctxImports
	specs   map[*ast.TypeSpec]*ast.TypeSpec
	funcs   map[*ast.FuncDecl]*ast.FuncDecl
	types   map[ast.Expr]ast.Expr
	members map[string]*ctxMember
	// fiberPath is the import path of the core fiber package the module uses.
	fiberPath  string
	fiberKnown bool
}

func newCtxModule(ix *moduleIndex) *ctxModule {
	if ix == nil {
		return nil
	}
	return &ctxModule{
		ix:      ix,
		imports: map[*ast.File]*ctxImports{},
		specs:   map[*ast.TypeSpec]*ast.TypeSpec{},
		funcs:   map[*ast.FuncDecl]*ast.FuncDecl{},
		types:   map[ast.Expr]ast.Expr{},
		members: map[string]*ctxMember{},
	}
}

// inModule reports whether the import path is a package of the module.
func (m *ctxModule) inModule(importPath string) bool {
	if m == nil {
		return false
	}
	_, ok := m.ix.dirOf(importPath)
	return ok
}

// pkg returns the package of the module with the import path.
func (m *ctxModule) pkg(importPath string) *modPackage {
	if m == nil {
		return nil
	}
	return m.ix.packageAt(importPath)
}

// importsOf returns the imports of the file.
func (m *ctxModule) importsOf(file *ast.File) *ctxImports {
	if m == nil || file == nil {
		return nil
	}
	if imps, ok := m.imports[file]; ok {
		return imps
	}

	imps := &ctxImports{paths: map[string]string{}}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		switch {
		case spec.Name != nil && spec.Name.Name == "_":
		case spec.Name != nil && spec.Name.Name == ".":
			imps.dots = append(imps.dots, importPath)
		case spec.Name != nil:
			imps.paths[spec.Name.Name] = importPath
		default:
			imps.paths[m.bindName(importPath)] = importPath
		}
	}
	m.imports[file] = imps
	return imps
}

// bindName is the identifier an import without a name binds. It is the last
// element of the path (without a major version), which is the name of the
// package unless the module says otherwise: a package of the module whose
// directory is not called like it is asked.
func (m *ctxModule) bindName(importPath string) string {
	name := defaultImportName(importPath)
	if m.inModule(importPath) && (strings.ContainsAny(name, "-.") || reMajorVersion.MatchString(ctxLastElem(importPath))) {
		if pkg := m.pkg(importPath); pkg != nil && pkg.name != "" {
			return pkg.name
		}
	}
	return name
}

// ctxLastElem returns the last element of an import path.
func ctxLastElem(importPath string) string {
	return importPath[strings.LastIndex(importPath, "/")+1:]
}

// fiberImportPath returns the import path of the core fiber package that the
// module's files use, "" when none of them imports it.
func (m *ctxModule) fiberImportPath() string {
	if m == nil {
		return ""
	}
	if m.fiberKnown {
		return m.fiberPath
	}
	m.fiberKnown = true

	for _, pkg := range m.ix.packages() {
		for _, f := range pkg.files {
			for _, imp := range f.imps {
				if reFiberCorePath.MatchString(imp.path) {
					m.fiberPath = imp.path
					return m.fiberPath
				}
			}
		}
	}
	return ""
}

// ctxTranslator translates the type expressions of one file.
type ctxTranslator struct {
	m    *ctxModule
	imps *ctxImports
	own  *modPackage
	// tparams are the type parameters in scope, which are no references.
	tparams map[string]bool
	ownPath string
	// abs is set for a declaration of another package, whose bare names are
	// references to that package.
	abs bool
}

func (m *ctxModule) translator(file *ast.File, ownPath string, abs bool) *ctxTranslator {
	tr := &ctxTranslator{m: m, imps: m.importsOf(file), ownPath: ownPath, abs: abs, tparams: map[string]bool{}}
	if abs {
		tr.own = m.pkg(ownPath)
	}
	return tr
}

// bind marks the names of the type parameters, which are no references.
func (tr *ctxTranslator) bind(lists ...*ast.FieldList) {
	for _, fl := range lists {
		for _, name := range ctxTypeParams(fl) {
			tr.tparams[name] = true
		}
	}
}

// bindReceiver marks the type parameters a receiver names: func (b *Box[T]).
func (tr *ctxTranslator) bindReceiver(recv *ast.FieldList) {
	for _, arg := range ctxTypeArgs(ctxFirstFieldType(recv)) {
		if id, ok := arg.(*ast.Ident); ok {
			tr.tparams[id.Name] = true
		}
	}
}

// pkgRef returns the reference to the type name of the package the identifier
// qualifies.
func (tr *ctxTranslator) pkgRef(importPath, name string) ast.Expr {
	switch {
	case reFiberCorePath.MatchString(importPath):
		return ctxFiberRef(name)
	case tr.m.inModule(importPath):
		return ctxAbsRef(importPath, name)
	}
	return ctxExtRef(importPath, name)
}

// ident translates a bare name.
func (tr *ctxTranslator) ident(id *ast.Ident) ast.Expr {
	name := id.Name
	if tr.tparams[name] || name == "_" || ctxIsPredeclaredType(name) {
		return id
	}

	if tr.abs && tr.own.lookup(name) != nil {
		return ctxAbsRef(tr.ownPath, name)
	}
	if tr.imps != nil && appIsExported(name) {
		for _, importPath := range tr.imps.dots {
			if reFiberCorePath.MatchString(importPath) {
				return ctxFiberRef(name)
			}
			if tr.m.pkg(importPath).lookup(name) != nil {
				return ctxAbsRef(importPath, name)
			}
		}
	}
	return id
}

// expr translates a type expression.
func (tr *ctxTranslator) expr(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case nil:
		return nil
	case *ast.Ident:
		return tr.ident(x)
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if importPath := tr.imps.pathOf(id.Name); importPath != "" {
				return tr.pkgRef(importPath, x.Sel.Name)
			}
		}
		return x
	case *ast.StarExpr:
		if inner := tr.expr(x.X); inner != x.X {
			return &ast.StarExpr{Star: x.Star, X: inner}
		}
	case *ast.ParenExpr:
		if inner := tr.expr(x.X); inner != x.X {
			return &ast.ParenExpr{Lparen: x.Lparen, X: inner, Rparen: x.Rparen}
		}
	case *ast.ArrayType:
		if elt := tr.expr(x.Elt); elt != x.Elt {
			return &ast.ArrayType{Lbrack: x.Lbrack, Len: x.Len, Elt: elt}
		}
	case *ast.Ellipsis:
		if elt := tr.expr(x.Elt); elt != x.Elt {
			return &ast.Ellipsis{Ellipsis: x.Ellipsis, Elt: elt}
		}
	case *ast.MapType:
		key, value := tr.expr(x.Key), tr.expr(x.Value)
		if key != x.Key || value != x.Value {
			return &ast.MapType{Map: x.Map, Key: key, Value: value}
		}
	case *ast.ChanType:
		if value := tr.expr(x.Value); value != x.Value {
			return &ast.ChanType{Begin: x.Begin, Arrow: x.Arrow, Dir: x.Dir, Value: value}
		}
	case *ast.IndexExpr:
		base, index := tr.expr(x.X), tr.expr(x.Index)
		if base != x.X || index != x.Index {
			return &ast.IndexExpr{X: base, Lbrack: x.Lbrack, Index: index, Rbrack: x.Rbrack}
		}
	case *ast.IndexListExpr:
		base, changed := tr.expr(x.X), false
		indices := make([]ast.Expr, len(x.Indices))
		for i, index := range x.Indices {
			indices[i] = tr.expr(index)
			changed = changed || indices[i] != index
		}
		if changed || base != x.X {
			return &ast.IndexListExpr{X: base, Lbrack: x.Lbrack, Indices: indices, Rbrack: x.Rbrack}
		}
	case *ast.FuncType:
		return tr.funcType(x)
	case *ast.StructType:
		if fields := tr.fields(x.Fields); fields != x.Fields {
			return &ast.StructType{Struct: x.Struct, Fields: fields, Incomplete: x.Incomplete}
		}
	case *ast.InterfaceType:
		if methods := tr.fields(x.Methods); methods != x.Methods {
			return &ast.InterfaceType{Interface: x.Interface, Methods: methods, Incomplete: x.Incomplete}
		}
	}
	return e
}

// fields translates the types of a list of fields; it returns fl itself when
// nothing changes.
func (tr *ctxTranslator) fields(fl *ast.FieldList) *ast.FieldList {
	if fl == nil {
		return nil
	}

	out, changed := make([]*ast.Field, len(fl.List)), false
	for i, f := range fl.List {
		out[i] = f
		if typ := tr.expr(f.Type); typ != f.Type {
			out[i], changed = &ast.Field{Doc: f.Doc, Names: f.Names, Type: typ, Tag: f.Tag, Comment: f.Comment}, true
		}
	}
	if !changed {
		return fl
	}
	return &ast.FieldList{Opening: fl.Opening, List: out, Closing: fl.Closing}
}

// funcType translates the parameters and results of a signature. Its type
// parameters stay, and they are names of the signature.
func (tr *ctxTranslator) funcType(ft *ast.FuncType) *ast.FuncType {
	tr.bind(ft.TypeParams)
	params, results := tr.fields(ft.Params), tr.fields(ft.Results)
	if params == ft.Params && results == ft.Results {
		return ft
	}
	return &ast.FuncType{Func: ft.Func, TypeParams: ft.TypeParams, Params: params, Results: results}
}

// translatedSpec returns the declaration of a type in terms that mean the same in
// every file. file is the file that declares it, ownPath the import path of its
// package when that is another package than the one of the environment.
func (m *ctxModule) translatedSpec(spec *ast.TypeSpec, file *ast.File, ownPath string, abs bool) *ast.TypeSpec {
	if cached, ok := m.specs[spec]; ok {
		return cached
	}

	tr := m.translator(file, ownPath, abs)
	tr.bind(spec.TypeParams)
	out := spec
	if typ := tr.expr(spec.Type); typ != spec.Type {
		out = &ast.TypeSpec{Doc: spec.Doc, Name: spec.Name, TypeParams: spec.TypeParams, Assign: spec.Assign, Type: typ, Comment: spec.Comment}
	}
	m.specs[spec] = out
	return out
}

// translatedFunc is translatedSpec for a function or a method. The copy has no
// body.
func (m *ctxModule) translatedFunc(fn *ast.FuncDecl, file *ast.File, ownPath string, abs bool) *ast.FuncDecl {
	if cached, ok := m.funcs[fn]; ok {
		return cached
	}

	tr := m.translator(file, ownPath, abs)
	tr.bindReceiver(fn.Recv)
	typ, recv := tr.funcType(fn.Type), tr.fields(fn.Recv)
	out := fn
	if typ != fn.Type || recv != fn.Recv {
		out = &ast.FuncDecl{Doc: fn.Doc, Recv: recv, Name: fn.Name, Type: typ}
	}
	m.funcs[fn] = out
	return out
}

// translatedType is translatedSpec for a type expression that is not part of a
// declaration of a type, the declared type of a variable.
func (m *ctxModule) translatedType(e ast.Expr, file *ast.File, ownPath string, abs bool) ast.Expr {
	if cached, ok := m.types[e]; ok {
		return cached
	}
	out := m.translator(file, ownPath, abs).expr(e)
	m.types[e] = out
	return out
}

// ctxMember is what a qualified name pkg.Name of a package of the module names, as
// far as its declaration tells.
type ctxMember struct {
	spec *ast.TypeSpec
	fn   *ast.FuncDecl
	// typ is the type of a variable, nil when it is not known.
	typ ast.Expr
	// key is the key of the declaration (ctxForeignKey).
	key  string
	kind modKind
}

// member returns what the name is in the package, nil when the package does not
// declare it (or is not of the module).
func (m *ctxModule) member(importPath, name string) *ctxMember {
	if m == nil {
		return nil
	}
	key := ctxForeignKey(importPath, name)
	if mem, ok := m.members[key]; ok {
		return mem
	}

	var mem *ctxMember
	if d := m.pkg(importPath).lookup(name); d != nil {
		file := d.file.g.file
		mem = &ctxMember{key: key, kind: d.kind}
		switch d.kind {
		case modTypeDecl:
			mem.spec = m.translatedSpec(d.typ, file, importPath, true)
		case modFunc:
			mem.fn = m.translatedFunc(d.fn, file, importPath, true)
		case modVar:
			mem.typ = m.varType(d, importPath)
		case modConst, modMethod:
		}
	}
	m.members[key] = mem
	return mem
}

// varType returns the type of a variable of another package: the type it is
// declared with, or the one its initializer makes when that is a composite literal,
// a conversion or a call of a function of the module whose results are declared.
func (m *ctxModule) varType(d *modDecl, importPath string) ast.Expr {
	file := d.file.g.file
	if t, ok := d.varType(); ok {
		return m.translatedType(t.expr, file, importPath, true)
	}

	e, idx, ok := d.varInit()
	if !ok {
		return nil
	}
	return m.initType(d.file, e, idx, importPath, 0)
}

// initType is the type of the idx-th value of the expression e, written in the file.
func (m *ctxModule) initType(f *modFile, e ast.Expr, idx int, importPath string, depth int) ast.Expr {
	if depth > modMaxDepth || e == nil {
		return nil
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return m.initType(f, x.X, idx, importPath, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return m.initType(f, x.X, idx, importPath, depth+1)
		}
	case *ast.CompositeLit:
		if idx == 0 && x.Type != nil {
			return m.translatedType(x.Type, f.g.file, importPath, true)
		}
	case *ast.TypeAssertExpr:
		if idx == 0 && x.Type != nil {
			return m.translatedType(x.Type, f.g.file, importPath, true)
		}
	case *ast.CallExpr:
		return m.callType(f, x, idx, importPath)
	}
	return nil
}

// callType is the type of the idx-th result of a call of a function of the module,
// or of a conversion.
func (m *ctxModule) callType(f *modFile, call *ast.CallExpr, idx int, importPath string) ast.Expr {
	fun := ast.Unparen(call.Fun)
	switch x := fun.(type) {
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.ChanType:
		if idx == 0 {
			return m.translatedType(x, f.g.file, importPath, true)
		}
		return nil
	case *ast.Ident:
		if x.Name == modBuiltinNew && len(call.Args) == 1 && f.declOf(x) == nil {
			if idx == 0 {
				return m.translatedType(call.Args[0], f.g.file, importPath, true)
			}
			return nil
		}
	}

	d := f.declOf(fun)
	if d == nil {
		return nil
	}
	switch d.kind {
	case modFunc:
		results := d.results()
		if idx < len(results) {
			return m.translatedType(results[idx].expr, d.file.g.file, d.file.pkg.importPath, true)
		}
	case modTypeDecl:
		if idx == 0 {
			return m.translatedType(fun, f.g.file, importPath, true)
		}
	case modMethod, modVar, modConst:
	}
	return nil
}

// method returns the declaration of the method of the type with the key, nil when
// the module declares none.
func (m *ctxModule) method(typeKey, name string) *ast.FuncDecl {
	importPath, typeName, ok := ctxSplitKey(typeKey)
	if !ok || m == nil {
		return nil
	}

	d := m.pkg(importPath).method(typeName, name)
	if d == nil {
		return nil
	}
	return m.translatedFunc(d.fn, d.file.g.file, importPath, true)
}
