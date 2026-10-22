package v3

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
)

// This file reads what declarations say about types for the moduleIndex: the
// results of a function, the type of a variable, the fields of a struct, and
// whether a type or an expression is one of the values a migration looks for. It
// stays at the level of declarations: what a local variable holds is for the
// migration to say, and the answer it needs from here is then about its
// initializer or its declared type, which are expressions and type expressions.

// modBuiltinNew is the builtin function that makes a pointer to a new value.
const modBuiltinNew = "new"

// modType is a type expression together with the file it is written in, which
// decides what its names mean.
type modType struct {
	file *modFile
	expr ast.Expr
}

func (t modType) valid() bool { return t.file != nil && t.expr != nil }

// modNamed is the type that a type expression names.
type modNamed struct {
	// decl is the declaration of the type, nil unless the module declares it.
	decl *modDecl
	// path is the import path of the package that declares the type, empty for a
	// predeclared type such as string or error.
	path string
	name string
	// pointer is set when the expression is a pointer to the type.
	pointer bool
}

// deref removes pointers and parentheses from the type.
func (t modType) deref() modType {
	for t.valid() {
		switch x := t.expr.(type) {
		case *ast.StarExpr:
			t.expr = x.X
		case *ast.ParenExpr:
			t.expr = x.X
		default:
			return t
		}
	}
	return t
}

// named returns the type the expression names, through pointers, parentheses,
// generic instantiations and the aliases that the module declares: for *app.App in
// a file that imports "example.com/app" and where the module declares
// "type App = fiber.App" that is the type App of the fiber package. It reports
// false for a type that is not named (a slice, a function, a struct).
func (t modType) named() (modNamed, bool) { return t.namedAt(0) }

func (t modType) namedAt(depth int) (modNamed, bool) {
	if depth > modMaxDepth || !t.valid() {
		return modNamed{}, false
	}

	pointer := false
	e := t.expr
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			pointer, e = true, x.X
			continue
		case *ast.ParenExpr:
			e = x.X
			continue
		case *ast.IndexExpr:
			e = x.X
			continue
		case *ast.IndexListExpr:
			e = x.X
			continue
		}
		break
	}

	switch x := e.(type) {
	case *ast.Ident:
		if d := t.file.declOf(x); d != nil {
			return modNamedOf(d, pointer, depth)
		}
		if importPath, name, ok := t.file.qualifiedName(x); ok {
			return modNamed{path: importPath, name: name, pointer: pointer}, true
		}
		return modNamed{name: x.Name, pointer: pointer}, true
	case *ast.SelectorExpr:
		if d := t.file.declOf(x); d != nil {
			return modNamedOf(d, pointer, depth)
		}
		if importPath, name, ok := t.file.qualifiedName(x); ok {
			return modNamed{path: importPath, name: name, pointer: pointer}, true
		}
	}
	return modNamed{}, false
}

// modNamedOf is the named type of a declaration; an alias is the type it stands for.
func modNamedOf(d *modDecl, pointer bool, depth int) (modNamed, bool) {
	if d.kind != modTypeDecl {
		return modNamed{}, false
	}
	if d.typ.Assign.IsValid() {
		n, ok := modType{d.file, d.typ.Type}.namedAt(depth + 1)
		n.pointer = n.pointer || pointer
		return n, ok
	}
	return modNamed{decl: d, path: d.file.pkg.importPath, name: d.name, pointer: pointer}, true
}

// qualifiedName splits a name written in the file into the import path of the
// package that declares it and its name: pkg.Name for a package the file imports,
// and a plain Name for a package it dot-imports. A name that the package of the
// file declares itself is not qualified.
func (f *modFile) qualifiedName(e ast.Expr) (importPath, name string, ok bool) {
	switch x := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		if imp, found := f.qualifier(x); found {
			return imp.path, x.Sel.Name, true
		}
	case *ast.Ident:
		if f.pkg.lookupIn(f, x.Name) != nil {
			return "", "", false
		}
		for _, imp := range f.dotImports() {
			if pkg := imp.pkg(); pkg == nil || pkg.lookup(x.Name) != nil {
				return imp.path, x.Name, true
			}
		}
	}
	return "", "", false
}

// under returns the type behind the names of t: the aliases and defined types of
// the module are followed to the type they are made of, a struct, slice, map,
// channel, function or interface. A name the module does not declare stays as it
// is, and so does a generic type, whose fields and elements depend on its
// arguments.
func (t modType) under() modType {
	for range modMaxDepth {
		if !t.valid() {
			return t
		}
		t.expr = ast.Unparen(t.expr)
		switch t.expr.(type) {
		case *ast.Ident, *ast.SelectorExpr:
		default:
			return t
		}
		d := t.file.declOf(t.expr)
		if d == nil || d.kind != modTypeDecl || d.typ.TypeParams != nil {
			return t
		}
		t = modType{d.file, d.typ.Type}
	}
	return t
}

// elem returns the type of the values of a slice, array, map or channel: the
// type of what ranging over a value of the type yields, a key aside.
func (t modType) elem() (modType, bool) {
	u := t.under()
	if !u.valid() {
		return modType{}, false
	}
	switch x := u.expr.(type) {
	case *ast.ArrayType:
		return modType{u.file, x.Elt}, true
	case *ast.MapType:
		return modType{u.file, x.Value}, true
	case *ast.ChanType:
		return modType{u.file, x.Value}, true
	}
	return modType{}, false
}

// key returns the type of the keys of a map.
func (t modType) key() (modType, bool) {
	u := t.under()
	if m, ok := u.expr.(*ast.MapType); ok && u.valid() {
		return modType{u.file, m.Key}, true
	}
	return modType{}, false
}

// modField is a field of a struct type.
type modField struct {
	typ  modType
	name string
	// embedded is set for a field that has no name of its own and is called like
	// its type.
	embedded bool
}

// fields returns the fields of the struct type that t is or names, through
// pointers, aliases and defined types, in the order they are declared. Nothing
// else has fields.
func (t modType) fields() []modField {
	u := t.deref().under()
	if !u.valid() {
		return nil
	}
	st, ok := u.expr.(*ast.StructType)
	if !ok || st.Fields == nil {
		return nil
	}

	var out []modField
	for _, fld := range st.Fields.List {
		ft := modType{u.file, fld.Type}
		if len(fld.Names) == 0 {
			out = append(out, modField{typ: ft, name: modEmbeddedName(fld.Type), embedded: true})
			continue
		}
		for _, id := range fld.Names {
			out = append(out, modField{typ: ft, name: id.Name})
		}
	}
	return out
}

// modEmbeddedName returns the name an embedded field has: the name of its type
// without the package.
func modEmbeddedName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.SelectorExpr:
			return x.Sel.Name
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// field returns the field called name of the struct type, or one promoted from a
// struct it embeds.
func (t modType) field(name string) (modField, bool) { return t.fieldAt(name, 0) }

func (t modType) fieldAt(name string, depth int) (modField, bool) {
	if depth > modMaxDepth {
		return modField{}, false
	}
	fields := t.fields()
	if i := slices.IndexFunc(fields, func(fld modField) bool { return fld.name == name }); i >= 0 {
		return fields[i], true
	}
	for _, fld := range fields {
		if !fld.embedded {
			continue
		}
		if promoted, ok := fld.typ.fieldAt(name, depth+1); ok {
			return promoted, true
		}
	}
	return modField{}, false
}

// method returns the declaration of the method called name of the type, or of one
// promoted from a struct it embeds, nil when the module declares none: a method
// of a type of another module is not here.
func (t modType) method(name string) *modDecl { return t.methodAt(name, 0) }

func (t modType) methodAt(name string, depth int) *modDecl {
	if depth > modMaxDepth {
		return nil
	}
	if n, ok := t.named(); ok && n.decl != nil {
		if d := n.decl.file.pkg.method(n.decl.name, name); d != nil {
			return d
		}
	}
	for _, fld := range t.fields() {
		if !fld.embedded {
			continue
		}
		if d := fld.typ.methodAt(name, depth+1); d != nil {
			return d
		}
	}
	return nil
}

// modTarget names the values a migration looks for: some types of one package and
// the functions of that package that make them. The package is given as a pattern
// so that the same target serves the code of either major version of a module.
type modTarget struct {
	// pkg matches the import path of the package.
	pkg *regexp.Regexp
	// types are the names of the types, such as "App".
	types []string
	// funcs are the functions of the package that return a value or a pointer of
	// one of the types, such as "New".
	funcs []string
}

// isType reports whether the type of the import path and name is one of the target.
func (m modTarget) isType(importPath, name string) bool {
	return m.pkg.MatchString(importPath) && slices.Contains(m.types, name)
}

// isFunc reports whether the function of the import path and name makes a value of
// the target.
func (m modTarget) isFunc(importPath, name string) bool {
	return m.pkg.MatchString(importPath) && slices.Contains(m.funcs, name)
}

// is reports whether the type is one of the target, or a pointer to one.
func (t modType) is(target modTarget) bool {
	n, ok := t.named()
	return ok && target.isType(n.path, n.name)
}

// holds reports whether a value of the type is, or reaches, a value of the target:
// the type is the target (or a pointer to it), a slice, array, map or channel of
// values that hold one, or a struct that embeds one.
func (t modType) holds(target modTarget) bool { return t.holdsAt(target, 0) }

func (t modType) holdsAt(target modTarget, depth int) bool {
	if depth > modMaxDepth || !t.valid() {
		return false
	}
	if t.is(target) {
		return true
	}
	if elem, ok := t.elem(); ok && elem.holdsAt(target, depth+1) {
		return true
	}
	for _, fld := range t.fields() {
		if fld.embedded && fld.typ.holdsAt(target, depth+1) {
			return true
		}
	}
	return false
}

// modParam is a parameter of a function.
type modParam struct {
	typ  modType
	name string
}

// params returns the parameters of a function or method, one per name, in order.
// A parameter without a name has an empty one.
func (d *modDecl) params() []modParam {
	if d == nil || d.fn == nil || d.fn.Type.Params == nil {
		return nil
	}
	var out []modParam
	for _, fld := range d.fn.Type.Params.List {
		pt := modType{d.file, fld.Type}
		if len(fld.Names) == 0 {
			out = append(out, modParam{typ: pt})
			continue
		}
		for _, id := range fld.Names {
			out = append(out, modParam{typ: pt, name: id.Name})
		}
	}
	return out
}

// results returns the types of the values a function or method returns, one per
// value, in order.
func (d *modDecl) results() []modType {
	if d == nil || d.fn == nil || d.fn.Type.Results == nil {
		return nil
	}
	var out []modType
	for _, fld := range d.fn.Type.Results.List {
		for range max(1, len(fld.Names)) {
			out = append(out, modType{d.file, fld.Type})
		}
	}
	return out
}

// varType returns the type a variable or constant is declared with.
func (d *modDecl) varType() (modType, bool) {
	if d == nil || d.value == nil || d.value.Type == nil {
		return modType{}, false
	}
	return modType{d.file, d.value.Type}, true
}

// varInit returns the expression that initializes a variable or constant, and
// which of its values the name gets: the first, unless the names of the
// specification share one call (var a, b = f()). The file of the expression is
// that of the declaration.
func (d *modDecl) varInit() (e ast.Expr, idx int, ok bool) {
	switch {
	case d == nil || d.value == nil:
		return nil, 0, false
	case len(d.value.Values) == len(d.value.Names):
		return d.value.Values[d.index], 0, true
	case len(d.value.Values) == 1:
		return d.value.Values[0], d.index, true
	}
	return nil, 0, false
}

// yields reports whether the idx-th value of the expression e, written in the file,
// is a value of the target, as far as declarations tell. It understands the
// expressions that make a value of a type: a composite literal, a conversion, new,
// a type assertion, &x and *x, a call of a function of the target package (the
// funcs of the target) or of a function of the module that returns the type, and a
// package level variable that is declared with the type or initialized with such an
// expression. It does not know what a local variable holds, pass the initializer
// of the variable instead, and it does not follow methods.
func (f *modFile) yields(e ast.Expr, idx int, target modTarget) bool {
	return f.yieldsAt(e, idx, target, 0)
}

func (f *modFile) yieldsAt(e ast.Expr, idx int, target modTarget, depth int) bool {
	if depth > modMaxDepth || e == nil {
		return false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.yieldsAt(x.X, idx, target, depth+1)
	case *ast.StarExpr:
		return f.yieldsAt(x.X, idx, target, depth+1)
	case *ast.UnaryExpr:
		return x.Op == token.AND && f.yieldsAt(x.X, idx, target, depth+1)
	case *ast.CompositeLit:
		return idx == 0 && x.Type != nil && modType{f, x.Type}.is(target)
	case *ast.TypeAssertExpr:
		return idx == 0 && x.Type != nil && modType{f, x.Type}.is(target)
	case *ast.Ident, *ast.SelectorExpr:
		d := f.declOf(x)
		if d == nil || d.kind != modVar {
			return false
		}
		return d.holdsVar(target, depth)
	case *ast.CallExpr:
		return f.callYields(x, idx, target)
	}
	return false
}

// holdsVar reports whether the variable is declared with a type of the target or
// initialized with a value of it.
func (d *modDecl) holdsVar(target modTarget, depth int) bool {
	if t, ok := d.varType(); ok {
		return t.is(target)
	}
	e, idx, ok := d.varInit()
	return ok && d.file.yieldsAt(e, idx, target, depth+1)
}

func (f *modFile) callYields(call *ast.CallExpr, idx int, target modTarget) bool {
	fun := ast.Unparen(call.Fun)

	switch x := fun.(type) {
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.InterfaceType:
		return idx == 0 && modType{f, x}.is(target) // a conversion such as (*T)(v)
	case *ast.Ident:
		if x.Name == modBuiltinNew && len(call.Args) == 1 && f.declOf(x) == nil {
			return idx == 0 && modType{f, call.Args[0]}.is(target)
		}
	}

	if d := f.declOf(fun); d != nil {
		switch d.kind {
		case modFunc:
			results := d.results()
			return idx < len(results) && results[idx].is(target)
		case modTypeDecl:
			return idx == 0 && modType{f, fun}.is(target) // a conversion T(v)
		default:
			return false
		}
	}

	importPath, name, ok := f.qualifiedName(fun)
	if !ok {
		return false
	}
	return idx == 0 && (target.isFunc(importPath, name) || target.isType(importPath, name))
}

// funcsReturning returns the functions and methods of the packages of the module,
// the test files left out, that return a value whose type the predicate accepts.
func (ix *moduleIndex) funcsReturning(match func(modType) bool) []*modDecl {
	var out []*modDecl
	for _, pkg := range ix.packages() {
		for _, d := range pkg.decls(false) {
			if (d.kind == modFunc || d.kind == modMethod) && slices.ContainsFunc(d.results(), match) {
				out = append(out, d)
			}
		}
	}
	return out
}

// varsHolding returns the package level variables of the module, the test files
// left out, that are declared with a type of the target or initialized with a
// value of it.
func (ix *moduleIndex) varsHolding(target modTarget) []*modDecl {
	var out []*modDecl
	for _, pkg := range ix.packages() {
		for _, d := range pkg.decls(false) {
			if d.kind == modVar && d.holdsVar(target, 0) {
				out = append(out, d)
			}
		}
	}
	return out
}

// modOwnedField is a field of a struct type that the module declares.
type modOwnedField struct {
	// owner is the declaration of the struct type.
	owner *modDecl
	modField
}

// fieldsOfType returns the fields of the struct types of the module, the test
// files left out, whose type the predicate accepts, embedded fields included.
func (ix *moduleIndex) fieldsOfType(match func(modType) bool) []modOwnedField {
	var out []modOwnedField
	for _, pkg := range ix.packages() {
		for _, d := range pkg.decls(false) {
			if d.kind != modTypeDecl || d.typ.Assign.IsValid() {
				continue
			}
			for _, fld := range (modType{d.file, d.typ.Type}).fields() {
				if match(fld.typ) {
					out = append(out, modOwnedField{owner: d, modField: fld})
				}
			}
		}
	}
	return out
}
