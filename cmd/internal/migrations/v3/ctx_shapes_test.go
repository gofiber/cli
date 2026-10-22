package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateRedirectMethods_QueriesEntry(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name: "params that hold nothing but the queries",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.RedirectToRoute("home", fiber.Map{"queries": map[string]string{"a": "b"}}, 301)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.Redirect().Status(301).Route("home", fiber.RedirectConfig{Queries: map[string]string{"a": "b"}})
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "the queries entry shares its line with another entry",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, q map[string]string) error {
	return c.RedirectToRoute("home", fiber.Map{
		"queries": q, "id": 1,
	}, 301)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, q map[string]string) error {
	return c.Redirect().Status(301).Route("home", fiber.RedirectConfig{Params: fiber.Map{
		"id": 1,
	}, Queries: q})
}
`,
			msg: "Migrating redirect methods",
		},
	})
}

func Test_MigrateRedirectMethods_Generics(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name: "a generic struct that embeds the context",
			in: `package main

import "github.com/gofiber/fiber/v2"

type typedCtx[T any] struct {
	*fiber.Ctx
	value T
}

type userCtx struct{ *typedCtx[string] }

func handler(c *typedCtx[int], u userCtx, other *plain[int]) error {
	_ = other.Redirect("/other")
	if err := u.Redirect("/user", 301); err != nil {
		return err
	}
	return c.Redirect("/x", 301)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type typedCtx[T any] struct {
	*fiber.Ctx
	value T
}

type userCtx struct{ *typedCtx[string] }

func handler(c *typedCtx[int], u userCtx, other *plain[int]) error {
	_ = other.Redirect("/other")
	if err := u.Redirect().Status(301).To("/user"); err != nil {
		return err
	}
	return c.Redirect().Status(301).To("/x")
}
`,
			msg: "Migrating redirect methods",
		},
	})
}

func Test_MigrateGenericHelpers_Closures(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name: "the results of a function literal are the ones of the call",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	id := func() (int, error) {
		return c.ParamsInt("id")
	}
	page := func() (int, error) {
		return c.ParamsInt("page", 1)
	}
	one := func() int {
		return c.QueryInt("one")
	}
	_, _, _ = id, page, one
	return nil
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	id := func() (int, error) {
		return strconv.Atoi(c.Params("id"))
	}
	page := func() (int, error) {
		return fiber.Params[int](c, "page", 1), error(nil)
	}
	one := func() int {
		return fiber.Query[int](c, "one")
	}
	_, _, _ = id, page, one
	return nil
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"ParamsInt without a default value"},
		},
	})
}

func Test_MigrateCtxRenames_RangeArguments(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCtxRenames, []ctxCase{
		{
			name: "a size that is converted already",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, size int) error {
	_, err := c.Range((int64(size)))
	return err
}
`,
		},
		{
			name: "a size in parentheses",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, size int) error {
	_, err := c.Range((size))
	return err
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, size int) error {
	_, err := c.Range(int64((size)))
	return err
}
`,
			msg: "Migrating context method renames",
		},
	})
}

func Test_MigrateHandlerSignatures_Conversions(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateHandlerSignatures, []ctxCase{
		{
			name: "a method expression",
			in: `package main

import "github.com/gofiber/fiber/v2"

var next = (*fiber.Ctx).Next
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

var next = (fiber.Ctx).Next
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "a nil pointer handed to another function",
			in: `package main

import "github.com/gofiber/fiber/v2"

func init() { registry.Register((*fiber.Ctx)(nil)) }
`,
			notes: []string{"(*fiber.Ctx)(nil) is a nil pointer to the Ctx interface now"},
		},
		{
			name: "a comment after the star",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c */* the context */ fiber.Ctx) error { return nil }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c /* the context */ fiber.Ctx) error { return nil }
`,
			msg: "Migrating handler signatures",
		},
	})
}

func Test_MigrateViewBind_Conversions(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateViewBind, []ctxCase{
		{
			name: "a conversion to fiber.Map hands over view variables",
			in: `package main

import "github.com/gofiber/fiber/v2"

func render(w writer, m map[string]any) error {
	return w.Bind(fiber.Map(m))
}
`,
			notes: []string{"w.Bind adds view variables in Fiber v2 and is called ViewBind in v3"},
		},
		{
			name: "another call is no sign of view variables",
			in: `package main

func render(w writer, m map[string]any) error {
	return w.Bind(build(m))
}
`,
		},
	})
}

func Test_MigrateParserMethods_UnknownAllParams(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateParserMethods, []ctxCase{
		{
			name: "AllParams of a receiver that is not known",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(w writer) map[string]string {
	return w.AllParams()
}

var _ = fiber.New
`,
			notes: []string{"w.AllParams looks like a Fiber v2 context method, but w is not known to be a fiber context here, so it was left as is: in v3 use a loop over Route().Params that calls Params"},
		},
	})
}
