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

// mw2Migration is the signature shared by every migration.
type mw2Migration func(cmd *cobra.Command, cwd string, curr, target *semver.Version) error

// mw2Run is the outcome of running a migration on a directory.
type mw2Run struct {
	// dir is the directory the files were written to.
	dir string
	// out is what the migration printed.
	out string
	// notes are the notes it left for the user.
	notes []internal.Note
}

// mw2Apply writes files (path relative to the directory -> content) to a new
// directory, runs the migration on it and returns the outcome.
func mw2Apply(t *testing.T, migrate mw2Migration, files map[string]string) mw2Run {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return mw2Again(t, migrate, dir)
}

// mw2Again runs the migration on a directory that already holds files.
func mw2Again(t *testing.T, migrate mw2Migration, dir string) mw2Run {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, migrate(newCmd(&buf), dir, nil, nil))
	return mw2Run{dir: dir, out: buf.String(), notes: internal.DrainNotes(dir)}
}

// file returns the current content of a file of the run.
func (r mw2Run) file(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(r.dir, filepath.FromSlash(name))) // #nosec G304
	require.NoError(t, err)
	return string(b)
}

// messages returns the text of the notes.
func (r mw2Run) messages() string {
	lines := make([]string, 0, len(r.notes))
	for _, n := range r.notes {
		lines = append(lines, n.String())
	}
	return strings.Join(lines, "\n")
}

// mw2Single runs a migration on one main.go file and returns its content.
func mw2Single(t *testing.T, migrate mw2Migration, src string) (content string, run mw2Run) {
	t.Helper()

	run = mw2Apply(t, migrate, map[string]string{"main.go": src})
	return run.file(t, "main.go"), run
}

// countImports counts the import lines of src that name path.
func countImports(src, path string) int {
	return strings.Count(src, `"`+path+`"`)
}

// countOf counts the non-overlapping occurrences of sub in s.
func countOf(s, sub string) int {
	return strings.Count(s, sub)
}
