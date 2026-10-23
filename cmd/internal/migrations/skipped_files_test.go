package migrations_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations"
)

// breakA is a rewrite that turns a.go into code that does not parse and moves OLD to NEW
// in every other file.
func breakA(content string) string {
	if strings.Contains(content, "package a") {
		return content + "\nfunc (\n"
	}
	return strings.ReplaceAll(content, "OLD", "NEW")
}

func writeSkippedFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range map[string]string{
		"a.go":     "package a\n\nvar A = 1 // OLD\n",
		"b.go":     "package b\n\nvar B = 1 // OLD\n",
		"sub/c.go": "package c\n\nvar C = 1 // OLD\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return dir
}

func readSkippedFixture(t *testing.T, dir, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))) //nolint:gosec // the path is this test's own temp dir
	require.NoError(t, err)
	return string(b)
}

// One file whose rewrite does not parse used to stop the walk with an error, so the files
// after it were never migrated.
func Test_ChangeFileContent_KeepsTheFileWhoseRewriteDoesNotParse(t *testing.T) {
	t.Parallel()

	dir := writeSkippedFixture(t)

	changed, err := internal.ChangeFileContent(dir, breakA)
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Equal(t, "package a\n\nvar A = 1 // OLD\n", readSkippedFixture(t, dir, "a.go"), "the file is left as it was")
	assert.Equal(t, "package b\n\nvar B = 1 // NEW\n", readSkippedFixture(t, dir, "b.go"), "the files after it are migrated")
	assert.Equal(t, "package c\n\nvar C = 1 // NEW\n", readSkippedFixture(t, dir, "sub/c.go"))

	skipped := internal.DrainSkippedFiles(dir)
	require.Len(t, skipped, 1)
	assert.Equal(t, "a.go", skipped[0].File)
	assert.True(t, strings.HasPrefix(skipped[0].Reason, "a.go:"), skipped[0].Reason)
	assert.NotContains(t, skipped[0].Reason, dir, "the path is relative to the migrated directory")
	assert.NotContains(t, skipped[0].Reason, "\n")
	assert.Empty(t, internal.DrainSkippedFiles(dir), "the files are forgotten once they are drained")
}

func Test_ChangeFileContent_ListsASkippedFileOnce(t *testing.T) {
	t.Parallel()

	dir := writeSkippedFixture(t)

	_, err := internal.ChangeFileContent(dir, breakA)
	require.NoError(t, err)
	// the next migration breaks the same file
	changed, err := internal.ChangeFileContent(dir, breakA)
	require.NoError(t, err)
	assert.False(t, changed, "b.go and c.go have nothing left to change")

	skipped := internal.DrainSkippedFiles(dir)
	require.Len(t, skipped, 1)
	assert.Equal(t, "a.go", skipped[0].File)
}

func Test_ChangeFileContent_SkippedFilesBelongToTheirDirectory(t *testing.T) {
	t.Parallel()

	one, other := writeSkippedFixture(t), writeSkippedFixture(t)

	_, err := internal.ChangeFileContent(one, breakA)
	require.NoError(t, err)

	assert.Empty(t, internal.DrainSkippedFiles(other))
	assert.Len(t, internal.DrainSkippedFiles(one), 1)
}

// This test replaces the list of migrations, so it does not run in parallel with the
// others.
func Test_DoMigration_NamesTheFilesItLeftAsTheyWere(t *testing.T) {
	orig := migrations.Migrations
	t.Cleanup(func() { migrations.Migrations = orig })
	migrations.Migrations = []migrations.Migration{{
		From: ">=1.0.0-0",
		To:   "<4.0.0-0",
		Functions: []migrations.MigrationFn{func(_ *cobra.Command, cwd string, _, _ *semver.Version) error {
			if _, err := internal.ChangeFileContent(cwd, breakA); err != nil {
				return fmt.Errorf("change files: %w", err)
			}
			return nil
		}},
	}}

	dir := writeSkippedFixture(t)
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	require.NoError(t, migrations.DoMigration(cmd, dir, semver.MustParse("2.52.0"), semver.MustParse("3.0.0"), true, false, nil, nil))

	out := buf.String()
	assert.Contains(t, out, "Not migrated: a rewrite of these files produced code that does not parse, so they are left as they were:")
	assert.Contains(t, out, "  a.go:")
	assert.NotContains(t, out, "b.go")
	assert.Equal(t, "package b\n\nvar B = 1 // NEW\n", readSkippedFixture(t, dir, "b.go"))

	// a run that has no such file says nothing
	buf.Reset()
	require.NoError(t, os.Remove(filepath.Join(dir, "a.go")))
	require.NoError(t, migrations.DoMigration(cmd, dir, semver.MustParse("2.52.0"), semver.MustParse("3.0.0"), true, false, nil, nil))
	assert.NotContains(t, buf.String(), "Not migrated")
}

func Test_PrintSkippedFiles(t *testing.T) {
	t.Parallel()

	dir := writeSkippedFixture(t)
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	migrations.PrintSkippedFiles(cmd, dir)
	assert.Empty(t, buf.String(), "nothing to say about a run that skipped no file")

	_, err := internal.ChangeFileContent(dir, breakA)
	require.NoError(t, err)

	migrations.PrintSkippedFiles(cmd, dir)
	assert.Contains(t, buf.String(), "  a.go:")

	buf.Reset()
	migrations.PrintSkippedFiles(cmd, dir)
	assert.Empty(t, buf.String(), "a file is named once")
}
