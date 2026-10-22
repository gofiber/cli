package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// clientRun is the outcome of MigrateClientUsage on a directory.
type clientRun struct {
	files  map[string]string
	output string
	// dir is the directory the files were migrated in.
	dir   string
	notes []string
}

// clientModule is the go.mod of the projects the tests migrate, which the copy of the
// Fiber v2 agent is added to.
const clientModule = "module example.com/app\n\ngo 1.25\n"

// clientShimImport is how the code that uses the copy imports it.
const clientShimImport = "example.com/app/internal/fibermigrate"

// clientRunMigration writes files (name to content) into a fresh directory with a
// go.mod, runs MigrateClientUsage on it and returns what it did. The files are valid
// Fiber v2 programs. MigrateGoPkgs moves the fiber import to v3 before the
// client migration runs, so the same is done here to the input.
func clientRunMigration(t *testing.T, files map[string]string) clientRun {
	t.Helper()
	return clientRunProject(t, files, true)
}

// clientRunProject is clientRunMigration for a project that has a go.mod or not. A
// go.mod among the files is the one used.
func clientRunProject(t *testing.T, files map[string]string, withModule bool) clientRun {
	t.Helper()

	dir := t.TempDir()
	if _, own := files["go.mod"]; !own && withModule {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(clientModule), 0o600))
	}
	for name, content := range files {
		content = strings.ReplaceAll(content, "github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v3")
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	run := clientRun{files: map[string]string{}, output: buf.String(), dir: dir}
	for name := range files {
		b, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304
		require.NoError(t, err)
		run.files[name] = string(b)
	}
	for _, n := range internal.DrainNotes(dir) {
		run.notes = append(run.notes, n.String())
	}

	// A second run must not change anything, nor print anything.
	buf.Reset()
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))
	assert.Empty(t, buf.String(), "the second run must print nothing")
	for name, content := range run.files {
		b, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304
		require.NoError(t, err)
		assert.Equal(t, content, string(b), "the second run must not change %s", name)
	}
	internal.DrainNotes(dir)

	return run
}

// clientMigrateSource migrates a single file and returns the result.
func clientMigrateSource(t *testing.T, src string) clientRun {
	t.Helper()
	return clientRunMigration(t, map[string]string{"main.go": src})
}

// clientAssertSource compares a migrated file with the expected source after
// both were formatted.
func clientAssertSource(t *testing.T, want, got string) {
	t.Helper()

	assert.Equal(t, clientGofmtSource(t, want), clientGofmtSource(t, got))
}

// clientCase is a migration of a package: the v2 files, the v3 files that are
// expected and the notes the migration prints.
type clientCase struct {
	files map[string]string
	want  map[string]string
	name  string
	notes []string
}

// clientRunCases migrates every case in a directory of its own and compares the
// files with the expected ones, and the notes with the expected ones in any
// order: each expected text has to be part of one note, and no note may be left.
func clientRunCases(t *testing.T, cases []clientCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := clientRunMigration(t, tc.files)
			for name, want := range tc.want {
				clientAssertSource(t, want, run.files[name])
			}

			left := slices.Clone(run.notes)
			for _, want := range tc.notes {
				at := slices.IndexFunc(left, func(note string) bool { return strings.Contains(note, want) })
				if at < 0 {
					t.Errorf("missing note %q in %q", want, run.notes)
					continue
				}
				left = slices.Delete(left, at, at+1)
			}
			assert.Empty(t, left, "unexpected notes")
		})
	}
}

// clientShimNote ends the note of a request that stays on the copy of the Fiber v2
// agent.
const clientShimNote = "; this request still runs on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go"

// clientShareNote is the note of a request that could be migrated but shares its
// agent with code that cannot.
const clientShareNote = "its agent is shared with code that stays on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go, so this request does too"

// clientShareClientNote is the note of a request that could be migrated but shares
// its client with requests that cannot.
const clientShareClientNote = "its client is shared with requests that stay on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go, so this request does too"
