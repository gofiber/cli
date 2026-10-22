package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateCtxRenames_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCtxRenames, []ctxCase{
		{
			name: "Format",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Greeting struct{ Message string }

func handler(c *fiber.Ctx) error {
	if c.Query("a") == "1" {
		return c.Format("Hello, World!")
	}
	if err := c.Status(fiber.StatusCreated).Format(Greeting{Message: "created"}); err != nil {
		return err
	}
	return c.Format(fiber.Map{"message": "hello"})
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Greeting struct{ Message string }

func handler(c *fiber.Ctx) error {
	if c.Query("a") == "1" {
		return c.AutoFormat("Hello, World!")
	}
	if err := c.Status(fiber.StatusCreated).AutoFormat(Greeting{Message: "created"}); err != nil {
		return err
	}
	return c.AutoFormat(fiber.Map{"message": "hello"})
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "Protocol and Hostname",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	if c.Protocol() == "https" {
		return c.SendString(c.Protocol() + "://" + c.Hostname() + c.OriginalURL())
	}
	return c.SendString(c.Hostname())
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	if c.Scheme() == "https" {
		return c.SendString(c.Scheme() + "://" + c.Host() + c.OriginalURL())
	}
	return c.SendString(c.Host())
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "Range",
			in: `package main

import "github.com/gofiber/fiber/v2"

var data = make([]byte, 1000)

func handler(c *fiber.Ctx) error {
	size := len(data)
	var total int64 = 1000
	if _, err := c.Range(1000); err != nil {
		return err
	}
	if _, err := c.Range(size); err != nil {
		return err
	}
	if _, err := c.Range(len(data) - 1); err != nil {
		return err
	}
	if _, err := c.Range(total); err != nil {
		return err
	}
	_, err := c.Range(int64(size))
	return err
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

var data = make([]byte, 1000)

func handler(c *fiber.Ctx) error {
	size := len(data)
	var total int64 = 1000
	if _, err := c.Range(1000); err != nil {
		return err
	}
	if _, err := c.Range(int64(size)); err != nil {
		return err
	}
	if _, err := c.Range(int64(len(data) - 1)); err != nil {
		return err
	}
	if _, err := c.Range(total); err != nil {
		return err
	}
	_, err := c.Range(int64(size))
	return err
}
`,
			msg:   "Migrating context method renames",
			notes: []string{"main.go:10: c.Range takes an int64 size in Fiber v3"},
		},
		{
			name: "aliased import and fields",
			in: `package main

import f "github.com/gofiber/fiber/v2"

type req struct{ c *f.Ctx }

func (r req) url() string { return r.c.Protocol() + "://" + r.c.Hostname() }

func (r req) body() error { return r.c.Format("x") }
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

type req struct{ c *f.Ctx }

func (r req) url() string { return r.c.Scheme() + "://" + r.c.Host() }

func (r req) body() error { return r.c.AutoFormat("x") }
`,
			msg: "Migrating context method renames",
		},
		{
			name: "other receivers stay",
			in: `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

type request struct{}

func (request) Protocol() string       { return "" }
func (request) Hostname() string       { return "" }
func (request) Format(s string) string { return s }
func (request) Range(n int) int        { return n }

func handler(c *fiber.Ctx) error {
	var r request
	_ = r.Protocol() + r.Hostname() + r.Format("x")
	_ = r.Range(3)
	_ = time.Now().Format(time.RFC3339)
	// c.Protocol() and c.Hostname() are v2 methods
	_ = "c.Format(x)"
	return nil
}
`,
		},
		{
			name: "the v3 forms are left alone",
			in: `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	_ = c.Scheme() + c.Host()
	if c.Query("a") == "1" {
		return c.Format(fiber.ResFmt{MediaType: fiber.MIMETextPlain, Handler: func(c fiber.Ctx) error { return nil }})
	}
	handlers := []fiber.ResFmt{}
	_ = c.Format(handlers...)
	return c.AutoFormat("x")
}
`,
		},
	})
}
