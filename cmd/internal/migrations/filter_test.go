package migrations_test

import (
	"bytes"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations"
)

func fnFoo(cmd *cobra.Command, cwd string, curr, target *semver.Version) error {
	cmd.Println("foo")
	return nil
}

func fnBar(cmd *cobra.Command, cwd string, curr, target *semver.Version) error {
	cmd.Println("bar")
	return nil
}

func Test_DoMigration_IncludeExclude(t *testing.T) {
	orig := migrations.Migrations
	migrations.Migrations = []migrations.Migration{
		{From: ">=0.0.0", To: ">=0.0.0", Functions: []migrations.MigrationFn{fnFoo, fnBar}},
	}
	t.Cleanup(func() { migrations.Migrations = orig })

	dir := t.TempDir()
	curr := semver.MustParse("0.0.0")
	target := semver.MustParse("1.0.0")

	t.Run("include glob", func(t *testing.T) {
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, false, []string{"fnF*"}, nil))
		out := buf.String()
		assert.Contains(t, out, "foo")
		assert.NotContains(t, out, "bar")
	})

	t.Run("exclude regex", func(t *testing.T) {
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, false, nil, []string{"^fnB.*"}))
		out := buf.String()
		assert.Contains(t, out, "foo")
		assert.NotContains(t, out, "bar")
	})
}
