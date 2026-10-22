package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shims of the migrations are files of one package in the module of the
// project, <module>/internal/fibermigrate, whichever of them the project needs.
// Each one is the source of a package of its own in the directory <name>shim of
// this package, so that it is built, linted and tested with the CLI; this test
// puts the sources together the way a project gets them and checks that they do
// not declare the same name, and that no name a file gives an import is a name
// another file declares.

var shimVersionSuffix = regexp.MustCompile(`^v\d+$`)

// shimSource is a parsed file of a shim directory.
type shimSource struct {
	dir   string
	name  string
	file  *ast.File
	decls []string
}

func parseShimSources(t *testing.T) []shimSource {
	t.Helper()

	dirs, err := filepath.Glob("*shim")
	require.NoError(t, err)
	sort.Strings(dirs)

	var sources []shimSource
	for _, dir := range dirs {
		info, statErr := os.Stat(dir)
		if statErr != nil || !info.IsDir() {
			continue
		}
		entries, readErr := os.ReadDir(dir)
		require.NoError(t, readErr)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
			require.NoError(t, parseErr, "%s/%s", dir, entry.Name())
			sources = append(sources, shimSource{dir: dir, name: entry.Name(), file: file, decls: shimTopLevelNames(file)})
		}
	}
	return sources
}

// shimTopLevelNames returns the names a file declares in the package block.
func shimTopLevelNames(file *ast.File) []string {
	var names []string
	for _, d := range file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil && decl.Name.Name != "init" {
				names = append(names, decl.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
				case *ast.ValueSpec:
					for _, id := range s.Names {
						if id.Name != "_" {
							names = append(names, id.Name)
						}
					}
				}
			}
		}
	}
	return names
}

// shimImportNames returns the names a file gives the packages it imports.
func shimImportNames(file *ast.File) []string {
	var names []string
	for _, spec := range file.Imports {
		switch {
		case spec.Name != nil && spec.Name.Name != "_" && spec.Name.Name != ".":
			names = append(names, spec.Name.Name)
		case spec.Name == nil:
			elems := strings.Split(strings.Trim(spec.Path.Value, `"`), "/")
			name := path.Base(strings.Join(elems, "/"))
			if shimVersionSuffix.MatchString(name) && len(elems) > 1 {
				name = elems[len(elems)-2]
			}
			names = append(names, name)
		}
	}
	return names
}

func Test_ModuleShimSourcesDoNotCollide(t *testing.T) {
	t.Parallel()

	sources := parseShimSources(t)
	require.NotEmpty(t, sources, "no shim source found")

	declaredBy := map[string]string{}
	for _, src := range sources {
		where := src.dir + "/" + src.name
		for _, name := range src.decls {
			if other, dup := declaredBy[name]; dup {
				assert.Failf(t, "declared twice", "%s is declared by %s and by %s, which end up in one package", name, other, where)
				continue
			}
			declaredBy[name] = where
		}
	}

	for _, src := range sources {
		where := src.dir + "/" + src.name
		for _, name := range shimImportNames(src.file) {
			if other, clash := declaredBy[name]; clash {
				assert.Failf(t, "import name taken", "%s imports a package as %s, a name %s declares in the package", where, name, other)
			}
		}
	}
}
