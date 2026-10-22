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

	semver "github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectedMigrations returns the names of the migration functions that run when
// moving from curr to target.
func selectedMigrations(t *testing.T, curr, target string) []string {
	t.Helper()

	var names []string
	for _, m := range Migrations {
		applies, err := m.matches(semver.MustParse(curr), semver.MustParse(target))
		require.NoError(t, err)
		if !applies {
			continue
		}
		for _, fn := range m.Functions {
			names = append(names, migrationName(fn))
		}
	}
	return names
}

func Test_Migrations_Bands(t *testing.T) {
	t.Parallel()

	const (
		v2Rewrite    = "MigrateHandlerSignatures"
		sendFile     = "MigrateSendFileConfig"
		preRelease   = "MigrateListenerCallbacks"
		ruleList     = "MigrateRuleList"
		goPkgs       = "MigrateGoPkgs"
		goVersionFix = "MigrateGoVersion"
	)

	tests := []struct {
		name    string
		curr    string
		target  string
		want    []string
		wantNot []string
	}{
		{
			name: "v2 to the latest v3", curr: "2.52.15", target: "3.6.1",
			want:    []string{v2Rewrite, sendFile, goVersionFix, ruleList, goPkgs},
			wantNot: []string{preRelease},
		},
		{
			name: "v2 to the first v3 release", curr: "2.0.0", target: "3.0.0",
			want:    []string{v2Rewrite, sendFile, goPkgs},
			wantNot: []string{preRelease, ruleList},
		},
		{
			name: "v2 to a v3 pre-release", curr: "2.52.15", target: "3.0.0-beta.4",
			want:    []string{v2Rewrite, sendFile},
			wantNot: []string{preRelease},
		},
		{
			name: "v2 pre-release to v3", curr: "2.0.0-beta.1", target: "3.1.0",
			want: []string{v2Rewrite},
		},
		{
			name: "v3 pre-release to the latest v3", curr: "3.0.0-beta.4", target: "3.6.1",
			want:    []string{preRelease, ruleList, goPkgs},
			wantNot: []string{v2Rewrite, sendFile, goVersionFix},
		},
		{
			name: "v3 release candidate to 3.0.0", curr: "3.0.0-rc.1", target: "3.0.0",
			want:    []string{preRelease},
			wantNot: []string{v2Rewrite, ruleList},
		},
		{
			name: "v3 to a newer v3", curr: "3.2.0", target: "3.6.1",
			want:    []string{ruleList, goPkgs},
			wantNot: []string{v2Rewrite, sendFile, preRelease, goVersionFix},
		},
		{
			name: "v3 to a v3 before RuleList", curr: "3.0.0", target: "3.5.0",
			want:    []string{goPkgs},
			wantNot: []string{v2Rewrite, sendFile, preRelease, ruleList},
		},
		{
			name: "v2 to a newer v2", curr: "2.40.0", target: "2.52.15",
			want:    []string{goPkgs},
			wantNot: []string{v2Rewrite, sendFile, preRelease, ruleList},
		},
		{
			name: "v3 leaving v3", curr: "3.6.1", target: "4.0.0",
			want:    []string{goPkgs},
			wantNot: []string{v2Rewrite, sendFile, preRelease, ruleList},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := selectedMigrations(t, tc.curr, tc.target)
			for _, name := range tc.want {
				assert.Contains(t, got, name)
			}
			for _, name := range tc.wantNot {
				assert.NotContains(t, got, name)
			}
		})
	}
}

// Test_Migrations_AllRegistered fails when a migration is written but never
// added to Migrations, which silently leaves its breaking change unmigrated.
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

// exportedMigrations returns the exported Migrate* functions of the package in
// dir: the ones that are a MigrationFn or build one.
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

// isMigrationShape reports whether ft is func(cmd, cwd, curr, target) error, or
// returns a MigrationFn.
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
