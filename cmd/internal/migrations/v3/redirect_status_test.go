package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// v2 answers a redirect with 302 Found unless it gets a status, and it uses the
// first status only. v3 answers 303 See Other, so the status is spelled out, and
// the statuses that v2 ignored are not carried over.
func Test_MigrateRedirectMethods_Statuses(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name: "no status gets the one v2 answered",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	if err := c.Redirect("/a"); err != nil {
		return err
	}
	if err := c.RedirectBack("/b"); err != nil {
		return err
	}
	return c.RedirectToRoute("home", f.Map{"id": 1})
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	if err := c.Redirect().Status(f.StatusFound).To("/a"); err != nil {
		return err
	}
	if err := c.Redirect().Status(f.StatusFound).Back("/b"); err != nil {
		return err
	}
	return c.Redirect().Status(f.StatusFound).Route("home", f.RedirectConfig{Params: f.Map{"id": 1}})
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "the statuses after the first are left out",
			in: `package main

import "github.com/gofiber/fiber/v2"

const permanent = 301

func handler(c *fiber.Ctx) error {
	if err := c.Redirect("/a", 301, 302); err != nil {
		return err
	}
	if err := c.RedirectBack("/b", permanent, fiber.StatusFound, int(10), len("x")); err != nil {
		return err
	}
	return c.RedirectToRoute("home", nil, 307, permanent) // moved
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

const permanent = 301

func handler(c *fiber.Ctx) error {
	if err := c.Redirect().Status(301).To("/a"); err != nil {
		return err
	}
	if err := c.Redirect().Status(permanent).Back("/b"); err != nil {
		return err
	}
	return c.Redirect().Status(307).Route("home") // moved
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "comments inside the statuses that are left out stay",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.Redirect(
		"/a",
		301,
		302, // ignored by v2
	)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.Redirect().Status(301).To("/a") /* ignored by v2 */
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "statuses that do something are still evaluated",
			in: `package main

import "github.com/gofiber/fiber/v2"

func pick() int { return 302 }

func handler(c *fiber.Ctx, next func() string) error {
	if c.Query("a") == "1" {
		return c.Redirect("/x", 301, pick())
	}
	if c.Query("a") == "2" {
		return c.RedirectBack(next(), pick())
	}
	return c.RedirectToRoute("home", fiber.Map{"id": next()}, pick(), 301, pick())
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func pick() int { return 302 }

func handler(c *fiber.Ctx, next func() string) error {
	if c.Query("a") == "1" {
		return func(_ ...int) error {
			return c.Redirect().Status(301).To("/x")
		}(pick())
	}
	if c.Query("a") == "2" {
		return func(fallback string, status int) error {
			return c.Redirect().Status(status).Back(fallback)
		}(next(), pick())
	}
	return func(cfg fiber.RedirectConfig, status int, _ ...int) error {
		return c.Redirect().Status(status).Route("home", cfg)
	}(fiber.RedirectConfig{Params: fiber.Map{"id": next()}}, pick(), 301, pick())
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a spread of statuses picks the first one",
			in: `package main

import "github.com/gofiber/fiber/v2"

func redirect(c *fiber.Ctx, to string, codes ...int) error {
	return c.Redirect(to, codes...)
}

func back(c *fiber.Ctx, codes []int) error {
	return c.RedirectBack("/", codes...)
}

func route(c *fiber.Ctx, opts struct{ codes []int }) error {
	return c.RedirectToRoute(
		"home",
		fiber.Map{"id": 1, "queries": map[string]string{"a": "b"}},
		opts.codes...,
	)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func redirect(c *fiber.Ctx, to string, codes ...int) error {
	return func() error {
		status := fiber.StatusFound
		if len(codes) > 0 {
			status = codes[0]
		}
		return c.Redirect().Status(status).To(to)
	}()
}

func back(c *fiber.Ctx, codes []int) error {
	return func() error {
		status := fiber.StatusFound
		if len(codes) > 0 {
			status = codes[0]
		}
		return c.Redirect().Status(status).Back("/")
	}()
}

func route(c *fiber.Ctx, opts struct{ codes []int }) error {
	return func() error {
		status := fiber.StatusFound
		if len(opts.codes) > 0 {
			status = opts.codes[0]
		}
		return c.Redirect().Status(status).Route("home", fiber.RedirectConfig{Params: fiber.Map{"id": 1}, Queries: map[string]string{"a": "b"}})
	}()
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "operands that do something are evaluated first, in order",
			in: `package main

import "github.com/gofiber/fiber/v2"

type config struct{ url string }

func (c *config) URL() string { return c.url }

func codes() []int { return nil }

func handler(c *fiber.Ctx, status *config) error {
	if c.Query("a") == "1" {
		return c.Status(200).Redirect(status.URL(), codes()...)
	}
	return c.Redirect(status.URL(), codes()...)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type config struct{ url string }

func (c *config) URL() string { return c.url }

func codes() []int { return nil }

func handler(c *fiber.Ctx, status *config) error {
	if c.Query("a") == "1" {
		return func(c2 fiber.Ctx, to string, codes2 ...int) error {
			status2 := fiber.StatusFound
			if len(codes2) > 0 {
				status2 = codes2[0]
			}
			return c2.Redirect().Status(status2).To(to)
		}(c.Status(200), status.URL(), codes()...)
	}
	return func(to string, codes2 ...int) error {
		status2 := fiber.StatusFound
		if len(codes2) > 0 {
			status2 = codes2[0]
		}
		return c.Redirect().Status(status2).To(to)
	}(status.URL(), codes()...)
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "dot import",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx, codes []int) error {
	if c.Query("a") == "1" {
		return c.Redirect("/a")
	}
	return c.Redirect(c.Path(), codes...)
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx, codes []int) error {
	if c.Query("a") == "1" {
		return c.Redirect().Status(StatusFound).To("/a")
	}
	return func(to string) error {
		status := StatusFound
		if len(codes) > 0 {
			status = codes[0]
		}
		return c.Redirect().Status(status).To(to)
	}(c.Path())
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a wrapper that hides Redirect hands the embedded context to the literal",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

func (c *Context) Redirect(to string, codes ...int) error { return c.Ctx.Redirect(to, codes...) }

func (c *Context) Back(codes ...int) error { return c.RedirectBack("/", codes...) }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

func (c *Context) Redirect(to string, codes ...int) error {
	return func() error {
		status := fiber.StatusFound
		if len(codes) > 0 {
			status = codes[0]
		}
		return c.Ctx.Redirect().Status(status).To(to)
	}()
}

func (c *Context) Back(codes ...int) error {
	return func() error {
		status := fiber.StatusFound
		if len(codes) > 0 {
			status = codes[0]
		}
		return c.Ctx.Redirect().Status(status).Back("/")
	}()
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "the v3 forms are left alone",
			in: `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx, codes []int) error {
	if len(codes) > 0 {
		return c.Redirect().Status(codes[0]).To("/a")
	}
	return c.Redirect().To("/b")
}
`,
		},
	})
}
