package migrations_test

import (
	"bytes"
	"os"
	"path/filepath"
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
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, false, nil, nil))
		assert.Empty(t, buf.String())
	})

	t.Run("verbose", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, true, nil, nil))
		out := buf.String()
		assert.Contains(t, out, "Skipping migration from >=1.0.0-0 to >=0.0.0-0")
		assert.Contains(t, out, "Skipping migration from >=2.0.0-0 to <4.0.0-0")
	})
}

func Test_DoMigration_Verbose_Run(t *testing.T) {
	curr := semver.MustParse("1.0.0")
	target := semver.MustParse("2.0.0")

	t.Run("no changes", func(t *testing.T) {
		fiberMod := `module github.com/gofiber/fiber/v2

go 1.22

require github.com/valyala/fasthttp v1.0.0`
		dir := t.TempDir()
		fiberGoMod := filepath.Join(dir, "fiber.mod")
		require.NoError(t, os.WriteFile(fiberGoMod, []byte(fiberMod), 0o600))

		restore := stubFiberDownload(t, fiberGoMod)
		t.Cleanup(restore)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\nrequire github.com/gofiber/fiber/v2 v2.0.0\n"), 0o600))
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, true, nil, nil))
		out := buf.String()
		assert.Contains(t, out, "MigrateGoPkgs: no changes")
		assert.Contains(t, out, "Skipping migration from >=2.0.0-0 to <4.0.0-0")
	})

	t.Run("changes", func(t *testing.T) {
		fiberMod := `module github.com/gofiber/fiber/v1

go 1.22

require github.com/valyala/fasthttp v1.0.0`
		dir := t.TempDir()
		fiberGoMod := filepath.Join(dir, "fiber.mod")
		require.NoError(t, os.WriteFile(fiberGoMod, []byte(fiberMod), 0o600))

		restore := stubFiberDownload(t, fiberGoMod)
		t.Cleanup(restore)

		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\nrequire github.com/gofiber/fiber/v1 v1.0.0\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nimport \"github.com/gofiber/fiber/v1\"\n"), 0o600))
		var buf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		require.NoError(t, migrations.DoMigration(cmd, dir, curr, target, true, true, nil, nil))
		out := buf.String()
		assert.Contains(t, out, "Migrating Go packages")
		assert.Contains(t, out, "MigrateGoPkgs: changed")
		assert.Contains(t, out, "Skipping migration from >=2.0.0-0 to <4.0.0-0")
	})
}
