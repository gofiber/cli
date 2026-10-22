package v3_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// contribSkipGoModCmd returns a command that was started with --skip_go_mod.
func contribSkipGoModCmd(t *testing.T, buf *bytes.Buffer) *cobra.Command {
	t.Helper()

	cmd := newCmd(buf)
	cmd.Flags().Bool("skip_go_mod", false, "")
	require.NoError(t, cmd.Flags().Set("skip_go_mod", "true"))
	return cmd
}

// A lookup that fails costs only its own module: it stays on the v2 path, in
// the imports and in go.mod, so the tree never holds half of a module's
// migration. The others are migrated.
func Test_MigrateContribPackages_FailedLookupLeavesThatModuleAlone(t *testing.T) {
	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		if module == "casbin" {
			return "", errors.New("go list: module lookup disabled by GOPROXY=off")
		}
		return "v1.2.6", nil
	})
	t.Cleanup(restore)
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/casbin"
	jwtware "github.com/gofiber/contrib/jwt"
)

var _ = casbin.New
var _ = jwtware.New
`)
	contribWriteFile(t, dir, "go.mod", `module example

go 1.22

require (
	github.com/gofiber/contrib/casbin v1.0.18
	github.com/gofiber/contrib/jwt v1.1.2
)
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `"github.com/gofiber/contrib/casbin"`)
	assert.Contains(t, content, `jwtware "github.com/gofiber/contrib/v3/jwt"`)
	assert.Equal(t, `module example

go 1.22

require (
	github.com/gofiber/contrib/casbin v1.0.18
	github.com/gofiber/contrib/v3/jwt v1.2.6
)
`, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Contains(t, buf.String(), "Migrating contrib packages")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "go.mod", notes[0].File)
	assert.Equal(t, 6, notes[0].Line)
	assert.Contains(t, notes[0].Message, "github.com/gofiber/contrib/v3/casbin")
	assert.Contains(t, notes[0].Message, "GOPROXY=off")
	assert.Contains(t, notes[0].Message, "left on the v2 module")
}

func Test_MigrateContribPackages_EveryLookupFails(t *testing.T) {
	restore := v3.SetContribV3VersionFetcher(func(string) (string, error) { return "", errors.New("offline") })
	t.Cleanup(restore)
	dir := t.TempDir()

	source := `package main

import jwtware "github.com/gofiber/contrib/jwt"

var _ = jwtware.New
`
	file := writeTempFile(t, dir, source)
	mod := "module example\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/jwt v1.1.2\n"
	contribWriteFile(t, dir, "go.mod", mod)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, source, readFile(t, file))
	assert.Equal(t, mod, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Empty(t, buf.String(), "nothing changed")
	require.Len(t, internal.DrainNotes(dir), 1)
}

// --skip_go_mod promises that the CLI does not run go commands: nothing is looked
// up, go.mod is left for `go mod tidy` and the imports are migrated.
func Test_MigrateContribPackages_SkipGoModDoesNotLookAnythingUp(t *testing.T) {
	var lookups atomic.Int32
	restore := v3.SetContribV3VersionFetcher(func(string) (string, error) {
		lookups.Add(1)
		return "", errors.New("no network")
	})
	t.Cleanup(restore)
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/fiberzap/v2"
	"github.com/gofiber/contrib/opafiber/v2"
)

var _, _ = fiberzap.New, opafiber.New
`)
	mod := "module example\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/fiberzap/v2 v2.1.6\nrequire github.com/gofiber/contrib/opafiber/v2 v2.0.6\n"
	contribWriteFile(t, dir, "go.mod", mod)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(contribSkipGoModCmd(t, &buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `fiberzap "github.com/gofiber/contrib/v3/zap"`)
	assert.Contains(t, content, `opafiber "github.com/gofiber/contrib/v3/opa"`)
	assert.Equal(t, mod, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Zero(t, lookups.Load())
	assert.Contains(t, buf.String(), "Migrating contrib packages")
	assert.Empty(t, internal.DrainNotes(dir))
}

// opafiber is renamed to opa. Before it was in the table, its import went to a
// module that does not exist and the failing lookup stopped the migration of
// every other contrib module.
func Test_MigrateContribPackages_OpafiberDoesNotStopTheOthers(t *testing.T) {
	contribVersions(t, map[string]string{"opa": "v1.0.13", "jwt": "v1.2.6", "websocket": "v1.2.8"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/opafiber/v2"
	jwtware "github.com/gofiber/contrib/jwt"
	"github.com/gofiber/contrib/websocket"
)

var _, _, _ = opafiber.New, jwtware.New, websocket.New
`)
	contribWriteFile(t, dir, "go.mod", `module example

go 1.22

require (
	github.com/gofiber/contrib/jwt v1.1.2
	github.com/gofiber/contrib/opafiber/v2 v2.0.6
	github.com/gofiber/contrib/websocket v1.3.4
)
`)

	require.NoError(t, v3.MigrateContribPackages(newCmd(&bytes.Buffer{}), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `opafiber "github.com/gofiber/contrib/v3/opa"`)
	assert.Contains(t, content, `jwtware "github.com/gofiber/contrib/v3/jwt"`)
	assert.Contains(t, content, "\t\"github.com/gofiber/contrib/v3/websocket\"\n")
	assert.Equal(t, `module example

go 1.22

require (
	github.com/gofiber/contrib/v3/jwt v1.2.6
	github.com/gofiber/contrib/v3/opa v1.0.13
	github.com/gofiber/contrib/v3/websocket v1.2.8
)
`, readFile(t, filepath.Join(dir, "go.mod")))
}

func Test_MigrateContribPackages_GoModShapes(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6", "paseto": "v1.1.12", "websocket": "v1.2.8"})
	dir := t.TempDir()

	contribWriteFile(t, dir, "go.mod", "module example\r\n\r\ngo 1.22\r\n\r\n"+
		"require github.com/gofiber/contrib/jwt v1.1.2 // indirect\r\n\r\n"+
		"require (\r\n"+
		"\tgithub.com/gofiber/contrib/paseto v1.2.4 // auth\r\n"+
		"\t\"github.com/gofiber/contrib/websocket\" v1.3.4\r\n"+
		")\r\n\r\n"+
		"replace (\r\n\tgithub.com/gofiber/contrib/paseto v1.2.4 => ../paseto\r\n)\r\n"+
		"exclude github.com/gofiber/contrib/jwt v1.0.0\r\n")
	nested := contribWriteFile(t, dir, "svc/go.mod", "module example/svc\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/jwt v1.1.2\n")
	contribWriteFile(t, dir, "vendor/go.mod", "module vendored\n\nrequire github.com/gofiber/contrib/jwt v1.1.2\n")

	require.NoError(t, v3.MigrateContribPackages(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, "module example\r\n\r\ngo 1.22\r\n\r\n"+
		"require github.com/gofiber/contrib/v3/jwt v1.2.6 // indirect\r\n\r\n"+
		"require (\r\n"+
		"\tgithub.com/gofiber/contrib/v3/paseto v1.1.12 // auth\r\n"+
		"\t\"github.com/gofiber/contrib/v3/websocket\" v1.2.8\r\n"+
		")\r\n\r\n"+
		"replace (\r\n\tgithub.com/gofiber/contrib/v3/paseto v1.1.12 => ../paseto\r\n)\r\n"+
		"exclude github.com/gofiber/contrib/jwt v1.0.0\r\n", readFile(t, filepath.Join(dir, "go.mod")))
	assert.Equal(t, "module example/svc\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/v3/jwt v1.2.6\n", readFile(t, nested))
	assert.Contains(t, readFile(t, filepath.Join(dir, "vendor", "go.mod")), "contrib/jwt v1.1.2", "vendor is not ours")
}

// Two old modules that end up in the same v3 module leave one requirement.
func Test_MigrateContribPackages_MergesModulesThatShareATarget(t *testing.T) {
	contribVersions(t, map[string]string{"websocket": "v1.2.8"})
	dir := t.TempDir()

	contribWriteFile(t, dir, "go.mod", `module example

go 1.22

require (
	github.com/gofiber/contrib/websocket v1.3.4
	github.com/gofiber/websocket/v2 v2.2.1
)
`)

	require.NoError(t, v3.MigrateContribPackages(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, `module example

go 1.22

require (
	github.com/gofiber/contrib/v3/websocket v1.2.8
)
`, readFile(t, filepath.Join(dir, "go.mod")))
}

func Test_MigrateContribPackages_ReportsAGoModItCannotRead(t *testing.T) {
	contribVersions(t, map[string]string{})
	dir := t.TempDir()

	mod := "module example\n\nrequire github.com/gofiber/contrib/jwt v1.1\nthis is not go.mod syntax {\n"
	contribWriteFile(t, dir, "go.mod", mod)

	require.NoError(t, v3.MigrateContribPackages(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, mod, readFile(t, filepath.Join(dir, "go.mod")))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "go.mod", notes[0].File)
	assert.Contains(t, notes[0].Message, "does not parse")
}
