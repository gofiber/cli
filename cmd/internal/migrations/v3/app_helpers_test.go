package v3_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// appMigration is the signature of the migrations of the app area.
type appMigration = func(cmd *cobra.Command, cwd string, curr, target *semver.Version) error

// appProject writes files (path relative to the project to source) in a new
// directory and returns it. The directory is a module unless the files have a
// go.mod of their own: the migrations that add a package of their own to the
// module need one.
func appProject(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files = maps.Clone(files)
		files["go.mod"] = "module example.com/app\n\ngo 1.25\n"
	}
	for name, src := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	}
	return dir
}

// appSingle writes src as main.go of a new project.
func appSingle(t *testing.T, src string) string {
	t.Helper()
	return appProject(t, map[string]string{"main.go": src})
}

// appRead returns the content of a file of the project.
func appRead(t *testing.T, dir, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))) // #nosec G304 -- test file
	require.NoError(t, err)
	return string(b)
}

// appRun runs the migration on the project and returns what it printed.
func appRun(t *testing.T, dir string, migrate appMigration) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, migrate(newCmd(&buf), dir, nil, nil))
	return buf.String()
}
