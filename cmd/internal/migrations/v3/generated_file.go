package v3

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// generatedFile describes a file that a migration adds to the packages of the
// project that need it, such as the adapter of fiber_migrate_httpfs.go.
type generatedFile struct {
	// name is the file name, next to the file being migrated.
	name string
	// marker is a sentence that is part of the source and tells that the file was
	// added by an earlier run, so that a second run leaves it alone.
	marker string
	// source is the text of the file, with a %s for the name of the package.
	source string
	// idents are the top level names the file declares. A package that declares
	// one of them already cannot have the file.
	idents []string
}

// ensureGeneratedFile makes sure the package of the file fi has the generated
// file, adding it on first use. It reports false when the package cannot have it:
// the files of an external test package (name_test) cannot see what the package
// declares, a file of that name that the project wrote is not ours to replace,
// and a name of the project's own is not ours to use.
func ensureGeneratedFile(fi internal.FileInfo, pkgName string, gf generatedFile) bool {
	if strings.HasSuffix(pkgName, "_test") {
		return false
	}

	path := filepath.Join(filepath.Dir(fi.Path), gf.name)
	data, err := os.ReadFile(path) // #nosec G304 -- next to a file of the project being migrated
	switch {
	case err == nil && bytes.Contains(data, []byte(gf.marker)):
		return true // added by an earlier run
	case err == nil:
		return false
	}

	declared := declaredInPackage(filepath.Dir(fi.Path), pkgName)
	for _, name := range gf.idents {
		if declared[name] {
			return false
		}
	}

	source := fmt.Sprintf(gf.source, pkgName)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil { //nolint:gosec // a source file of the project, not a secret
		return false
	}
	return true
}

// declaredInPackage returns the top level names that the files of dir declare in
// the package pkgName. A file that does not parse is skipped.
func declaredInPackage(dir, pkgName string) map[string]bool {
	declared := map[string]bool{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return declared
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, entry.Name())) // #nosec G304 -- a file of the project being migrated
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), src, parser.SkipObjectResolution)
		if err != nil || file.Name.Name != pkgName {
			continue
		}

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						declared[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, id := range s.Names {
							declared[id.Name] = true
						}
					}
				}
			}
		}
	}
	return declared
}
