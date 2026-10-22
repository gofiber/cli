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

// writeTree writes files (path relative to the returned directory -> content).
func writeTree(t *testing.T, files map[string]string) string {
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

	dir := writeTree(t, map[string]string{
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

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "testdata/bad/go.mod", notes[0].File)
}

func Test_MigrateGoVersion_PrintsOnlyWhenItChangedSomething(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{
		"go.mod": "module example\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
	})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.52.15"), semver.MustParse("3.6.1")))
	assert.Empty(t, buf.String())
	assert.Equal(t, "module example\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n", readFile(t, filepath.Join(dir, "go.mod")))
}

func Test_MigrateGoVersion_Idempotent(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{
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

			dir := writeTree(t, map[string]string{
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
	dir := writeTree(t, map[string]string{"go.mod": src})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoVersion(newCmd(&buf), dir, semver.MustParse("2.40.0"), semver.MustParse("2.52.15")))
	assert.Empty(t, buf.String())
	assert.Equal(t, src, readFile(t, filepath.Join(dir, "go.mod")))
}

func Test_MigrateGoVersion_WorkspaceFollowsItsModules(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{
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
