package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// loggerSource wraps the body of a Setup function in a valid v2 file.
func loggerSource(body string) string {
	return `package main

import (
	"io"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

var _ io.Writer = os.Stdout

func Setup(app *fiber.App, f *os.File) {
` + body + `
}
`
}

func Test_MigrateLoggerConfig(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerConfig, loggerSource(`	app.Use(logger.New(logger.Config{
		Format:     "[${time}] ${pid} ${status} - ${method} ${path} ${latency}\n",
		TimeFormat: "02-Jan-2006",
		Output:     f,
		Done: func(c *fiber.Ctx, logString []byte) {
			if c.Response().StatusCode() != fiber.StatusOK {
				_ = logString
			}
		},
		Next: func(c *fiber.Ctx) bool { return c.Path() == "/health" },
	}))
	app.Use(logger.New(logger.Config{Output: io.MultiWriter(os.Stdout, f)}))`))

	assert.Contains(t, content, "Stream:     f,")
	assert.Contains(t, content, "Stream: io.MultiWriter(os.Stdout, f)")
	assert.NotContains(t, content, "Output:")
	assert.Contains(t, content, "Format:     ")
	assert.Contains(t, run.out, "Migrating logger middleware configs")
	assert.Empty(t, run.notes)
}

func Test_MigrateLoggerConfig_SingleLine(t *testing.T) {
	t.Parallel()

	// The old regexp wanted a brace or a line break before the field.
	content, _ := mw2Single(t, v3.MigrateLoggerConfig, loggerSource(`	app.Use(logger.New(logger.Config{Format: "${status}\n", Output: os.Stdout}))
	app.Use(logger.New(logger.Config{TimeZone: "UTC", Format: "${status}\n", Output: os.Stderr, DisableColors: true}))
	app.Use(logger.New(logger.Config{Output: os.Stdout}))`))

	assert.Contains(t, content, `logger.Config{Format: "${status}\n", Stream: os.Stdout}`)
	assert.Contains(t, content, `Stream: os.Stderr, DisableColors: true}`)
	assert.Contains(t, content, "logger.Config{Stream: os.Stdout}")
	assert.NotContains(t, content, "Output")
}

func Test_MigrateLoggerConfig_Assignments(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerConfig, loggerSource(`	cfg := logger.ConfigDefault
	cfg.Output = f
	cfg.Format = "${status} ${path}\n"
	app.Use(logger.New(cfg))

	var cfg2 logger.Config
	cfg2.Output = os.Stderr
	app.Use(logger.New(cfg2))

	_ = logger.ConfigDefault.Output`))

	assert.Contains(t, content, "cfg.Stream = f")
	assert.Contains(t, content, "cfg2.Stream = os.Stderr")
	assert.Contains(t, content, "logger.ConfigDefault.Stream")
	assert.NotContains(t, content, "Output")
	assert.Empty(t, run.notes)
}

func Test_MigrateLoggerConfig_Alias(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateLoggerConfig, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	lg "github.com/gofiber/fiber/v2/middleware/logger"
)

func Setup(app *fiber.App) {
	app.Use(lg.New(lg.Config{
		Format: "${status}\n",
		Output: os.Stdout,
	}))
}
`)

	assert.Contains(t, content, "Stream: os.Stdout,")
	assert.NotContains(t, content, "Output")
}

func Test_MigrateLoggerConfig_OutputInStringLiteral(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateLoggerConfig, loggerSource(`	app.Use(logger.New(logger.Config{
		Format: "Output: ${status}\n", // Output: the format
		Output: os.Stdout,
	}))`))

	assert.Contains(t, content, "Stream: os.Stdout,")
	assert.Contains(t, content, `"Output: ${status}\n"`, "string literal should not be modified")
	assert.Contains(t, content, "// Output: the format")
}

func Test_MigrateLoggerConfig_Timestamp(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func Setup(app *fiber.App) {
	app.Use(logger.New(logger.Config{
		Format: "${ts} ${tag}\n",
		CustomTags: map[string]logger.LogFunc{
			"ts": func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return output.WriteString(data.Timestamp.Load().(string))
			},
			"tag": tag,
		},
	}))
}

func tag(output logger.Buffer, c *fiber.Ctx, d *logger.Data, extraParam string) (int, error) {
	return output.WriteString("[" + d.Timestamp.Load().(string) + "]")
}

type other struct{ Timestamp interface{ Load() any } }

func unrelated(o other) string { return o.Timestamp.Load().(string) }
`)

	assert.Contains(t, content, "return output.WriteString(data.Timestamp)")
	assert.Contains(t, content, `return output.WriteString("[" + d.Timestamp + "]")`)
	assert.Contains(t, content, "o.Timestamp.Load().(string)", "only the Data of the logger is a string in v3")
	assert.Contains(t, run.out, "Migrating logger middleware configs")
}

func Test_MigrateLoggerConfig_OutputAndStreamAreLeftAlone(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerConfig, loggerSource(`	app.Use(logger.New(logger.Config{
		Output: os.Stdout,
		Stream: os.Stderr,
	}))`))

	assert.Contains(t, content, "Output: os.Stdout,")
	require.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0].Message, "sets both Output and Stream")
}

func Test_MigrateLoggerConfig_IgnoresOtherTypes(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"os"

	"github.com/gofiber/fiber/v2/middleware/logger"
)

type sink struct{ Output *os.File }

var _ = logger.New

func main() {
	s := sink{Output: os.Stdout}
	s.Output = os.Stderr
	_ = s
}
`
	content, run := mw2Single(t, v3.MigrateLoggerConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateLoggerConfig_NoChange(t *testing.T) {
	t.Parallel()

	_, run := mw2Single(t, v3.MigrateLoggerConfig, `package main
import "fmt"
func main() { fmt.Println("hello") }
`)

	assert.Empty(t, run.out)
}

func Test_MigrateLoggerConfig_Idempotent(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLoggerConfig, map[string]string{"main.go": loggerSource(`	cfg := logger.ConfigDefault
	cfg.Output = f
	app.Use(logger.New(logger.Config{Format: "x", Output: f}))`)})
	first := run.file(t, "main.go")
	assert.Contains(t, run.out, "Migrating logger middleware configs")

	second := mw2Again(t, v3.MigrateLoggerConfig, run.dir)

	assert.Equal(t, first, second.file(t, "main.go"))
	assert.Empty(t, second.out)
}
