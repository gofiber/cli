package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// mw1Migration is the signature of the exported migrations.
type mw1Migration func(cmd *cobra.Command, cwd string, curr, target *semver.Version) error

// mw1Result is what a migration did to a project.
type mw1Result struct {
	files  map[string]string
	stdout string
	notes  []string
}

// file returns the migrated main.go.
func (r mw1Result) file() string { return r.files["main.go"] }

// mw1RunFiles runs the migration over a project made of the given files
// (name to content) and returns the migrated files and what was printed.
func mw1RunFiles(t *testing.T, migrate mw1Migration, files map[string]string) mw1Result {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	var buf bytes.Buffer
	require.NoError(t, migrate(mw1Cmd(&buf), dir, nil, nil))

	res := mw1Result{files: map[string]string{}, stdout: buf.String()}
	for name := range files {
		data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- a test fixture
		require.NoError(t, err)
		res.files[name] = string(data)
	}
	for _, n := range internal.DrainNotes(dir) {
		res.notes = append(res.notes, n.String())
	}
	return res
}

// mw1Run runs the migration over a single main.go.
func mw1Run(t *testing.T, migrate mw1Migration, src string) mw1Result {
	t.Helper()
	return mw1RunFiles(t, migrate, map[string]string{"main.go": src})
}

func mw1Cmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd
}

// mw1LineOf returns the 1-based number of the first line of src that contains
// needle.
func mw1LineOf(t *testing.T, src, needle string) int {
	t.Helper()
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	require.FailNow(t, "line not found", needle)
	return 0
}
