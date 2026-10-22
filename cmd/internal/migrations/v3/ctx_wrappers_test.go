package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A type that embeds the context and declares a member of the name a rewrite
// introduces hides it from calls on the type, so the rewritten call goes through
// the embedded context: c.Ctx.Redirect().Back(...) where c.Redirect().Back(...)
// would call the method of the wrapper. The members of the wrapper itself stay.
func Test_MigrateRedirectMethods_WrapperThatHidesRedirect(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name: "calls through the embedded context",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Redirect hides the one of the embedded context.
func (c *Context) Redirect(to string) error { return c.Ctx.Redirect(to, 301) }

func (c *Context) Back() error { return c.RedirectBack("/") }

func (c *Context) Home() error {
	return c.RedirectToRoute("home", fiber.Map{"id": 1}, 301)
}

func handler(c *Context) error {
	return c.Redirect("/x")
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Redirect hides the one of the embedded context.
func (c *Context) Redirect(to string) error { return c.Ctx.Redirect().Status(301).To(to) }

func (c *Context) Back() error { return c.Ctx.Redirect().Status(fiber.StatusFound).Back("/") }

func (c *Context) Home() error {
	return c.Ctx.Redirect().Status(301).Route("home", fiber.RedirectConfig{Params: fiber.Map{"id": 1}})
}

func handler(c *Context) error {
	return c.Redirect("/x")
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a wrapper without the hiding method needs no detour",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

func (c *Context) Back() error { return c.RedirectBack("/") }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

func (c *Context) Back() error { return c.Redirect().Status(fiber.StatusFound).Back("/") }
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a wrapper of a wrapper goes down to the context",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

func (c *Context) Redirect(to string) error { return c.Ctx.Redirect(to, 301) }

type Page struct{ *Context }

func (p *Page) Back() error { return p.RedirectBack("/") }

func (p *Page) Own() error { return p.Redirect("/own") }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

func (c *Context) Redirect(to string) error { return c.Ctx.Redirect().Status(301).To(to) }

type Page struct{ *Context }

func (p *Page) Back() error { return p.Context.Ctx.Redirect().Status(fiber.StatusFound).Back("/") }

func (p *Page) Own() error { return p.Redirect("/own") }
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "a wrapper that holds the context in a named field is no wrapper",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ ctx *fiber.Ctx }

func (c *Context) Redirect(to string) error { return c.ctx.Redirect(to, 301) }

func (c *Context) Back() error { return c.ctx.RedirectBack("/", 301) }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ ctx *fiber.Ctx }

func (c *Context) Redirect(to string) error { return c.ctx.Redirect().Status(301).To(to) }

func (c *Context) Back() error { return c.ctx.Redirect().Status(301).Back("/") }
`,
			msg: "Migrating redirect methods",
		},
	})
}

func Test_MigrateParserMethods_WrapperThatHidesBind(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateParserMethods, []ctxCase{
		{
			name: "calls through the embedded context",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Bind hides the one of the embedded context.
func (c *Context) Bind(v any) error {
	if err := c.QueryParser(v); err != nil {
		return err
	}
	return c.BodyParser(v)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Bind hides the one of the embedded context.
func (c *Context) Bind(v any) error {
	if err := c.Ctx.Bind().Query(v); err != nil {
		return err
	}
	return c.Ctx.Bind().Body(v)
}
`,
			msg: "Migrating parser methods",
		},
	})
}

func Test_MigrateCtxRenames_WrapperThatHidesTheNewName(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCtxRenames, []ctxCase{
		{
			name: "calls through the embedded context",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Scheme is the wrapper's own, and says something else.
func (c *Context) Scheme() string { return "custom" }

func (c *Context) Proto() string { return c.Protocol() }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

// Scheme is the wrapper's own, and says something else.
func (c *Context) Scheme() string { return "custom" }

func (c *Context) Proto() string { return c.Ctx.Scheme() }
`,
			msg: "Migrating context method renames",
		},
	})
}
