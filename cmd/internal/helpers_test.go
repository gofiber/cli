package internal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func readGoFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path) // #nosec G304 -- the path is this test's own temp dir
	require.NoError(t, err)
	return string(b)
}

func Test_ChangeFileContent_RewritesGoFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	a := writeGoFile(t, dir, "a.go", "package a\n\nvar x = 1\n")
	sub := writeGoFile(t, dir, "sub/b.go", "package b\n\nvar x = 1\n")
	vendored := writeGoFile(t, dir, "vendor/c.go", "package c\n\nvar x = 1\n")
	text := writeGoFile(t, dir, "notes.txt", "var x = 1\n")

	changed, err := internal.ChangeFileContent(dir, func(content string) string {
		return strings.ReplaceAll(content, "x = 1", "x = 2")
	})
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Contains(t, readGoFile(t, a), "x = 2")
	assert.Contains(t, readGoFile(t, sub), "x = 2")
	assert.Contains(t, readGoFile(t, vendored), "x = 1", "vendor is skipped")
	assert.Contains(t, readGoFile(t, text), "x = 1", "only .go files are touched")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_ChangeFileContent_SkipsBrokenFilesAndContinues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Walk order is lexical, so the broken file sits between two good ones.
	first := writeGoFile(t, dir, "a_first.go", "package a\n\nvar x = 1\n")
	broken := writeGoFile(t, dir, "b_broken.go", "package a\n\nvar y = (1\nvar x = 1\n")
	last := writeGoFile(t, dir, "c_last.go", "package a\n\nvar z = 1\nvar x = 1\n")

	changed, err := internal.ChangeFileContent(dir, func(content string) string {
		return strings.ReplaceAll(content, "x = 1", "x = 2")
	})
	require.NoError(t, err, "a file that does not parse must not fail the run")
	assert.True(t, changed)

	assert.Contains(t, readGoFile(t, first), "x = 2")
	assert.Contains(t, readGoFile(t, last), "x = 2", "files after the broken one are still migrated")
	assert.Equal(t, "package a\n\nvar y = (1\nvar x = 1\n", readGoFile(t, broken), "the broken file is left as it was")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "b_broken.go", notes[0].File)
	assert.Positive(t, notes[0].Line)
	assert.Contains(t, notes[0].Message, "does not parse")
	assert.Empty(t, internal.DrainNotes(dir), "draining forgets the notes")
}

func Test_ChangeFileContent_RewriteThatBreaksTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeGoFile(t, dir, "a.go", "package a\n\nvar x = 1\n")

	changed, err := internal.ChangeFileContent(dir, func(content string) string {
		return strings.ReplaceAll(content, "x = 1", "x = (1")
	})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, "package a\n\nvar x = 1\n", readGoFile(t, path))

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "rewritten code does not parse")
}

func Test_ChangeFileContent_Filters(t *testing.T) {
	// Not parallel: the filters are process-wide.
	dir := t.TempDir()
	keep := writeGoFile(t, dir, "keep.go", "package a\n\nvar x = 1\n")
	skip := writeGoFile(t, dir, "skip.go", "package a\n\nvar x = 1\n")

	internal.SetFileFilters(nil, []string{"skip.go"})
	defer internal.SetFileFilters(nil, nil)

	changed, err := internal.ChangeFileContent(dir, func(content string) string {
		return strings.ReplaceAll(content, "x = 1", "x = 2")
	})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Contains(t, readGoFile(t, keep), "x = 2")
	assert.Contains(t, readGoFile(t, skip), "x = 1")
}

func Test_ChangeFileContentInfo_PassesTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeGoFile(t, dir, "pkg/a.go", "package a\n")

	var seen []internal.FileInfo
	changed, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		seen = append(seen, fi)
		return content
	})
	require.NoError(t, err)
	assert.False(t, changed)

	require.Len(t, seen, 1)
	assert.Equal(t, dir, seen[0].Root)
	assert.Equal(t, filepath.Join(dir, "pkg", "a.go"), seen[0].Path)
	assert.Equal(t, filepath.Join("pkg", "a.go"), seen[0].Rel)
}
