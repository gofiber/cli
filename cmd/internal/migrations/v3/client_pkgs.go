package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
}

// clientPackages knows the package level names of the packages being
// migrated, so that the code the migration adds never collides with a name
// declared in another file of the same package, and which package level
// variables are fiber clients.
type clientPackages struct {
	pkgs map[string]*clientPackage
	mu   sync.Mutex
}

func clientNewPackages() *clientPackages {
	return &clientPackages{pkgs: map[string]*clientPackage{}}
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
	cp.tainted[domain] = true
}

// tainted reports whether the package was tainted for a kind of value.
func (p *clientPackages) tainted(dir, pkg string, domain clientShimDomain) bool {
	cp := p.get(dir, pkg)

	p.mu.Lock()
	defer p.mu.Unlock()
	return cp.tainted[domain]
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
	cp := &clientPackage{names: map[string]bool{}, clients: map[string]bool{}, agentFuncs: map[string]bool{}, tainted: map[clientShimDomain]bool{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return cp
	}
	var foreign []*ast.File
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil || file.Name.Name != pkg {
			continue
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
					if clientReturnsAgent(x.Type, fiberNames) {
						cp.agentFuncs[x.Name.Name] = true
					}
				}
			case *ast.GenDecl:
				cp.scanGenDecl(x, fiberNames)
			}
		}
	}

	// A file that does not import the fiber package is not migrated, so the code
	// in it that uses an agent or a client of another file stays as it is.
	for _, file := range foreign {
		if clientMentions(file, cp.agentFuncs) {
			cp.tainted[clientDomainAgent] = true
		}
		if clientMentions(file, cp.clients) {
			cp.tainted[clientDomainClient] = true
		}
	}
	return cp
}

// clientMentions reports whether the file refers to one of the names, leaving out
// the names of fields, methods and declarations.
func clientMentions(file *ast.File, names map[string]bool) bool {
	if len(names) == 0 {
		return false
	}
	found := false
	var visit func(n ast.Node)
	visit = func(root ast.Node) {
		if root == nil {
			return
		}
		ast.Inspect(root, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				visit(x.X)
				return false
			case *ast.ImportSpec:
				return false
			case *ast.FuncDecl:
				if x.Recv != nil {
					visit(x.Recv)
				}
				visit(x.Type)
				if x.Body != nil {
					visit(x.Body)
				}
				return false
			case *ast.Ident:
				found = found || names[x.Name]
			}
			return !found
		})
	}
	for _, d := range file.Decls {
		visit(d)
	}
	return found
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
