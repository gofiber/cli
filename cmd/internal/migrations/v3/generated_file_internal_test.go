package v3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

func Test_ensureGeneratedFile(t *testing.T) {
	t.Parallel()

	gf := generatedFile{
		name:   "fiber_migrate_test_shim.go",
		marker: `added by "fiber migrate" (test)`,
		source: "// This file was " + `added by "fiber migrate" (test)` + "\n\npackage {{pkg}}\n\ntype fiberMigrateShim struct{}\n\nvar fiberMigrateFormat = \"%d %s\"\n",
		idents: []string{"fiberMigrateShim"},
	}

	read := func(t *testing.T, path string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Clean(path))
		require.NoError(t, err)
		return string(b)
	}
	write := func(t *testing.T, dir, name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}

	t.Run("adds the file once", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		main := write(t, dir, "main.go", "package app\n")
		fi := internal.FileInfo{Root: dir, Path: main, Rel: "main.go"}

		assert.True(t, ensureGeneratedFile(fi, "app", gf))
		got := read(t, filepath.Join(dir, gf.name))
		assert.Contains(t, got, "package app\n")

		// a second run finds its own file
		require.NoError(t, os.WriteFile(filepath.Join(dir, gf.name), []byte(got+"// edited\n"), 0o600))
		assert.True(t, ensureGeneratedFile(fi, "app", gf))
		assert.Contains(t, read(t, filepath.Join(dir, gf.name)), "// edited")
	})

	t.Run("a file of the project with that name stays", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		main := write(t, dir, "main.go", "package app\n")
		mine := write(t, dir, gf.name, "package app\n\n// mine\n")
		fi := internal.FileInfo{Root: dir, Path: main, Rel: "main.go"}

		assert.False(t, ensureGeneratedFile(fi, "app", gf))
		assert.Equal(t, "package app\n\n// mine\n", read(t, mine))
	})

	t.Run("a name the package declares is not ours", func(t *testing.T) {
		t.Parallel()

		for name, decl := range map[string]string{
			"type":  "type fiberMigrateShim int\n",
			"func":  "func fiberMigrateShim() {}\n",
			"var":   "var fiberMigrateShim = 1\n",
			"const": "const fiberMigrateShim = 1\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				dir := t.TempDir()
				main := write(t, dir, "main.go", "package app\n")
				write(t, dir, "other.go", "package app\n\n"+decl)
				fi := internal.FileInfo{Root: dir, Path: main, Rel: "main.go"}

				assert.False(t, ensureGeneratedFile(fi, "app", gf))
				assert.NoFileExists(t, filepath.Join(dir, gf.name))
			})
		}
	})

	t.Run("a name of another package of the directory does not matter", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		main := write(t, dir, "main.go", "package app\n")
		write(t, dir, "x_test.go", "package app_test\n\ntype fiberMigrateShim int\n")
		fi := internal.FileInfo{Root: dir, Path: main, Rel: "main.go"}

		assert.True(t, ensureGeneratedFile(fi, "app", gf))
	})

	t.Run("an external test package cannot have it", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		main := write(t, dir, "x_test.go", "package app_test\n")
		fi := internal.FileInfo{Root: dir, Path: main, Rel: "x_test.go"}

		assert.False(t, ensureGeneratedFile(fi, "app_test", gf))
		assert.NoFileExists(t, filepath.Join(dir, gf.name))
	})
}
