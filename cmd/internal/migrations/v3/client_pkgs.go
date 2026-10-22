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
	cp := &clientPackage{names: map[string]bool{}, clients: map[string]bool{}, agentFuncs: map[string]bool{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return cp
	}
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
	return cp
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
