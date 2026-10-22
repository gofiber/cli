package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
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
	notes  []string
}

// clientRunMigration writes files (name to content) into a fresh directory,
// runs MigrateClientUsage on it and returns what it did. The files are valid
// Fiber v2 programs. MigrateGoPkgs moves the fiber import to v3 before the
// client migration runs, so the same is done here to the input.
func clientRunMigration(t *testing.T, files map[string]string) clientRun {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		content = strings.ReplaceAll(content, "github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v3")
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	run := clientRun{files: map[string]string{}, output: buf.String()}
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
