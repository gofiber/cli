package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateParserMethods_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateParserMethods, []ctxCase{
		{
			name: "parameters, closures and chains",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	var v any
	if err := c.BodyParser(&v); err != nil {
		return err
	}
	_ = c.CookieParser(&v)
	_ = c.ParamsParser(&v)
	_ = c.QueryParser(&v)
	return c.Status(fiber.StatusCreated).BodyParser(&v)
}

func main() {
	app := fiber.New()
	app.Post("/", func(ctx *fiber.Ctx) error {
		var in struct{ Name string }
		return ctx.BodyParser(
			&in,
		)
	})
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	var v any
	if err := c.Bind().Body(&v); err != nil {
		return err
	}
	_ = c.Bind().Cookie(&v)
	_ = c.Bind().URI(&v)
	_ = c.Bind().Query(&v)
	return c.Status(fiber.StatusCreated).Bind().Body(&v)
}

func main() {
	app := fiber.New()
	app.Post("/", func(ctx *fiber.Ctx) error {
		var in struct{ Name string }
		return ctx.Bind().Body(
			&in,
		)
	})
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "struct fields, embedded and acquired contexts",
			in: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type Handler struct {
	Ctx *fiber.Ctx
	c   *fiber.Ctx
}

func (h Handler) Parse(out any) error {
	_ = h.c.QueryParser(out)
	return h.Ctx.BodyParser(out)
}

type MyCtx struct {
	*fiber.Ctx
	user string
}

func (m *MyCtx) Load(out any) error { return m.BodyParser(out) }

func TestParse(t *testing.T) {
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	var v any
	_ = ctx.BodyParser(&v)
}
`,
			want: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type Handler struct {
	Ctx *fiber.Ctx
	c   *fiber.Ctx
}

func (h Handler) Parse(out any) error {
	_ = h.c.Bind().Query(out)
	return h.Ctx.Bind().Body(out)
}

type MyCtx struct {
	*fiber.Ctx
	user string
}

func (m *MyCtx) Load(out any) error { return m.Bind().Body(out) }

func TestParse(t *testing.T) {
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	var v any
	_ = ctx.Bind().Body(&v)
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "aliased import",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	params := c.AllParams()
	_ = params
	return c.BodyParser(nil)
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	params := func(c f.Ctx) map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	}(c)
	_ = params
	return c.Bind().Body(nil)
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "strings, comments and other types stay",
			in: `package main

import "github.com/gofiber/fiber/v2"

type decoder struct{}

func (decoder) BodyParser(any) error { return nil }

func handler(c *fiber.Ctx) error {
	// c.BodyParser(&v) is the v2 way.
	msg := "call c.QueryParser(&v)"
	_ = msg
	d := decoder{}
	if err := d.BodyParser(nil); err != nil {
		return err
	}
	var other decoder
	return other.BodyParser(nil)
}

func inner(c *fiber.Ctx) error {
	run := func(c *decoder) error { return c.BodyParser(nil) }
	return run(nil)
}
`,
		},
		{
			name: "shadowed context",
			in: `package main

import "github.com/gofiber/fiber/v2"

type decoder struct{}

func (decoder) BodyParser(any) error { return nil }

func handler(c *fiber.Ctx) error {
	{
		c := &decoder{}
		_ = c.BodyParser(nil)
	}
	return c.BodyParser(nil)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type decoder struct{}

func (decoder) BodyParser(any) error { return nil }

func handler(c *fiber.Ctx) error {
	{
		c := &decoder{}
		_ = c.BodyParser(nil)
	}
	return c.Bind().Body(nil)
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "AllParams becomes the map it returned",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	params := c.AllParams() // map[string]string
	for k, v := range c.AllParams() {
		fmt.Println(k, v)
	}
	return c.JSON(params)
}
`,
			want: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	params := func(c fiber.Ctx) map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	}(c) // map[string]string
	for k, v := range func(c fiber.Ctx) map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	}(c) {
		fmt.Println(k, v)
	}
	return c.JSON(params)
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "AllParams that cannot be rewritten is reported",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	all := c.AllParams
	_ = all
	return nil
}
`,
			notes: []string{"main.go:6: AllParams does not exist in Fiber v3"},
		},
		{
			name: "unknown receiver is reported",
			in: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	ctx := web.Current(c)
	var v any
	return ctx.BodyParser(&v)
}
`,
			notes: []string{"main.go:12: ctx.BodyParser looks like a Fiber v2 context method"},
		},
		{
			name: "struct tags are left to MigrateParamsTagKeys",
			in: `package main

import "github.com/gofiber/fiber/v2"

type params struct {
	ID string §params:"id" json:"id"§
}

func handler(c *fiber.Ctx) error {
	var p params
	return c.ParamsParser(&p)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type params struct {
	ID string §params:"id" json:"id"§
}

func handler(c *fiber.Ctx) error {
	var p params
	return c.Bind().URI(&p)
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "types of the other files of the package",
			others: map[string]string{
				"context.go": `package main

import f "github.com/gofiber/fiber/v2"

type Input struct{ Name string }

// AppContext is the context handlers get.
type AppContext struct {
	*f.Ctx
	User string
}
`,
			},
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *AppContext, other *elsewhere.Ctx) error {
	var in Input
	if err := c.BodyParser(&in); err != nil {
		return err
	}
	_ = other.BodyParser(&in)
	return c.Next()
}

var _ = fiber.New
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *AppContext, other *elsewhere.Ctx) error {
	var in Input
	if err := c.Bind().Body(&in); err != nil {
		return err
	}
	_ = other.BodyParser(&in)
	return c.Next()
}

var _ = fiber.New
`,
			msg: "Migrating parser methods",
		},
		{
			name: "no fiber import",
			in: `package main

type ctx struct{}

func (ctx) BodyParser(any) {}

func handler(c ctx) {
	c.BodyParser(nil)
}
`,
		},
	})
}
