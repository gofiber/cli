package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// An interface that mirrors the context of v2 stops being implemented by the Ctx of v3 when
// it asks for a method v3 removed. The context goes in a LegacyCtx where the code hands it to
// the interface.
func Test_MigrateContextMirrors(t *testing.T) {
	t.Parallel()

	const msg = "Migrating interfaces that mirror the context"
	module := map[string]string{"go.mod": "module example.com/app\n\ngo 1.25\n"}

	ctxRunCases(t, v3.MigrateContextMirrors, []ctxCase{
		{
			name:   "a handler written against the interface",
			others: module,
			in: `package main

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

// Context is the part of the context the handlers use.
type Context interface {
	Params(key string, defaultValue ...string) string
	ParamsInt(key string, defaultValue ...int) (int, error)
	BodyParser(out interface{}) error
	Status(status int) *fiber.Ctx
	UserContext() context.Context
}

type HandlerFunc func(Context) error

func Wrap(h HandlerFunc) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return h(c)
	}
}
`,
			want: `package main

import (
	"context"

	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

// Context is the part of the context the handlers use.
type Context interface {
	Params(key string, defaultValue ...string) string
	ParamsInt(key string, defaultValue ...int) (int, error)
	BodyParser(out interface{}) error
	Status(status int) *fiber.Ctx
	UserContext() context.Context
}

type HandlerFunc func(Context) error

func Wrap(h HandlerFunc) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return h(fibermigrate.LegacyCtx{Ctx: c})
	}
}
`,
			msg:   msg,
			notes: []string{"main.go:10: Context declares methods of the context of Fiber v2 that v3 removed or changed (BodyParser, ParamsInt, UserContext)"},
		},
		{
			name:   "wherever a context is handed to the interface",
			others: module,
			in: `package main

import "github.com/gofiber/fiber/v2"

type Binder interface {
	BodyParser(out any) error
}

type server struct {
	binder  Binder
	binders []Binder
}

func parse(b Binder) error { return b.BodyParser(nil) }

func (s *server) use(b Binder) {}

func variadic(bs ...Binder) {}

func result(c *fiber.Ctx) Binder { return c }

func two(c *fiber.Ctx) (int, Binder) { return 0, c.Status(200) }

func handler(c *fiber.Ctx, s *server, f func(Binder)) error {
	_ = parse(c)
	s.use(c)
	f(c)
	variadic(c, c)
	var b Binder = c
	b = c
	_ = Binder(c)
	s.binder = c
	_ = server{binder: c, binders: []Binder{c, c.Status(200)}}
	_ = map[string]Binder{"a": c}
	ch := make(chan Binder, 1)
	ch <- c
	_ = b
	return nil
}
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

type Binder interface {
	BodyParser(out any) error
}

type server struct {
	binder  Binder
	binders []Binder
}

func parse(b Binder) error { return b.BodyParser(nil) }

func (s *server) use(b Binder) {}

func variadic(bs ...Binder) {}

func result(c *fiber.Ctx) Binder { return fibermigrate.LegacyCtx{Ctx: c} }

func two(c *fiber.Ctx) (int, Binder) { return 0, fibermigrate.LegacyCtx{Ctx: c.Status(200)} }

func handler(c *fiber.Ctx, s *server, f func(Binder)) error {
	_ = parse(fibermigrate.LegacyCtx{Ctx: c})
	s.use(fibermigrate.LegacyCtx{Ctx: c})
	f(fibermigrate.LegacyCtx{Ctx: c})
	variadic(fibermigrate.LegacyCtx{Ctx: c}, fibermigrate.LegacyCtx{Ctx: c})
	var b Binder = fibermigrate.LegacyCtx{Ctx: c}
	b = fibermigrate.LegacyCtx{Ctx: c}
	_ = Binder(fibermigrate.LegacyCtx{Ctx: c})
	s.binder = fibermigrate.LegacyCtx{Ctx: c}
	_ = server{binder: fibermigrate.LegacyCtx{Ctx: c}, binders: []Binder{fibermigrate.LegacyCtx{Ctx: c}, fibermigrate.LegacyCtx{Ctx: c.Status(200)}}}
	_ = map[string]Binder{"a": fibermigrate.LegacyCtx{Ctx: c}}
	ch := make(chan Binder, 1)
	ch <- fibermigrate.LegacyCtx{Ctx: c}
	_ = b
	return nil
}
`,
			msg:   msg,
			notes: []string{"main.go:5: Binder declares methods of the context of Fiber v2 that v3 removed or changed (BodyParser)"},
		},
		{
			name:   "an interface with what v3 has, with what the context never had, and with another signature",
			others: module,
			in: `package main

import "github.com/gofiber/fiber/v2"

type Reader interface {
	Params(key string, defaultValue ...string) string
	Query(key string, defaultValue ...string) string
}

type Own interface {
	ParamsInt(key string, defaultValue ...int) (int, error)
	User() string
}

type Short interface {
	ParamsInt(key string) (int, error)
}

func read(r Reader)  {}
func own(o Own)      {}
func short(s Short)  {}

func handler(c *fiber.Ctx) {
	read(c)
	own(nil)
	short(nil)
}
`,
		},
		{
			name:   "an interface embedded in another, a name of the package for the context",
			others: module,
			in: `package main

import f "github.com/gofiber/fiber/v2"

type Params interface {
	ParamsInt(key string, defaultValue ...int) (int, error)
	QueryInt(key string, defaultValue ...int) int
}

type Handler interface {
	Params
	Query(key string, defaultValue ...string) string
	JSON(data any, ctype ...string) error
}

func serve(h Handler) error { return h.JSON(nil) }

func page(c *f.Ctx) error { return serve(c) }
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	f "github.com/gofiber/fiber/v2"
)

type Params interface {
	ParamsInt(key string, defaultValue ...int) (int, error)
	QueryInt(key string, defaultValue ...int) int
}

type Handler interface {
	Params
	Query(key string, defaultValue ...string) string
	JSON(data any, ctype ...string) error
}

func serve(h Handler) error { return h.JSON(nil) }

func page(c *f.Ctx) error { return serve(fibermigrate.LegacyCtx{Ctx: c}) }
`,
			msg:   msg,
			notes: []string{"main.go:10: Handler declares methods of the context of Fiber v2 that v3 removed or changed (ParamsInt, QueryInt)"},
		},
		{
			name:   "a method that v3 has with another signature cannot be added",
			others: module,
			in: `package main

import "github.com/gofiber/fiber/v2"

type Redirecter interface {
	Redirect(location string, status ...int) error
	ParamsInt(key string, defaultValue ...int) (int, error)
}

func serve(r Redirecter) error { return r.Redirect("/") }

func page(c *fiber.Ctx) error { return serve(c) }
`,
			notes: []string{
				"main.go:12: c is given to Redirecter, which a fiber.Ctx of Fiber v3 does not implement (it has no ParamsInt, Redirect): The changed methods (Redirect) cannot be added",
				"main.go:5: Redirecter declares methods of the context of Fiber v2 that v3 removed or changed (ParamsInt, Redirect)",
			},
		},
		{
			name:   "a type that embeds the context is none to wrap",
			others: module,
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct {
	*fiber.Ctx
	User string
}

type Binder interface {
	BodyParser(out any) error
}

func serve(b Binder) error { return b.BodyParser(nil) }

func page(c *Context) error { return serve(c) }
`,
			notes: []string{
				"main.go:16: c embeds the fiber context and is given to Binder, which the Ctx of Fiber v3 does not implement (it has no BodyParser): add the methods to the type of the project",
				"main.go:10: Binder declares methods of the context of Fiber v2 that v3 removed or changed (BodyParser)",
			},
		},
		{
			name:   "a name of the function that means the package",
			others: module,
			in: `package main

import "github.com/gofiber/fiber/v2"

type Binder interface {
	BodyParser(out any) error
}

func serve(b Binder) error { return b.BodyParser(nil) }

func page(c *fiber.Ctx) error {
	fibermigrate := 1
	_ = fibermigrate
	return serve(c)
}
`,
			want: `package main

import (
	fibermigratectx "example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

type Binder interface {
	BodyParser(out any) error
}

func serve(b Binder) error { return b.BodyParser(nil) }

func page(c *fiber.Ctx) error {
	fibermigrate := 1
	_ = fibermigrate
	return serve(fibermigratectx.LegacyCtx{Ctx: c})
}
`,
			msg:   msg,
			notes: []string{"main.go:5: Binder declares methods of the context of Fiber v2 that v3 removed or changed (BodyParser)"},
		},
		{
			name: "a module cannot have the package",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Binder interface {
	BodyParser(out any) error
}

func serve(b Binder) error { return b.BodyParser(nil) }

func page(c *fiber.Ctx) error { return serve(c) }
`,
			notes: []string{
				"main.go:11: c is given to Binder, which a fiber.Ctx of Fiber v3 does not implement (it has no BodyParser): The migration wraps the context in fibermigrate.LegacyCtx",
				"main.go:5: Binder declares methods of the context of Fiber v2 that v3 removed or changed (BodyParser)",
			},
		},
	})
}

// The interface can be declared in a package that knows nothing of Fiber, and the context
// handed to it in another one.
func Test_MigrateContextMirrors_Packages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "an interface of a package that does not import Fiber",
			migrate: v3.MigrateContextMirrors,
			files: map[string]string{
				"internal/contract/contract.go": `package contract

import "context"

// Context is what the handlers know of a request.
type Context interface {
	Params(key string, defaultValue ...string) string
	ParamsInt(key string, defaultValue ...int) (int, error)
	UserContext() context.Context
}

// HandlerFunc handles a request.
type HandlerFunc func(Context) error
`,
				xpkgHandlers: `package handlers

import (
	"github.com/gofiber/fiber/v2"

	"example.com/app/internal/contract"
	"example.com/app/internal/web"
)

func Wrap(h contract.HandlerFunc) fiber.Handler {
	return func(c *fiber.Ctx) error { return h(c) }
}

func Alias(h contract.HandlerFunc, c *web.Ctx) error { return h(c) }

func Wrapped(h contract.HandlerFunc, c *web.Context) error { return h(c) }
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"github.com/gofiber/fiber/v2"

	"example.com/app/internal/contract"
	"example.com/app/internal/fibermigrate"
	"example.com/app/internal/web"
)

func Wrap(h contract.HandlerFunc) fiber.Handler {
	return func(c *fiber.Ctx) error { return h(fibermigrate.LegacyCtx{Ctx: c}) }
}

func Alias(h contract.HandlerFunc, c *web.Ctx) error { return h(fibermigrate.LegacyCtx{Ctx: c}) }

func Wrapped(h contract.HandlerFunc, c *web.Context) error { return h(c) }
`},
			msg: "Migrating interfaces that mirror the context",
			notes: []string{
				"internal/contract/contract.go:6: Context declares methods of the context of Fiber v2 that v3 removed or changed (ParamsInt, UserContext)",
				"internal/handlers/handlers.go:16: c embeds the fiber context and is given to contract.Context",
			},
		},
	})
}

func Test_MigrateContextMirrors_TheShimFile(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v2"

type Binder interface{ BodyParser(out any) error }

func serve(b Binder) error { return b.BodyParser(nil) }

func page(c *fiber.Ctx) error { return serve(c) }
`})
	assert.Contains(t, appRun(t, dir, v3.MigrateContextMirrors), "Migrating interfaces that mirror the context")

	shim := appRead(t, dir, "internal/fibermigrate/ctx.go")
	assert.Contains(t, shim, `This file was added by "fiber migrate": the methods of the context of Fiber v2 that v3 removed`)
	assert.Contains(t, shim, "package fibermigrate\n")
	assert.Contains(t, shim, "type LegacyCtx struct {\n\tfiber.Ctx\n}")
	assert.Contains(t, shim, "func (c LegacyCtx) ParamsInt(key string, defaultValue ...int) (int, error) {")
	assert.NotContains(t, shim, "ctxshim")

	// a second run leaves everything as it is
	before := appRead(t, dir, "main.go")
	assert.Empty(t, appRun(t, dir, v3.MigrateContextMirrors))
	assert.Equal(t, before, appRead(t, dir, "main.go"))
	assert.Equal(t, shim, appRead(t, dir, "internal/fibermigrate/ctx.go"))
	assert.Empty(t, internal.DrainNotes(dir)[1:], "the second run has nothing new to say")
}
