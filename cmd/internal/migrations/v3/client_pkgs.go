package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// clientPackages knows the package level names of the packages being
// migrated, so that the code the migration adds never collides with a name
// declared in another file of the same package.
type clientPackages struct {
	names map[string]map[string]bool
	mu    sync.Mutex
}

func newClientPackages() *clientPackages {
	return &clientPackages{names: map[string]map[string]bool{}}
}

// declared reports whether name is declared at package level in a file of dir
// that belongs to the package pkg.
func (p *clientPackages) declared(dir, pkg, name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := dir + "\x00" + pkg
	set, ok := p.names[key]
	if !ok {
		set = clientScanPackage(dir, pkg)
		p.names[key] = set
	}
	return set[name]
}

// clientScanPackage returns the package level names declared by the Go files
// of dir that belong to the package pkg.
func clientScanPackage(dir, pkg string) map[string]bool {
	names := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
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
		for _, d := range file.Decls {
			switch x := d.(type) {
			case *ast.FuncDecl:
				if x.Recv == nil {
					names[x.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range x.Specs {
					switch sp := spec.(type) {
					case *ast.ValueSpec:
						for _, id := range sp.Names {
							names[id.Name] = true
						}
					case *ast.TypeSpec:
						names[sp.Name.Name] = true
					}
				}
			}
		}
	}
	return names
}
