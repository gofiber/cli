package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A redirect method used as a value, without being called, becomes a function literal with the
// signature of the v2 method that calls the builder of v3. The value is bound to its receiver where it is
// made, as Go binds a method value.
func Test_MigrateRedirectMethods_Values(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name: "a variable that is never assigned is used by the literal",
			in: `package main

import "github.com/gofiber/fiber/v2"

func apply(do func(string, ...int) error) error { return do("/x", 301) }

func handler(c *fiber.Ctx) error {
	back := c.RedirectBack
	to := c.Redirect
	_, _ = back, to
	return apply(c.Redirect)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func apply(do func(string, ...int) error) error { return do("/x", 301) }

func handler(c *fiber.Ctx) error {
	back := func(fallback string, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		return c.Redirect().Status(code).Back(fallback)
	}
	to := func(location string, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		return c.Redirect().Status(code).To(location)
	}
	_, _ = back, to
	return apply(func(location string, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		return c.Redirect().Status(code).To(location)
	})
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "RedirectToRoute takes the queries out of the params",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	route := c.RedirectToRoute
	return route("home", fiber.Map{"id": 1, "queries": map[string]string{"a": "b"}}, 301)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	route := func(routeName string, params fiber.Map, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		cfg := fiber.RedirectConfig{Params: params}
		if queries, ok := params["queries"].(map[string]string); ok {
			cfg.Queries = queries
		}
		return c.Redirect().Status(code).Route(routeName, cfg)
	}
	return route("home", fiber.Map{"id": 1, "queries": map[string]string{"a": "b"}}, 301)
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "the receiver is evaluated where the value is made",
			in: `package main

import "github.com/gofiber/fiber/v2"

var created int

func ctxOf(c *fiber.Ctx) *fiber.Ctx {
	created++
	return c
}

type server struct{ c *fiber.Ctx }

func handler(c *fiber.Ctx, s *server) error {
	back := ctxOf(c).RedirectBack
	to := s.c.Redirect
	status := c.Status(fiber.StatusTeapot).Redirect
	_ = status
	_ = to
	return back("/x")
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

var created int

func ctxOf(c *fiber.Ctx) *fiber.Ctx {
	created++
	return c
}

type server struct{ c *fiber.Ctx }

func handler(c *fiber.Ctx, s *server) error {
	back := func(c fiber.Ctx) func(fallback string, status ...int) error {
		return func(fallback string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).Back(fallback)
		}
	}(ctxOf(c))
	to := func(c fiber.Ctx) func(location string, status ...int) error {
		return func(location string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).To(location)
		}
	}(s.c)
	status := func(c fiber.Ctx) func(location string, status ...int) error {
		return func(location string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).To(location)
		}
	}(c.Status(fiber.StatusTeapot))
	_ = status
	_ = to
	return back("/x")
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a variable that is assigned again is evaluated where the value is made",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, other *fiber.Ctx) error {
	back := c.RedirectBack
	c = other
	return back("/x")
}

func loop(ctxs []*fiber.Ctx) []func(string, ...int) error {
	var fns []func(string, ...int) error
	for _, c := range ctxs {
		fns = append(fns, c.Redirect)
	}
	return fns
}

func pointer(c *fiber.Ctx) func(string, ...int) error {
	reset(&c)
	return c.Redirect
}

func reset(c **fiber.Ctx) { *c = nil }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, other *fiber.Ctx) error {
	back := func(c fiber.Ctx) func(fallback string, status ...int) error {
		return func(fallback string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).Back(fallback)
		}
	}(c)
	c = other
	return back("/x")
}

func loop(ctxs []*fiber.Ctx) []func(string, ...int) error {
	var fns []func(string, ...int) error
	for _, c := range ctxs {
		fns = append(fns, func(location string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).To(location)
		})
	}
	return fns
}

func pointer(c *fiber.Ctx) func(string, ...int) error {
	reset(&c)
	return func(c fiber.Ctx) func(location string, status ...int) error {
		return func(location string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).To(location)
		}
	}(c)
}

func reset(c **fiber.Ctx) { *c = nil }
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a context from a type switch or a channel",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(v interface{}, ch chan *fiber.Ctx) {
	switch c := v.(type) {
	case *fiber.Ctx:
		_ = c.RedirectBack
	}
	c := <-ch
	_ = c.Redirect
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(v interface{}, ch chan *fiber.Ctx) {
	switch c := v.(type) {
	case *fiber.Ctx:
		_ = func(fallback string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).Back(fallback)
		}
	}
	c := <-ch
	_ = func(c fiber.Ctx) func(location string, status ...int) error {
		return func(location string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).To(location)
		}
	}(c)
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "the names of the literal stay clear of the ones the receiver uses",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(code *fiber.Ctx, status *fiber.Ctx, location *fiber.Ctx) {
	a := code.Redirect
	b := status.RedirectBack
	c := location.RedirectToRoute
	_, _, _ = a, b, c
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(code *fiber.Ctx, status *fiber.Ctx, location *fiber.Ctx) {
	a := func(location string, status ...int) error {
		code2 := fiber.StatusFound
		if len(status) > 0 {
			code2 = status[0]
		}
		return code.Redirect().Status(code2).To(location)
	}
	b := func(fallback string, status2 ...int) error {
		code := fiber.StatusFound
		if len(status2) > 0 {
			code = status2[0]
		}
		return status.Redirect().Status(code).Back(fallback)
	}
	c := func(routeName string, params fiber.Map, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		cfg := fiber.RedirectConfig{Params: params}
		if queries, ok := params["queries"].(map[string]string); ok {
			cfg.Queries = queries
		}
		return location.Redirect().Status(code).Route(routeName, cfg)
	}
	_, _, _ = a, b, c
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "aliased import and a multi-line value",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	redirect := c.
		Redirect
	return (redirect)("/multi",
		f.StatusFound)
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	redirect := func(location string, status ...int) error {
		code := f.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		return c.Redirect().Status(code).To(location)
	}
	return (redirect)("/multi",
		f.StatusFound)
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "dot import",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	route := c.RedirectToRoute
	return route("home", Map{"id": 1})
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	route := func(routeName string, params Map, status ...int) error {
		code := StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		cfg := RedirectConfig{Params: params}
		if queries, ok := params["queries"].(map[string]string); ok {
			cfg.Queries = queries
		}
		return c.Redirect().Status(code).Route(routeName, cfg)
	}
	return route("home", Map{"id": 1})
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "method expressions take the context as their first argument",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	back := (*fiber.Ctx).RedirectBack
	if err := back(c, "/x"); err != nil {
		return err
	}
	return (*fiber.Ctx).Redirect(c, "/y", 301)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	back := func(c fiber.Ctx, fallback string, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		return c.Redirect().Status(code).Back(fallback)
	}
	if err := back(c, "/x"); err != nil {
		return err
	}
	return func(c fiber.Ctx, location string, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		return c.Redirect().Status(code).To(location)
	}(c, "/y", 301)
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a type that embeds the context hands the embedded one over",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Redirect hides the one of the embedded context.
func (c *Context) Redirect(to string) error { return c.Ctx.Redirect(to, 301) }

type Page struct{ *Context }

func (c *Context) viaValue() func(string, ...int) error { return c.RedirectBack }

func (p *Page) viaDeep() func(string, ...int) error { return p.RedirectBack }

func (p *Page) own() func(string) error { return p.Redirect }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Redirect hides the one of the embedded context.
func (c *Context) Redirect(to string) error { return c.Ctx.Redirect().Status(301).To(to) }

type Page struct{ *Context }

func (c *Context) viaValue() func(string, ...int) error {
	return func(c fiber.Ctx) func(fallback string, status ...int) error {
		return func(fallback string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).Back(fallback)
		}
	}(c.Ctx)
}

func (p *Page) viaDeep() func(string, ...int) error {
	return func(c fiber.Ctx) func(fallback string, status ...int) error {
		return func(fallback string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).Back(fallback)
		}
	}(p.Context.Ctx)
}

func (p *Page) own() func(string) error { return p.Redirect }
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a wrapper that declares the method itself keeps it",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

func (c *Context) RedirectBack(fallback string) error { return nil }

func handler(c *Context) func(string) error {
	return c.RedirectBack
}
`,
		},
		{
			name: "a receiver that is not known is reported, a field of another type is left alone",
			in: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

type router struct {
	RedirectBack func(string) error
}

func handler(c *fiber.Ctx, r router) {
	ctx := web.Current(c)
	back := ctx.RedirectBack
	own := r.RedirectBack
	redirect := ctx.Redirect
	_, _, _ = back, own, redirect
}
`,
			notes: []string{"main.go:15: ctx.RedirectBack looks like a Fiber v2 context method used as a value"},
		},
	})
}
