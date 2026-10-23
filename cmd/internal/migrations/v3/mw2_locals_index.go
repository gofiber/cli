package v3

import (
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/gofiber/cli/cmd/internal"
)

// mw2LocalsIndex knows, for each module of a project, which middleware keep a
// value in the request locals, under which keys, and which keys the application
// writes itself. It is built from every file of the module, because the config
// that names a key and the code that reads it are rarely in the same file.
type mw2LocalsIndex struct {
	modules *mw2Modules
	pkgs    *mw2Packages
	byRoot  map[string]*mw2LocalsModule
	// run types the receivers of the reads: whether a value is a fiber context is
	// what every migration of the request context asks (see ctxEnv).
	run *ctxRun
}

func newMW2LocalsIndex(root string) *mw2LocalsIndex {
	return &mw2LocalsIndex{modules: newMW2Modules(root), pkgs: newMW2PackagesIn(root), byRoot: map[string]*mw2LocalsModule{}, run: newCtxRun()}
}

// module returns what is known about the module that holds file.
func (ix *mw2LocalsIndex) module(file string) *mw2LocalsModule {
	root := ix.modules.moduleOf(file)
	if m, ok := ix.byRoot[root]; ok {
		return m
	}

	m := &mw2LocalsModule{
		usedPkgs: map[string]bool{},
		bindings: map[mw2Key][]*mw2Role{},
		setters:  map[mw2Key]string{},
		blocked:  map[mw2Key]int{},
		plans:    map[string]*mw2LocalsPlan{},
	}
	ix.byRoot[root] = m

	// A module that cannot be walked has nothing the middleware could have configured.
	if err := mw2Walk(root, func(path string) {
		if ix.modules.moduleOf(path) != root {
			return
		}
		if f := ix.read(path); f != nil {
			m.files = append(m.files, f)
		}
	}); err != nil {
		return m
	}

	for _, f := range m.files {
		m.collect(f)
	}
	for _, role := range mw2Roles {
		if role.defaultKey != "" && m.usedPkgs[role.pkg] {
			m.bind(mw2Key{val: role.defaultKey}, role)
		}
	}
	return m
}

// planned returns the module once every file has a plan.
func (m *mw2LocalsModule) planned() *mw2LocalsModule {
	if m.files != nil {
		for _, f := range m.files {
			m.plans[f.path] = m.plan(f)
		}
		m.files = nil
	}
	return m
}

// mw2LocalsFile is a parsed file of the module.
type mw2LocalsFile struct {
	g     *goSource
	kr    *mw2KeyResolver
	names map[string][]*mw2Role
	run   *ctxRun
	// env types the receivers of the file, built when first needed; it is nil
	// for a file that has nothing to do with contexts.
	env  *ctxEnv
	path string
	// rel is the path relative to the migrated directory.
	rel string
	// fiber are the identifiers the file binds to the core package.
	fiber  []string
	fi     internal.FileInfo
	hasEnv bool
}

// typing returns the environment that tells what the receivers of the file are,
// nil when the file reaches no fiber context at all.
func (f *mw2LocalsFile) typing() *ctxEnv {
	if !f.hasEnv {
		f.hasEnv = true
		f.env = f.run.envFor(f.fi, f.g, f.fiber)
	}
	return f.env
}

// read parses a file; nil when it cannot be read or does not parse (the
// migrations that rewrite it say so).
func (ix *mw2LocalsIndex) read(path string) *mw2LocalsFile {
	src, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		return nil
	}
	g, ok := parseSource(string(src))
	if !ok {
		return nil
	}

	dir := filepath.Dir(filepath.Clean(path))
	names := mw2RoleNames(string(src))
	rel, err := filepath.Rel(ix.modules.root, path)
	if err != nil {
		rel = path
	}
	return &mw2LocalsFile{
		g:     g,
		path:  filepath.Clean(path),
		rel:   filepath.ToSlash(rel),
		names: names,
		fiber: fiberNames(string(src)),
		kr:    &mw2KeyResolver{g: g, pkg: ix.pkgs.of(dir, g.file.Name.Name), pkgs: ix.pkgs, names: names, dir: dir},
		fi:    internal.FileInfo{Root: ix.modules.root, Path: path, Rel: rel},
		run:   ix.run,
	}
}

// location renders where a node is: file:line.
func (f *mw2LocalsFile) location(n ast.Node) string {
	return f.rel + ":" + strconv.Itoa(f.g.line(n.Pos()))
}

// imports reports whether the file imports the package of the role.
func (f *mw2LocalsFile) imports(role *mw2Role) bool {
	for _, roles := range f.names {
		if slices.Contains(roles, role) {
			return true
		}
	}
	return false
}

// mw2LocalsModule is the picture of one module.
type mw2LocalsModule struct {
	usedPkgs map[string]bool
	bindings map[mw2Key][]*mw2Role
	// setters maps the keys the application writes to the place where it does.
	setters map[mw2Key]string
	// blocked counts, per key, the reads that could not be migrated.
	blocked map[mw2Key]int
	plans   map[string]*mw2LocalsPlan
	// exprReads are the reads whose key is an expression, not a constant.
	exprReads []mw2ExprRead
	// files are the parsed files, until they are planned.
	files []*mw2LocalsFile
}

// mw2ExprRead is a read of a key that is not a constant.
type mw2ExprRead struct {
	loc string
	key mw2Key
}

// bind records that a role stores its value under the key.
func (m *mw2LocalsModule) bind(key mw2Key, role *mw2Role) {
	if !slices.Contains(m.bindings[key], role) {
		m.bindings[key] = append(m.bindings[key], role)
	}
}

// candidates returns the middleware that store a value under the key.
func (m *mw2LocalsModule) candidates(key mw2Key) []*mw2Role {
	var roles []*mw2Role
	for _, role := range m.bindings[key] {
		if m.usedPkgs[role.pkg] {
			roles = append(roles, role)
		}
	}
	return roles
}

// owner returns the middleware that is the only one to store a value under the
// key, and that the application does not write itself.
func (m *mw2LocalsModule) owner(key mw2Key) (*mw2Role, bool) {
	roles := m.candidates(key)
	if _, written := m.setters[key]; len(roles) != 1 || written {
		return nil, false
	}
	return roles[0], true
}

// unresolvedRead returns the location of a read whose key is not a constant and
// that no config or write of the application accounts for, so that it may be a
// read of a key the middleware stores under; "" when there is none.
func (m *mw2LocalsModule) unresolvedRead() string {
	for _, r := range m.exprReads {
		_, written := m.setters[r.key]
		if len(m.bindings[r.key]) == 0 && !written {
			return r.loc
		}
	}
	return ""
}

// collect reads the facts of one file: the middleware it uses, the keys it
// configures and the keys the application writes.
func (m *mw2LocalsModule) collect(f *mw2LocalsFile) {
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			m.markUsed(f, x)
		case *ast.CompositeLit:
			m.collectLiteral(f, x, stack)
		case *ast.AssignStmt:
			m.collectAssignment(f, x, stack)
		case *ast.CallExpr:
			m.collectLocals(f, x, stack)
		}
		return true
	})
}

// markUsed notes a call of a middleware constructor.
func (m *mw2LocalsModule) markUsed(f *mw2LocalsFile, sel *ast.SelectorExpr) {
	id, ok := sel.X.(*ast.Ident)
	if !ok || sel.Sel.Name != mw2New {
		return
	}
	for _, role := range f.names[id.Name] {
		m.usedPkgs[role.pkg] = true
	}
}

// collectLiteral reads the key fields of a middleware config literal.
func (m *mw2LocalsModule) collectLiteral(f *mw2LocalsFile, lit *ast.CompositeLit, stack []ast.Node) {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if lit.Type == nil || !ok || sel.Sel.Name != mw2ConfigType {
		return
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	for _, role := range f.names[id.Name] {
		m.usedPkgs[role.pkg] = true
		if _, kv := mw2Field(lit, role.field); kv != nil {
			if key, ok := f.kr.key(kv.Value, stack); ok {
				m.bind(key, role)
			}
		}
	}
}

// collectAssignment reads cfg.ContextKey = key for a middleware config variable.
func (m *mw2LocalsModule) collectAssignment(f *mw2LocalsFile, as *ast.AssignStmt, stack []ast.Node) {
	role, ok := f.configAssignment(as, stack)
	if !ok {
		return
	}
	if key, ok := f.kr.key(as.Rhs[0], stack); ok {
		m.bind(key, role)
	}
}

// configAssignment recognizes an assignment to a key field of a middleware config
// held in a variable, and returns the role of the field.
func (f *mw2LocalsFile) configAssignment(as *ast.AssignStmt, stack []ast.Node) (*mw2Role, bool) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return nil, false
	}
	sel, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	for alias, roles := range f.names {
		for _, role := range roles {
			if role.field == sel.Sel.Name && mw2IsTyped(sel.X, []string{alias}, mw2ConfigType, stack, 0) {
				return role, true
			}
		}
	}
	return nil, false
}

// collectLocals notes the keys the application writes and reads.
func (m *mw2LocalsModule) collectLocals(f *mw2LocalsFile, call *ast.CallExpr, stack []ast.Node) {
	_, sel, ok := mw2IsLocalsCall(call)
	if !ok {
		return
	}
	key, resolved := f.kr.key(call.Args[0], stack)
	if !resolved {
		return
	}

	if len(call.Args) > 1 {
		if _, seen := m.setters[key]; !seen {
			m.setters[key] = f.location(sel)
		}
		return
	}
	if key.expr {
		m.exprReads = append(m.exprReads, mw2ExprRead{key: key, loc: f.location(sel)})
	}
}
