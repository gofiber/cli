package migrations_test

import (
	"bytes"
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

func replaceFoo(cmd *cobra.Command, cwd string, curr, target *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return strings.ReplaceAll(content, "foo", "bar")
	})
	if changed {
		cmd.Println("replaceFoo")
	}
	return err //nolint:wrapcheck // returning raw error is fine for tests
}

func Test_DoMigration_FileIncludeExclude(t *testing.T) {
	orig := migrations.Migrations
	migrations.Migrations = []migrations.Migration{
		{From: ">=0.0.0", To: ">=0.0.0", Functions: []migrations.MigrationFn{replaceFoo}},
	}
	t.Cleanup(func() { migrations.Migrations = orig })

	curr := semver.MustParse("0.0.0")
	target := semver.MustParse("1.0.0")

	t.Run("include glob", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main\nvar foo = 1\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bar.go"), []byte("package main\nvar foo = 1\n"), 0o600))
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, false, []string{"*foo.go"}, nil))
		b, err := os.ReadFile(filepath.Join(dir, "foo.go")) // #nosec G304
		require.NoError(t, err)
		assert.Contains(t, string(b), "var bar = 1")
		b, err = os.ReadFile(filepath.Join(dir, "bar.go")) // #nosec G304
		require.NoError(t, err)
		assert.Contains(t, string(b), "var foo = 1")
	})

	t.Run("exclude regex", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main\nvar foo = 1\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bar.go"), []byte("package main\nvar foo = 1\n"), 0o600))
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, false, nil, []string{"^bar\\.go$"}))
		b, err := os.ReadFile(filepath.Join(dir, "foo.go")) // #nosec G304
		require.NoError(t, err)
		assert.Contains(t, string(b), "var bar = 1")
		b, err = os.ReadFile(filepath.Join(dir, "bar.go")) // #nosec G304
		require.NoError(t, err)
		assert.Contains(t, string(b), "var foo = 1")
	})
}
