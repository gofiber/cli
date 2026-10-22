package v3

import (
	"bytes"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal"
)

// A moduleIndex is the view of the packages of one Go module that a migration
// needs when the code it rewrites is tied to code in another package: the
// constructor in internal/server that returns the app a main package starts, the
// store that an auth package hands to the handlers, the healthcheck config that a
// library exports. Every other analysis of the migrations reads one directory;
// this one reaches the whole module, without type checking (the dependencies of
// the module are not downloaded when the migrations run).
//
//   - Packages are found by import path, and only the packages of the module are:
//     a nested module (a directory with a go.mod of its own), vendor, testdata,
//     node_modules and the directories Go itself ignores (a name that starts with
//     "." or "_") are not part of it. Files marked "go:build ignore" belong to no
//     package. The other build constraints are not evaluated, so a name that two
//     files declare for different platforms has two declarations (see modDecl.dup).
//   - Everything is read when it is first asked for, and once: a file is parsed at
//     most once per index. The text is the one on disk at that moment, so the index
//     sees the files as the migrations that ran before have left them; a migration
//     that rewrites files while it uses the index calls invalidate for each file it
//     has written.
//   - A name is resolved through declarations only: package level functions,
//     variables, constants, types, the methods of a type and the fields of a
//     struct, through the imports of the file the name is written in (aliases,
//     dot imports, blank imports). Local variables and parameters are not looked
//     at, that is the job of the migration (see resolveIdent and modDeclaredLocally):
//     the index answers what a name at package level is, not what a variable holds.
//   - A nil index knows nothing, which is what newModuleIndex returns outside of a
//     module. Every method accepts it, so a migration does not check for nil.
//
// An index is built per run and cached by the caller (see moduleIndexes), and it
// is not safe for concurrent use. A migration uses it like this:
//
//	var indexes moduleIndexes                   // one per run of the migration
//	ix := indexes.of(fi)                        // nil outside of a module
//	f := ix.fileAt(fi.Path)                     // the file being migrated, as the index sees it
//	d := f.declOf(sel)                          // the declaration pkg.Name or Name refers to
//	for _, res := range d.results() { ... }     // what a function returns: res.is(target)
//	for _, user := range ix.importers(path) { } // the files that call into a package
//
// The types (modType, modTarget) and the finders (funcsReturning, varsHolding,
// fieldsOfType) are in modindex_types.go.
type moduleIndex struct {
	dirs   map[string]*modDir
	files  map[string]*modFile
	pkgs   map[string]*modPackage
	users  map[string][]*modFile
	member map[string]bool
	// hub hands out the indexes of the other modules of the repository, nil for an
	// index that knows only its own module.
	hub   *moduleIndexes
	mod   goModule
	order []string
	// listed is set once order holds every directory of the module.
	listed bool
}

// moduleIndexes hands out the index of the module a file belongs to, one per
// module for the length of a run.
//
// A repository can hold several modules below the directory the run starts in: a
// go.work workspace, a module next to the one that imports it with a replace
// directive, a nested module. The index of each one reads its own module, and
// answers for the import paths and directories of the others too (dirOf, packageAt,
// packageIn, importPathOf, fileAt and importers): a package of another module of
// the repository is as much the project's code as one of the module itself. The
// packages the finders go through (packages) stay those of the module.
type moduleIndexes struct {
	byRoot map[string]*moduleIndex
	// root is the directory the workspace was found for.
	root string
	// workspace are the modules below the directory the run started in, longest
	// path first, found when the first index is asked for.
	workspace []goModule
}

// of returns the index of the module the file belongs to, nil when it belongs to
// none.
func (m *moduleIndexes) of(fi internal.FileInfo) *moduleIndex {
	mod, ok := moduleOf(fi.Path, fi.Root)
	if !ok {
		return nil
	}
	m.findWorkspace(fi.Root)
	return m.index(mod)
}

// index returns the index of the module, made when it is first asked for.
func (m *moduleIndexes) index(mod goModule) *moduleIndex {
	if ix := m.byRoot[mod.root]; ix != nil {
		return ix
	}
	if m.byRoot == nil {
		m.byRoot = map[string]*moduleIndex{}
	}
	ix := newModuleIndexOf(mod)
	ix.hub = m
	m.byRoot[mod.root] = ix
	return ix
}

// findWorkspace finds the modules below the directory the run starts in, once.
func (m *moduleIndexes) findWorkspace(root string) {
	abs, err := filepath.Abs(root)
	if err != nil || (m.root == abs && m.workspace != nil) {
		return
	}
	m.root = abs
	m.workspace = []goModule{}

	//nolint:errcheck // a directory that cannot be read only means its modules are not in the workspace
	_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // keep walking
		}
		if d.IsDir() {
			if p != abs && modSkippedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		if data, readErr := os.ReadFile(p); readErr == nil { // #nosec G304 -- the user's own project tree
			if modPath := modfile.ModulePath(data); modPath != "" {
				m.workspace = append(m.workspace, goModule{root: filepath.Dir(p), path: modPath})
			}
		}
		return nil
	})
	slices.SortFunc(m.workspace, func(a, b goModule) int { return len(b.path) - len(a.path) })
}

// newModuleIndex returns the index of the module the file belongs to: the module
// of the nearest go.mod, not above the directory the migration runs in. It is nil
// when there is none. An index made this way knows no other module.
func newModuleIndex(fi internal.FileInfo) *moduleIndex {
	mod, ok := moduleOf(fi.Path, fi.Root)
	if !ok {
		return nil
	}
	return newModuleIndexOf(mod)
}

func newModuleIndexOf(mod goModule) *moduleIndex {
	return &moduleIndex{
		mod:    mod,
		dirs:   map[string]*modDir{},
		files:  map[string]*modFile{},
		pkgs:   map[string]*modPackage{},
		users:  map[string][]*modFile{},
		member: map[string]bool{},
	}
}

// peerFor returns the index of the other module of the repository the import path
// names, nil when it names this module or none of the repository.
func (ix *moduleIndex) peerFor(importPath string) *moduleIndex {
	if ix == nil || ix.hub == nil {
		return nil
	}
	var best *goModule
	for i := range ix.hub.workspace {
		mod := &ix.hub.workspace[i]
		if (importPath == mod.path || strings.HasPrefix(importPath, mod.path+"/")) && (best == nil || len(mod.path) > len(best.path)) {
			best = mod
		}
	}
	if best == nil || best.root == ix.mod.root {
		return nil
	}
	return ix.hub.index(*best)
}

// peerAt returns the index of the other module of the repository the directory is
// in, nil when it is in this module or in none of the repository.
func (ix *moduleIndex) peerAt(dir string) *moduleIndex {
	if ix == nil || ix.hub == nil {
		return nil
	}
	var best *goModule
	for i := range ix.hub.workspace {
		mod := &ix.hub.workspace[i]
		if isWithin(mod.root, dir) && (best == nil || len(mod.root) > len(best.root)) {
			best = mod
		}
	}
	if best == nil || best.root == ix.mod.root {
		return nil
	}
	return ix.hub.index(*best)
}

// peers returns the indexes of the other modules of the repository, in the order
// of their directories.
func (ix *moduleIndex) peers() []*moduleIndex {
	if ix == nil || ix.hub == nil {
		return nil
	}
	var out []*moduleIndex
	for _, mod := range ix.hub.workspace {
		if mod.root != ix.mod.root {
			out = append(out, ix.hub.index(mod))
		}
	}
	slices.SortFunc(out, func(a, b *moduleIndex) int { return strings.Compare(a.mod.root, b.mod.root) })
	return out
}

// modDir is a directory of the module that holds Go files.
type modDir struct {
	// pkg is the package the other packages import: the package clause that most
	// of the files of the directory have, the tests of the package included.
	pkg *modPackage
	// xtest is the package of the external tests (package name_test), nil when there
	// are none.
	xtest *modPackage
	dir   string
	// names are the Go files of the directory, sorted.
	names  []string
	loaded bool
	// complete is false when a file of the directory does not parse or belongs to
	// another package than the rest: it may declare or use anything.
	complete bool
}

// pkgOrNil and xtestOrNil read the packages of a directory that may be unknown.
func (md *modDir) pkgOrNil() *modPackage {
	if md == nil {
		return nil
	}
	return md.pkg
}

func (md *modDir) xtestOrNil() *modPackage {
	if md == nil {
		return nil
	}
	return md.xtest
}

// modMaxDepth bounds how far the resolvers follow aliases, declarations and
// embedded types, which also keeps them from looping on cyclic ones.
const modMaxDepth = 8

// modSkippedDir reports whether Go leaves the directory out of the packages of a
// module, or it is somewhere no Go package of the project is.
func modSkippedDir(name string) bool {
	switch name {
	case appVendorDir, "testdata", nameNodeModules:
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// contains reports whether the directory is part of the module: below its root,
// not inside a nested module and not in a directory Go ignores.
func (ix *moduleIndex) contains(dir string) bool {
	if dir == ix.mod.root {
		return true
	}
	if !isWithin(ix.mod.root, dir) {
		return false
	}
	if in, known := ix.member[dir]; known {
		return in
	}

	in := ix.contains(filepath.Dir(dir)) && !modSkippedDir(filepath.Base(dir))
	if in {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		in = err != nil
	}
	ix.member[dir] = in
	return in
}

// importPathOf returns the import path of a directory of the module, or of another
// module of the repository.
func (ix *moduleIndex) importPathOf(dir string) (string, bool) {
	if ix == nil {
		return "", false
	}
	if !ix.contains(dir) {
		if peer := ix.peerAt(dir); peer != nil {
			return peer.importPathOf(dir)
		}
		return "", false
	}
	rel, err := filepath.Rel(ix.mod.root, dir)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return ix.mod.path, true
	}
	return path.Join(ix.mod.path, filepath.ToSlash(rel)), true
}

// dirOf returns the directory of an import path of the module, or of another
// module of the repository.
func (ix *moduleIndex) dirOf(importPath string) (string, bool) {
	if ix == nil {
		return "", false
	}
	if peer := ix.peerFor(importPath); peer != nil {
		return peer.dirOf(importPath)
	}
	var dir string
	if importPath == ix.mod.path {
		dir = ix.mod.root
	} else if rest, found := strings.CutPrefix(importPath, ix.mod.path+"/"); found {
		dir = filepath.Join(ix.mod.root, filepath.FromSlash(rest))
	} else {
		return "", false
	}
	return dir, ix.contains(dir)
}

// dir returns the directory of the module, nil when it is not part of it or holds
// no Go file.
func (ix *moduleIndex) dir(dir string) *modDir {
	if md, seen := ix.dirs[dir]; seen {
		return md
	}

	var md *modDir
	if ix.contains(dir) {
		entries, err := os.ReadDir(dir)
		if err == nil {
			md = &modDir{dir: dir}
			for _, e := range entries {
				name := e.Name()
				if !e.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_") {
					md.names = append(md.names, name)
				}
			}
		}
		if md != nil && len(md.names) == 0 {
			md = nil
		}
	}
	ix.dirs[dir] = md
	return md
}

// list finds every directory of the module that holds Go files.
func (ix *moduleIndex) list() []string {
	if ix.listed {
		return ix.order
	}
	ix.listed = true
	ix.order = nil

	//nolint:errcheck // a directory that cannot be read only means its packages are not in the index
	_ = filepath.WalkDir(ix.mod.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // keep walking
		}
		if p != ix.mod.root && !ix.contains(p) {
			return filepath.SkipDir
		}
		if ix.dir(p) != nil {
			ix.order = append(ix.order, p)
		}
		return nil
	})
	return ix.order
}

// packageAt returns the package an import path of the module names: the one the
// other packages import, not the external tests. It is nil for any other path.
func (ix *moduleIndex) packageAt(importPath string) *modPackage {
	if ix == nil {
		return nil
	}
	if peer := ix.peerFor(importPath); peer != nil {
		return peer.packageAt(importPath)
	}
	dir, ok := ix.dirOf(importPath)
	if !ok {
		return nil
	}
	return ix.packageIn(dir)
}

// packageIn returns the package of a directory of the module, nil when there is
// none.
func (ix *moduleIndex) packageIn(dir string) *modPackage {
	if ix == nil {
		return nil
	}
	if peer := ix.peerAt(dir); peer != nil {
		return peer.packageIn(dir)
	}
	md := ix.dir(dir)
	if md == nil {
		return nil
	}
	ix.load(md)
	return md.pkg
}

// packages returns every package of the module that other packages can import,
// in the order of their directories. It reads the whole module.
func (ix *moduleIndex) packages() []*modPackage {
	if ix == nil {
		return nil
	}
	var out []*modPackage
	for _, dir := range ix.list() {
		if pkg := ix.packageIn(dir); pkg != nil {
			out = append(out, pkg)
		}
	}
	return out
}

// allPackages is packages with the packages of the external tests, which only a
// test file of the module can see.
func (ix *moduleIndex) allPackages() []*modPackage {
	var out []*modPackage
	for _, pkg := range ix.packages() {
		out = append(out, pkg)
		if md := ix.dir(pkg.dir); md != nil && md.xtest != nil {
			out = append(out, md.xtest)
		}
	}
	return out
}

// fileAt returns the file at path as it is on disk, nil when it is not a Go file
// of the module or does not parse. Whether the file is part of a package is
// another matter, see modFile.pkg.
func (ix *moduleIndex) fileAt(p string) *modFile {
	if ix == nil {
		return nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil
	}
	if dir := filepath.Dir(abs); !ix.contains(dir) {
		if peer := ix.peerAt(dir); peer != nil {
			return peer.fileAt(abs)
		}
		return nil
	}
	return ix.readFile(abs)
}

// invalidate forgets what was read of the files and of the directories they are
// in, so that the next question reads them again. A migration that writes a file
// while it uses the index calls it. Packages and files handed out before the call
// are not updated.
func (ix *moduleIndex) invalidate(paths ...string) {
	if ix == nil {
		return
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if peer := ix.peerAt(filepath.Dir(abs)); peer != nil && !ix.contains(filepath.Dir(abs)) {
			peer.invalidate(abs)
			continue
		}
		delete(ix.files, abs)

		dir := filepath.Dir(abs)
		md := ix.dirs[dir]
		if md == nil {
			ix.listed = false // the directory may be new
		}
		for _, pkg := range []*modPackage{md.pkgOrNil(), md.xtestOrNil()} {
			if pkg != nil {
				delete(ix.pkgs, pkg.importPath)
			}
		}
		delete(ix.dirs, dir)
	}
	clear(ix.users)
}

// importers returns the files of the module that import the package, tests
// included, in the order of their directories, and then those of the other modules
// of the repository. A file that does not import fiber but calls the function of a
// package that returns an app is one of them.
func (ix *moduleIndex) importers(importPath string) []*modFile {
	if ix == nil {
		return nil
	}
	out := ix.ownImporters(importPath)
	for _, peer := range ix.peers() {
		out = slices.Concat(out, peer.ownImporters(importPath))
	}
	return out
}

// ownImporters is importers for the files of the module itself.
func (ix *moduleIndex) ownImporters(importPath string) []*modFile {
	if users, ok := ix.users[importPath]; ok {
		return users
	}

	var out []*modFile
	for _, dir := range ix.list() {
		md := ix.dir(dir)
		if md == nil {
			continue
		}
		for _, name := range md.names {
			p := filepath.Join(dir, name)
			f, seen := ix.files[p]
			switch {
			case seen && f == nil:
				continue // it does not parse
			case !seen && !modMentions(p, importPath):
				continue // the text rules it out, so it is not worth parsing
			case !seen:
				f = ix.readFile(p)
			}
			if f != nil && !f.ignored && f.imports(importPath) {
				out = append(out, f)
			}
		}
	}
	ix.users[importPath] = out
	return out
}

// modMentions reports whether the file holds the text.
func modMentions(p, text string) bool {
	src, err := os.ReadFile(p) // #nosec G304 -- the user's own project tree
	return err == nil && bytes.Contains(src, []byte(text))
}

// modFile is a parsed Go file of the module.
type modFile struct {
	ix  *moduleIndex
	pkg *modPackage
	g   *goSource
	// path is absolute.
	path string
	// imports are the imports of the file in the order they are written.
	imps []modImport
	// test is set for the files named _test.go.
	test bool
	// ignored is set for a file that "go:build ignore" leaves out of every build.
	ignored bool
}

// readFile parses the file once; nil when it cannot be read or parsed.
func (ix *moduleIndex) readFile(p string) *modFile {
	if f, seen := ix.files[p]; seen {
		return f
	}

	var f *modFile
	if src, err := os.ReadFile(p); err == nil { // #nosec G304 -- the user's own project tree
		if g, ok := parseSource(string(src)); ok {
			f = &modFile{ix: ix, g: g, path: p, test: strings.HasSuffix(p, "_test.go"), ignored: modBuildIgnored(g)}
			for _, spec := range g.file.Imports {
				importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
				if unquoteErr != nil {
					continue
				}
				imp := modImport{file: f, path: importPath}
				if spec.Name != nil {
					imp.name = spec.Name.Name
				}
				f.imps = append(f.imps, imp)
			}
		}
	}
	ix.files[p] = f
	return f
}

// modBuildIgnored reports whether a build constraint before the package clause
// excludes the file from every build.
func modBuildIgnored(g *goSource) bool {
	for _, group := range g.file.Comments {
		if group.End() >= g.file.Package {
			break
		}
		for _, c := range group.List {
			if text := strings.TrimSpace(c.Text); text == "//go:build ignore" || text == "// +build ignore" {
				return true
			}
		}
	}
	return false
}

// load reads the files of the directory and sorts them into the package, its
// tests and the external tests.
func (ix *moduleIndex) load(md *modDir) {
	if md.loaded {
		return
	}
	md.loaded = true
	md.complete = true

	var libs, tests []*modFile
	for _, name := range md.names {
		f := ix.readFile(filepath.Join(md.dir, name))
		switch {
		case f == nil:
			md.complete = false
		case f.ignored:
		case f.test:
			tests = append(tests, f)
		default:
			libs = append(libs, f)
		}
	}

	name := modMainClause(libs)
	if name == "" && len(tests) > 0 {
		name = strings.TrimSuffix(tests[0].g.file.Name.Name, "_test")
	}
	if name == "" {
		return
	}

	importPath, _ := ix.importPathOf(md.dir)
	md.pkg = &modPackage{ix: ix, dir: md.dir, importPath: importPath, name: name}
	xtest := &modPackage{ix: ix, dir: md.dir, name: name + "_test", external: true}

	for _, f := range slices.Concat(libs, tests) {
		switch clause := f.g.file.Name.Name; {
		case clause == name:
			f.pkg = md.pkg
			md.pkg.files = append(md.pkg.files, f)
		case f.test && clause == name+"_test":
			f.pkg = xtest
			xtest.files = append(xtest.files, f)
		default:
			md.complete = false
		}
	}
	md.pkg.complete, xtest.complete = md.complete, md.complete
	if len(xtest.files) > 0 {
		md.xtest = xtest
	}
	ix.pkgs[importPath] = md.pkg
}

// modMainClause returns the package clause most of the files have, the one
// whose name comes first when there is a tie.
func modMainClause(files []*modFile) string {
	counts := map[string]int{}
	for _, f := range files {
		counts[f.g.file.Name.Name]++
	}

	best := ""
	for name, n := range counts {
		if n > counts[best] || (n == counts[best] && name < best) {
			best = name
		}
	}
	return best
}

// modPackage is a package of the module.
type modPackage struct {
	ix *moduleIndex
	// scopes are the declarations of the files other packages see (lib) and of the
	// files of the package including its internal tests (all), built when asked.
	lib, all *modScope
	// importPath is empty for the package of the external tests.
	importPath string
	dir        string
	name       string
	// files are the files of the package, the non-test files first and the tests of
	// the package after them, in the order of their names. For the package of the
	// external tests they are the files of those tests.
	files []*modFile
	// external is set for the package of the external tests (package name_test).
	external bool
	// complete is false when a file of the directory could not be read or parsed, or
	// has the clause of another package: the package may be more than it shows.
	complete bool
}

// modKind says what a declaration declares.
type modKind uint8

const (
	modFunc modKind = iota
	modMethod
	modVar
	modConst
	modTypeDecl
)

// modDecl is a declaration at package level.
type modDecl struct {
	file *modFile
	// fn is the declaration of a function or method.
	fn *ast.FuncDecl
	// typ is the specification of a type.
	typ *ast.TypeSpec
	// value is the specification of a variable or constant, and index the position
	// of the name in it.
	value *ast.ValueSpec
	name  string
	// recv is the name of the type a method is declared on.
	recv  string
	index int
	kind  modKind
	// dup is set when the package declares the name more than once, which valid
	// code only does for files built on different platforms.
	dup bool
}

// modScope holds the declarations of some files of a package.
type modScope struct {
	names   map[string][]*modDecl
	methods map[string][]*modDecl
	all     []*modDecl
}

// scope returns the declarations of the package: those of the files other
// packages import, and with tests also those of the tests of the package. The
// package of the external tests has only its own.
func (p *modPackage) scope(withTests bool) *modScope {
	if withTests || p.external {
		if p.all == nil {
			p.all = newModScope(p.files, func(*modFile) bool { return true })
		}
		return p.all
	}
	if p.lib == nil {
		p.lib = newModScope(p.files, func(f *modFile) bool { return !f.test })
	}
	return p.lib
}

// newModScope collects the declarations of the files that keep accepts.
func newModScope(files []*modFile, keep func(*modFile) bool) *modScope {
	s := &modScope{names: map[string][]*modDecl{}, methods: map[string][]*modDecl{}}
	for _, f := range files {
		if keep(f) {
			s.add(f)
		}
	}
	return s
}

// add records the declarations of a file.
func (s *modScope) add(f *modFile) {
	declare := func(d *modDecl) {
		s.all = append(s.all, d)
		if d.kind == modMethod {
			key := d.recv + "." + d.name
			s.methods[key] = append(s.methods[key], d)
			return
		}
		s.names[d.name] = append(s.names[d.name], d)
	}

	for _, decl := range f.g.file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			switch {
			case d.Recv != nil:
				declare(&modDecl{file: f, kind: modMethod, name: d.Name.Name, fn: d, recv: modBaseName(d.Recv.List[0].Type)})
			case d.Name.Name != "init" && d.Name.Name != "_":
				declare(&modDecl{file: f, kind: modFunc, name: d.Name.Name, fn: d})
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					declare(&modDecl{file: f, kind: modTypeDecl, name: sp.Name.Name, typ: sp})
				case *ast.ValueSpec:
					kind := modVar
					if d.Tok == token.CONST {
						kind = modConst
					}
					for i, id := range sp.Names {
						if id.Name != "_" {
							declare(&modDecl{file: f, kind: kind, name: id.Name, value: sp, index: i})
						}
					}
				}
			}
		}
	}

	for _, group := range s.names {
		for _, d := range group {
			d.dup = len(group) > 1
		}
	}
	for _, group := range s.methods {
		for _, d := range group {
			d.dup = len(group) > 1
		}
	}
}

// modBaseName returns the name of a type expression that is a plain, possibly
// pointer or generic, type name, empty for any other.
func modBaseName(e ast.Expr) string {
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

// lookup returns the declaration of the name at package level as the other
// packages see it, nil when there is none. When the name is declared more than
// once the first declaration is returned, with dup set.
func (p *modPackage) lookup(name string) *modDecl {
	if p == nil {
		return nil
	}
	return modFirst(p.scope(false).names[name])
}

// lookupIn is lookup for code in the file f of the package: a test file of the
// package sees the declarations of the other test files too.
func (p *modPackage) lookupIn(f *modFile, name string) *modDecl {
	if p == nil {
		return nil
	}
	return modFirst(p.scope(f != nil && f.test).names[name])
}

// method returns the declaration of the method of the named type, as the other
// packages see it.
func (p *modPackage) method(recv, name string) *modDecl {
	if p == nil {
		return nil
	}
	return modFirst(p.scope(false).methods[recv+"."+name])
}

// decls returns every declaration at package level, methods included, in the
// order of the files. The tests of the package are included only if withTests.
func (p *modPackage) decls(withTests bool) []*modDecl {
	if p == nil {
		return nil
	}
	return p.scope(withTests).all
}

func modFirst(group []*modDecl) *modDecl {
	if len(group) == 0 {
		return nil
	}
	return group[0]
}

// exported reports whether the name starts with an upper case letter.
func (d *modDecl) exported() bool { return appIsExported(d.name) }

// node returns the declaration the way the syntax tree has it.
func (d *modDecl) node() ast.Node {
	switch d.kind {
	case modFunc, modMethod:
		return d.fn
	case modTypeDecl:
		return d.typ
	case modVar, modConst:
		return d.value
	}
	return nil
}

// modImport is an import of a file.
type modImport struct {
	file *modFile
	// name is the name written in the import: empty when there is none, "_" for a
	// blank import and "." for a dot import.
	name string
	path string
}

// imports reports whether the file imports the package, whatever name it gives it.
func (f *modFile) imports(importPath string) bool {
	return slices.ContainsFunc(f.imps, func(imp modImport) bool { return imp.path == importPath })
}

// pkg returns the package of the module that the import names, nil for a package
// outside of it.
func (i modImport) pkg() *modPackage { return i.file.ix.packageAt(i.path) }

// binds returns the identifier the file refers to the package by: the name in the
// import, or the name of the package. It is empty for a blank or dot import.
func (i modImport) binds() string {
	switch i.name {
	case "_", ".":
		return ""
	case "":
		if pkg := i.pkg(); pkg != nil {
			return pkg.name
		}
		return defaultImportName(i.path)
	}
	return i.name
}

// qualifier returns the import that a selector pkg.Name refers to through pkg, when
// pkg names a package the file imports. The caller has made sure a local
// declaration does not hide the name (see modDeclaredLocally).
func (f *modFile) qualifier(sel *ast.SelectorExpr) (modImport, bool) {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return modImport{}, false
	}
	return f.importFor(id.Name)
}

// importFor returns the import that binds the identifier at the top of the file.
// The caller has made sure a local declaration does not hide it.
func (f *modFile) importFor(name string) (modImport, bool) {
	if name == "" {
		return modImport{}, false
	}
	for _, imp := range f.imps {
		if imp.binds() == name {
			return imp, true
		}
	}
	return modImport{}, false
}

// dotImports returns the imports of the file that bring their names into the file
// block.
func (f *modFile) dotImports() []modImport {
	var out []modImport
	for _, imp := range f.imps {
		if imp.name == "." {
			out = append(out, imp)
		}
	}
	return out
}

// importNames returns the identifiers the file binds to the imports whose path
// matches re, sorted and without duplicates: the explicit names, "." for a dot
// import, and for an import without a name each of defaults, or the name of the
// package when there are none. Blank imports are left out.
func (f *modFile) importNames(re *regexp.Regexp, defaults ...string) []string {
	var names []string
	for _, imp := range f.imps {
		switch {
		case !re.MatchString(imp.path) || imp.name == "_":
		case imp.name != "":
			names = append(names, imp.name)
		case len(defaults) > 0:
			names = append(names, defaults...)
		default:
			names = append(names, imp.binds())
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// declOf returns the declaration at package level that e names: a name of the
// package of the file or of a package it dot-imports, or a qualified name
// pkg.Name of a package of the module the file imports. A generic instantiation
// F[T] is the declaration of F. It is nil for anything else, and a local
// declaration that hides the name must be excluded by the caller.
func (f *modFile) declOf(e ast.Expr) *modDecl {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.declOf(x.X)
	case *ast.IndexExpr:
		return f.declOf(x.X)
	case *ast.IndexListExpr:
		return f.declOf(x.X)
	case *ast.Ident:
		if d := f.pkg.lookupIn(f, x.Name); d != nil {
			return d
		}
		for _, imp := range f.dotImports() {
			if d := imp.pkg().lookup(x.Name); d != nil {
				return d
			}
		}
	case *ast.SelectorExpr:
		if imp, found := f.qualifier(x); found {
			return imp.pkg().lookup(x.Sel.Name)
		}
	}
	return nil
}

// modDeclaredLocally reports whether a declaration inside a function hides the
// package level name at pos: a parameter, a result, a receiver, a variable, a
// range or type switch variable. stack holds the ancestors of the use, outermost
// first (see goSource.walk). It decides whether pkg.Name is a qualified name or a
// field of a local called pkg.
func modDeclaredLocally(name string, pos token.Pos, stack []ast.Node) bool {
	for _, node := range slices.Backward(stack) {
		var found bool
		switch n := node.(type) {
		case *ast.BlockStmt:
			_, found, _ = declInStmts(name, pos, n.List)
		case *ast.CaseClause:
			_, found, _ = declInStmts(name, pos, n.Body)
		case *ast.CommClause:
			_, found, _ = declInStmts(name, pos, n.Body)
		case *ast.IfStmt:
			_, found, _ = declInStmts(name, pos, []ast.Stmt{n.Init})
		case *ast.ForStmt:
			_, found, _ = declInStmts(name, pos, []ast.Stmt{n.Init})
		case *ast.SwitchStmt:
			_, found, _ = declInStmts(name, pos, []ast.Stmt{n.Init})
		case *ast.RangeStmt:
			found = n.Tok == token.DEFINE && (isIdentNamed(n.Key, name) || isIdentNamed(n.Value, name))
		case *ast.TypeSwitchStmt:
			as, isAssign := n.Assign.(*ast.AssignStmt)
			if found = isAssign && len(as.Lhs) == 1 && isIdentNamed(as.Lhs[0], name); !found {
				_, found, _ = declInStmts(name, pos, []ast.Stmt{n.Init})
			}
		case *ast.FuncLit:
			_, found = declInFields(name, n.Type)
		case *ast.FuncDecl:
			if n.Recv != nil {
				_, found = declInFieldList(name, n.Recv)
			}
			if !found {
				_, found = declInFields(name, n.Type)
			}
		}
		if found {
			return true
		}
	}
	return false
}
