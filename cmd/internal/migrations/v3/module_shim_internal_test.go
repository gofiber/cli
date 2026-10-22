package v3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// shimTestFile is a file of the package as a migration would describe it.
func shimTestFile(name, marker string) moduleShimFile {
	return moduleShimFile{
		name:   name,
		marker: marker,
		source: "// " + marker + "\n\npackage " + moduleShimPkg + "\n",
	}
}

// shimTestProject lays out a module at dir with a go.mod and returns the file
// info of a Go file in its sub directory sub.
func shimTestProject(t *testing.T, dir, modPath, sub string) internal.FileInfo {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+modPath+"\n\ngo 1.25\n"), 0o600))
	file := filepath.Join(dir, sub, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package main\n"), 0o600))

	rel, err := filepath.Rel(dir, file)
	require.NoError(t, err)
	return internal.FileInfo{Root: dir, Path: file, Rel: rel}
}

func Test_ModuleOf(t *testing.T) {
	t.Parallel()

	t.Run("the nearest go.mod above the file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fi := shimTestProject(t, dir, "example.com/app", "cmd/api")

		mod, ok := moduleOf(fi.Path, fi.Root)
		require.True(t, ok)
		assert.Equal(t, goModule{root: dir, path: "example.com/app"}, mod)
	})

	t.Run("a nested module is its own", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		shimTestProject(t, dir, "example.com/app", "cmd")
		inner := shimTestProject(t, filepath.Join(dir, "tools"), "example.com/tools/v2", "gen")
		inner.Root = dir

		mod, ok := moduleOf(inner.Path, inner.Root)
		require.True(t, ok)
		assert.Equal(t, goModule{root: filepath.Join(dir, "tools"), path: "example.com/tools/v2"}, mod)
	})

	t.Run("the search stops at the directory the migration runs in", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		shimTestProject(t, dir, "example.com/app", "cmd")
		sub := filepath.Join(dir, "cmd")

		_, ok := moduleOf(filepath.Join(sub, "main.go"), sub)
		assert.False(t, ok, "the go.mod above the directory of the run is not looked at")
	})

	t.Run("no go.mod", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, ok := moduleOf(filepath.Join(dir, "main.go"), dir)
		assert.False(t, ok)
	})

	t.Run("a go.mod without a module path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.25\n"), 0o600))
		_, ok := moduleOf(filepath.Join(dir, "main.go"), dir)
		assert.False(t, ok)
	})
}

func Test_EnsureModuleShim(t *testing.T) {
	t.Parallel()

	t.Run("adds the file once and names the package", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fi := shimTestProject(t, dir, "example.com/app", "cmd/api")
		f := shimTestFile("agent.go", moduleShimMarker+": the agent")

		importPath, ok := ensureModuleShim(fi, f)
		require.True(t, ok)
		assert.Equal(t, "example.com/app/internal/fibermigrate", importPath)

		file := filepath.Join(dir, "internal", "fibermigrate", "agent.go")
		got, err := os.ReadFile(file) // #nosec G304 -- a file of the temp project
		require.NoError(t, err)
		assert.Equal(t, f.source, string(got))

		// a later run leaves the file as it is, even when it was edited
		edited := string(got) + "// edited\n"
		require.NoError(t, os.WriteFile(file, []byte(edited), 0o600))
		again, ok := ensureModuleShim(fi, f)
		require.True(t, ok)
		assert.Equal(t, importPath, again)
		got, err = os.ReadFile(file) // #nosec G304 -- a file of the temp project
		require.NoError(t, err)
		assert.Equal(t, edited, string(got))
	})

	t.Run("two migrations share the package", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fi := shimTestProject(t, dir, "example.com/app", ".")

		_, ok := ensureModuleShim(fi, shimTestFile("agent.go", moduleShimMarker+": the agent"))
		require.True(t, ok)
		_, ok = ensureModuleShim(fi, shimTestFile("listen.go", moduleShimMarker+": the listener settings"))
		require.True(t, ok)

		for _, name := range []string{"agent.go", "listen.go"} {
			assert.FileExists(t, filepath.Join(dir, "internal", "fibermigrate", name))
		}
	})

	t.Run("a nested module gets its own package", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		shimTestProject(t, dir, "example.com/app", "cmd")
		inner := shimTestProject(t, filepath.Join(dir, "svc"), "example.com/svc", "api")
		inner.Root = dir

		importPath, ok := ensureModuleShim(inner, shimTestFile("agent.go", moduleShimMarker))
		require.True(t, ok)
		assert.Equal(t, "example.com/svc/internal/fibermigrate", importPath)
		assert.FileExists(t, filepath.Join(dir, "svc", "internal", "fibermigrate", "agent.go"))
		assert.NoDirExists(t, filepath.Join(dir, "internal"))
	})

	t.Run("a file of that name the migrations did not write", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fi := shimTestProject(t, dir, "example.com/app", ".")
		pkg := filepath.Join(dir, "internal", "fibermigrate")
		require.NoError(t, os.MkdirAll(pkg, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(pkg, "agent.go"), []byte("package fibermigrate\n"), 0o600))

		_, ok := ensureModuleShim(fi, shimTestFile("agent.go", moduleShimMarker))
		assert.False(t, ok)
	})

	t.Run("a package of the project's own", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fi := shimTestProject(t, dir, "example.com/app", ".")
		pkg := filepath.Join(dir, "internal", "fibermigrate")
		require.NoError(t, os.MkdirAll(pkg, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(pkg, "other.go"), []byte("package fibermigrate\n"), 0o600))

		_, ok := ensureModuleShim(fi, shimTestFile("agent.go", moduleShimMarker))
		assert.False(t, ok)
		assert.NoFileExists(t, filepath.Join(pkg, "agent.go"))
	})

	t.Run("outside of a module", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		file := filepath.Join(dir, "main.go")
		require.NoError(t, os.WriteFile(file, []byte("package main\n"), 0o600))

		_, ok := ensureModuleShim(internal.FileInfo{Root: dir, Path: file, Rel: "main.go"}, shimTestFile("agent.go", moduleShimMarker))
		assert.False(t, ok)
		assert.NoDirExists(t, filepath.Join(dir, "internal"))
	})
}

func Test_ModuleShimPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fi := shimTestProject(t, dir, "example.com/app/v3", "cmd")

	importPath, ok := moduleShimPath(fi)
	require.True(t, ok)
	assert.Equal(t, "example.com/app/v3/internal/fibermigrate", importPath)
	assert.NoDirExists(t, filepath.Join(dir, "internal"), "asking for the path adds nothing")
}
