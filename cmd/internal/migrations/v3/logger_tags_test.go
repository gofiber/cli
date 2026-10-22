package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateLoggerTags(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerTags, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

var _ = logger.TagHeader

func Setup(app *fiber.App) {
	app.Use(logger.New(logger.Config{Format: "${ip} ${header:X-Request-ID} ${status}\n"}))
	app.Use(logger.New(logger.Config{Format: "${ip} ${" + logger.TagHeader + "X-Correlation-ID} ${status}\n"}))
	app.Use(logger.New(logger.Config{Format: `+"`${header:A} ${header:B}`"+`}))
	app.Use(logger.New(logger.Config{Format: "${ip} ${reqHeader:X-Request-ID} ${respHeader:X-Served-By} ${status}\n"}))
}
`)

	assert.NotContains(t, content, "TagHeader")
	assert.Contains(t, content, "var _ = logger.TagReqHeader")
	assert.Contains(t, content, `"${ip} ${reqHeader:X-Request-ID} ${status}\n"`)
	assert.Contains(t, content, `"${ip} ${" + logger.TagReqHeader + "X-Correlation-ID} ${status}\n"`)
	assert.Contains(t, content, "`${reqHeader:A} ${reqHeader:B}`")
	assert.Contains(t, content, `${reqHeader:X-Request-ID} ${respHeader:X-Served-By}`)
	assert.NotContains(t, content, "${header:")
	assert.Contains(t, run.out, "Migrating logger tag constants")
	assert.Empty(t, run.notes)
}

func Test_MigrateLoggerTags_Alias(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateLoggerTags, `package main

import (
	"github.com/gofiber/fiber/v2"
	lg "github.com/gofiber/fiber/v2/middleware/logger"
)

func Setup(app *fiber.App) {
	app.Use(lg.New(lg.Config{Format: "${" + lg.TagHeader + "X-Foo} ${status}\n"}))
}
`)

	assert.Contains(t, content, `"${" + lg.TagReqHeader + "X-Foo}`)
}

func Test_MigrateLoggerTags_FormatKeptInAnotherFile(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLoggerTags, map[string]string{
		"go.mod": "module sample\n\ngo 1.25\n",
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	app := fiber.New()
	app.Use(logger.New(logger.Config{Format: accessLogFormat}))
}
`,
		"format.go": `package main

const accessLogFormat = "${ip} ${header:X-Request-ID} ${status}\n"
`,
		"other/other.go": `package other

const template = "${header:Authorization}"
`,
	})

	assert.Contains(t, run.file(t, "format.go"), "${reqHeader:X-Request-ID}")
	assert.Contains(t, run.file(t, "other/other.go"), "${reqHeader:Authorization}", "the module uses the logger, so the tag is the logger's")
}

func Test_MigrateLoggerTags_ModulesWithoutLoggerAreLeftAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import "fmt"

func main() {
	fmt.Println("${header:X} is the syntax of another template language")
}
`
	content, run := mw2Single(t, v3.MigrateLoggerTags, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateLoggerTags_NestedModulesAreSeparate(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLoggerTags, map[string]string{
		"go.mod": "module sample\n\ngo 1.25\n",
		"main.go": `package main

import "github.com/gofiber/fiber/v2/middleware/logger"

var _ = logger.New
`,
		"tools/go.mod": "module sample/tools\n\ngo 1.25\n",
		"tools/tpl.go": `package tools

const tpl = "${header:X}"
`,
	})

	assert.Contains(t, run.file(t, "tools/tpl.go"), "${header:X}")
}

func Test_MigrateLoggerTags_Idempotent(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLoggerTags, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v2/middleware/logger"

var _ = logger.New(logger.Config{Format: "${header:X} ${" + logger.TagHeader + "Y}"})
`})
	first := run.file(t, "main.go")
	assert.Contains(t, run.out, "Migrating logger tag constants")

	second := mw2Again(t, v3.MigrateLoggerTags, run.dir)

	assert.Equal(t, first, second.file(t, "main.go"))
	assert.Empty(t, second.out)
}

func Test_MigrateLoggerTags_LocalsOfAMiddleware(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerTags, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func Setup(app *fiber.App) {
	app.Use(requestid.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${ip} - ${locals:requestid} - ${status} ${method} ${path}\n",
	}))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(c.Locals("requestid").(string))
	})
}
`)

	assert.Contains(t, content, `"[${time}] ${ip} - ${requestid} - ${status} ${method} ${path}\n"`)
	assert.NotContains(t, content, "${locals:")
	assert.Empty(t, run.notes, "requestid is constructed before the logger, so nothing is left to say")
}

func Test_MigrateLoggerTags_LocalsOfAMiddlewareConstructedLater(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLoggerTags, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/keyauth"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func Setup(app *fiber.App) {
	app.Use(logger.New(logger.Config{Format: "${locals:requestid} ${locals:token} ${locals:other}\n"}))
	app.Use(requestid.New())
	app.Use(keyauth.New(keyauth.Config{Validator: func(c *fiber.Ctx, key string) (bool, error) { return true, nil }}))
}
`,
	})

	assert.Contains(t, run.file(t, "main.go"), `"${requestid} ${api-key} ${locals:other}\n"`)
	require.Len(t, run.notes, 2)
	assert.Contains(t, run.messages(), "construct it before logger.New()")
	assert.Contains(t, run.messages(), "v3 prints a masked prefix")
}

func Test_MigrateLoggerTags_LocalsOfTheApplicationAreLeftAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func Setup(app *fiber.App) {
	app.Use(requestid.New())
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("requestid", "mine")
		c.Locals("user", "bob")
		return c.Next()
	})
	app.Use(logger.New(logger.Config{Format: "${locals:requestid} ${locals:user} ${locals:username}\n"}))
}
`
	content, run := mw2Single(t, v3.MigrateLoggerTags, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
	assert.Empty(t, run.notes)
}

func Test_MigrateLoggerTags_LocalsOfAMiddlewareWithoutATag(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerTags, `package main

import (
	jwtware "github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func Setup(app *fiber.App) {
	app.Use(jwtware.New(jwtware.Config{SigningKey: jwtware.SigningKey{Key: []byte("k")}}))
	app.Use(logger.New(logger.Config{Format: "${locals:user} ${status}\n"}))
}
`)

	assert.Contains(t, content, "${locals:user}")
	require.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0].Message, "prints nothing in v3")
	assert.Contains(t, run.notes[0].Message, "jwtware.FromContext(c)")
}
