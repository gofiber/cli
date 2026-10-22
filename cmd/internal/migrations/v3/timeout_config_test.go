package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const timeoutSourceHead = `package main

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

var errSlow = errors.New("slow upstream")

const reqTimeout = 5 * time.Second

func slow(c *fiber.Ctx) error { return c.SendString("done") }

var _ = context.Canceled

func routes(app *fiber.App, d time.Duration) {
`

func Test_MigrateTimeoutConfig_New(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateTimeoutConfig, timeoutSourceHead+`	app.Get("/a", timeout.New(slow, 5*time.Second))
	app.Get("/b", timeout.New(slow, reqTimeout))
	app.Get("/c", timeout.New(slow, d))
	app.Get("/d", timeout.New(slow, time.Duration(3)*time.Second))
	app.Get("/e", timeout.New(func(c *fiber.Ctx) error {
		return c.SendString("hello")
	}, 2*time.Second))
	app.Get("/f", timeout.New(wrap("a", "b"), 2*time.Second))
}

func wrap(a, b string) fiber.Handler { return slow }
`)

	assert.Contains(t, content, "timeout.New(slow, timeout.Config{Timeout: 5 * time.Second})")
	assert.Contains(t, content, "timeout.New(slow, timeout.Config{Timeout: reqTimeout})")
	assert.Contains(t, content, "timeout.New(slow, timeout.Config{Timeout: d})")
	assert.Contains(t, content, "timeout.New(slow, timeout.Config{Timeout: time.Duration(3) * time.Second})")
	assert.Contains(t, content, "}, timeout.Config{Timeout: 2 * time.Second}))")
	assert.Contains(t, content, `timeout.New(wrap("a", "b"), timeout.Config{Timeout: 2 * time.Second})`)
	assert.Contains(t, run.out, "Migrating timeout middleware configs")
	assert.Empty(t, run.notes)
}

func Test_MigrateTimeoutConfig_NewWithContext(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateTimeoutConfig, timeoutSourceHead+`	app.Get("/f", timeout.NewWithContext(slow, 5*time.Second))
	app.Get("/g", timeout.NewWithContext(slow, 5*time.Second, errSlow))
	app.Get("/h", timeout.NewWithContext(slow, reqTimeout, errSlow, context.Canceled))
	app.Use(timeout.NewWithContext(func(c *fiber.Ctx) error { return c.Next() }, 30*time.Second))
	h := timeout.NewWithContext(slow, 1*time.Second)
	app.Get("/i", h)
	errs := []error{errSlow}
	app.Get("/j", timeout.NewWithContext(slow, d, errs...))
	app.Get("/k", timeout.NewWithContext(
		slow,
		d,
		errSlow,
	))
}
`)

	assert.Contains(t, content, "timeout.New(slow, timeout.Config{Timeout: 5 * time.Second})")
	assert.Contains(t, content, "timeout.New(slow, timeout.Config{Timeout: 5 * time.Second, Errors: []error{errSlow}})")
	assert.Contains(t, content, "timeout.New(slow, timeout.Config{Timeout: reqTimeout, Errors: []error{errSlow, context.Canceled}})")
	assert.Contains(t, content, "timeout.New(func(c *fiber.Ctx) error { return c.Next() }, timeout.Config{Timeout: 30 * time.Second})")
	assert.Contains(t, content, "h := timeout.New(slow, timeout.Config{Timeout: 1 * time.Second})")
	assert.Contains(t, content, "timeout.New(slow, timeout.Config{Timeout: d, Errors: errs})")
	assert.Contains(t, content, "timeout.New(\n\t\tslow,\n\t\ttimeout.Config{Timeout: d, Errors: []error{errSlow}},\n\t)")
	assert.NotContains(t, content, "NewWithContext")
	assert.Contains(t, run.out, "Migrating timeout middleware configs")
	assert.Empty(t, run.notes)
}

func Test_MigrateTimeoutConfig_Alias(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateTimeoutConfig, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	to "github.com/gofiber/fiber/v2/middleware/timeout"
)

func slow(c *fiber.Ctx) error { return c.SendString("done") }

func routes(app *fiber.App) {
	app.Get("/a", to.New(slow, 5*time.Second))
	app.Get("/b", to.NewWithContext(slow, 5*time.Second))
}
`)

	assert.Contains(t, content, "to.New(slow, to.Config{Timeout: 5 * time.Second})")
	assert.NotContains(t, content, "NewWithContext")
	assert.NotContains(t, content, "timeout.Config")
}

func Test_MigrateTimeoutConfig_LeavesV3CallsAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"
)

func slow(c fiber.Ctx) error { return c.SendString("done") }

func routes(app *fiber.App) {
	cfg := timeout.Config{Timeout: time.Second}
	app.Get("/a", timeout.New(slow, timeout.Config{Timeout: 5 * time.Second}))
	app.Get("/b", timeout.New(slow, cfg))
	app.Get("/c", timeout.New(slow, timeout.ConfigDefault))
	app.Get("/d", timeout.New(slow))
}
`
	content, run := mw2Single(t, v3.MigrateTimeoutConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateTimeoutConfig_IgnoresOtherPackages(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"time"

	"example.com/timeout"
)

func main() {
	_ = timeout.New(nil, time.Second)
	_ = timeout.NewWithContext(nil, time.Second)
}
`
	content, run := mw2Single(t, v3.MigrateTimeoutConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateTimeoutConfig_Idempotent(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateTimeoutConfig, map[string]string{"main.go": timeoutSourceHead + `	app.Get("/a", timeout.New(slow, 5*time.Second))
	app.Get("/g", timeout.NewWithContext(slow, 5*time.Second, errSlow))
}
`})
	first := run.file(t, "main.go")
	assert.Contains(t, run.out, "Migrating timeout middleware configs")

	second := mw2Again(t, v3.MigrateTimeoutConfig, run.dir)

	assert.Equal(t, first, second.file(t, "main.go"))
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}
