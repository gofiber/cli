package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateViewBind_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateViewBind, []ctxCase{
		{
			name: "fiber contexts",
			in: `package main

import "github.com/gofiber/fiber/v2"

func version() string { return "1" }

func handler(c *fiber.Ctx) error {
	if err := c.Bind(fiber.Map{"version": version(), "tags": []string{"a", "b"}}); err != nil {
		return err
	}
	return c.Bind(fiber.Map{})
}

type view struct{ c *fiber.Ctx }

func (v view) bind() error { return v.c.Bind(fiber.Map{"a": 1}) }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func version() string { return "1" }

func handler(c *fiber.Ctx) error {
	if err := c.ViewBind(fiber.Map{"version": version(), "tags": []string{"a", "b"}}); err != nil {
		return err
	}
	return c.ViewBind(fiber.Map{})
}

type view struct{ c *fiber.Ctx }

func (v view) bind() error { return v.c.ViewBind(fiber.Map{"a": 1}) }
`,
			msg: "Migrating view binding helpers",
		},
		{
			name: "aliased import",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	return c.Bind(f.Map{"a": 1})
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	return c.ViewBind(f.Map{"a": 1})
}
`,
			msg: "Migrating view binding helpers",
		},
		{
			name: "other receivers and the v3 form stay",
			in: `package main

import "github.com/gofiber/fiber/v2"

type queries struct{}

func (queries) Bind(any, any) {}

func handler(c *fiber.Ctx) error {
	var q queries
	q.Bind(1, 2)
	// c.Bind(fiber.Map{}) in a comment
	_ = "c.Bind(fiber.Map{})"
	var out any
	return c.Bind().Body(&out)
}
`,
		},
		{
			name: "unknown receiver with a view map is reported",
			in: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	ctx := web.Current(c)
	return ctx.Bind(fiber.Map{"a": 1})
}
`,
			notes: []string{"main.go:11: ctx.Bind adds view variables in Fiber v2"},
		},
		{
			name: "no fiber import",
			in: `package main

func handler() {
	queries.Bind(results, &resultSlice)
}
`,
		},
	})
}
