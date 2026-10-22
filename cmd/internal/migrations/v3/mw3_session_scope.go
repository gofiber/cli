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
	if !pkgs.of(fi, head.Name.Name).sessions().holdsSessions() {
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

// reader returns the reader of the kinds of this file.
func (f *mw3SessionFile) reader() *mw3KindReader {
	return &mw3KindReader{facts: f.facts(), names: f.names}
}

// isStore reports whether e is a session store: a variable declared as one or
// created by session.New, a package level store, a field of a struct that holds
// one (also of a struct in one that embeds it), an element of a collection of
// stores, a function that returns one, or a type assertion to one.
func (f *mw3SessionFile) isStore(e ast.Expr, stack []ast.Node) bool {
	return f.reader().ofExpr(e, stack).base == mw3KindStore
}

// isSession reports whether e is a session: a variable declared as one, or the
// result of getting one from a store or of a function that returns one, in the
// same ways as a store.
func (f *mw3SessionFile) isSession(e ast.Expr, stack []ast.Node) bool {
	return f.reader().ofExpr(e, stack).base == mw3KindSession
}

// isConfig reports whether e is a session Config, or a store, which embeds one.
func (f *mw3SessionFile) isConfig(e ast.Expr, stack []ast.Node) bool {
	kind := f.reader().ofExpr(e, stack)
	return kind.base == mw3KindStore || kind.base == mw3KindConfig
}

// isConfigValue reports whether e is a session Config: a variable that holds
// one, a copy of one, a field of that type, or session.ConfigDefault.
func (f *mw3SessionFile) isConfigValue(e ast.Expr, stack []ast.Node) bool {
	return f.reader().ofExpr(e, stack).base == mw3KindConfig
}
