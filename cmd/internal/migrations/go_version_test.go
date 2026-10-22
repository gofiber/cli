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

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	return string(b)
}

func newCmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd
}

// importsWriteTree writes files (path relative to the returned directory -> content).
func importsWriteTree(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return dir
}

func Test_MigrateGoVersion(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod":              "module example\n\ngo 1.21\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		"vendor/go.mod":       "module vendor\n\ngo 1.10\n",
		"tools/go.mod":        "module example/tools\n\ngo 1.22\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		"unrelated/go.mod":    "module example/unrelated\n\ngo 1.18\n\nrequire github.com/google/uuid v1.6.0\n",
		"newer/go.mod":        "module example/newer\n\ngo 1.27.1\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		"contribs/go.mod":     "module example/contribs\n\ngo 1.18\n\nrequire github.com/gofiber/fiber-contrib v1.0.0\n",
		"toolchain/go.mod":    "module example/toolchain\n\ngo 1.22.0\n\ntoolchain go1.22.5\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		"commented/go.mod":    "module example/commented\n\ngo 1.21 // keep in sync with the Dockerfile\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		"nogo/go.mod":         "module example/nogo\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		"crlf/go.mod":         "module example/crlf\r\n\r\ngo 1.21\r\n\r\nrequire github.com/gofiber/fiber/v3 v3.6.1\r\n",
		"testdata/bad/go.mod": "module {{ .Name }}\n",
	})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1")))

	assert.Equal(t, "Ensuring go version >= 1.26.0\n", buf.String())
	assert.Equal(t, "module example\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", readFile(t, filepath.Join(dir, "go.mod")))
	assert.Equal(t, "module example/tools\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", importsRead(t, dir, "tools/go.mod"))
	assert.Equal(t, "module vendor\n\ngo 1.10\n", importsRead(t, dir, "vendor/go.mod"), "vendor is skipped")
	assert.Equal(t, "module example/unrelated\n\ngo 1.18\n\nrequire github.com/google/uuid v1.6.0\n", importsRead(t, dir, "unrelated/go.mod"), "modules without fiber are not ours")
	assert.Equal(t, "module example/contribs\n\ngo 1.18\n\nrequire github.com/gofiber/fiber-contrib v1.0.0\n", importsRead(t, dir, "contribs/go.mod"), "a module with fiber in its name is not fiber")
	assert.Equal(t, "module example/newer\n\ngo 1.27.1\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", importsRead(t, dir, "newer/go.mod"), "a newer go line is never lowered")
	assert.Equal(t, "module example/toolchain\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", importsRead(t, dir, "toolchain/go.mod"), "a toolchain line older than the go line goes")
	assert.Equal(t, "module example/commented\n\ngo 1.26.0 // keep in sync with the Dockerfile\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", importsRead(t, dir, "commented/go.mod"))
	assert.Equal(t, "module example/nogo\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", importsRead(t, dir, "nogo/go.mod"))
	assert.Equal(t, "module example/crlf\r\n\r\ngo 1.26.0\r\n\r\nrequire github.com/gofiber/fiber/v3 v3.6.1\r\n", importsRead(t, dir, "crlf/go.mod"))

	// Every directive that moved by several releases says what the new one switches on, the bad go.mod says it was skipped.
	files := map[string]int{}
	for _, n := range internal.DrainNotes(dir) {
		files[n.File]++
	}
	assert.Equal(t, map[string]int{
		"go.mod":              1,
		"tools/go.mod":        1,
		"toolchain/go.mod":    1,
		"commented/go.mod":    1,
		"nogo/go.mod":         1,
		"crlf/go.mod":         1,
		"testdata/bad/go.mod": 1,
	}, files)
}

func Test_MigrateGoVersion_SaysWhatAJumpSwitchesOn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from string
		// contains and absent are parts of the note, absent ones must not be in it
		contains []string
		absent   []string
		note     bool
	}{
		{
			name:     "go 1.13 crosses everything",
			from:     "1.13",
			note:     true,
			contains: []string{"moved from 1.13 to 1.26.0", "since go 1.22 each iteration", "since go 1.23 timer", "since go 1.24 go vet", "since go 1.25 GOMAXPROCS", "GODEBUG"},
		},
		{
			name:     "go 1.21.5 with a patch version",
			from:     "1.21.5",
			note:     true,
			contains: []string{"moved from 1.21.5 to 1.26.0", "since go 1.22 each iteration", "since go 1.24 go vet"},
		},
		{
			name:     "go 1.23 only has the later ones left",
			from:     "1.23",
			note:     true,
			contains: []string{"since go 1.24 go vet", "since go 1.25 GOMAXPROCS"},
			absent:   []string{"each iteration", "timer"},
		},
		{name: "go 1.24 moves by two releases", from: "1.24"},
		{name: "go 1.25.0 moves by one", from: "1.25.0"},
		{name: "go 1.26.0 stays", from: "1.26.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := importsWriteTree(t, map[string]string{
				"go.mod": "module example\n\n// comment\ngo " + tc.from + "\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
			})
			var buf bytes.Buffer
			require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1")))

			notes := internal.DrainNotes(dir)
			if !tc.note {
				assert.Empty(t, notes)
				return
			}
			require.Len(t, notes, 1)
			assert.Equal(t, "go.mod", notes[0].File)
			assert.Equal(t, 4, notes[0].Line, "the note is on the go line")
			for _, part := range tc.contains {
				assert.Contains(t, notes[0].Message, part)
			}
			for _, part := range tc.absent {
				assert.NotContains(t, notes[0].Message, part)
			}

			// The second run has nothing left to raise and says nothing.
			require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1")))
			assert.Empty(t, internal.DrainNotes(dir))
		})
	}
}

func Test_MigrateGoVersion_NoGoLineIsGo116(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod": "module example\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
	})
	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1")))

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "moved from 1.16 to 1.26.0")
	assert.Zero(t, notes[0].Line, "there was no go line to point at")
}

func Test_MigrateGoVersion_PrintsOnlyWhenItChangedSomething(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod": "module example\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
	})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1")))
	assert.Empty(t, buf.String())
	assert.Equal(t, "module example\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", readFile(t, filepath.Join(dir, "go.mod")))
}

func Test_MigrateGoVersion_Idempotent(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod": "module example\n\ngo 1.21\n\nrequire github.com/gofiber/fiber/v3 v3.5.0\n",
	})

	var first bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&first), dir, semver.MustParse("2.52.15"), semver.MustParse("3.5.0")))
	assert.Equal(t, "Ensuring go version >= 1.25.0\n", first.String())

	var second bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&second), dir, semver.MustParse("2.52.15"), semver.MustParse("3.5.0")))
	assert.Empty(t, second.String())
}

func Test_MigrateGoVersion_FollowsTheTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target string
		want   string
	}{
		{target: "3.0.0-beta.2", want: "1.21"},
		{target: "3.0.0-beta.4", want: "1.23"},
		{target: "3.0.0-beta.5", want: "1.24.0"},
		{target: "3.0.0-rc.1", want: "1.25.0"},
		{target: "3.0.0", want: "1.25.0"},
		{target: "3.5.0", want: "1.25.0"},
		{target: "3.6.0", want: "1.26.0"},
		{target: "3.6.1", want: "1.26.0"},
		{target: "3.12.4", want: "1.26.0"},
		{target: "3.6.2-0.20261020120000-0123456789ab", want: "1.26.0"},
		{target: "3.0.1-0.20250101000000-0123456789ab", want: "1.25.0"},
	}

	for _, tc := range tests {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()

			dir := importsWriteTree(t, map[string]string{
				"go.mod": "module example\n\ngo 1.19\n\nrequire github.com/gofiber/fiber/v3 v3.0.0\n",
			})
			var buf bytes.Buffer
			require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.52.15"), semver.MustParse(tc.target)))

			assert.Equal(t, "module example\n\ngo "+tc.want+"\n\nrequire github.com/gofiber/fiber/v3 v3.0.0\n", readFile(t, filepath.Join(dir, "go.mod")))
		})
	}
}

func Test_MigrateGoVersion_NotAFiberV3Target(t *testing.T) {
	t.Parallel()

	src := "module example\n\ngo 1.19\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n"
	dir := importsWriteTree(t, map[string]string{"go.mod": src})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.40.0"), semver.MustParse("2.52.15")))
	assert.Empty(t, buf.String())
	assert.Equal(t, src, readFile(t, filepath.Join(dir, "go.mod")))
}

func Test_MigrateGoVersion_WorkspaceFollowsItsModules(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.work":       "go 1.24.0\n\nuse (\n\t./api\n\t./lib\n)\n",
		"api/go.mod":    "module example/api\n\ngo 1.22\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		"lib/go.mod":    "module example/lib\n\ngo 1.25.0\n",
		"other/go.work": "go 1.19\n\nuse ./\n",
		"other/go.mod":  "module example/other\n\ngo 1.19\n",
	})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1")))

	assert.Equal(t, "Ensuring go version >= 1.26.0\nEnsuring the go version of go.work is not behind its modules\n", buf.String())
	assert.Equal(t, "module example/api\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", importsRead(t, dir, "api/go.mod"))
	assert.Equal(t, "module example/lib\n\ngo 1.25.0\n", importsRead(t, dir, "lib/go.mod"), "lib does not use Fiber")
	assert.Equal(t, "go 1.26.0\n\nuse (\n\t./api\n\t./lib\n)\n", importsRead(t, dir, "go.work"))
	assert.Equal(t, "go 1.19\n\nuse ./\n", importsRead(t, dir, "other/go.work"), "a workspace that is not behind is left alone")
}
