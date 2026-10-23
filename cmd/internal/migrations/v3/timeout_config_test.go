package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// timeoutOn is the field every rewritten config gets, so that a timeout is still
// answered with 408 Request Timeout.
const timeoutOn = `OnTimeout: func(c fiber.Ctx) error { return fiber.ErrRequestTimeout }`

// timeoutCompact puts the text on one line, so that a test does not depend on
// how gofmt lays out the fields of a config.
func timeoutCompact(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// timeoutConfig is the config the migration writes for the given fields, in the
// form timeoutCompact leaves it in.
func timeoutConfig(pkg string, fields ...string) string {
	return pkg + ".Config{ " + strings.Join(fields, ", ") + ", " + timeoutOn + ", }"
}

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

	compact := timeoutCompact(content)
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: 5 * time.Second")+")")
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: reqTimeout")+")")
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: d")+")")
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: time.Duration(3) * time.Second")+")")
	assert.Contains(t, compact, "}, "+timeoutConfig("timeout", "Timeout: 2 * time.Second")+"))")
	assert.Contains(t, compact, `timeout.New(wrap("a", "b"), `+timeoutConfig("timeout", "Timeout: 2 * time.Second")+")")
	assert.Contains(t, run.out, "Migrating timeout middleware configs")

	// the project is told once that the handlers of v2 swallowed their errors
	if assert.Len(t, run.notes, 1, "%v", run.messages()) {
		assert.Contains(t, run.notes[0].Message, "timeout.New(handler, duration) of v2 logged an error the handler returned")
		assert.Len(t, run.notes[0].More, 5, "the project is told where the other calls are")
	}
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

	compact := timeoutCompact(content)
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: 5 * time.Second")+")")
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: 5 * time.Second", "Errors: []error{errSlow}")+")")
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: reqTimeout", "Errors: []error{errSlow, context.Canceled}")+")")
	assert.Contains(t, compact, "timeout.New(func(c *fiber.Ctx) error { return c.Next() }, "+timeoutConfig("timeout", "Timeout: 30 * time.Second")+")")
	assert.Contains(t, compact, "h := timeout.New(slow, "+timeoutConfig("timeout", "Timeout: 1 * time.Second")+")")
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: d", "Errors: errs")+")")
	assert.Contains(t, compact, "timeout.New( slow, "+timeoutConfig("timeout", "Timeout: d", "Errors: []error{errSlow}")+", )")
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

	assert.Contains(t, timeoutCompact(content), "to.New(slow, "+timeoutConfig("to", "Timeout: 5 * time.Second")+")")
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

func Test_MigrateTimeoutConfig_UsesTheNamesOfTheFile(t *testing.T) {
	t.Parallel()

	// the core package under an alias of its own
	res := mw1Run(t, v3.MigrateTimeoutConfig, `package main

import (
	"time"

	f "github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

func slow(c *f.Ctx) error { return c.SendString("done") }

func routes(app *f.App) {
	app.Get("/a", timeout.New(slow, 5*time.Second))
	app.Get("/b", timeout.NewWithContext(slow, 5*time.Second))
}
`)

	file := res.file()
	assert.Contains(t, timeoutCompact(file), "timeout.New(slow, timeout.Config{ Timeout: 5 * time.Second, OnTimeout: func(c f.Ctx) error { return f.ErrRequestTimeout }, })")
	assert.Equal(t, 2, countOf(file, "OnTimeout: func(c f.Ctx)"))
	assert.NotContains(t, file, "fiber.")
	assert.Len(t, res.notes, 1, "only the timeout.New of v2 swallowed the errors of its handler: %v", res.notes)

	// and under the name of the parameter the function literal would declare
	res = mw1Run(t, v3.MigrateTimeoutConfig, `package main

import (
	"time"

	c "github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

func slow(ctx *c.Ctx) error { return ctx.SendString("done") }

func routes(app *c.App) {
	app.Get("/a", timeout.New(slow, 5*time.Second))
}
`)

	assert.Contains(t, res.file(), "OnTimeout: func(_ c.Ctx) error { return c.ErrRequestTimeout }")
}

func Test_MigrateTimeoutConfig_AddsTheFiberImport(t *testing.T) {
	t.Parallel()

	// the handler comes from another package: the file does not import fiber
	res := mw1Run(t, v3.MigrateTimeoutConfig, `package main

import (
	"time"

	"example.com/app/handlers"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

var slow = timeout.NewWithContext(handlers.Slow, 5*time.Second)
`)

	file := res.file()
	assert.Contains(t, file, `"github.com/gofiber/fiber/v3"`)
	assert.Equal(t, 1, countOf(file, `"github.com/gofiber/fiber/v3"`))
	assert.Contains(t, timeoutCompact(file), "var slow = timeout.New(handlers.Slow, "+timeoutConfig("timeout", "Timeout: 5 * time.Second")+")")
	assert.Contains(t, res.stdout, "Migrating timeout middleware configs")
}

func Test_MigrateTimeoutConfig_KeepsAConfigThatHasItsOwnOnTimeout(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"
)

func slow(c fiber.Ctx) error { return c.SendString("done") }

func routes(app *fiber.App) {
	app.Get("/a", timeout.New(slow, timeout.Config{
		Timeout:   5 * time.Second,
		OnTimeout: func(c fiber.Ctx) error { return c.Status(fiber.StatusGatewayTimeout).SendString("too slow") },
	}))
	app.Get("/b", timeout.New(slow, timeout.Config{Timeout: 5 * time.Second}))
}
`
	res := mw1Run(t, v3.MigrateTimeoutConfig, src)

	// a config of v3 is what its author wants it to be
	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)
}

func Test_MigrateTimeoutConfig_Layout(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateTimeoutConfig, `package main

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

var errSlow = errors.New("slow upstream")

func slow(c *fiber.Ctx) error { return c.SendString("done") }

func routes(app *fiber.App) {
	app.Get("/a", timeout.New(slow, 5*time.Second))
	app.Get("/b", timeout.NewWithContext(
		slow,
		time.Minute,
		errSlow,
	))
}
`)

	assert.Equal(t, `package main

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

var errSlow = errors.New("slow upstream")

func slow(c *fiber.Ctx) error { return c.SendString("done") }

func routes(app *fiber.App) {
	app.Get("/a", timeout.New(slow, timeout.Config{
		Timeout:   5 * time.Second,
		OnTimeout: func(c fiber.Ctx) error { return fiber.ErrRequestTimeout },
	}))
	app.Get("/b", timeout.New(
		slow,
		timeout.Config{
			Timeout:   time.Minute,
			Errors:    []error{errSlow},
			OnTimeout: func(c fiber.Ctx) error { return fiber.ErrRequestTimeout },
		},
	))
}
`, res.file())
}

// timeoutHandled is the OnTimeout a timeout.New and a timeout.NewWithContext get
// when an application of the project has an ErrorHandler of its own, in the form
// timeoutCompact leaves it in.
const timeoutHandled = `OnTimeout: func(c fiber.Ctx) error { err := fiber.ErrRequestTimeout if c.Context().Err() != nil { // v3 sends this answer itself: the error handler of the application gives it the shape it had in v2 _ = c.App().Config().ErrorHandler(c, err) } return err },`

const timeoutRoutes = `package server

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

func slow(c *fiber.Ctx) error { return c.SendString("done") }

func Routes(app *fiber.App) {
	app.Get("/a", timeout.New(slow, 5*time.Second))
	app.Get("/b", timeout.NewWithContext(slow, 5*time.Second))
}
`

func Test_MigrateTimeoutConfig_CallsTheErrorHandlerOfTheApplication(t *testing.T) {
	t.Parallel()

	res := mw1RunFiles(t, v3.MigrateTimeoutConfig, map[string]string{
		// the application is made in another package than the middleware
		"internal/app/app.go": `package app

import "github.com/gofiber/fiber/v2"

func New() *fiber.App {
	return fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
}
`,
		"internal/server/routes.go": timeoutRoutes,
	})

	compact := timeoutCompact(res.files["internal/server/routes.go"])
	// v2 handed the 408 of timeout.New and of timeout.NewWithContext to the error handler through
	// the router, and v3 only does that for a handler that has returned: when the time is up while
	// the handler still runs, which includes the handler that waits for its context, OnTimeout
	// calls the error handler itself, once the deadline has passed
	assert.Equal(t, 2, strings.Count(compact, "timeout.New(slow, timeout.Config{ Timeout: 5 * time.Second, "+timeoutHandled+" })"), compact)
	assert.Contains(t, res.files["internal/server/routes.go"], "\t\t\tif c.Context().Err() != nil {\n")
	assert.Contains(t, res.files["internal/server/routes.go"], "\t\t\t\t_ = c.App().Config().ErrorHandler(c, err)\n")

	// one note, about what v2 swallowed; the application is known to have a handler
	if assert.Len(t, res.notes, 1, "%v", res.notes) {
		assert.Contains(t, res.notes[0], "timeout.New(handler, duration) of v2 logged an error the handler returned")
		assert.NotContains(t, res.notes[0], "ErrorHandler")
	}

	// the second run changes nothing
	again := mw1RunFiles(t, v3.MigrateTimeoutConfig, res.files)
	assert.Equal(t, res.files, again.files)
	assert.Empty(t, again.stdout)
}

func Test_MigrateTimeoutConfig_FindsTheErrorHandlerInEveryForm(t *testing.T) {
	t.Parallel()

	handlers := map[string]string{
		"literal of an alias": `package app

import "github.com/gofiber/fiber/v2"

type Config = fiber.Config

func New(h fiber.ErrorHandler) *fiber.App { return fiber.New(Config{ErrorHandler: h}) }
`,
		"assignment": `package app

import "github.com/gofiber/fiber/v2"

func New(h fiber.ErrorHandler) *fiber.App {
	cfg := fiber.Config{}
	cfg.ErrorHandler = h
	return fiber.New(cfg)
}
`,
		"aliased import": `package app

import f "github.com/gofiber/fiber/v2"

func New(h f.ErrorHandler) *f.App { return f.New(f.Config{ErrorHandler: h}) }
`,
		"element of a slice": `package app

import "github.com/gofiber/fiber/v2"

var configs = []fiber.Config{{ErrorHandler: nil}}
`,
	}
	for name, src := range handlers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := mw1RunFiles(t, v3.MigrateTimeoutConfig, map[string]string{"internal/app/app.go": src, "internal/server/routes.go": timeoutRoutes})
			assert.Contains(t, timeoutCompact(res.files["internal/server/routes.go"]), "Timeout: 5 * time.Second, "+timeoutHandled)
		})
	}
}

func Test_MigrateTimeoutConfig_ErrorHandlersThatAreNotTheApplications(t *testing.T) {
	t.Parallel()

	res := mw1RunFiles(t, v3.MigrateTimeoutConfig, map[string]string{
		// the config of another middleware, and the config of an application in a test
		"internal/app/app.go": `package app

import (
	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/contrib/jwt"
)

func New() *fiber.App {
	app := fiber.New(fiber.Config{AppName: "api"})
	app.Use(jwtware.New(jwtware.Config{ErrorHandler: func(c *fiber.Ctx, err error) error { return c.SendStatus(401) }}))
	return app
}
`,
		"internal/app/app_test.go": `package app

import "github.com/gofiber/fiber/v2"

var _ = fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error { return nil }})
`,
		"internal/server/routes.go": timeoutRoutes,
	})

	compact := timeoutCompact(res.files["internal/server/routes.go"])
	assert.Contains(t, compact, "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: 5 * time.Second")+")")
	assert.NotContains(t, compact, "ErrorHandler")
	// the application is made with a literal that has none: nothing more to say
	if assert.Len(t, res.notes, 1, "%v", res.notes) {
		assert.NotContains(t, res.notes[0], "ErrorHandler")
	}
}

func Test_MigrateTimeoutConfig_SaysWhenTheErrorHandlerCannotBeFound(t *testing.T) {
	t.Parallel()

	// a library of middleware: no application to look at
	res := mw1RunFiles(t, v3.MigrateTimeoutConfig, map[string]string{"internal/server/routes.go": timeoutRoutes})
	assert.Contains(t, timeoutCompact(res.files["internal/server/routes.go"]), "timeout.New(slow, "+timeoutConfig("timeout", "Timeout: 5 * time.Second")+")")
	if assert.Len(t, res.notes, 1, "%v", res.notes) {
		assert.Contains(t, res.notes[0], "no application with a default ErrorHandler could be found in this project")
		assert.Contains(t, res.notes[0], "if c.Context().Err() != nil { _ = c.App().Config().ErrorHandler(c, err) }")
	}

	// an application made from a config that is held somewhere else
	res = mw1RunFiles(t, v3.MigrateTimeoutConfig, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"

	"example.com/shared/config"
)

func main() { _ = fiber.New(config.Fiber()) }
`,
		"internal/server/routes.go": timeoutRoutes,
	})
	if assert.Len(t, res.notes, 1, "%v", res.notes) {
		assert.Contains(t, res.notes[0], "no application with a default ErrorHandler could be found in this project")
	}
}

func Test_MigrateTimeoutConfig_NamesTheErrorHandlerCallDoesNotClash(t *testing.T) {
	t.Parallel()

	// the core package is called c or err: the locals of the function take other names
	want := map[string][]string{
		"c":   {"OnTimeout: func(ctx c.Ctx) error {", "err := c.ErrRequestTimeout", "if ctx.Context().Err() != nil {", "_ = ctx.App().Config().ErrorHandler(ctx, err)", "return err"},
		"err": {"OnTimeout: func(c err.Ctx) error {", "reqErr := err.ErrRequestTimeout", "if c.Context().Err() != nil {", "_ = c.App().Config().ErrorHandler(c, reqErr)", "return reqErr"},
	}
	for alias, lines := range want {
		res := mw1RunFiles(t, v3.MigrateTimeoutConfig, map[string]string{
			"main.go": `package main

import (
	"time"

	` + alias + ` "github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

func slow(x *` + alias + `.Ctx) error { return nil }

var app = ` + alias + `.New(` + alias + `.Config{ErrorHandler: nil})

var h = timeout.New(slow, time.Second)
`,
		})
		for _, line := range lines {
			assert.Contains(t, res.files["main.go"], line, alias)
		}
	}
}
