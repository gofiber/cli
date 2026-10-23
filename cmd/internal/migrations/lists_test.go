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
		assert.Contains(t, out, "Skipping migration from >=2.0.0-0 <3.0.0-0 to >=3.0.0-0 <4.0.0-0")
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
		assert.Contains(t, out, "Skipping migration from >=2.0.0-0 <3.0.0-0 to >=3.0.0-0 <4.0.0-0")
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
		assert.Contains(t, out, "Skipping migration from >=2.0.0-0 <3.0.0-0 to >=3.0.0-0 <4.0.0-0")
	})
}

// The rewrites of the v2 to v3 list are for code written against Fiber v2. On a project that
// is on v3 already they changed valid v3 code: c.Context() is the context.Context of v3, and
// the list turned it into c.RequestCtx().
func Test_DoMigration_V2ListLeavesV3ProjectsAlone(t *testing.T) {
	const source = `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	ctx := c.Context()
	_ = ctx
	return nil
}
`

	tests := []struct {
		name      string
		curr      string
		target    string
		rewritten bool
	}{
		{name: "v2 project", curr: "2.52.0", target: "3.6.1", rewritten: true},
		{name: "v3.5.0 going to 3.6.1", curr: "3.5.0", target: "3.6.1", rewritten: false},
		{name: "v3.0.0 going to 3.6.1", curr: "3.0.0", target: "3.6.1", rewritten: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.go")
			require.NoError(t, os.WriteFile(main, []byte(source), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte("module test\n\nrequire github.com/gofiber/fiber/v3 v3.6.0\n"), 0o600))

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)
			require.NoError(t, migrations.DoMigration(cmd, dir,
				semver.MustParse(tc.curr), semver.MustParse(tc.target), true, false, nil, nil))

			content, err := os.ReadFile(main) //nolint:gosec // the path is this test's own temp dir
			require.NoError(t, err)
			if tc.rewritten {
				assert.Contains(t, string(content), "c.RequestCtx()")
			} else {
				assert.Equal(t, source, string(content))
			}
		})
	}
}

// The same rewrites turn valid v2 code into code that does not build against v2, so a project
// that goes from one release of v2 to another does not get them. It still gets the new
// version, which is what the first list is for.
func Test_DoMigration_V2ListNeedsAV3Target(t *testing.T) {
	const source = `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.SendFile("a.txt", true)
}
`

	tests := []struct {
		name      string
		target    string
		rewritten bool
	}{
		{name: "v3", target: "3.0.0", rewritten: true},
		{name: "another release of v2", target: "2.52.1", rewritten: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.go")
			goMod := filepath.Join(dir, "go.mod")
			require.NoError(t, os.WriteFile(main, []byte(source), 0o600))
			require.NoError(t, os.WriteFile(goMod, []byte("module test\n\nrequire github.com/gofiber/fiber/v2 v2.52.0\n"), 0o600))

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)
			require.NoError(t, migrations.DoMigration(cmd, dir,
				semver.MustParse("2.52.0"), semver.MustParse(tc.target), true, false, nil, nil))

			content, err := os.ReadFile(main) //nolint:gosec // the path is this test's own temp dir
			require.NoError(t, err)
			mod, err := os.ReadFile(goMod) //nolint:gosec // the path is this test's own temp dir
			require.NoError(t, err)
			assert.Contains(t, string(mod), tc.target, "the new version is required either way")
			if tc.rewritten {
				assert.Contains(t, string(content), "fiber.SendFile{")
			} else {
				assert.Equal(t, source, string(content))
			}
		})
	}
}

func Test_DoMigration_RuleListFollowsTheTarget(t *testing.T) {
	source := `package main

import "github.com/gofiber/fiber/v3/middleware/redirect"

var _ = redirect.New(redirect.Config{
	Rules: map[string]string{"/old": "/new"},
})`

	tests := []struct {
		name    string
		curr    string
		target  string
		migrate bool
	}{
		{name: "3.5.0 asking for 3.6.0", curr: "3.5.0", target: "3.6.0", migrate: true},
		{name: "v2 asking for 3.6.0", curr: "2.52.0", target: "3.6.0", migrate: true},
		{name: "already on 3.6.0, going further", curr: "3.6.0", target: "3.9.1", migrate: true},
		{name: "target predates the field", curr: "3.5.0", target: "3.5.9", migrate: false},
		{name: "target leaves v3", curr: "3.6.0", target: "4.0.0", migrate: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.go")
			require.NoError(t, os.WriteFile(main, []byte(source), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte("module test\n\nrequire github.com/gofiber/fiber/v3 v3.6.0\n"), 0o600))

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)
			require.NoError(t, migrations.DoMigration(cmd, dir,
				semver.MustParse(tc.curr), semver.MustParse(tc.target), true, false, nil, nil))

			content, err := os.ReadFile(main) //nolint:gosec // the path is this test's own temp dir
			require.NoError(t, err)
			if tc.migrate {
				assert.Contains(t, string(content), "RuleList: []redirect.Rule{")
			} else {
				assert.Contains(t, string(content), "map[string]string")
			}
		})
	}
}
