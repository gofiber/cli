package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// zerologSource wraps the body of a program that imports the zerolog middleware
// the way its README does.
func zerologSource(body string) string {
	return `package main

import (
	"os"

	fiberzerolog "github.com/gofiber/contrib/fiberzerolog"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

var logger = zerolog.New(os.Stderr)

func skipped(c *fiber.Ctx) bool { return false }

var paths = []string{"/health"}

` + body + `
`
}

func Test_MigrateZerologConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, zerologSource(`var _ = fiberzerolog.New(fiberzerolog.Config{
	Logger:   &logger,
	Fields:   []string{fiberzerolog.FieldBody, fiberzerolog.FieldResBody},
	SkipURIs: []string{"/health", "/metrics"},
	SkipBody: func(c *fiber.Ctx) bool {
		return c.Path() == "/login"
	},
	SkipResBody: func(c *fiber.Ctx) bool {
		return c.Get("X-No-Body") != ""
	},
	Next: func(c *fiber.Ctx) bool {
		return c.Method() == fiber.MethodOptions
	},
	WrapHeaders: true,
})`))

	out := contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateZerologConfig)

	content := readFile(t, file)
	contribRequireParses(t, content)
	for _, gone := range []string{"SkipURIs", "SkipBody:", "SkipResBody:"} {
		assert.NotContains(t, content, gone)
	}
	compact := contribCompact(content)
	// Next runs first, then the paths, as in v2.
	assert.Contains(t, compact, `return c.Method() == fiber.MethodOptions || slices.Contains([]string{"/health", "/metrics"}, c.Path())`)
	assert.Contains(t, compact, `SkipField: func(field string, c fiber.Ctx) bool { switch field { `+
		`case fiberzerolog.FieldBody: return c.Path() == "/login" `+
		`case fiberzerolog.FieldResBody: return c.Get("X-No-Body") != "" } return false },`)
	assert.Contains(t, content, "\t\"slices\"\n")
	assert.Contains(t, compact, "WrapHeaders: true,")
	assert.Contains(t, compact, "Logger: &logger,")
	assert.Contains(t, out, "Migrating zerolog configs")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateZerologConfig_SkipURIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "alone",
			config: `SkipURIs: []string{"/health"},`,
			want:   `Next: func(c fiber.Ctx) bool { return slices.Contains([]string{"/health"}, c.Path()) },`,
		},
		{
			name:   "paths in a variable",
			config: `SkipURIs: paths,`,
			want:   `Next: func(c fiber.Ctx) bool { return slices.Contains(paths, c.Path()) },`,
		},
		{
			name:   "next is a function of the program",
			config: "SkipURIs: paths,\n\tNext: skipped,",
			want:   `Next: func(c fiber.Ctx) bool { return skipped(c) || slices.Contains(paths, c.Path()) },`,
		},
		{
			name:   "next is a literal with a parameter of its own",
			config: "Next: func(ctx *fiber.Ctx) bool { return ctx.IsFromLocal() },\n\tSkipURIs: paths,",
			want:   `Next: func(ctx fiber.Ctx) bool { return ctx.IsFromLocal() || slices.Contains(paths, ctx.Path()) },`,
		},
		{
			name:   "next is a literal with several statements",
			config: "Next: func(c *fiber.Ctx) bool {\n\t\tif c.Method() == \"GET\" {\n\t\t\treturn true\n\t\t}\n\t\treturn false\n\t},\n\tSkipURIs: paths,",
			want:   "Next: func(ctx fiber.Ctx) bool { return (func(c fiber.Ctx) bool { if c.Method() == \"GET\" { return true } return false })(ctx) || slices.Contains(paths, ctx.Path()) },",
		},
		{
			name:   "next is nil",
			config: "Next: nil,\n\tSkipURIs: paths,",
			want:   `Next: func(c fiber.Ctx) bool { return slices.Contains(paths, c.Path()) },`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, zerologSource("var _ = fiberzerolog.New(fiberzerolog.Config{\n\tLogger: &logger,\n\t"+tt.config+"\n})"))

			contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateZerologConfig)

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.NotContains(t, content, "SkipURIs")
			assert.Contains(t, contribCompact(content), contribCompact(tt.want))
			assert.Equal(t, 1, countOccurrences(content, "Next:"))
			assert.Contains(t, content, "\t\"slices\"\n")
			assert.Empty(t, internal.DrainNotes(dir))
		})
	}
}

func Test_MigrateZerologConfig_SkipURIsThatIsNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, zerologSource(`var _ = fiberzerolog.New(fiberzerolog.Config{Logger: &logger, SkipURIs: nil})
var _ = fiberzerolog.New(fiberzerolog.Config{Logger: &logger, SkipURIs: []string{}})`))

	contribMigrate(t, dir, v3.MigrateZerologConfig)

	content := readFile(t, file)
	assert.Equal(t, 2, countOccurrences(content, "fiberzerolog.Config{Logger: &logger}"))
	assert.NotContains(t, content, `"slices"`)
}

func Test_MigrateZerologConfig_SkipFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "body alone",
			config: `SkipBody: func(c *fiber.Ctx) bool { return c.Path() == "/login" },`,
			want:   `SkipField: func(field string, c fiber.Ctx) bool { switch field { case fiberzerolog.FieldBody: return c.Path() == "/login" } return false },`,
		},
		{
			name:   "response body alone",
			config: `SkipResBody: func(ctx *fiber.Ctx) bool { return ctx.Get("X-No-Body") != "" },`,
			want:   `SkipField: func(field string, ctx fiber.Ctx) bool { switch field { case fiberzerolog.FieldResBody: return ctx.Get("X-No-Body") != "" } return false },`,
		},
		{
			name:   "both, with different parameter names",
			config: "SkipBody: func(c *fiber.Ctx) bool { return c.Path() == \"/login\" },\n\tSkipResBody: func(ctx *fiber.Ctx) bool { return ctx.IsFromLocal() },",
			want: `SkipField: func(field string, c fiber.Ctx) bool { switch field { case fiberzerolog.FieldBody: return c.Path() == "/login" ` +
				`case fiberzerolog.FieldResBody: return (func(ctx fiber.Ctx) bool { return ctx.IsFromLocal() })(c) } return false },`,
		},
		{
			name:   "named functions",
			config: "SkipBody: skipped,\n\tSkipResBody: skipped,",
			want: `SkipField: func(field string, c fiber.Ctx) bool { switch field { case fiberzerolog.FieldBody: return skipped(c) ` +
				`case fiberzerolog.FieldResBody: return skipped(c) } return false },`,
		},
		{
			name:   "nil is nothing to skip",
			config: "SkipBody: nil,\n\tSkipResBody: skipped,",
			want:   `SkipField: func(field string, c fiber.Ctx) bool { switch field { case fiberzerolog.FieldResBody: return skipped(c) } return false },`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, zerologSource("var _ = fiberzerolog.New(fiberzerolog.Config{\n\tLogger: &logger,\n\t"+tt.config+"\n})"))

			contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateZerologConfig)

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.NotContains(t, content, "SkipBody")
			assert.NotContains(t, content, "SkipResBody")
			assert.Contains(t, contribCompact(content), contribCompact(tt.want))
			assert.NotContains(t, content, `"slices"`)
			assert.Empty(t, internal.DrainNotes(dir))
		})
	}
}

func Test_MigrateZerologConfig_AssignedFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, zerologSource(`func config() fiberzerolog.Config {
	cfg := fiberzerolog.Config{Logger: &logger}
	cfg.SkipURIs = paths
	cfg.SkipBody = skipped
	return cfg
}`))

	contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateZerologConfig)

	content := readFile(t, file)
	contribRequireParses(t, content)
	compact := contribCompact(content)
	assert.Contains(t, compact, `cfg.Next = func(c fiber.Ctx) bool { return slices.Contains(paths, c.Path()) }`)
	assert.Contains(t, compact, `cfg.SkipField = func(field string, c fiber.Ctx) bool { switch field { case fiberzerolog.FieldBody: return skipped(c) } return false }`)
	assert.NotContains(t, content, "SkipURIs")
	assert.NotContains(t, content, "SkipBody")
}

func Test_MigrateZerologConfig_ReportsWhatItCannotMove(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := zerologSource(`func list() []string { return nil }

var _ = fiberzerolog.New(fiberzerolog.Config{Logger: &logger, SkipURIs: list()})

func config(on bool) fiberzerolog.Config {
	cfg := fiberzerolog.Config{Logger: &logger}
	if on {
		cfg.SkipBody = skipped
	}
	return cfg
}`)
	file := writeTempFile(t, dir, source)

	out := contribMigrate(t, dir, v3.MigrateZerologConfig)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	assert.Contains(t, notes[0].Message, "list() is a call")
	assert.Contains(t, notes[1].Message, "set conditionally")
}

func Test_MigrateZerologConfig_NameInUse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import (
	fiberzerolog "github.com/gofiber/contrib/fiberzerolog"
)

var slices = []string{"a"}

var _ = fiberzerolog.Config{SkipURIs: slices}
`
	file := writeTempFile(t, dir, source)

	contribMigrate(t, dir, v3.MigrateZerologConfig)

	assert.Equal(t, source, readFile(t, file))
	require.Len(t, internal.DrainNotes(dir), 1)
}

func Test_MigrateZerologConfig_UnaliasedImportAfterTheContribMigration(t *testing.T) {
	contribVersions(t, map[string]string{"zerolog": "v1.1.7"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/fiberzerolog"
	"github.com/gofiber/fiber/v2"
)

var _ = fiberzerolog.New(fiberzerolog.Config{SkipBody: func(c *fiber.Ctx) bool { return true }})
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateHandlerSignatures, v3.MigrateZerologConfig)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "\tfiberzerolog \"github.com/gofiber/contrib/v3/zerolog\"\n")
	assert.Contains(t, contribCompact(content), "case fiberzerolog.FieldBody: return true")
}

func Test_MigrateZerologConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, zerologSource(`var _ = fiberzerolog.New(fiberzerolog.Config{SkipURIs: paths, SkipBody: skipped})`))

	contribMigrate(t, dir, v3.MigrateZerologConfig)
	migrated := readFile(t, file)

	out := contribMigrate(t, dir, v3.MigrateZerologConfig)

	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateZerologConfig_LeavesV3ConfigsAndOtherPackagesAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import (
	"example.com/zerolog"
	zl "github.com/gofiber/contrib/v3/zerolog"
	"github.com/gofiber/fiber/v3"
)

var _ = zl.Config{SkipField: func(field string, c fiber.Ctx) bool { return false }}

var _ = zerolog.Config{SkipURIs: nil}
`
	file := writeTempFile(t, dir, source)

	out := contribMigrate(t, dir, v3.MigrateZerologConfig)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
}
