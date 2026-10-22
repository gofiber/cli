package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateReqHeaderParser_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateReqHeaderParser, []ctxCase{
		{
			name: "fiber contexts",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Headers struct {
	Token string §header:"X-Token"§
}

type req struct{ c *fiber.Ctx }

func (r req) headers(h *Headers) error { return r.c.ReqHeaderParser(h) }

func handler(c *fiber.Ctx) error {
	var h Headers
	if err := c.ReqHeaderParser(&h); err != nil {
		return err
	}
	return c.Status(fiber.StatusOK).ReqHeaderParser(&h)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Headers struct {
	Token string §header:"X-Token"§
}

type req struct{ c *fiber.Ctx }

func (r req) headers(h *Headers) error { return r.c.Bind().Header(h) }

func handler(c *fiber.Ctx) error {
	var h Headers
	if err := c.Bind().Header(&h); err != nil {
		return err
	}
	return c.Status(fiber.StatusOK).Bind().Header(&h)
}
`,
			msg: "Migrating request header parser helper",
		},
		{
			name: "aliased import",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	var v any
	return c.ReqHeaderParser(&v)
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	var v any
	return c.Bind().Header(&v)
}
`,
			msg: "Migrating request header parser helper",
		},
		{
			name: "strings, comments and other types stay",
			in: `package main

import "github.com/gofiber/fiber/v2"

type parser struct{}

func (parser) ReqHeaderParser(any) error { return nil }

func handler(c *fiber.Ctx) error {
	// c.ReqHeaderParser(&h) was the v2 way
	_ = "c.ReqHeaderParser(&h)"
	var p parser
	return p.ReqHeaderParser(nil)
}
`,
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
	return ctx.ReqHeaderParser(nil)
}
`,
			notes: []string{"main.go:11: ctx.ReqHeaderParser looks like a Fiber v2 context method"},
		},
	})
}
