package migrations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_Migrations_AllRegistered fails when a migration is written but never added to
// Migrations, which leaves its breaking change unmigrated without a word: MigrateSendFileConfig
// was in the package for months before it was registered.
func Test_Migrations_AllRegistered(t *testing.T) {
	t.Parallel()

	// Migrate* functions that are not run through Migrations on purpose.
	notMigrations := []string{
		"MigrateDependencies", // runs after the code migrations, see DoMigration
	}

	registered := map[string]bool{}
	for _, m := range Migrations {
		for _, fn := range m.Functions {
			registered[migrationName(fn)] = true
		}
	}

	for _, dir := range []string{".", "v3"} {
		for _, name := range exportedMigrations(t, dir) {
			if slices.Contains(notMigrations, name) {
				continue
			}
			assert.True(t, registered[name], "%s/%s is not registered in Migrations", dir, name)
		}
	}
}

// exportedMigrations returns the exported Migrate* functions of the package in dir: the
// ones that are a MigrationFn or build one.
func exportedMigrations(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
		require.NoError(t, err)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Migrate") || !fn.Name.IsExported() {
				continue
			}
			if isMigrationShape(fn.Type) {
				names = append(names, fn.Name.Name)
			}
		}
	}

	require.NotEmpty(t, names, "no migrations found in %s", dir)
	return names
}

// isMigrationShape reports whether ft is func(cmd, cwd, curr, target) error, or returns a
// MigrationFn.
func isMigrationShape(ft *ast.FuncType) bool {
	if ft.Results == nil || len(ft.Results.List) != 1 {
		return false
	}
	result, ok := ft.Results.List[0].Type.(*ast.Ident)
	if !ok {
		return false
	}
	if result.Name == "MigrationFn" {
		return true
	}

	params := 0
	for _, f := range ft.Params.List {
		params += max(len(f.Names), 1)
	}
	return result.Name == "error" && params == 4
}
