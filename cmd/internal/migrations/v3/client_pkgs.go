package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// clientPackage is what the migration knows about the files of one package
// besides the file it is migrating.
type clientPackage struct {
	// names are the identifiers declared at package level.
	names map[string]bool
	// clients are the package level variables that hold a fiber.Client.
	clients map[string]bool
	// agentFuncs are the functions that return a *fiber.Agent.
	agentFuncs map[string]bool
	// tainted are the kinds of value the code of the package hands around in ways
	// the migration does not follow.
	tainted map[clientShimDomain]bool
	// params are the kinds of value that the parameters of the functions of the
	// package take.
	params map[string][]clientShimDomain
	// facts are the kinds of value that the package shares with the other packages
	// of the module. They are worked out once, when first asked for.
	facts *clientFacts
	// imports are the import paths of the files of the package.
	imports []string
	// exports are the kinds of value that the exported declarations of the package
	// use.
	exports clientDomainSet
}

// clientFacts are the kinds of value that a package shares with the other packages
// of the module, and why.
type clientFacts struct {
	// via names, for each kind, the package that makes the package share it: the
	// path of an import, empty when the package exports it itself.
	via   map[clientShimDomain]string
	kinds clientDomainSet
}

// add records that the kinds are shared, because of the package at path.
func (cf *clientFacts) add(kinds clientDomainSet, path string) {
	for _, d := range clientKindOrder {
		if kinds.has(d) && !cf.kinds.has(d) {
			cf.kinds = cf.kinds.with(d)
			cf.via[d] = path
		}
	}
}

// clientDirExports are the kinds of value that the packages of a directory export.
type clientDirExports struct {
	// pub are the kinds that the files of the packages that others can import
	// export.
	pub clientDomainSet
	// test are the kinds that the test files of those packages export, which only
	// the external test package of the directory can use.
	test clientDomainSet
}

// clientPackages knows the package level names of the packages being
// migrated, so that the code the migration adds never collides with a name
// declared in another file of the same package, and which package level
// variables are fiber clients.
type clientPackages struct {
	pkgs map[string]*clientPackage
	// dirs are the exports of the directories packages are imported from.
	dirs map[string]clientDirExports
	// root is the directory the migration runs in: modules are looked for below it.
	root string
	// taints counts the kinds of value the scans found out that packages share, so
	// that a scan can tell it learned something.
	taints int
	mu     sync.Mutex
}

func clientNewPackages(root string) *clientPackages {
	return &clientPackages{pkgs: map[string]*clientPackage{}, dirs: map[string]clientDirExports{}, root: root}
}

// learned returns how many facts the scans found out so far.
func (p *clientPackages) learned() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.taints
}

// get returns the package pkg of the directory dir.
func (p *clientPackages) get(dir, pkg string) *clientPackage {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := dir + "\x00" + pkg
	cp, ok := p.pkgs[key]
	if !ok {
		cp = clientScanPackage(dir, pkg)
		p.pkgs[key] = cp
	}
	return cp
}

// declared reports whether name is declared at package level in a file of dir
// that belongs to the package pkg.
func (p *clientPackages) declared(dir, pkg, name string) bool {
	return p.get(dir, pkg).names[name]
}

// agentFunc reports whether name is a function of the package that returns a
// *fiber.Agent.
func (p *clientPackages) agentFunc(dir, pkg, name string) bool {
	return p.get(dir, pkg).agentFuncs[name]
}

// taint records that the code of the package hands the values of a kind around
// in ways the migration does not follow.
func (p *clientPackages) taint(dir, pkg string, domain clientShimDomain) {
	cp := p.get(dir, pkg)

	p.mu.Lock()
	defer p.mu.Unlock()
	if !cp.tainted[domain] {
		cp.tainted[domain] = true
		p.taints++
	}
}

// tainted reports whether the values of a kind stay on the copy of the v2 agent in
// the package: the code of the package hands them around in ways the migration
// does not follow, or the package shares them with other packages of the module
// (see clientFacts).
func (p *clientPackages) tainted(dir, pkg string, domain clientShimDomain) bool {
	cp := p.get(dir, pkg)

	p.mu.Lock()
	defer p.mu.Unlock()
	return cp.tainted[domain] || p.factsOf(dir, pkg, cp).kinds.has(domain)
}

// sharedVia returns the import path of the package that makes the package share
// the values of a kind; it is empty when the package exports them itself.
func (p *clientPackages) sharedVia(dir, pkg string, domain clientShimDomain) string {
	cp := p.get(dir, pkg)

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.factsOf(dir, pkg, cp).via[domain]
}

// factsOf works out which kinds of value the package shares with the others. The
// lock is held.
//
// A package shares the values its own exported declarations use, and the values of
// the packages of the module it imports, because it can be handed one of theirs. The
// external test package of a directory imports the package under test, even though
// no import says so. Only the packages the files import are looked at: a package
// that imports a package that imports an exporter is not.
func (p *clientPackages) factsOf(dir, pkg string, cp *clientPackage) *clientFacts {
	if cp.facts != nil {
		return cp.facts
	}
	cf := &clientFacts{via: map[clientShimDomain]string{}}
	cp.facts = cf
	cf.add(cp.exports, "")

	mod, ok := moduleOf(filepath.Join(dir, "x.go"), p.root)
	if !ok {
		return cf
	}
	for _, imp := range cp.imports {
		rel, within := strings.CutPrefix(imp, mod.path)
		if !within || (rel != "" && !strings.HasPrefix(rel, "/")) {
			continue
		}
		cf.add(p.dirExports(filepath.Join(mod.root, filepath.FromSlash(strings.TrimPrefix(rel, "/")))).pub, imp)
	}

	if strings.HasSuffix(pkg, "_test") {
		d := p.dirExports(dir)
		path := mod.path
		if rel, err := filepath.Rel(mod.root, dir); err == nil && rel != "." {
			path = mod.path + "/" + filepath.ToSlash(rel)
		}
		cf.add(d.pub|d.test, path)
	}
	return cf
}

// dirExports returns the kinds of value that the packages of a directory export.
// The lock is held.
func (p *clientPackages) dirExports(dir string) clientDirExports {
	if d, ok := p.dirs[dir]; ok {
		return d
	}
	var d clientDirExports
	p.dirs[dir] = d // a directory that imports itself is not looked at twice

	entries, err := os.ReadDir(dir)
	if err != nil {
		return d
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil || file.Name.Name == appMainPackage || strings.HasSuffix(file.Name.Name, "_test") {
			continue
		}
		var kinds clientDomainSet
		for _, ex := range clientFileExports(file, clientAgentFuncs(file)) {
			kinds |= ex.kinds
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			d.test |= kinds
		} else {
			d.pub |= kinds
		}
	}
	p.dirs[dir] = d
	return d
}

// clientAgentFuncs returns the functions of a file that return a *fiber.Agent.
func clientAgentFuncs(file *ast.File) map[string]bool {
	funcs := map[string]bool{}
	names := clientFiberNames(file)
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && clientReturnsAgent(fn.Type, names) {
			funcs[fn.Name.Name] = true
		}
	}
	return funcs
}

// clientReturnsAgent reports whether a function type has exactly one result, a
// *fiber.Agent.
func clientReturnsAgent(ft *ast.FuncType, fiberNames []string) bool {
	if ft.Results == nil || len(ft.Results.List) != 1 || len(ft.Results.List[0].Names) > 1 {
		return false
	}
	star, ok := ft.Results.List[0].Type.(*ast.StarExpr)
	return ok && isFiberType(star.X, fiberNames, "Agent")
}

// clientVar reports whether name is a package level variable that holds a
// fiber.Client.
func (p *clientPackages) clientVar(dir, pkg, name string) bool {
	return p.get(dir, pkg).clients[name]
}

// clientScanPackage reads the Go files of dir that belong to the package pkg.
func clientScanPackage(dir, pkg string) *clientPackage {
	cp := &clientPackage{names: map[string]bool{}, clients: map[string]bool{}, agentFuncs: map[string]bool{}, tainted: map[clientShimDomain]bool{}, params: map[string][]clientShimDomain{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return cp
	}
	var foreign, files []*ast.File
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil || file.Name.Name != pkg {
			continue
		}
		files = append(files, file)
		for _, spec := range file.Imports {
			if path, err := strconv.Unquote(spec.Path.Value); err == nil && !slices.Contains(cp.imports, path) {
				cp.imports = append(cp.imports, path)
			}
		}
		fiberNames := clientFiberNames(file)
		if len(fiberNames) == 0 {
			foreign = append(foreign, file)
		}
		for _, d := range file.Decls {
			switch x := d.(type) {
			case *ast.FuncDecl:
				if x.Recv == nil {
					cp.names[x.Name.Name] = true
					cp.params[x.Name.Name] = clientParamKinds(x.Type, fiberNames)
					if clientReturnsAgent(x.Type, fiberNames) {
						cp.agentFuncs[x.Name.Name] = true
					}
				}
			case *ast.GenDecl:
				cp.scanGenDecl(x, fiberNames)
			}
		}
	}

	// What the exported declarations of an importable package use is shared with the
	// packages that import it.
	if pkg != appMainPackage && !strings.HasSuffix(pkg, "_test") {
		for _, file := range files {
			for _, ex := range clientFileExports(file, cp.agentFuncs) {
				cp.exports |= ex.kinds
			}
		}
	}

	// A file that does not import the fiber package is not migrated, so the code
	// in it that uses an agent or a client of another file stays as it is.
	for _, file := range foreign {
		agents, clients := cp.foreignUses(file)
		cp.tainted[clientDomainAgent] = cp.tainted[clientDomainAgent] || agents
		cp.tainted[clientDomainClient] = cp.tainted[clientDomainClient] || clients
	}
	return cp
}

// foreignUses reports whether a file that does not import the fiber package uses
// an agent returned by a function of the package, or a client the package keeps
// in a variable, in a way the migration cannot follow.
func (cp *clientPackage) foreignUses(file *ast.File) (agents, clients bool) {
	if len(cp.agentFuncs) == 0 && len(cp.clients) == 0 {
		return false, false
	}

	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if id, ok := n.(*ast.Ident); ok && !clientIsName(id, stack) {
			switch {
			case cp.agentFuncs[id.Name] && !cp.agentResultFollowed(id, stack):
				agents = true
			case cp.clients[id.Name] && !cp.passedToParam(id, stack, clientDomainClient):
				clients = true
			}
		}
		stack = append(stack, n)
		return true
	})
	return agents, clients
}

// clientIsName reports whether the identifier whose ancestors are stack only
// names something: a declaration, a field or a method.
func clientIsName(id *ast.Ident, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	switch parent := stack[len(stack)-1].(type) {
	case *ast.FuncDecl:
		return parent.Name == id
	case *ast.SelectorExpr:
		return parent.Sel == id
	case *ast.Field:
		return slices.Contains(parent.Names, id)
	case *ast.ValueSpec:
		return slices.Contains(parent.Names, id)
	case *ast.TypeSpec:
		return parent.Name == id
	case *ast.ImportSpec:
		return true
	}
	return false
}

// agentResultFollowed reports whether the identifier of a function that returns an
// agent is called, and the agent it returns goes to a parameter of a function of
// the package that takes an agent. The value goes from one place the migration
// knows to another, so it fits whether the agents are the client package's or the
// copy's.
func (cp *clientPackage) agentResultFollowed(id *ast.Ident, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	call, ok := stack[len(stack)-1].(*ast.CallExpr)
	return ok && call.Fun == ast.Expr(id) && cp.passedToParam(call, stack[:len(stack)-1], clientDomainAgent)
}

// passedToParam reports whether e is an argument of a call of a function of the
// package, and the parameter at its position takes a value of the kind. ancestors
// are the ancestors of e.
func (cp *clientPackage) passedToParam(e ast.Expr, ancestors []ast.Node, domain clientShimDomain) bool {
	if len(ancestors) == 0 {
		return false
	}
	call, ok := ancestors[len(ancestors)-1].(*ast.CallExpr)
	if !ok || call.Ellipsis.IsValid() {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	at := slices.IndexFunc(call.Args, func(arg ast.Expr) bool { return arg == e })
	params := cp.params[fn.Name]
	return at >= 0 && at < len(params) && params[at] == domain
}

// clientParamKinds returns the kind of value each parameter of a function takes
// (zero for the others).
func clientParamKinds(ft *ast.FuncType, fiberNames []string) []clientShimDomain {
	if ft.Params == nil {
		return nil
	}
	var kinds []clientShimDomain
	for _, field := range ft.Params.List {
		kind := clientShimDomain(0)
		if star, ok := field.Type.(*ast.StarExpr); ok {
			switch {
			case isFiberType(star.X, fiberNames, nameAgent):
				kind = clientDomainAgent
			case isFiberType(star.X, fiberNames, nameClient):
				kind = clientDomainClient
			}
		}
		for range max(1, len(field.Names)) {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// scanGenDecl records the names a declaration introduces.
func (cp *clientPackage) scanGenDecl(gd *ast.GenDecl, fiberNames []string) {
	for _, spec := range gd.Specs {
		switch sp := spec.(type) {
		case *ast.ValueSpec:
			for i, id := range sp.Names {
				cp.names[id.Name] = true
				isClient := sp.Type != nil && isFiberType(sp.Type, fiberNames, "Client")
				if i < len(sp.Values) && clientIsInit(sp.Values[i], fiberNames) {
					isClient = true
				}
				if isClient && gd.Tok == token.VAR {
					cp.clients[id.Name] = true
				}
			}
		case *ast.TypeSpec:
			cp.names[sp.Name.Name] = true
		}
	}
}

// clientFiberNames returns the names a file binds to the core fiber package.
func clientFiberNames(file *ast.File) []string {
	var names []string
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !reFiberCorePath.MatchString(path) {
			continue
		}
		switch {
		case spec.Name == nil:
			names = append(names, "fiber")
		case spec.Name.Name != "_" && spec.Name.Name != ".":
			names = append(names, spec.Name.Name)
		}
	}
	return names
}
