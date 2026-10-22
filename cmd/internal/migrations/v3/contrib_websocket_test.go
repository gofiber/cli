package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// websocketSource wraps the body of a program that imports the websocket
// middleware.
func websocketSource(body string) string {
	return `package main

import (
	"strings"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

var _ = strings.HasPrefix

func handler(c *websocket.Conn) {}

func isInternal(c *fiber.Ctx) bool { return strings.HasPrefix(c.Path(), "/ws/internal") }

type guard struct{}

func (guard) allow(c *fiber.Ctx) bool { return true }

` + body + `
`
}

// v2 handled the request when Filter returned true, v3 skips the middleware when
// Next returns true: every shape of Filter has to come out inverted.
func Test_MigrateWebsocketConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "literal that returns a call",
			config: "Filter: func(c *fiber.Ctx) bool {\n\t\treturn strings.HasPrefix(c.Path(), \"/ws/internal\")\n\t},",
			want:   "Next: func(c fiber.Ctx) bool {\n\t\treturn !strings.HasPrefix(c.Path(), \"/ws/internal\")\n\t},",
		},
		{
			name:   "literal that returns a negation",
			config: "Filter: func(c *fiber.Ctx) bool { return !strings.HasPrefix(c.Path(), \"/api\") },",
			want:   "Next: func(c fiber.Ctx) bool { return strings.HasPrefix(c.Path(), \"/api\") },",
		},
		{
			name:   "literal that returns a comparison",
			config: `Filter: func(c *fiber.Ctx) bool { return c.Get("X-Skip") == "1" },`,
			want:   `Next: func(c fiber.Ctx) bool { return c.Get("X-Skip") != "1" },`,
		},
		{
			name:   "literal that returns a conjunction",
			config: `Filter: func(c *fiber.Ctx) bool { return c.Secure() && c.Path() != "/" },`,
			want:   `Next: func(c fiber.Ctx) bool { return !(c.Secure() && c.Path() != "/") },`,
		},
		{
			name:   "literal that returns a constant",
			config: `Filter: func(*fiber.Ctx) bool { return true },`,
			want:   `Next: func(fiber.Ctx) bool { return !true },`,
		},
		{
			name:   "literal with several statements",
			config: "Filter: func(ctx *fiber.Ctx) bool {\n\t\tif ctx.Method() != \"GET\" {\n\t\t\treturn false\n\t\t}\n\t\treturn true\n\t},",
			want:   "Next: func(c fiber.Ctx) bool { return !(func(ctx fiber.Ctx) bool {\n\t\tif ctx.Method() != \"GET\" {\n\t\t\treturn false\n\t\t}\n\t\treturn true\n\t})(c) },",
		},
		{
			name:   "named function",
			config: `Filter: isInternal,`,
			want:   `Next: func(c fiber.Ctx) bool { return !isInternal(c) },`,
		},
		{
			name:   "method value",
			config: `Filter: guard{}.allow,`,
			want:   `Next: func(c fiber.Ctx) bool { return !guard{}.allow(c) },`,
		},
		{
			name:   "nil",
			config: `Filter: nil,`,
			want:   `Next: nil,`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, websocketSource("var _ = websocket.New(handler, websocket.Config{\n\tOrigins: []string{\"*\"},\n\t"+tt.config+"\n})"))

			out := contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateWebsocketConfig)

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.NotContains(t, content, "Filter:")
			assert.Contains(t, contribCompact(content), contribCompact(tt.want))
			assert.Contains(t, content, `Origins:`)
			assert.Contains(t, out, "Migrating websocket configs")
			assert.Empty(t, internal.DrainNotes(dir))
		})
	}
}

func Test_MigrateWebsocketConfig_ConfigInAVariable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, websocketSource(`var cfg = websocket.Config{
	Origins: []string{"*"},
	Filter: func(c *fiber.Ctx) bool {
		return c.Get("X-Skip") == "1"
	},
}

func config() websocket.Config {
	cfg := websocket.Config{}
	cfg.Filter = isInternal
	return cfg
}`))

	contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateWebsocketConfig)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, `return c.Get("X-Skip") != "1"`)
	assert.Contains(t, contribCompact(content), "cfg.Next = func(c fiber.Ctx) bool { return !isInternal(c) }")
	assert.NotContains(t, content, "Filter")
}

func Test_MigrateWebsocketConfig_NeedsTheFiberImportOfItsOwn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package server

import "github.com/gofiber/contrib/websocket"

var Config = websocket.Config{Filter: Upgradable}
`)

	contribMigrate(t, dir, v3.MigrateWebsocketConfig)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3"`)
	assert.Contains(t, content, "Next: func(c fiber.Ctx) bool { return !Upgradable(c) }")
}

func Test_MigrateWebsocketConfig_ChoosesAParameterNameThatIsFree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package server

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

func filter(c string) func(*fiber.Ctx) bool { return nil }

var c = "ws"

var Config = websocket.Config{Filter: filter(c)}
`)

	contribMigrate(t, dir, v3.MigrateWebsocketConfig)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "Next: func(ctx fiber.Ctx) bool { return !(filter(c))(ctx) }")
}

func Test_MigrateWebsocketConfig_AliasesAndTheStandaloneModule(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"github.com/gofiber/contrib/websocket", "github.com/gofiber/contrib/v3/websocket", "github.com/gofiber/websocket/v2"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/fiber/v2"
	ws "`+path+`"
)

var _ = ws.Config{Filter: ok}

func ok(c *fiber.Ctx) bool { return true }
`)

			contribMigrate(t, dir, v3.MigrateWebsocketConfig)

			content := readFile(t, file)
			assert.NotContains(t, content, "Filter")
			assert.Contains(t, content, "Next: func(c fiber.Ctx) bool { return !ok(c) }")
		})
	}
}

func Test_MigrateWebsocketConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, websocketSource(`var _ = websocket.New(handler, websocket.Config{Filter: isInternal})`))

	contribMigrate(t, dir, v3.MigrateWebsocketConfig)
	migrated := readFile(t, file)

	out := contribMigrate(t, dir, v3.MigrateWebsocketConfig)

	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateWebsocketConfig_LeavesV3ConfigsAndOtherPackagesAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import (
	"example.com/websocket"
	ws "github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

var _ = ws.Config{Next: func(c fiber.Ctx) bool { return c.Path() == "/" }}

var _ = websocket.Config{Filter: nil}
`
	file := writeTempFile(t, dir, source)

	out := contribMigrate(t, dir, v3.MigrateWebsocketConfig)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
}

func Test_MigrateWebsocketConfig_SkipsBrokenFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import "github.com/gofiber/contrib/websocket"

var _ = websocket.Config{Filter: ok
`
	file := writeTempFile(t, dir, source)

	contribMigrate(t, dir, v3.MigrateWebsocketConfig)

	assert.Equal(t, source, readFile(t, file))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "does not parse")
}
