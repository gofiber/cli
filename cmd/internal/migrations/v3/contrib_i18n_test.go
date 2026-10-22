package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The program of the README of the module, plus the Next that the config has.
const i18nREADME = `package main

import (
	"log"

	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

func main() {
	app := fiber.New()
	app.Use(
		fiberi18n.New(&fiberi18n.Config{
			RootPath:        "./example/localize",
			AcceptLanguages: []language.Tag{language.Chinese, language.English},
			DefaultLanguage: language.Chinese,
			Next: func(c *fiber.Ctx) bool {
				return c.Path() == "/health"
			},
		}),
	)
	app.Get("/", func(c *fiber.Ctx) error {
		localize, err := fiberi18n.Localize(c, "welcome")
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendString(localize)
	})
	app.Get("/:name", func(ctx *fiber.Ctx) error {
		return ctx.SendString(fiberi18n.MustLocalize(ctx, &i18n.LocalizeConfig{
			MessageID: "welcomeWithName",
			TemplateData: map[string]string{
				"name": ctx.Params("name"),
			},
		}))
	})
	log.Fatal(app.Listen(":3000"))
}
`

func Test_MigrateI18nTranslator(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, i18nREADME)

	out := contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)

	content := readFile(t, file)
	contribRequireParses(t, content)
	compact := contribCompact(content)

	assert.Contains(t, compact, "var translator *fiberi18n.I18n")
	assert.Contains(t, compact, "translator = fiberi18n.New(&fiberi18n.Config{ RootPath: \"./example/localize\", "+
		"AcceptLanguages: []language.Tag{language.Chinese, language.English}, DefaultLanguage: language.Chinese, })")
	assert.NotContains(t, content, "app.Use(")
	assert.NotContains(t, content, "Next:")
	assert.Contains(t, content, `localize, err := translator.Localize(c, "welcome")`)
	assert.Contains(t, content, "translator.MustLocalize(ctx, &i18n.LocalizeConfig{")
	assert.NotContains(t, content, "fiberi18n.Localize")
	assert.NotContains(t, content, "fiberi18n.MustLocalize")
	assert.Contains(t, out, "Migrating i18n middleware to the translator")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, 19, notes[0].Line)
	assert.Contains(t, notes[0].Message, "Config.Next is gone")
}

func Test_MigrateI18nTranslator_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, i18nREADME)

	contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)
	migrated := readFile(t, file)
	internal.DrainNotes(dir)

	out := contribMigrate(t, dir, v3.MigrateI18nTranslator)

	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateI18nTranslator_Shapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		use  string
		want string
	}{
		{name: "no config", use: `app.Use(fiberi18n.New())`, want: "translator = fiberi18n.New()"},
		{name: "a path in front", use: `app.Use("/api", fiberi18n.New(&fiberi18n.Config{RootPath: "./l"}))`, want: `translator = fiberi18n.New(&fiberi18n.Config{RootPath: "./l"})`},
		{name: "a group", use: `app.Group("/api").Use(fiberi18n.New())`, want: "translator = fiberi18n.New()"},
		{name: "one line with a comment", use: `app.Use(fiberi18n.New(cfg)) // translations`, want: "translator = fiberi18n.New(cfg) // translations"},
		{name: "arguments on lines of their own", use: "app.Use(\n\t\tfiberi18n.New(cfg),\n\t)", want: "translator = fiberi18n.New(cfg)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

var cfg = &fiberi18n.Config{}

func main() {
	app := fiber.New()
	`+tt.use+`
	app.Get("/", func(c *fiber.Ctx) error {
		msg, _ := fiberi18n.Localize(c, "welcome")
		return c.SendString(msg)
	})
}
`)

			contribMigrate(t, dir, v3.MigrateI18nTranslator)

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.Contains(t, content, tt.want)
			assert.NotContains(t, content, ".Use(fiberi18n")
			assert.Contains(t, content, "var translator *fiberi18n.I18n")
			assert.Contains(t, content, `msg, _ := translator.Localize(c, "welcome")`)
			assert.Empty(t, internal.DrainNotes(dir))
		})
	}
}

// The handlers of a program are rarely in the function that registers the
// middleware: the variable is declared where the registration is and the other
// files of the package use it.
func Test_MigrateI18nTranslator_SeveralFilesOfAPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	main := contribWriteFile(t, dir, "main.go", `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Use(fiberi18n.New(&fiberi18n.Config{RootPath: "./l"}))
	app.Get("/", home)
}
`)
	handlers := contribWriteFile(t, dir, "handlers.go", `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func home(c *fiber.Ctx) error {
	msg, err := fiberi18n.Localize(c, "welcome")
	if err != nil {
		return err
	}
	return c.SendString(msg)
}
`)
	tests := contribWriteFile(t, dir, "main_test.go", `package main

import (
	"testing"

	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func TestHome(t *testing.T) {
	app := fiber.New()
	app.Use(fiberi18n.New())
	app.Get("/", home)
}
`)
	other := contribWriteFile(t, dir, "other/other.go", `package other

import fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"

var _ = fiberi18n.Localize
`)

	contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)

	mainContent := readFile(t, main)
	assert.Contains(t, mainContent, "var translator *fiberi18n.I18n")
	assert.Contains(t, mainContent, `translator = fiberi18n.New(&fiberi18n.Config{RootPath: "./l"})`)

	handlersContent := readFile(t, handlers)
	contribRequireParses(t, handlersContent)
	assert.Contains(t, handlersContent, `msg, err := translator.Localize(c, "welcome")`)
	assert.NotContains(t, handlersContent, "fiberi18n", "the import is not used any more")

	testsContent := readFile(t, tests)
	assert.Contains(t, testsContent, "translator = fiberi18n.New()")
	assert.NotContains(t, testsContent, "var translator")

	// Another package has no translator to call.
	assert.Contains(t, readFile(t, other), "fiberi18n.Localize")
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "other/other.go", notes[0].File)
	assert.Contains(t, notes[0].Message, "no code of this package registers fiberi18n.New")
}

func Test_MigrateI18nTranslator_PicksANameThatIsFree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	main := contribWriteFile(t, dir, "main.go", `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Use(fiberi18n.New())
	app.Get("/", home)
}
`)
	other := contribWriteFile(t, dir, "other.go", `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func home(c *fiber.Ctx) error {
	translator := "local"
	msg, _ := fiberi18n.Localize(c, translator)
	return c.SendString(msg)
}
`)

	contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)

	assert.Contains(t, readFile(t, main), "i18nTranslator = fiberi18n.New()")
	assert.Contains(t, readFile(t, other), "msg, _ := i18nTranslator.Localize(c, translator)")
}

func Test_MigrateI18nTranslator_LeavesWhatItCannotMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   map[string]string
		message string
		kept    string
	}{
		{
			name: "two registrations",
			files: map[string]string{
				"main.go": `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func main() {
	a, b := fiber.New(), fiber.New()
	a.Use(fiberi18n.New())
	b.Use(fiberi18n.New(&fiberi18n.Config{RootPath: "./b"}))
}

func home(c *fiber.Ctx) error {
	msg, _ := fiberi18n.Localize(c, "welcome")
	return c.SendString(msg)
}
`,
			},
			message: "registers fiberi18n.New more than once",
			kept:    "a.Use(fiberi18n.New())",
		},
		{
			name: "calls without a registration",
			files: map[string]string{
				"handlers.go": `package handlers

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func Home(c *fiber.Ctx) error {
	msg, _ := fiberi18n.MustLocalize(c, "welcome"), 0
	return c.SendString(msg)
}
`,
			},
			message: "no code of this package registers fiberi18n.New",
			kept:    "fiberi18n.MustLocalize(c",
		},
		{
			name: "a handler variable",
			files: map[string]string{
				"main.go": `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	mw := fiberi18n.New()
	app.Use(mw)
}
`,
			},
			message: "do not register it with Use",
			kept:    "mw := fiberi18n.New()",
		},
		{
			name: "a middleware among others",
			files: map[string]string{
				"main.go": `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	app := fiber.New()
	app.Use(logger.New(), fiberi18n.New())
}
`,
			},
			message: "it panics when the program starts",
			kept:    "app.Use(logger.New(), fiberi18n.New())",
		},
		{
			name: "returned as a handler",
			files: map[string]string{
				"main.go": `package main

import (
	fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
)

func middleware() fiber.Handler { return fiberi18n.New() }
`,
			},
			message: "not a handler",
			kept:    "return fiberi18n.New()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			paths := map[string]string{}
			for name, source := range tt.files {
				paths[name] = contribWriteFile(t, dir, name, source)
			}

			out := contribMigrate(t, dir, v3.MigrateI18nTranslator)

			for name, source := range tt.files {
				assert.Equal(t, source, readFile(t, paths[name]), name)
			}
			assert.Empty(t, out)
			assert.Contains(t, tt.files[firstKey(tt.files)], tt.kept)
			notes := internal.DrainNotes(dir)
			require.NotEmpty(t, notes)
			assert.Contains(t, notes[0].Message, tt.message)
		})
	}
}

func firstKey(m map[string]string) string {
	for k := range m {
		return k
	}
	return ""
}

// The v3 way, written by hand or left by an earlier run, is nothing to report.
func Test_MigrateI18nTranslator_LeavesTheTranslatorAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import (
	"github.com/gofiber/fiber/v3"
	i18n "github.com/gofiber/contrib/v3/i18n"
)

func main() {
	app := fiber.New()
	translator := i18n.New(&i18n.Config{RootPath: "./l"})
	app.Get("/", func(c fiber.Ctx) error {
		msg, _ := translator.Localize(c, "welcome")
		return c.SendString(msg)
	})
}
`
	file := writeTempFile(t, dir, source)

	out := contribMigrate(t, dir, v3.MigrateI18nTranslator)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateI18nTranslator_UnaliasedImportAfterTheContribMigration(t *testing.T) {
	contribVersions(t, map[string]string{"i18n": "v1.0.13"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/fiberi18n/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

func main() {
	app := fiber.New()
	app.Use(fiberi18n.New())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(fiberi18n.MustLocalize(c, &i18n.LocalizeConfig{MessageID: "welcome"}))
	})
}
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "\tfiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n")
	assert.Contains(t, content, "\t\"github.com/nicksnyder/go-i18n/v2/i18n\"\n")
	assert.Contains(t, content, "var translator *fiberi18n.I18n")
	assert.Contains(t, content, "translator = fiberi18n.New()")
	assert.Contains(t, content, "translator.MustLocalize(c, &i18n.LocalizeConfig{")
}

func Test_MigrateI18nTranslator_SkipsBrokenFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import fiberi18n "github.com/gofiber/contrib/fiberi18n/v2"

func main() { app.Use(fiberi18n.New() }
`
	file := writeTempFile(t, dir, source)

	contribMigrate(t, dir, v3.MigrateI18nTranslator)

	assert.Equal(t, source, readFile(t, file))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "does not parse")
}
