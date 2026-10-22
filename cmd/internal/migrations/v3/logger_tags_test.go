package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
