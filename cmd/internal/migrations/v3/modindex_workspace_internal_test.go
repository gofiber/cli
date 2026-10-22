package v3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// A repository can hold several modules: the index of each answers for the import
// paths and the directories of the others, and keeps its finders to its own packages.
func Test_ModuleIndex_Workspace(t *testing.T) {
	// the subtests share the indexes, which are not safe for concurrent use
	dir := modTestFiles(t, map[string]string{
		"go.mod":               "// the repository itself is no module\n",
		"go.work":              "go 1.25\n\nuse (\n\t./app\n\t./shared\n)\n",
		"app/go.mod":           "module example.com/app\n\ngo 1.25\n",
		"app/main.go":          "package main\n\nimport \"example.com/shared/web\"\n\nfunc main() { web.Run() }\n",
		"app/tools/go.mod":     "module example.com/app/tools\n\ngo 1.25\n",
		"app/tools/gen.go":     "package tools\n\nimport \"example.com/shared/web\"\n\nvar _ = web.Run\n",
		"shared/go.mod":        "module example.com/shared\n\ngo 1.25\n",
		"shared/web/web.go":    "package web\n\nfunc Run() {}\n",
		"shared/web/x_test.go": "package web\n\nimport _ \"example.com/shared/web\"\n",
		"shared/vendor/x/x.go": "package x\n\nimport \"example.com/shared/web\"\n",
	})
	file := func(rel string) internal.FileInfo {
		return internal.FileInfo{Root: dir, Path: filepath.Join(dir, filepath.FromSlash(rel)), Rel: rel}
	}
	var cache moduleIndexes
	app := cache.of(file("app/main.go"))
	shared := cache.of(file("shared/web/web.go"))
	tools := cache.of(file("app/tools/gen.go"))
	require.NotNil(t, app)
	require.NotNil(t, shared)
	require.NotNil(t, tools)

	t.Run("the import path of another module", func(t *testing.T) {
		d, ok := app.dirOf("example.com/shared/web")
		assert.True(t, ok)
		assert.Equal(t, filepath.Join(dir, "shared", "web"), d)

		d, ok = shared.dirOf("example.com/app")
		assert.True(t, ok)
		assert.Equal(t, filepath.Join(dir, "app"), d)

		assert.Nil(t, app.packageAt("example.com/shared/missing"), "a directory the module does not have")
		_, ok = app.dirOf("example.com/sharedx/web")
		assert.False(t, ok, "a module whose name only starts like it")
	})

	t.Run("a nested module is told from the module around it", func(t *testing.T) {
		d, ok := app.dirOf("example.com/app/tools")
		assert.True(t, ok)
		assert.Equal(t, filepath.Join(dir, "app", "tools"), d)

		got, ok := app.importPathOf(filepath.Join(dir, "app", "tools"))
		assert.True(t, ok)
		assert.Equal(t, "example.com/app/tools", got)

		got, ok = tools.importPathOf(filepath.Join(dir, "app"))
		assert.True(t, ok)
		assert.Equal(t, "example.com/app", got)
	})

	t.Run("the package of another module", func(t *testing.T) {
		pkg := app.packageAt("example.com/shared/web")
		require.NotNil(t, pkg)
		assert.Equal(t, "web", pkg.name)
		assert.Equal(t, "example.com/shared/web", pkg.importPath)
		assert.NotNil(t, pkg.lookup("Run"))
		assert.Same(t, pkg, app.packageIn(filepath.Join(dir, "shared", "web")))
		assert.Same(t, pkg, shared.packageAt("example.com/shared/web"))
	})

	t.Run("the directories and files of another module", func(t *testing.T) {
		got, ok := app.importPathOf(filepath.Join(dir, "shared", "web"))
		assert.True(t, ok)
		assert.Equal(t, "example.com/shared/web", got)

		f := app.fileAt(filepath.Join(dir, "shared", "web", "web.go"))
		require.NotNil(t, f)
		assert.Equal(t, "web", f.g.file.Name.Name)
		assert.Same(t, f, shared.fileAt(filepath.Join(dir, "shared", "web", "web.go")))
		assert.Nil(t, app.fileAt(filepath.Join(dir, "shared", "vendor", "x", "x.go")))
		_, ok = app.importPathOf(filepath.Join(dir, "elsewhere"))
		assert.False(t, ok)
	})

	t.Run("the finders stay in the module", func(t *testing.T) {
		var got []string
		for _, p := range app.packages() {
			got = append(got, p.importPath)
		}
		assert.Equal(t, []string{"example.com/app"}, got)
	})

	t.Run("the files of every module that import a package", func(t *testing.T) {
		var got []string
		for _, f := range app.importers("example.com/shared/web") {
			rel, err := filepath.Rel(dir, f.path)
			require.NoError(t, err)
			got = append(got, filepath.ToSlash(rel))
		}
		assert.Equal(t, []string{"app/main.go", "app/tools/gen.go", "shared/web/x_test.go"}, got)
	})

	t.Run("an index of its own module only", func(t *testing.T) {
		alone := newModuleIndex(file("app/main.go"))
		require.NotNil(t, alone)
		_, ok := alone.dirOf("example.com/shared/web")
		assert.False(t, ok)
		assert.Nil(t, alone.packageAt("example.com/shared/web"))
	})

	// last, because it writes
	t.Run("what is written is forgotten in the module it belongs to", func(t *testing.T) {
		web := filepath.Join(dir, "shared", "web", "web.go")
		require.NotNil(t, app.packageAt("example.com/shared/web").lookup("Run"))
		require.NoError(t, os.WriteFile(web, []byte("package web\n\nfunc Stop() {}\n"), 0o600))

		app.invalidate(web)

		pkg := app.packageAt("example.com/shared/web")
		require.NotNil(t, pkg)
		assert.Nil(t, pkg.lookup("Run"))
		assert.NotNil(t, pkg.lookup("Stop"))
	})
}
