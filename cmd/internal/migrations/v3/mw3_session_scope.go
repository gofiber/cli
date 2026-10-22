package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3SessionPath matches the import path of the fiber session middleware at any
// major version: v3 once MigrateGoPkgs has run, v2 when a migration is run on
// its own.
var mw3SessionPath = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/session$`)

// The functions that make a session store: New in v2, NewStore in v3.
const (
	mw3SessionNew      = "New"
	mw3SessionNewStore = "NewStore"
)

// mw3NeedImport returns the identifier the file uses for the package at path.
// A file that does not import it yet gets the import scheduled, under the first
// of the candidate names that the file does not use for something else.
func mw3NeedImport(g *goSource, imports *mw3ImportEdits, path string, candidates ...string) string {
	if name := mw3ImportName(g.src, path); name != "" {
		return name
	}
	declared := mw3DeclaredNames(g.file)
	name := mw3FreeName(func(name string) bool {
		_, taken := declared[name]
		return taken
	}, candidates...)
	imports.require(path, name)
	return name
}

// mw3ImportHidden reports whether the file imports the package at path under a
// name that a declaration hides where the node whose ancestors are stack sits,
// so that the package cannot be used there.
func mw3ImportHidden(g *goSource, path string, pos token.Pos, stack []ast.Node) bool {
	name := mw3ImportName(g.src, path)
	if name == "" {
		return false
	}
	_, hidden := resolveIdent(name, pos, stack)
	return hidden
}

// mw3SessionFacts is what the files of a package say about session stores,
// which a function in another file of the package may use.
type mw3SessionFacts struct {
	// storeVars are the package level variables that hold a session store.
	storeVars map[string]struct{}
	// storeFields are the fields that hold a session store, by struct type.
	storeFields map[string]map[string]struct{}
	// storeFuncs return a session store, sessionFuncs a session.
	storeFuncs   map[string]struct{}
	sessionFuncs map[string]struct{}
}

func newMw3SessionFacts() mw3SessionFacts {
	return mw3SessionFacts{
		storeVars:    map[string]struct{}{},
		storeFields:  map[string]map[string]struct{}{},
		storeFuncs:   map[string]struct{}{},
		sessionFuncs: map[string]struct{}{},
	}
}

// scan reads the package level declarations of a file that imports the session
// middleware.
func (f *mw3SessionFacts) scan(file *ast.File, src string) {
	if !strings.Contains(src, "middleware/session") {
		return
	}
	names := importNames(src, mw3SessionPath, "session")
	if len(names) == 0 {
		return
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil || d.Type.Results == nil {
				continue
			}
			for _, res := range d.Type.Results.List {
				switch {
				case mw3IsSessionType(res.Type, names, "Store"):
					f.storeFuncs[d.Name.Name] = struct{}{}
				case mw3IsSessionType(res.Type, names, "Session"):
					f.sessionFuncs[d.Name.Name] = struct{}{}
				}
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for i, id := range s.Names {
						if mw3IsSessionType(s.Type, names, "Store") ||
							(len(s.Values) == len(s.Names) && mw3IsStoreCtor(s.Values[i], names)) {
							f.storeVars[id.Name] = struct{}{}
						}
					}
				case *ast.TypeSpec:
					f.scanStruct(s, names)
				}
			}
		}
	}
}

func (f *mw3SessionFacts) scanStruct(spec *ast.TypeSpec, names []string) {
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return
	}
	for _, field := range st.Fields.List {
		if !mw3IsSessionType(field.Type, names, "Store") {
			continue
		}
		if f.storeFields[spec.Name.Name] == nil {
			f.storeFields[spec.Name.Name] = map[string]struct{}{}
		}
		for _, id := range field.Names {
			f.storeFields[spec.Name.Name][id.Name] = struct{}{}
		}
	}
}

// mw3IsSessionType reports whether e names the type typeName of the session
// package, possibly behind pointers.
func mw3IsSessionType(e ast.Expr, names []string, typeName string) bool {
	return e != nil && isFiberType(e, names, typeName)
}

// mw3IsStoreCtor reports whether e creates a store: session.New or NewStore.
func mw3IsStoreCtor(e ast.Expr, names []string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != mw3SessionNew && sel.Sel.Name != mw3SessionNewStore) {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains(names, id.Name)
}

// mw3SessionFile is a file that imports the session middleware, parsed, with
// what it takes to tell the session package and its values from other things
// of the same name.
type mw3SessionFile struct {
	g     *goSource
	pkgs  *mw3Pkgs
	pkg   *mw3Pkg
	fi    internal.FileInfo
	names []string
	fiber []string
}

// mw3OpenSessionFile parses src when it imports the fiber session middleware.
func mw3OpenSessionFile(fi internal.FileInfo, src string, pkgs *mw3Pkgs) (*mw3SessionFile, bool) {
	if !strings.Contains(src, "middleware/session") {
		return nil, false
	}
	names := importNames(src, mw3SessionPath, "session")
	if len(names) == 0 {
		return nil, false
	}
	g, ok := parseFile(fi, src)
	if !ok {
		return nil, false
	}
	return &mw3SessionFile{g: g, pkgs: pkgs, fi: fi, names: names, fiber: fiberNames(src)}, true
}

// mw3OpenStoreUser is mw3OpenSessionFile for the migrations of what is done
// with a store and its sessions, which a file can do without importing the
// session package: the store is a variable, a field or a function of the
// package that another file declares. cheap tells from the text whether the
// file has any call the migration looks for, so that the package is only read
// for files that may use a store.
func mw3OpenStoreUser(fi internal.FileInfo, src string, pkgs *mw3Pkgs, cheap func(src string) bool) (*mw3SessionFile, bool) {
	if !cheap(src) {
		return nil, false
	}
	if f, ok := mw3OpenSessionFile(fi, src, pkgs); ok {
		return f, true
	}

	head, err := parser.ParseFile(token.NewFileSet(), fi.Path, src, parser.PackageClauseOnly)
	if err != nil {
		return nil, false
	}
	facts := pkgs.of(fi, head.Name.Name).sessions()
	if len(facts.storeVars)+len(facts.storeFields)+len(facts.storeFuncs) == 0 {
		return nil, false
	}
	g, ok := parseFile(fi, src)
	if !ok {
		return nil, false
	}
	return &mw3SessionFile{g: g, pkgs: pkgs, fi: fi, fiber: fiberNames(src)}, true
}

// facts returns what the package says about session stores.
func (f *mw3SessionFile) facts() *mw3SessionFacts {
	if f.pkg == nil {
		f.pkg = f.pkgs.of(f.fi, f.g.file.Name.Name)
	}
	return f.pkg.sessions()
}

// strings returns the reader of string values of the file.
func (f *mw3SessionFile) strings() mw3Strings {
	if f.pkg == nil {
		f.pkg = f.pkgs.of(f.fi, f.g.file.Name.Name)
	}
	return newMw3Strings(f.g, f.pkg)
}

// isPackage reports whether the identifier names the session package here and
// not a variable that has the same name.
func (f *mw3SessionFile) isPackage(id *ast.Ident, stack []ast.Node) bool {
	if !slices.Contains(f.names, id.Name) {
		return false
	}
	_, shadowed := resolveIdent(id.Name, id.Pos(), stack)
	return !shadowed
}

// isMember reports whether e is session.member, through the name the file
// gives the package.
func (f *mw3SessionFile) isMember(e ast.Expr, member string, stack []ast.Node) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != member {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && f.isPackage(id, stack)
}

// isConfigLit reports whether e is a literal of the session Config.
func (f *mw3SessionFile) isConfigLit(e ast.Expr, stack []ast.Node) (*ast.CompositeLit, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok || lit.Type == nil {
		return nil, false
	}
	return lit, f.isMember(lit.Type, mw3Config, stack)
}

// isStoreCall reports whether e creates a store, and with which function.
func (f *mw3SessionFile) isStoreCall(e ast.Expr, stack []ast.Node) (*ast.CallExpr, string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil, "", false
	}
	for _, name := range []string{mw3SessionNew, mw3SessionNewStore} {
		if f.isMember(call.Fun, name, stack) {
			return call, name, true
		}
	}
	return nil, "", false
}

// isStore reports whether e is a session store: a variable declared as one or
// created by session.New, a package level store, a field of a struct that
// holds one, or a function that returns one.
func (f *mw3SessionFile) isStore(e ast.Expr, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.isStore(x.X, stack)
	case *ast.CallExpr:
		if _, _, ok := f.isStoreCall(x, stack); ok {
			return true
		}
		id, ok := x.Fun.(*ast.Ident)
		if !ok {
			return false
		}
		_, found := f.facts().storeFuncs[id.Name]
		return found
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			_, found := f.facts().storeVars[x.Name]
			return found
		}
		if d.typ != nil {
			return mw3IsSessionType(d.typ, f.names, "Store")
		}
		return d.init != nil && f.isStore(d.init, stack)
	case *ast.SelectorExpr:
		return f.isStoreField(x, stack)
	}
	return false
}

// isStoreField reports whether sel reads a field that holds a store from a
// variable whose struct type is declared in the package.
func (f *mw3SessionFile) isStoreField(sel *ast.SelectorExpr, stack []ast.Node) bool {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	if !ok || d.typ == nil {
		return false
	}
	typ := d.typ
	for {
		star, isStar := typ.(*ast.StarExpr)
		if !isStar {
			break
		}
		typ = star.X
	}
	name, ok := typ.(*ast.Ident)
	if !ok {
		return false
	}
	_, found := f.facts().storeFields[name.Name][sel.Sel.Name]
	return found
}

// isSession reports whether e is a session: a variable declared as one, or
// the result of getting one from a store or of a function that returns one.
func (f *mw3SessionFile) isSession(e ast.Expr, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.isSession(x.X, stack)
	case *ast.CallExpr:
		return f.returnsSession(x, stack)
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return mw3IsSessionType(d.typ, f.names, "Session")
		}
		call, isCall := d.init.(*ast.CallExpr)
		return isCall && f.returnsSession(call, stack)
	}
	return false
}

// returnsSession reports whether the call hands back a session: Get or GetByID
// of a store, or a function whose result is one.
func (f *mw3SessionFile) returnsSession(call *ast.CallExpr, stack []ast.Node) bool {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		if fun.Sel.Name == appMethodGet || fun.Sel.Name == "GetByID" {
			return f.isStore(fun.X, stack)
		}
	case *ast.Ident:
		if _, found := f.facts().sessionFuncs[fun.Name]; found {
			if _, shadowed := resolveIdent(fun.Name, fun.Pos(), stack); !shadowed {
				return true
			}
		}
		d, ok := resolveIdent(fun.Name, fun.Pos(), stack)
		if !ok {
			return false
		}
		lit, isLit := d.init.(*ast.FuncLit)
		if !isLit || lit.Type.Results == nil {
			return false
		}
		return slices.ContainsFunc(lit.Type.Results.List, func(r *ast.Field) bool {
			return mw3IsSessionType(r.Type, f.names, "Session")
		})
	}
	return false
}

// isConfig reports whether e is a session Config, or a store, which embeds one.
func (f *mw3SessionFile) isConfig(e ast.Expr, stack []ast.Node) bool {
	return f.isStore(e, stack) || f.isConfigValue(e, stack)
}

// isConfigValue reports whether e is a variable that holds a session Config.
func (f *mw3SessionFile) isConfigValue(e ast.Expr, stack []ast.Node) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	if !ok {
		return false
	}
	if d.typ != nil {
		return mw3IsSessionType(d.typ, f.names, mw3Config)
	}
	init := d.init
	if un, isUnary := init.(*ast.UnaryExpr); isUnary {
		init = un.X
	}
	if _, isLit := f.isConfigLit(init, stack); isLit {
		return true
	}
	if init == nil {
		return false
	}
	if f.isMember(init, mw3ConfigDefault, stack) {
		return true
	}

	// A copy of a config is one: cfg := base, cfg := *ptr, cfg := store.Config.
	switch x := init.(type) {
	case *ast.Ident:
		return f.isConfigValue(x, stack)
	case *ast.StarExpr:
		return f.isConfigValue(x.X, stack)
	case *ast.SelectorExpr:
		return x.Sel.Name == mw3Config && f.isStore(x.X, stack)
	}
	return false
}
