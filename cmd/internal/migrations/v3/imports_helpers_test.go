package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// importsTree writes files (path relative to the returned directory -> content)
// and returns the directory.
func importsTree(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return dir
}

// importsRead returns the content of the file name below dir.
func importsRead(t *testing.T, dir, name string) string {
	t.Helper()

	return readFile(t, filepath.Join(dir, filepath.FromSlash(name)))
}

// importsNotes returns the rendered notes the migrations left for dir.
func importsNotes(dir string) []string {
	var out []string
	for _, n := range internal.DrainNotes(dir) {
		out = append(out, n.String())
	}
	return out
}

// importsOutput runs fn with a command that collects what it prints.
func importsOutput(t *testing.T, fn func(buf *bytes.Buffer) error) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, fn(&buf))
	return buf.String()
}
