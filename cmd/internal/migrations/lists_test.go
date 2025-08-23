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

func Test_DoMigration_Verbose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	curr := semver.MustParse("0.0.0")
	target := semver.MustParse("0.1.0")

	t.Run("silent", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, false))
		assert.Equal(t, "", buf.String())
	})

	t.Run("verbose", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, true))
		out := buf.String()
		assert.Contains(t, out, "Skipping migration from >=1.0.0 to >=0.0.0-0")
		assert.Contains(t, out, "Skipping migration from >=2.0.0 to <4.0.0-0")
	})
}
