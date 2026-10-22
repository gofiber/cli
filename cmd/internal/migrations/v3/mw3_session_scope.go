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

// mw3Shadowed reports whether a declaration hides the name at pos, where the
// node whose ancestors are stack sits. resolveIdent finds the variables that
// say something about their type or value; the declarations that do not are
// looked for here: the variables of a range clause, the binding of a type switch
// or of a select case, the constants and types a function declares, and the
// type parameters of a function.
func mw3Shadowed(name string, pos token.Pos, stack []ast.Node) bool {
	if _, declared := resolveIdent(name, pos, stack); declared {
		return true
	}
	for _, node := range slices.Backward(stack) {
		switch n := node.(type) {
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE && n.Body != nil && pos >= n.Body.Pos() &&
				(isIdentNamed(n.Key, name) || isIdentNamed(n.Value, name)) {
				return true
			}
		case *ast.TypeSwitchStmt:
			if as, ok := n.Assign.(*ast.AssignStmt); ok && n.Body != nil && pos >= n.Body.Pos() &&
				len(as.Lhs) == 1 && isIdentNamed(as.Lhs[0], name) {
				return true
			}
		case *ast.CommClause:
			if as, ok := n.Comm.(*ast.AssignStmt); ok && as.Tok == token.DEFINE && pos > as.End() &&
				slices.ContainsFunc(as.Lhs, func(e ast.Expr) bool { return isIdentNamed(e, name) }) {
				return true
			}
			if mw3DeclaresConstOrType(n.Body, name, pos) {
				return true
			}
		case *ast.BlockStmt:
			if mw3DeclaresConstOrType(n.List, name, pos) {
				return true
			}
		case *ast.CaseClause:
			if mw3DeclaresConstOrType(n.Body, name, pos) {
				return true
			}
		case *ast.FuncDecl:
			if _, found := declInFieldList(name, n.Type.TypeParams); found {
				return true
			}
		}
	}
	return false
}

// mw3DeclaresConstOrType reports whether a statement of the list that ends
// before pos declares a constant or a type called name.
func mw3DeclaresConstOrType(stmts []ast.Stmt, name string, pos token.Pos) bool {
	for _, stmt := range stmts {
		ds, ok := stmt.(*ast.DeclStmt)
		if !ok || ds.End() > pos {
			continue
		}
		gd, ok := ds.Decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.TYPE) {
			continue
		}
		for _, spec := range gd.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if s.Name.Name == name {
					return true
				}
			case *ast.ValueSpec:
				if slices.ContainsFunc(s.Names, func(id *ast.Ident) bool { return id.Name == name }) {
					return true
				}
			}
		}
	}
	return false
}

// mw3SessionFile is a file that imports the session middleware, parsed, with
// what it takes to tell the session package and its values from other things
// of the same name.
type mw3SessionFile struct {
	g    *goSource
	pkgs *mw3Pkgs
	pkg  *mw3Pkg
	env  *mw3Env
	// declared are the names the file declares anywhere, read when first asked for.
	declared map[string]struct{}
	fi       internal.FileInfo
	names    []string
	fiber    []string
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
	return &mw3SessionFile{g: g, pkgs: pkgs, fi: fi, names: names, fiber: fiberNames(src), env: pkgs.envOf(fi, g.file, src)}, true
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

	head, err := parser.ParseFile(token.NewFileSet(), fi.Path, src, parser.ImportsOnly)
	if err != nil {
		return nil, false
	}
	pkg := pkgs.of(fi, head.Name.Name)
	if !pkg.sessions().holdsSessions() && !pkg.importsSessions(head) {
		return nil, false
	}
	g, ok := parseFile(fi, src)
	if !ok {
		return nil, false
	}
	return &mw3SessionFile{g: g, pkgs: pkgs, fi: fi, fiber: fiberNames(src), env: pkgs.envOf(fi, g.file, src)}, true
}

// pkgOf returns what is known about the package the file belongs to.
func (f *mw3SessionFile) pkgOf() *mw3Pkg {
	if f.pkg == nil {
		f.pkg = f.pkgs.of(f.fi, f.g.file.Name.Name)
	}
	return f.pkg
}

// facts returns what the package says about session stores.
func (f *mw3SessionFile) facts() *mw3SessionFacts {
	return f.pkgOf().sessions()
}

// strings returns the reader of string values of the file.
func (f *mw3SessionFile) strings() mw3Strings {
	return newMw3Strings(f.g, f.pkgOf())
}

// isPackage reports whether the identifier names the session package here and
// not a variable that has the same name.
func (f *mw3SessionFile) isPackage(id *ast.Ident, stack []ast.Node) bool {
	return slices.Contains(f.names, id.Name) && !mw3Shadowed(id.Name, id.Pos(), stack)
}

// taken tells whether a new import of the file cannot have the name: something
// declares it in the file or in the package, an import that is about to be added
// has it, or the code to be written declares it itself (reserved).
func (f *mw3SessionFile) taken(imports *mw3ImportEdits, reserved []string) func(name string) bool {
	if f.declared == nil {
		f.declared = mw3DeclaredNames(f.g.file)
	}
	return func(name string) bool {
		if _, ok := f.declared[name]; ok || slices.Contains(reserved, name) {
			return true
		}
		if _, ok := f.pkgOf().declared()[name]; ok {
			return true
		}
		return slices.ContainsFunc(imports.add, func(a mw3ImportAdd) bool { return a.name == name })
	}
}

// importAt returns the identifier that code written at pos, where the node whose
// ancestors are stack sits, uses for the package at path. A file that does not
// import the package gets the import scheduled, under the first of the
// candidates that is free in the file and in its package. A file that does, but
// under a name that a declaration hides at pos or that the code declares itself
// (reserved), gets the package imported a second time under a name that is free.
func (f *mw3SessionFile) importAt(imports *mw3ImportEdits, path string, pos token.Pos, stack []ast.Node, reserved []string, candidates ...string) string {
	name := mw3ImportName(f.g.src, path)
	if name == "" {
		name, _ = imports.scheduled(path, false)
	}
	if name != "" && !slices.Contains(reserved, name) && !mw3Shadowed(name, pos, stack) {
		return name
	}

	if name == "" {
		name = mw3FreeName(f.taken(imports, reserved), candidates...)
		imports.require(path, name)
		return name
	}
	if alias, ok := imports.scheduled(path, true); ok {
		return alias
	}
	alias := mw3FreeName(f.taken(imports, reserved), candidates...)
	imports.requireExtra(path, alias)
	return alias
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

// reader returns the reader of the kinds of this file.
func (f *mw3SessionFile) reader() *mw3KindReader {
	return f.facts().reader(f.env)
}

// member says where x.name comes from, for the expression x whose ancestors are
// stack: the session type that provides it when x is, or embeds, a store, a
// session or a config. x is a store when it is a variable declared as one or
// created by session.New, a package level store, a field of a struct that holds
// one (also of a struct in one that embeds it), an element of a collection of
// stores, a function that returns one, or a type assertion to one; a session and
// a config are found in the same ways.
func (f *mw3SessionFile) member(x ast.Expr, name string, stack []ast.Node) mw3Member {
	r := f.reader()
	return r.lookup(r.ofExpr(x, stack), name)
}

// providedBy is member for the callers that look for one session type: ok is
// whether x.name is a member of base.
func (f *mw3SessionFile) providedBy(x ast.Expr, name string, base mw3Base, stack []ast.Node) (m mw3Member, ok bool) {
	m = f.member(x, name, stack)
	return m, m.base == base
}
