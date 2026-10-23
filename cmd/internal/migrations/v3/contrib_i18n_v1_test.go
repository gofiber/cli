package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The program of the example of the v1 module (fiberi18n v1.0.x): the messages
// come from functions that take the translator and the context of the request from
// package variables that the middleware set.
const i18nV1Example = `package main

import (
	"log"

	"github.com/gofiber/contrib/fiberi18n"
	"github.com/gofiber/fiber/v2"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

func main() {
	app := fiber.New()
	app.Use(
		fiberi18n.New(&fiberi18n.Config{
			RootPath:        "./localize",
			AcceptLanguages: []language.Tag{language.Chinese, language.English},
			DefaultLanguage: language.Chinese,
		}),
	)
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(fiberi18n.MustGetMessage("welcome"))
	})
	app.Get("/:name", func(ctx *fiber.Ctx) error {
		msg, err := fiberi18n.GetMessage(&i18n.LocalizeConfig{
			MessageID: "welcomeWithName",
			TemplateData: map[string]string{
				"name": ctx.Params("name"),
			},
		})
		if err != nil {
			return err
		}
		return ctx.SendString(msg)
	})
	log.Fatal(app.Listen(":3000"))
}
`

// GetMessage and MustGetMessage of v1 are Localize and MustLocalize of the
// translator with the context of the handler, which the v1 module kept in a
// package variable.
func Test_MigrateI18nTranslator_V1Functions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, i18nV1Example)

	out := contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "var translator *fiberi18n.I18n")
	assert.Contains(t, content, "translator = fiberi18n.New(")
	assert.Contains(t, content, `c.SendString(translator.MustLocalize(c, "welcome"))`)
	assert.Contains(t, content, "msg, err := translator.Localize(ctx, &i18n.LocalizeConfig{")
	assert.NotContains(t, content, "GetMessage")
	assert.NotContains(t, content, "app.Use(")
	assert.Contains(t, out, "Migrating i18n middleware to the translator")
	assert.Empty(t, internal.DrainNotes(dir))

	again := contribMigrate(t, dir, v3.MigrateI18nTranslator)
	assert.Equal(t, content, readFile(t, file))
	assert.Empty(t, again)
	assert.Empty(t, internal.DrainNotes(dir))
}

// A closure inside the handler sees the context of the handler, and the import
// can carry a name of its own.
func Test_MigrateI18nTranslator_V1FunctionsInClosures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"strings"

	i18nmw "github.com/gofiber/contrib/fiberi18n"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Use(i18nmw.New())
	app.Get("/", func(c *fiber.Ctx) error {
		upper := func(id string) string { return strings.ToUpper(i18nmw.MustGetMessage(id)) }
		return c.SendString(upper("welcome") + i18nmw.MustGetMessage(
			"bye",
		))
	})
}
`)

	contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "strings.ToUpper(translator.MustLocalize(c, id))")
	assert.Contains(t, content, "translator.MustLocalize(c,\n\t\t\t\"bye\",")
	assert.NotContains(t, content, "GetMessage")
}

// Where no handler's context is in scope, or the function is not called, the
// migration cannot say which request is meant: the use stays and is reported.
func Test_MigrateI18nTranslator_V1FunctionsWithoutContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/fiberi18n"
	"github.com/gofiber/fiber/v2"
)

func greeting() string { return fiberi18n.MustGetMessage("welcome") }

var getter = fiberi18n.GetMessage

func main() {
	app := fiber.New()
	app.Use(fiberi18n.New())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(greeting())
	})
}
`)

	contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, `func greeting() string { return fiberi18n.MustGetMessage("welcome") }`)
	assert.Contains(t, content, "var getter = fiberi18n.GetMessage")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	assert.Contains(t, notes[0].Message, "no context of a request is in scope here, so pass one to translator.MustLocalize(c, ...)")
	assert.Contains(t, notes[1].Message, "GetMessage of the v1 module is used as a value; call translator.Localize(c, ...)")
}

// A package that never registers the middleware has no translator to call.
func Test_MigrateI18nTranslator_V1FunctionsOfAPackageThatDoesNotRegister(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package handlers

import (
	"github.com/gofiber/contrib/fiberi18n"
	"github.com/gofiber/fiber/v2"
)

func Hello(c *fiber.Ctx) error { return c.SendString(fiberi18n.MustGetMessage("welcome")) }
`)

	contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateI18nTranslator)

	assert.Contains(t, readFile(t, file), `fiberi18n.MustGetMessage("welcome")`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "MustGetMessage of the v1 module read the translator and the context of the request from package variables")
	assert.Contains(t, notes[0].Message, "translator.MustLocalize(c, ...)")
	assert.Contains(t, notes[0].Message, "no code of this package registers fiberi18n.New")
}
