package v3_test

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// contribVersions stubs the version lookup: the fetcher answers from versions and
// fails for every other module.
func contribVersions(t *testing.T, versions map[string]string) {
	t.Helper()

	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		if v, ok := versions[module]; ok {
			return v, nil
		}
		return "", fmt.Errorf("unexpected module %s", module)
	})
	t.Cleanup(restore)
}

// contribWriteFile writes name below dir, creating the directories on the way.
func contribWriteFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// contribRequireParses fails the test when src is not valid Go.
func contribRequireParses(t *testing.T, src string) {
	t.Helper()

	_, err := parser.ParseFile(token.NewFileSet(), "main.go", src, parser.ParseComments)
	require.NoError(t, err, src)
}

func Test_MigrateContribPackages(t *testing.T) {
	contribVersions(t, map[string]string{"session": "v3.1.0"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/session"
)

func main() {
	_ = session.NewStore
}
`)

	contribWriteFile(t, dir, "go.mod", `module example

go 1.22

require (
    github.com/gofiber/fiber/v2 v2.0.0
    github.com/gofiber/contrib/session v1.2.3
)
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `"github.com/gofiber/contrib/v3/session"`)
	assert.NotContains(t, content, `"github.com/gofiber/contrib/session"`)

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "    github.com/gofiber/contrib/v3/session v3.1.0\n")
	assert.NotContains(t, mod, "contrib/session v1.2.3")
	assert.Contains(t, mod, "    github.com/gofiber/fiber/v2 v2.0.0\n", "other requirements stay as they are")

	assert.Contains(t, buf.String(), "Migrating contrib packages")
	assert.Empty(t, internal.DrainNotes(dir))
}

// Renamed modules change their package name as well. The README of every one
// of them imports the module without a name, so the migration has to keep the
// identifier the code uses.
func Test_MigrateContribPackages_RenamedModuleKeepsItsIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		oldPath  string
		newPath  string
		ident    string
		newMod   string
		oldMod   string
		oldVer   string
		newVer   string
		usedWith string
		usedAs   string
	}{
		{
			name: "zap", oldPath: "github.com/gofiber/contrib/fiberzap/v2", newPath: "github.com/gofiber/contrib/v3/zap",
			ident: "fiberzap", newMod: "zap", oldMod: "github.com/gofiber/contrib/fiberzap/v2", oldVer: "v2.1.6", newVer: "v1.0.14",
			usedWith: "go.uber.org/zap", usedAs: "zap",
		},
		{
			name: "zerolog", oldPath: "github.com/gofiber/contrib/fiberzerolog", newPath: "github.com/gofiber/contrib/v3/zerolog",
			ident: "fiberzerolog", newMod: "zerolog", oldMod: "github.com/gofiber/contrib/fiberzerolog", oldVer: "v1.0.3", newVer: "v1.1.7",
			usedWith: "github.com/rs/zerolog", usedAs: "zerolog",
		},
		{
			name: "otel", oldPath: "github.com/gofiber/contrib/otelfiber/v2", newPath: "github.com/gofiber/contrib/v3/otel",
			ident: "otelfiber", newMod: "otel", oldMod: "github.com/gofiber/contrib/otelfiber/v2", oldVer: "v2.2.3", newVer: "v1.3.1",
			usedWith: "go.opentelemetry.io/otel", usedAs: "otel",
		},
		{
			name: "sentry", oldPath: "github.com/gofiber/contrib/fibersentry", newPath: "github.com/gofiber/contrib/v3/sentry",
			ident: "fibersentry", newMod: "sentry", oldMod: "github.com/gofiber/contrib/fibersentry", oldVer: "v1.0.8", newVer: "v1.1.13",
			usedWith: "github.com/getsentry/sentry-go", usedAs: "sentry",
		},
		{
			name: "newrelic", oldPath: "github.com/gofiber/contrib/fibernewrelic", newPath: "github.com/gofiber/contrib/v3/newrelic",
			ident: "fibernewrelic", newMod: "newrelic", oldMod: "github.com/gofiber/contrib/fibernewrelic", oldVer: "v1.3.1", newVer: "v1.1.13",
			usedWith: "github.com/newrelic/go-agent/v3/newrelic", usedAs: "newrelic",
		},
		{
			name: "i18n", oldPath: "github.com/gofiber/contrib/fiberi18n/v2", newPath: "github.com/gofiber/contrib/v3/i18n",
			ident: "fiberi18n", newMod: "i18n", oldMod: "github.com/gofiber/contrib/fiberi18n/v2", oldVer: "v2.0.6", newVer: "v1.0.13",
			usedWith: "github.com/nicksnyder/go-i18n/v2/i18n", usedAs: "i18n",
		},
		{
			name: "opa", oldPath: "github.com/gofiber/contrib/opafiber/v2", newPath: "github.com/gofiber/contrib/v3/opa",
			ident: "opafiber", newMod: "opa", oldMod: "github.com/gofiber/contrib/opafiber/v2", oldVer: "v2.0.6", newVer: "v1.0.13",
			usedWith: "github.com/open-policy-agent/opa/rego", usedAs: "rego",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contribVersions(t, map[string]string{tt.newMod: tt.newVer})
			dir := t.TempDir()

			// A program that imports the other package of the same name, which is
			// the reason the new package must not take the identifier over.
			file := writeTempFile(t, dir, fmt.Sprintf(`package main

import (
	%q
	%q
)

var _ = %s.New
var _ = %s.New
`, tt.oldPath, tt.usedWith, tt.ident, tt.usedAs))
			contribWriteFile(t, dir, "go.mod", fmt.Sprintf("module example\n\ngo 1.22\n\nrequire %s %s\n", tt.oldMod, tt.oldVer))

			var buf bytes.Buffer
			require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.Contains(t, content, fmt.Sprintf("\t%s %q\n", tt.ident, tt.newPath))
			assert.Contains(t, content, fmt.Sprintf("\t%q\n", tt.usedWith))
			assert.Contains(t, content, "var _ = "+tt.ident+".New")
			assert.NotContains(t, content, tt.oldPath)

			assert.Equal(t, fmt.Sprintf("module example\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/v3/%s %s\n", tt.newMod, tt.newVer),
				readFile(t, filepath.Join(dir, "go.mod")))
			assert.Contains(t, buf.String(), "Migrating contrib packages")
		})
	}
}

// jwt and paseto keep their package name (jwtware, pasetoware), which differs
// from the last element of the path. goimports takes that element for the name
// when it cannot load the package and would drop an import without a name.
func Test_MigrateContribPackages_NamesThePackageOfJWTAndPaseto(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6", "paseto": "v1.1.12"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/jwt"
	"github.com/gofiber/contrib/paseto"
)

var _ = jwtware.New
var _ = pasetoware.New
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "\tjwtware \"github.com/gofiber/contrib/v3/jwt\"\n")
	assert.Contains(t, content, "\tpasetoware \"github.com/gofiber/contrib/v3/paseto\"\n")
}

func Test_MigrateContribPackages_KeepsNamesBlankAndDotImports(t *testing.T) {
	contribVersions(t, map[string]string{"zap": "v1.0.14", "otel": "v1.3.1", "sentry": "v1.1.13", "websocket": "v1.2.8"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import (
	. "github.com/gofiber/contrib/fiberzap/v2"
	_ "github.com/gofiber/contrib/otelfiber/v2"
	sentryfiber "github.com/gofiber/contrib/fibersentry"
	"github.com/gofiber/contrib/websocket"
)

var _ = New
var _ = sentryfiber.New
var _ = websocket.New
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "\t. \"github.com/gofiber/contrib/v3/zap\"\n")
	assert.Contains(t, content, "\t_ \"github.com/gofiber/contrib/v3/otel\"\n")
	assert.Contains(t, content, "\tsentryfiber \"github.com/gofiber/contrib/v3/sentry\"\n")
	assert.Contains(t, content, "\t\"github.com/gofiber/contrib/v3/websocket\"\n", "the package name did not change: no name needed")
}

// Only import declarations are rewritten: a path in a string, a comment or a
// struct tag is the project's own business.
func Test_MigrateContribPackages_OnlyTouchesImports(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import jwtware "github.com/gofiber/contrib/jwt"

// docs lives at "github.com/gofiber/contrib/jwt".
const docs = "github.com/gofiber/contrib/jwt"

type module struct {
	Path string `+"`json:\"github.com/gofiber/contrib/jwt\"`"+`
}

var _ = jwtware.New
`)

	require.NoError(t, v3.MigrateContribPackages(newCmd(&bytes.Buffer{}), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `import jwtware "github.com/gofiber/contrib/v3/jwt"`)
	assert.Contains(t, content, "// docs lives at \"github.com/gofiber/contrib/jwt\".")
	assert.Contains(t, content, `const docs = "github.com/gofiber/contrib/jwt"`)
	assert.Contains(t, content, "`json:\"github.com/gofiber/contrib/jwt\"`")
}

func Test_MigrateContribPackages_LeavesOtherProjectsAlone(t *testing.T) {
	contribVersions(t, map[string]string{})
	dir := t.TempDir()

	original := `package main

import (
	"example.com/gofiber/contrib/jwt"
	"github.com/gofiber/contrib/v3/session"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/storage/redis/v3"
)

var _, _, _, _ = jwt.New, session.NewStore, fiber.New, redis.New
`
	file := writeTempFile(t, dir, original)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, original, readFile(t, file))
	assert.Empty(t, buf.String())
}

// contrib/socketio was a plain WebSocket event bus, v3/socketio speaks
// Socket.IO. The import goes to the shim that keeps the old protocol.
func Test_MigrateContribPackages_SocketioKeepsThePlainWebSocketProtocol(t *testing.T) {
	contribVersions(t, map[string]string{"socketio": "v1.3.7"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/contrib/socketio"

func main() {
	socketio.On(socketio.EventMessage, func(ep *socketio.EventPayload) {})
}
`)
	contribWriteFile(t, dir, "go.mod", "module example\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/socketio v1.1.6\n")

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, `import socketio "github.com/gofiber/contrib/v3/socketio/legacy"`)
	assert.Contains(t, content, "socketio.On(socketio.EventMessage")
	assert.Equal(t, "module example\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/v3/socketio v1.3.7\n",
		readFile(t, filepath.Join(dir, "go.mod")), "the legacy package lives in the socketio module")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "main.go", notes[0].File)
	assert.Equal(t, 3, notes[0].Line)
	assert.Contains(t, notes[0].Message, "plain WebSocket")
	assert.Contains(t, notes[0].Message, "contrib/v3/socketio/legacy")
}

// The standalone modules that were folded into gofiber/contrib: the project
// otherwise ends up with fiber v2 and v3 in its module graph.
func Test_MigrateContribPackages_LegacyStandaloneModules(t *testing.T) {
	contribVersions(t, map[string]string{"websocket": "v1.2.8", "jwt": "v1.2.6"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/websocket/v2"
	jwtware "github.com/gofiber/jwt/v3"
)

var _ = websocket.New
var _ = jwtware.New
`)
	other := contribWriteFile(t, dir, "other.go", `package main

import "github.com/gofiber/jwt/v3"

var _ = jwtware.New
`)
	contribWriteFile(t, dir, "go.mod", `module example

go 1.22

require (
	github.com/gofiber/jwt/v3 v3.3.10
	github.com/gofiber/websocket/v2 v2.2.1 // indirect
)
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "\t\"github.com/gofiber/contrib/v3/websocket\"\n")
	assert.Contains(t, content, "\tjwtware \"github.com/gofiber/contrib/v3/jwt\"\n")
	assert.Contains(t, readFile(t, other), "import jwtware \"github.com/gofiber/contrib/v3/jwt\"")

	assert.Equal(t, `module example

go 1.22

require (
	github.com/gofiber/contrib/v3/jwt v1.2.6
	github.com/gofiber/contrib/v3/websocket v1.2.8 // indirect
)
`, readFile(t, filepath.Join(dir, "go.mod")))
}

func Test_MigrateContribPackages_Replace(t *testing.T) {
	contribVersions(t, map[string]string{"websocket": "v1.2.8"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/contrib/websocket"

var _ = websocket.New
`)
	contribWriteFile(t, dir, "go.mod", `module example

go 1.22

require github.com/gofiber/contrib/websocket v1.0.0

replace github.com/gofiber/contrib/websocket => ../local
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	assert.Contains(t, readFile(t, file), "github.com/gofiber/contrib/v3/websocket")
	assert.Equal(t, `module example

go 1.22

require github.com/gofiber/contrib/v3/websocket v1.2.8

replace github.com/gofiber/contrib/v3/websocket => ../local
`, readFile(t, filepath.Join(dir, "go.mod")))

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "go.mod", notes[0].File)
	assert.Equal(t, 7, notes[0].Line)
	assert.Contains(t, notes[0].Message, "replace directive")
}

func Test_MigrateContribPackages_RenameSubpackage(t *testing.T) {
	contribVersions(t, map[string]string{"otel": "v1.3.1"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import middleware "github.com/gofiber/contrib/otelfiber/v2/middleware"

var _ = middleware.Middleware
`)
	contribWriteFile(t, dir, "go.mod", "module example\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/otelfiber/v2 v2.0.0\n")

	require.NoError(t, v3.MigrateContribPackages(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Contains(t, readFile(t, file), `middleware "github.com/gofiber/contrib/v3/otel/middleware"`)
	assert.Contains(t, readFile(t, filepath.Join(dir, "go.mod")), "github.com/gofiber/contrib/v3/otel v1.3.1")
}

func Test_MigrateContribPackages_Idempotent(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6", "zap": "v1.0.14", "socketio": "v1.3.7"})
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/fiberzap/v2"
	"github.com/gofiber/contrib/jwt"
	"github.com/gofiber/contrib/socketio"
)

var _, _, _ = fiberzap.New, jwtware.New, socketio.New
`)
	contribWriteFile(t, dir, "go.mod", `module example

go 1.22

require (
	github.com/gofiber/contrib/fiberzap/v2 v2.1.6
	github.com/gofiber/contrib/jwt v1.1.2
	github.com/gofiber/contrib/socketio v1.1.6
)
`)

	var first bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&first), dir, nil, nil))
	assert.Contains(t, first.String(), "Migrating contrib packages")
	migrated, migratedMod := readFile(t, file), readFile(t, filepath.Join(dir, "go.mod"))
	internal.DrainNotes(dir)

	// The second run must not even ask for a version.
	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		return "", fmt.Errorf("unexpected lookup of %s", module)
	})
	t.Cleanup(restore)

	var second bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&second), dir, nil, nil))

	assert.Equal(t, migrated, readFile(t, file))
	assert.Equal(t, migratedMod, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Empty(t, second.String())
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateContribPackages_SkipsBrokenFiles(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()

	broken := contribWriteFile(t, dir, "broken.go", `package main

import jwtware "github.com/gofiber/contrib/jwt"

func broken( {
`)
	good := contribWriteFile(t, dir, "good.go", `package main

import jwtware "github.com/gofiber/contrib/jwt"

var _ = jwtware.New
`)

	require.NoError(t, v3.MigrateContribPackages(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Contains(t, readFile(t, broken), `"github.com/gofiber/contrib/jwt"`)
	assert.Contains(t, readFile(t, good), `"github.com/gofiber/contrib/v3/jwt"`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "broken.go", notes[0].File)
	assert.Contains(t, notes[0].Message, "does not parse")
}
