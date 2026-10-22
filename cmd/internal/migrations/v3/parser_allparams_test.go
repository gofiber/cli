package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// AllParams used as a value, without calling it, is a func() map[string]string.
func Test_MigrateParserMethods_AllParamsAsValue(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateParserMethods, []ctxCase{
		{
			name: "a context in a variable is used by the function as it is",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func render(params func() map[string]string) {}

func handler(c *f.Ctx) error {
	render(c.AllParams)
	all := (c.AllParams)
	_ = all()
	return nil
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func render(params func() map[string]string) {}

func handler(c *f.Ctx) error {
	render(func() map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	})
	all := (func() map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	})
	_ = all()
	return nil
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "the variables of the function do not hide the context",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(params *fiber.Ctx, name *fiber.Ctx) {
	a := params.AllParams
	b := name.AllParams
	_, _ = a, b
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(params *fiber.Ctx, name *fiber.Ctx) {
	a := func() map[string]string {
		params2 := make(map[string]string, len(params.Route().Params))
		for _, name := range params.Route().Params {
			params2[name] = params.Params(name)
		}
		return params2
	}
	b := func() map[string]string {
		params := make(map[string]string, len(name.Route().Params))
		for _, name2 := range name.Route().Params {
			params[name2] = name.Params(name2)
		}
		return params
	}
	_, _ = a, b
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "any other receiver is evaluated where the method value was",
			in: `package main

import "github.com/gofiber/fiber/v2"

type handler struct{ c *fiber.Ctx }

func (h *handler) all() func() map[string]string {
	return h.c.AllParams
}

func other(c *fiber.Ctx) func() map[string]string {
	return c.Status(200).AllParams
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type handler struct{ c *fiber.Ctx }

func (h *handler) all() func() map[string]string {
	return func(c fiber.Ctx) func() map[string]string {
		return func() map[string]string {
			params := make(map[string]string, len(c.Route().Params))
			for _, name := range c.Route().Params {
				params[name] = c.Params(name)
			}
			return params
		}
	}(h.c)
}

func other(c *fiber.Ctx) func() map[string]string {
	return func(c fiber.Ctx) func() map[string]string {
		return func() map[string]string {
			params := make(map[string]string, len(c.Route().Params))
			for _, name := range c.Route().Params {
				params[name] = c.Params(name)
			}
			return params
		}
	}(c.Status(200))
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "a wrapper that hides Bind hands the embedded context over",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Bind hides the one of the embedded context.
func (c *Context) Bind(v any) error { return nil }

func (c *Context) params() map[string]string { return c.AllParams() }

func (c *Context) paramsFunc() func() map[string]string { return c.AllParams }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Bind hides the one of the embedded context.
func (c *Context) Bind(v any) error { return nil }

func (c *Context) params() map[string]string {
	return func(c fiber.Ctx) map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	}(c.Ctx)
}

func (c *Context) paramsFunc() func() map[string]string {
	return func(c fiber.Ctx) func() map[string]string {
		return func() map[string]string {
			params := make(map[string]string, len(c.Route().Params))
			for _, name := range c.Route().Params {
				params[name] = c.Params(name)
			}
			return params
		}
	}(c.Ctx)
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "a type that embeds the context hands the embedded context over",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Params hides the one of the embedded context, which AllParams of v2 used.
func (c *Context) Params(key string, defaults ...string) string { return "x" }

func (c *Context) all() map[string]string { return c.AllParams() }

type Page struct {
	*Context
	title string
}

func (p *Page) all() func() map[string]string { return p.AllParams }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Params hides the one of the embedded context, which AllParams of v2 used.
func (c *Context) Params(key string, defaults ...string) string { return "x" }

func (c *Context) all() map[string]string {
	return func(c fiber.Ctx) map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	}(c.Ctx)
}

type Page struct {
	*Context
	title string
}

func (p *Page) all() func() map[string]string {
	return func(c fiber.Ctx) func() map[string]string {
		return func() map[string]string {
			params := make(map[string]string, len(c.Route().Params))
			for _, name := range c.Route().Params {
				params[name] = c.Params(name)
			}
			return params
		}
	}(p.Context.Ctx)
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "a method value of another type stays",
			in: `package main

import "github.com/gofiber/fiber/v2"

type store struct{}

func (store) AllParams() map[string]string { return nil }

func handler(c *fiber.Ctx, s store) {
	all := s.AllParams
	_ = all
}
`,
		},
	})
}
