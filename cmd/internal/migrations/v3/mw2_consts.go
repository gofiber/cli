package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// A key of the request locals is rarely a string literal. It is the name of a
// constant, of a variable that nothing assigns to, of an alias of one of those, a
// concatenation of them, and in the packages that read what a middleware stores
// it is mostly a name of another package of the module (c.Locals(auth.ContextKey)
// where the config says ContextKey: ContextKey). The compiler resolves all of it;
// mw2ConstScope reads it the same way, as far as the module goes.

// mw2ConstDepth bounds how many declarations a name is followed through.
const mw2ConstDepth = 8

// mw2ConstScope is where an expression is written, which is what gives its names a
// meaning: the package it belongs to and the imports of its file.
type mw2ConstScope struct {
	pkgs    *mw2Packages
	pkg     *mw2Package
	imports []*ast.ImportSpec
}

// str evaluates e as a string that is known before the program runs.
func (s mw2ConstScope) str(e ast.Expr, depth int) (string, bool) {
	if depth > mw2ConstDepth || s.pkg == nil {
		return "", false
	}

	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(x.Value)
		return v, err == nil
	case *ast.ParenExpr:
		return s.str(x.X, depth+1)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, ok := s.str(x.X, depth+1)
		if !ok {
			return "", false
		}
		r, ok := s.str(x.Y, depth+1)
		return l + r, ok
	case *ast.Ident:
		return s.declared(s.pkg, x.Name, depth, false)
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		target := s.imported(id.Name)
		if target == nil {
			return "", false
		}
		return s.declared(target, x.Sel.Name, depth, true)
	}
	return "", false
}

// declared evaluates the declaration of name in pkg: a constant, or a variable
// that nothing in the module assigns to. A variable has to be a string or leave its
// type to the initializer, and so does a constant that another package names: of
// another type it is another key (see mw2Key.typ), which only a name of the
// package itself can tell apart from the string.
func (s mw2ConstScope) declared(pkg *mw2Package, name string, depth int, qualified bool) (string, bool) {
	if pkg.twice[name] {
		return "", false
	}
	c, isConst := pkg.consts[name]
	if !isConst {
		var ok bool
		if c, ok = pkg.vars[name]; !ok || s.pkgs == nil || s.pkgs.assigned(pkg.dir, name) {
			return "", false
		}
	}
	if c.value == nil || ((qualified || !isConst) && c.typ != nil && mw2TypeText(c.typ) != nameString) {
		return "", false
	}
	return mw2ConstScope{pkgs: s.pkgs, pkg: pkg, imports: c.imports}.str(c.value, depth+1)
}

// imported returns the package of the module that the identifier names in the
// file, nil when it names none.
func (s mw2ConstScope) imported(name string) *mw2Package {
	if s.pkgs == nil {
		return nil
	}
	for _, spec := range s.imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || (spec.Name != nil && spec.Name.Name != name) {
			continue
		}
		if pkg := s.pkgs.atImport(s.pkg.dir, importPath); pkg != nil && (spec.Name != nil || pkg.name == name) {
			return pkg
		}
	}
	return nil
}

// atImport returns the package that the import path names when it is a package of
// the module that the directory belongs to, nil for any other package.
func (p *mw2Packages) atImport(fromDir, importPath string) *mw2Package {
	if p.root == "" {
		return nil
	}
	root := mw2ModuleRoot(p.root, filepath.Clean(fromDir))
	modPath := p.modulePath(root)
	if modPath == "" {
		return nil
	}

	var rel string
	switch {
	case importPath == modPath:
		rel = "."
	case strings.HasPrefix(importPath, modPath+"/"):
		rel = importPath[len(modPath)+1:]
	default:
		return nil
	}

	dir := filepath.Join(root, filepath.FromSlash(rel))
	// a directory with a go.mod of its own is another module
	if mw2ModuleRoot(p.root, dir) != root {
		return nil
	}
	name := p.packageName(dir)
	if name == "" {
		return nil
	}
	return p.of(dir, name)
}

// modulePath returns the path of the module with the go.mod at root, "" when there
// is none.
func (p *mw2Packages) modulePath(root string) string {
	if path, ok := p.modules[root]; ok {
		return path
	}
	path := ""
	if data, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil { // #nosec G304 -- the go.mod of the project being migrated
		path = modfile.ModulePath(data)
	}
	p.modules[root] = path
	return path
}

// packageName returns the name that most files of the directory give their
// package, test files left out; "" when it has none.
func (p *mw2Packages) packageName(dir string) string {
	if name, ok := p.names[dir]; ok {
		return name
	}

	counts := map[string]int{}
	var order []string
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.PackageClauseOnly)
			if parseErr != nil || file == nil {
				continue
			}
			if counts[file.Name.Name] == 0 {
				order = append(order, file.Name.Name)
			}
			counts[file.Name.Name]++
		}
	}

	name := ""
	for _, candidate := range order {
		if name == "" || counts[candidate] > counts[name] {
			name = candidate
		}
	}
	p.names[dir] = name
	return name
}

// assigned reports whether any code of the module that holds dir assigns to a
// variable called name, takes its address or counts it: then the variable may not
// hold its initializer when a key is read. The test is on the name, so a field
// that is called like the variable counts as well, and a variable that cannot be
// told apart from an assigned one is never taken for a constant. Without a module
// to read nothing is known.
func (p *mw2Packages) assigned(dir, name string) bool {
	if p.root == "" {
		return true
	}
	root := mw2ModuleRoot(p.root, filepath.Clean(dir))
	names, ok := p.assignedIn[root]
	if !ok {
		names = p.assignedNames(root)
		p.assignedIn[root] = names
	}
	return names[name]
}

// assignedNames collects the names that the code of the module at root assigns to.
func (p *mw2Packages) assignedNames(root string) map[string]bool {
	names := map[string]bool{}
	mark := func(e ast.Expr) {
		for e != nil {
			switch x := e.(type) {
			case *ast.ParenExpr:
				e = x.X
			case *ast.Ident:
				names[x.Name] = true
				return
			case *ast.SelectorExpr:
				names[x.Sel.Name] = true
				return
			default:
				return
			}
		}
	}

	// A module that cannot be read has nothing the variable could be assigned by.
	if err := mw2Walk(root, func(path string) {
		if mw2ModuleRoot(p.root, filepath.Dir(filepath.Clean(path))) != root {
			return
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				if x.Tok != token.DEFINE {
					for _, lhs := range x.Lhs {
						mark(lhs)
					}
				}
			case *ast.IncDecStmt:
				mark(x.X)
			case *ast.UnaryExpr:
				if x.Op == token.AND {
					mark(x.X)
				}
			case *ast.RangeStmt:
				if x.Tok == token.ASSIGN {
					mark(x.Key)
					mark(x.Value)
				}
			}
			return true
		})
	}); err != nil {
		return names
	}
	return names
}

// scope returns where the code of file is read.
func (kr *mw2KeyResolver) scope() mw2ConstScope {
	return mw2ConstScope{pkgs: kr.pkgs, pkg: kr.pkg, imports: kr.g.file.Imports}
}

// qualified resolves pkg.Name, a name of another package of the module: its value
// when it is a string known beforehand, otherwise the declaration itself, which
// is the same key wherever it is named from (the config of one package and the
// read of another).
func (kr *mw2KeyResolver) qualified(sel *ast.SelectorExpr, stack []ast.Node) (mw2Key, bool) {
	id, ok := sel.X.(*ast.Ident)
	if !ok || kr.pkgs == nil || modDeclaredLocally(id.Name, id.Pos(), stack) {
		return mw2Key{}, false
	}
	scope := kr.scope()
	target := scope.imported(id.Name)
	if target == nil || !target.decls[sel.Sel.Name] {
		return mw2Key{}, false
	}
	if v, known := scope.str(sel, 0); known {
		return mw2Key{val: v}, true
	}
	return mw2Key{val: sel.Sel.Name, dir: target.dir, expr: true}, true
}
