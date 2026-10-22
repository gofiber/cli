package v3_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// otelCoreNeeded is the release of the core module that the v3 module in these tests requires.
const otelCoreNeeded = "v1.47.0"

// stubOtelLookups answers the lookups of the module versions like the go command does for a v3 module of
// otelfiber that requires the core module at otelCoreNeeded: every module of the family has that release,
// except the jaeger exporter, whose last release is older. It counts the lookups of family modules.
func stubOtelLookups(t *testing.T, dir string) *atomic.Int32 {
	t.Helper()

	t.Cleanup(v3.SetContribV3VersionFetcher(func(string) (string, error) { return "v1.3.1", nil }))
	goMod := contribWriteFile(t, dir, "cache/otel.mod", "module github.com/gofiber/contrib/v3/otel\n\ngo 1.26.0\n\nrequire (\n\tgo.opentelemetry.io/otel "+otelCoreNeeded+"\n\tgo.opentelemetry.io/otel/trace "+otelCoreNeeded+"\n)\n")

	var lookups atomic.Int32
	t.Cleanup(v3.SetContribModuleFetcher(func(path, query string) (v3.ContribModule, error) {
		if path == "github.com/gofiber/contrib/v3/otel" {
			return v3.ContribModule{Version: query, GoMod: goMod}, nil
		}
		lookups.Add(1)
		switch {
		case strings.Contains(path, "/jaeger"):
			if query != "latest" {
				return v3.ContribModule{}, errors.New("go: " + path + "@" + query + ": invalid version: unknown revision")
			}
			return v3.ContribModule{Version: "v1.17.0"}, nil
		case strings.Contains(path, "/unreachable"):
			return v3.ContribModule{}, errors.New("go: module lookup disabled by GOPROXY=off")
		case query == "latest":
			return v3.ContribModule{Version: "v1.49.0"}, nil
		}
		return v3.ContribModule{Version: query}, nil
	}))
	return &lookups
}

const otelProjectGoMod = `module example.com/app

go 1.22

require (
	github.com/gofiber/contrib/otelfiber v1.0.10
	github.com/gofiber/fiber/v2 v2.52.15
	go.opentelemetry.io/otel v1.16.0
	go.opentelemetry.io/otel/exporters/jaeger v1.16.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.16.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.16.0
	go.opentelemetry.io/otel/sdk v1.16.0
)

require go.opentelemetry.io/otel/trace v1.16.0 // indirect
`

const otelProjectSource = `package main

import (
	"github.com/gofiber/contrib/otelfiber"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Use(otelfiber.Middleware())
}
`

func Test_MigrateContribPackages_RaisesTheOpenTelemetryModulesTogether(t *testing.T) {
	dir := t.TempDir()
	lookups := stubOtelLookups(t, dir)
	contribWriteFile(t, dir, "main.go", otelProjectSource)
	contribWriteFile(t, dir, "go.mod", otelProjectGoMod)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, `module example.com/app

go 1.22

require (
	github.com/gofiber/contrib/v3/otel v1.3.1
	github.com/gofiber/fiber/v2 v2.52.15
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/exporters/jaeger v1.17.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.47.0
	go.opentelemetry.io/otel/sdk v1.47.0
)

require go.opentelemetry.io/otel/trace v1.47.0 // indirect
`, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Positive(t, lookups.Load())

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1, "%v", notes)
	assert.Equal(t, "go.mod", notes[0].File)
	assert.Equal(t, 8, notes[0].Line, "at the first requirement that was raised")
	assert.Contains(t, notes[0].Message, "raised go.opentelemetry.io/otel v1.16.0 to v1.47.0, go.opentelemetry.io/otel/exporters/jaeger v1.16.0 to v1.17.0, ")
	assert.Contains(t, notes[0].Message, "and 2 more")
	assert.Contains(t, notes[0].Message, "github.com/gofiber/contrib/v3/otel requires go.opentelemetry.io/otel v1.47.0, and the OpenTelemetry modules have to be on one release")

	// a second run has nothing to raise
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateContribPackages_OpenTelemetryModulesOfAnotherReleaseCountOnlyWhenTheyAreBehind(t *testing.T) {
	dir := t.TempDir()
	stubOtelLookups(t, dir)
	contribWriteFile(t, dir, "main.go", otelProjectSource)
	// the core module is ahead of what the v3 module needs: the move adds no skew
	mod := strings.Replace(otelProjectGoMod, "go.opentelemetry.io/otel v1.16.0", "go.opentelemetry.io/otel v1.48.0", 1)
	contribWriteFile(t, dir, "go.mod", mod)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	got := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, got, "go.opentelemetry.io/otel v1.48.0\n")
	assert.Contains(t, got, "go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.16.0\n", "left alone")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateContribPackages_OpenTelemetryReplacedAndUnreachableModules(t *testing.T) {
	dir := t.TempDir()
	stubOtelLookups(t, dir)
	contribWriteFile(t, dir, "main.go", otelProjectSource)
	mod := strings.Replace(otelProjectGoMod, "\tgo.opentelemetry.io/otel/sdk v1.16.0\n", "\tgo.opentelemetry.io/otel/sdk v1.16.0\n\tgo.opentelemetry.io/otel/exporters/unreachable v1.16.0\n", 1) +
		"\nreplace go.opentelemetry.io/otel/exporters/otlp/otlptrace => ../otlptrace\n"
	contribWriteFile(t, dir, "go.mod", mod)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	got := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, got, "go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.16.0\n", "a module with a replace directive is the project's business")
	assert.Contains(t, got, "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.47.0\n")
	assert.Contains(t, got, "go.opentelemetry.io/otel/exporters/unreachable v1.16.0\n", "no lookup, no change")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2, "%v", notes)
	var missing string
	for _, n := range notes {
		if strings.Contains(n.Message, "could not look up") {
			missing = n.Message
		}
	}
	assert.Contains(t, missing, "could not look up a newer release of go.opentelemetry.io/otel/exporters/unreachable (go: module lookup disabled by GOPROXY=off")
	assert.Contains(t, missing, "go get go.opentelemetry.io/otel/exporters/unreachable@latest")
}

func Test_MigrateContribPackages_OpenTelemetryNeedsTheMoveAndNetwork(t *testing.T) {
	dir := t.TempDir()
	lookups := stubOtelLookups(t, dir)

	// without otelfiber nothing moves and nothing is looked up
	contribWriteFile(t, dir, "main.go", "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ = fiber.New\n")
	mod := strings.Replace(otelProjectGoMod, "\tgithub.com/gofiber/contrib/otelfiber v1.0.10\n", "", 1)
	contribWriteFile(t, dir, "go.mod", mod)
	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))
	assert.Equal(t, mod, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Zero(t, lookups.Load())

	// with --skip_go_mod go.mod is not touched and the go command is not asked
	contribWriteFile(t, dir, "main.go", otelProjectSource)
	contribWriteFile(t, dir, "go.mod", otelProjectGoMod)
	require.NoError(t, v3.MigrateContribPackages(contribSkipGoModCmd(t, &buf), dir, nil, nil))
	assert.Equal(t, otelProjectGoMod, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Zero(t, lookups.Load())
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateContribPackages_OpenTelemetryWithoutTheRequirementsOfTheV3Module(t *testing.T) {
	dir := t.TempDir()
	// the go.mod of the v3 module cannot be read: every module of the family goes to its latest release
	t.Cleanup(v3.SetContribV3VersionFetcher(func(string) (string, error) { return "v1.3.1", nil }))
	t.Cleanup(v3.SetContribModuleFetcher(func(path, query string) (v3.ContribModule, error) {
		if path == "github.com/gofiber/contrib/v3/otel" {
			return v3.ContribModule{Version: query}, nil
		}
		return v3.ContribModule{Version: "v1.49.0"}, nil
	}))
	contribWriteFile(t, dir, "main.go", otelProjectSource)
	contribWriteFile(t, dir, "go.mod", otelProjectGoMod)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	got := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, got, "go.opentelemetry.io/otel v1.49.0\n")
	assert.Contains(t, got, "go.opentelemetry.io/otel/sdk v1.49.0\n")
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.NotContains(t, notes[0].Message, "requires go.opentelemetry.io/otel", "the release the v3 module needs is not known")
}
