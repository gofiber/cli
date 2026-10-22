package v3

import (
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal"
)

// appIndex lets the app migrations look beyond the file they rewrite: the other
// files of its package (a test or a main.go often starts an app that another
// file creates) and, through the module's own import paths, the other packages
// of the project. Files are parsed once and never re-read, so a migration sees
// the project as it was when the migration started.
type appIndex struct {
	files    map[string]*appFile
	pkgs     map[appPkgKey]*appPackage
	fields   map[appFieldKey]appOrigins
	root     string
	mods     []appModule
	all      []*appFile
	modsDone bool
}

// appFieldKey identifies a field of a declared struct type.
type appFieldKey struct {
	td    *appTypeDecl
	field string
}

// appPkgKey identifies a package: a directory can hold several (p and p_test).
type appPkgKey struct {
	dir  string
	name string
}

// appModule is a Go module found under the migrated directory.
type appModule struct {
	path string
	dir  string
}

func newAppIndex(root string) *appIndex {
	return &appIndex{
		root:   root,
		files:  map[string]*appFile{},
		pkgs:   map[appPkgKey]*appPackage{},
		fields: map[appFieldKey]appOrigins{},
	}
}

// appFile is a parsed Go file together with what the app migrations need to
// know about it.
type appFile struct {
	ix     *appIndex
	g      *goSource
	parent map[ast.Node]ast.Node
	pkg    *appPackage
	// fi describes the file for notes; walked is false for files that were only
	// read to understand the project, which a migration must not change.
	fi      internal.FileInfo
	path    string
	names   []string
	imports []appImportSpec
	walked  bool
}

// appImportSpec is one import of a file.
type appImportSpec struct {
	// name is the explicit alias, empty when the import has none.
	name string
	path string
}

// appPackage holds the declarations of the files that share a directory and a
// package clause.
type appPackage struct {
	funcs   map[string]*appFunc
	methods map[string]*appFunc
	types   map[string]*appTypeDecl
	vars    map[string]*appVarDecl
	consts  map[string]*appVarDecl
	// names holds every package-level name, whatever it declares.
	names map[string]bool
	dir   string
	name  string
	files []*appFile
}

// appFunc is a function or method declaration.
type appFunc struct {
	file *appFile
	decl *ast.FuncDecl
}

// appTypeDecl is a package-level type declaration.
type appTypeDecl struct {
	file *appFile
	spec *ast.TypeSpec
}

// appVarDecl is a package-level variable or constant. index is the position of
// the name in the declaration.
type appVarDecl struct {
	file  *appFile
	spec  *ast.ValueSpec
	index int
}

// openSource parses src as the file at path and makes it the index's version of
// that file. ok is false when src is not valid Go.
func (ix *appIndex) openSource(path, src string) (f *appFile, ok bool) {
	if f = ix.files[path]; f != nil && f.g.src == src {
		return f, true
	}
	g, ok := parseSource(src)
	if !ok {
		return nil, false
	}
	return ix.adopt(path, g), true
}

// openFile is openSource for a file that is being migrated: when it does not
// parse, the user is told that it was skipped.
func (ix *appIndex) openFile(fi internal.FileInfo, src string) (f *appFile, ok bool) {
	f = ix.files[fi.Path]
	if f == nil || f.g.src != src {
		g, parsed := parseFile(fi, src)
		if !parsed {
			return nil, false
		}
		f = ix.adopt(fi.Path, g)
	}
	f.fi, f.walked = fi, true
	return f, true
}

// adopt makes g the index's version of the file at path.
func (ix *appIndex) adopt(path string, g *goSource) *appFile {
	f := ix.newFile(path, g)
	ix.files[path] = f

	// the packages of this directory were built from the previous version
	for key, pkg := range ix.pkgs {
		if key.dir != filepath.Dir(path) {
			continue
		}
		for _, pf := range pkg.files {
			pf.pkg = nil
		}
		delete(ix.pkgs, key)
	}
	return f
}

// fileAt returns the file at path as it is on disk, nil when it cannot be read
// or parsed.
func (ix *appIndex) fileAt(path string) *appFile {
	if f, seen := ix.files[path]; seen {
		return f
	}
	var f *appFile
	if src, err := os.ReadFile(path); err == nil { // #nosec G304 -- the user's own project tree
		if g, ok := parseSource(string(src)); ok {
			f = ix.newFile(path, g)
		}
	}
	ix.files[path] = f
	return f
}

func (ix *appIndex) newFile(path string, g *goSource) *appFile {
	f := &appFile{
		ix:     ix,
		path:   path,
		g:      g,
		names:  fiberNames(g.src),
		parent: map[ast.Node]ast.Node{},
	}

	var stack []ast.Node
	ast.Inspect(g.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			f.parent[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})

	for _, spec := range g.file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		imp := appImportSpec{path: p}
		if spec.Name != nil {
			imp.name = spec.Name.Name
		}
		f.imports = append(f.imports, imp)
	}
	return f
}

// pkgOf returns the package f belongs to.
func (ix *appIndex) pkgOf(f *appFile) *appPackage {
	if f.pkg != nil {
		return f.pkg
	}
	key := appPkgKey{dir: filepath.Dir(f.path), name: f.g.file.Name.Name}
	pkg := ix.pkgs[key]
	if pkg == nil {
		pkg = ix.loadPackage(key.dir, key.name, f)
		ix.pkgs[key] = pkg
	}
	f.pkg = pkg
	return pkg
}

// loadPackage gathers the files of dir whose package clause is name. self is
// the file that is being migrated and is used as it is in memory.
func (ix *appIndex) loadPackage(dir, name string, self *appFile) *appPackage {
	pkg := &appPackage{
		dir:     dir,
		name:    name,
		funcs:   map[string]*appFunc{},
		methods: map[string]*appFunc{},
		types:   map[string]*appTypeDecl{},
		vars:    map[string]*appVarDecl{},
		consts:  map[string]*appVarDecl{},
		names:   map[string]bool{},
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		entries = nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f := self
		if self == nil || path != self.path {
			f = ix.fileAt(path)
		}
		if f == nil || f.g.file.Name.Name != name {
			continue
		}
		pkg.files = append(pkg.files, f)
	}
	if self != nil && !slices.Contains(pkg.files, self) {
		pkg.files = append(pkg.files, self)
	}

	for _, f := range pkg.files {
		f.pkg = pkg
		pkg.declare(f)
	}
	return pkg
}

// declare records the package-level declarations of f.
func (p *appPackage) declare(f *appFile) {
	for _, d := range f.g.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				p.funcs[d.Name.Name] = &appFunc{file: f, decl: d}
				p.names[d.Name.Name] = true
			} else if recv := appReceiverTypeName(d.Recv); recv != "" {
				p.methods[recv+"."+d.Name.Name] = &appFunc{file: f, decl: d}
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					p.types[spec.Name.Name] = &appTypeDecl{file: f, spec: spec}
					p.names[spec.Name.Name] = true
				case *ast.ValueSpec:
					for i, id := range spec.Names {
						p.names[id.Name] = true
						switch d.Tok {
						case token.VAR:
							p.vars[id.Name] = &appVarDecl{file: f, spec: spec, index: i}
						case token.CONST:
							p.consts[id.Name] = &appVarDecl{file: f, spec: spec, index: i}
						default:
						}
					}
				}
			}
		}
	}
}

// appReceiverTypeName returns the name of the type a method is declared on.
func appReceiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	return appBaseTypeName(recv.List[0].Type)
}

// appBaseTypeName returns the name of a possibly pointer or generic type
// expression, empty when it is not a plain type name.
func appBaseTypeName(e ast.Expr) string {
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
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// loadModules finds the go.mod files below the root.
func (ix *appIndex) loadModules() {
	if ix.modsDone {
		return
	}
	ix.modsDone = true

	//nolint:errcheck // an unreadable directory only means a package cannot be followed
	_ = filepath.WalkDir(ix.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // keep walking
		}
		if d.IsDir() {
			switch d.Name() {
			case appVendorDir, nameDotGit, nameNodeModules:
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		data, readErr := os.ReadFile(path) // #nosec G304 -- the user's own project tree
		if readErr != nil {
			return nil //nolint:nilerr // keep walking
		}
		if mod := modfile.ModulePath(data); mod != "" {
			ix.mods = append(ix.mods, appModule{path: mod, dir: filepath.Dir(path)})
		}
		return nil
	})
	slices.SortFunc(ix.mods, func(a, b appModule) int { return len(b.path) - len(a.path) })
}

// moduleFiles returns every Go file below the root that can be parsed.
func (ix *appIndex) moduleFiles() []*appFile {
	if ix.all != nil {
		return ix.all
	}
	ix.all = []*appFile{}

	//nolint:errcheck // an unreadable directory only means its files are not looked at
	_ = filepath.WalkDir(ix.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // keep walking
		}
		if d.IsDir() {
			switch d.Name() {
			case appVendorDir, ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			if f := ix.fileAt(path); f != nil {
				ix.all = append(ix.all, f)
			}
		}
		return nil
	})
	return ix.all
}

// appVendorDir is the directory the migrations never touch.
const appVendorDir = "vendor"

// dirOfImport maps an import path of the project's own modules to its
// directory.
func (ix *appIndex) dirOfImport(importPath string) (string, bool) {
	ix.loadModules()
	for _, m := range ix.mods {
		switch {
		case importPath == m.path:
			return m.dir, true
		case strings.HasPrefix(importPath, m.path+"/"):
			return filepath.Join(m.dir, filepath.FromSlash(strings.TrimPrefix(importPath, m.path+"/"))), true
		}
	}
	return "", false
}

// packageAtImport returns the package behind an import path of the project's
// own modules.
func (ix *appIndex) packageAtImport(importPath string) *appPackage {
	dir, ok := ix.dirOfImport(importPath)
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	counts := map[string]int{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if f := ix.fileAt(filepath.Join(dir, e.Name())); f != nil {
			counts[f.g.file.Name.Name]++
		}
	}
	best, bestCount := "", 0
	for name, n := range counts {
		if n > bestCount || (n == bestCount && name < best) {
			best, bestCount = name, n
		}
	}
	if best == "" {
		return nil
	}

	key := appPkgKey{dir: dir, name: best}
	if pkg := ix.pkgs[key]; pkg != nil {
		return pkg
	}
	pkg := ix.loadPackage(dir, best, nil)
	ix.pkgs[key] = pkg
	return pkg
}

// packageNamed returns the package an identifier of f names through its
// imports, restricted to the project's own modules.
func (f *appFile) packageNamed(name string) *appPackage {
	for _, imp := range f.imports {
		switch {
		case imp.name == "_" || imp.name == ".":
			continue
		case imp.name != "":
			if imp.name != name {
				continue
			}
		case defaultImportName(imp.path) != name:
			// the package clause may differ from the last path element; look
			// at the project's own packages before giving up
			pkg := f.ix.packageAtImport(imp.path)
			if pkg == nil || pkg.name != name {
				continue
			}
		}
		return f.ix.packageAtImport(imp.path)
	}
	return nil
}

// stack returns the ancestors of n, outermost first.
func (f *appFile) stack(n ast.Node) []ast.Node {
	var rev []ast.Node
	for p := f.parent[n]; p != nil; p = f.parent[p] {
		rev = append(rev, p)
	}
	slices.Reverse(rev)
	return rev
}

// enclosingDecl returns the top-level function declaration that contains n.
func (f *appFile) enclosingDecl(n ast.Node) *ast.FuncDecl {
	var decl *ast.FuncDecl
	for p := f.parent[n]; p != nil; p = f.parent[p] {
		if fd, ok := p.(*ast.FuncDecl); ok {
			decl = fd
		}
	}
	return decl
}
