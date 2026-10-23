package migrations_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func Test_DoMigration_ReportsWhatItCouldNotMigrate(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a_good.go")
	broken := filepath.Join(dir, "b_broken.go")
	later := filepath.Join(dir, "c_later.go")
	handler := "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nfunc %s(c *fiber.Ctx) error {\n\treturn %s\n}\n"
	require.NoError(t, os.WriteFile(good, fmt.Appendf(nil, handler, "first", "nil"), 0o600))
	require.NoError(t, os.WriteFile(broken, fmt.Appendf(nil, handler, "second", "(nil"), 0o600))
	require.NoError(t, os.WriteFile(later, fmt.Appendf(nil, handler, "third", "nil"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module test\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n"), 0o600))

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	require.NoError(t, migrations.DoMigration(cmd, dir,
		semver.MustParse("2.52.15"), semver.MustParse("3.6.1"), true, false, nil, nil))

	out := buf.String()
	assert.Contains(t, out, "1 item to review by hand:")
	assert.Contains(t, out, "b_broken.go:")
	assert.Contains(t, out, "skipped because the file does not parse")
	assert.Equal(t, 1, strings.Count(out, "b_broken.go:"), "the same finding from several migrations is listed once")

	for _, path := range []string{good, later} {
		content, err := os.ReadFile(path) //nolint:gosec // the path is this test's own temp dir
		require.NoError(t, err)
		assert.Contains(t, string(content), "c fiber.Ctx", "%s is migrated although a file before it is broken", path)
	}
	content, err := os.ReadFile(broken) //nolint:gosec // the path is this test's own temp dir
	require.NoError(t, err)
	assert.Contains(t, string(content), "c *fiber.Ctx", "the broken file is left as it was")
}

func Test_DoMigration_NoNotesNoReport(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module test\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n"), 0o600))

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	require.NoError(t, migrations.DoMigration(cmd, dir,
		semver.MustParse("2.52.15"), semver.MustParse("3.6.1"), true, false, nil, nil))
	assert.NotContains(t, buf.String(), "review by hand")
}

// The files are formatted while the migrations rewrite them, and the author's text is put back where no
// migration changed it: the comment that is not formatted, the order of the imports and the line endings.
func Test_DoMigration_KeepsWhatTheAuthorWrote(t *testing.T) {
	source := `package main

import (
	"os"
	"github.com/gofiber/fiber/v2"
)

//Handler says hi.
func Handler(c *fiber.Ctx) error {
	_ = os.Args
	return c.SendString("hi")
}

//Other is not touched.
func Other(a   int) int { return a }
`
	want := strings.NewReplacer("github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v3", "c *fiber.Ctx", "c fiber.Ctx").Replace(source)

	for _, tc := range []struct {
		name    string
		newline string
	}{
		{name: "LF", newline: "\n"},
		{name: "CRLF", newline: "\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.go")
			require.NoError(t, os.WriteFile(main, []byte(strings.ReplaceAll(source, "\n", tc.newline)), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte("module test\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n"), 0o600))

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)
			require.NoError(t, migrations.DoMigration(cmd, dir,
				semver.MustParse("2.52.15"), semver.MustParse("3.6.1"), true, false, nil, nil))

			content, err := os.ReadFile(main) //nolint:gosec // the path is this test's own temp dir
			require.NoError(t, err)
			assert.Equal(t, strings.ReplaceAll(want, "\n", tc.newline), string(content))

			// a second run changes nothing
			var again bytes.Buffer
			cmd.SetOut(&again)
			require.NoError(t, migrations.DoMigration(cmd, dir,
				semver.MustParse("2.52.15"), semver.MustParse("3.6.1"), true, false, nil, nil))
			secondContent, err := os.ReadFile(main) //nolint:gosec // the path is this test's own temp dir
			require.NoError(t, err)
			assert.Equal(t, string(content), string(secondContent))
		})
	}
}

// A note points at the line the file has when the run is over, also when putting the author's text back moved it.
func Test_DoMigration_NotesFollowTheLinesOfTheRestoredFile(t *testing.T) {
	// the author wrote three blank lines before the handler, the formatter keeps one
	source := "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\n\n\nfunc Handler(c *fiber.Ctx) error {\n\treturn c.Status(404).SendString(\"Cannot GET /missing\")\n}\n"
	dir := t.TempDir()
	main := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(main, []byte(source), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module test\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n"), 0o600))

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	require.NoError(t, migrations.DoMigration(cmd, dir,
		semver.MustParse("2.52.15"), semver.MustParse("3.6.1"), true, false, nil, nil))

	content, err := os.ReadFile(main) //nolint:gosec // the path is this test's own temp dir
	require.NoError(t, err)
	assert.Contains(t, string(content), "\n\n\n\nfunc Handler(c fiber.Ctx) error {", "the blank lines of the author are back")
	lines := strings.Split(string(content), "\n")
	found := false
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "main.go:") || !strings.Contains(line, "answers an unmatched route") {
			continue
		}
		// "main.go:N: ..." must name the line of the file that has the text the note is about
		number := strings.SplitN(strings.TrimSpace(line), ":", 3)[1]
		n, convErr := strconv.Atoi(number)
		require.NoError(t, convErr, line)
		assert.Contains(t, lines[n-1], "Cannot GET /missing", "the note %q points at line %d: %q", line, n, lines[n-1])
		found = true
	}
	assert.True(t, found, "a note about the body of an unmatched route: %s", buf.String())
}

// A migration that adds an import to the only import of a file does not move the blank lines the author wrote
// after it into the new import block.
func Test_DoMigration_BlankLinesStayOutOfANewImportBlock(t *testing.T) {
	source := "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\n\n\nfunc Handler(c *fiber.Ctx) error {\n\treturn c.Download(\"report.txt\")\n}\n"
	dir := t.TempDir()
	main := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(main, []byte(source), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module test\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n"), 0o600))

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	require.NoError(t, migrations.DoMigration(cmd, dir,
		semver.MustParse("2.52.15"), semver.MustParse("3.6.1"), true, false, nil, nil))

	content, err := os.ReadFile(main) //nolint:gosec // the path is this test's own temp dir
	require.NoError(t, err)
	want := "package main\n\nimport (\n\t\"test/internal/fibermigrate\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n\n" +
		"func Handler(c fiber.Ctx) error {\n\treturn fibermigrate.Download(c, \"report.txt\")\n}\n"
	assert.Equal(t, want, string(content))
}

// When go mod tidy fails, go.mod holds the edits of the migration and is not tidied: the report says so, and
// names the cause the go command printed when the migration knows it.
func Test_DoMigration_GoModFailureIsExplained(t *testing.T) {
	dir := t.TempDir()
	fiberGoMod := filepath.Join(dir, "fiber.mod")
	require.NoError(t, os.WriteFile(fiberGoMod, []byte("module github.com/gofiber/fiber/v3\n\ngo 1.26.0\n"), 0o600))
	t.Cleanup(stubFiberDownload(t, fiberGoMod))
	t.Cleanup(stubFailingGoMod(t, "go: finding module for package go.opentelemetry.io/otel/exporters/otlp/internal/envconfig\n"+
		"module go.opentelemetry.io/otel/exporters/otlp@latest found (v0.20.1), but does not contain package go.opentelemetry.io/otel/exporters/otlp/internal/envconfig"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module test\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n"), 0o600))

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	err := migrations.DoMigration(cmd, dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1"), false, false, nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "the OpenTelemetry modules of go.mod are on different releases, and they have to be on one")
	out := buf.String()
	assert.Contains(t, out, "1 item to review by hand:", "the note is there once, although the go command failed twice")
	assert.Contains(t, out, "go mod tidy failed (see its output above), so go.mod and go.sum hold the edits of the migration")
}

// A dependency that is built for Fiber v2 is looked for in the go.mod files of the module cache when the
// migration did not tidy go.mod: with --skip_go_mod, and when go mod tidy failed.
func Test_DoMigration_DependenciesAreJudgedFromTheModuleCacheWhenGoModIsNotTidied(t *testing.T) {
	const want = "example.org/legacy v1.0.0 requires github.com/gofiber/fiber/v2, so it is probably built for Fiber v2"

	tests := []struct {
		setup func(t *testing.T)
		name  string
		skip  bool
	}{
		{name: "with --skip_go_mod", skip: true, setup: func(*testing.T) {}},
		{name: "when go mod tidy fails", setup: func(t *testing.T) {
			t.Helper()
			t.Cleanup(stubFailingGoMod(t, "go: updates to go.mod needed"))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cache := t.TempDir()
			t.Setenv("GOMODCACHE", cache)
			mod := filepath.Join(cache, "cache", "download", "example.org", "legacy", "@v", "v1.0.0.mod")
			require.NoError(t, os.MkdirAll(filepath.Dir(mod), 0o750))
			require.NoError(t, os.WriteFile(mod, []byte("module example.org/legacy\n\ngo 1.20\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n"), 0o600))

			fiberGoMod := filepath.Join(dir, "fiber.mod")
			require.NoError(t, os.WriteFile(fiberGoMod, []byte("module github.com/gofiber/fiber/v3\n\ngo 1.26.0\n"), 0o600))
			t.Cleanup(stubFiberDownload(t, fiberGoMod))
			tc.setup(t)

			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport _ \"example.org/legacy\"\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte("module test\n\ngo 1.26.0\n\nrequire (\n\tgithub.com/gofiber/fiber/v2 v2.52.15\n\texample.org/legacy v1.0.0\n)\n"), 0o600))

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)
			cmd.Flags().Bool("skip_go_mod", tc.skip, "")
			err := migrations.DoMigration(cmd, dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1"), tc.skip, false, nil, nil)
			if tc.skip {
				require.NoError(t, err)
			} else {
				require.Error(t, err, "go mod tidy failed")
			}

			assert.Contains(t, buf.String(), "main.go:3: "+want)
			assert.Contains(t, buf.String(), "go.mod was not tidied by the migration")
		})
	}
}
