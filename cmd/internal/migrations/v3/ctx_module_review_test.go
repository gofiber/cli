package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Review_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.25\n",
		"internal/web/web.go": `package web

import "github.com/gofiber/fiber/v3"

type Context struct{ fiber.Ctx }

type Ctx = fiber.Ctx

type Plain struct{}

func (Plain) QueryInt(string) int { return 0 }

func (Plain) SendFile(string) error { return nil }

func (Plain) Redirect(string) error { return nil }
`,
		"internal/handlers/handlers.go": `package handlers

import "example.com/app/internal/web"

func page(c *web.Context, a web.Ctx, p web.Plain) int {
	return c.QueryInt("a") + a.QueryInt("b") + p.QueryInt("c")
}

func files(c *web.Context, a web.Ctx, p web.Plain, name string) error {
	_ = p.SendFile(name)
	_ = p.Redirect(name)
	_ = a.Redirect(name)
	return c.SendFile(name)
}
`,
		"internal/mw/mw.go": `package mw

import (
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"example.com/app/internal/web"
)

var _ = requestid.New

func n(c *web.Context) int { return c.QueryInt("n") }
`,
		"internal/other/other.go": `package other

type reader struct{}

func (reader) QueryInt(string) int { return 0 }

func (reader) SendFile(string) error { return nil }

func read(r reader, name string) int {
	_ = r.SendFile(name)
	return r.QueryInt("a")
}
`,
	})

	// the wrapper is told how to call the replacement, the alias is the context; a file that
	// imports a middleware but not the core package is looked at too, and the methods of types
	// that merely have the names of the context's are left alone
	assert.ElementsMatch(t, []string{
		"internal/handlers/handlers.go:6: QueryInt was removed in Fiber v3: use fiber.Query[int](c, \"name\"); " +
			"c is not a fiber.Ctx but embeds one, so if it declares a method of the replacement's name, call the replacement on c.Ctx",
		"internal/handlers/handlers.go:6: QueryInt was removed in Fiber v3: use fiber.Query[int](c, \"name\")",
		"internal/handlers/handlers.go:12: Redirect takes no arguments in Fiber v3: use c.Redirect().To(url) and Status(code)",
		"internal/handlers/handlers.go:13: SendFile takes the file name literally in Fiber v3 instead of percent-decoding it, so a name taken from a route " +
			"parameter keeps its %XX escapes unless fiber.Config{UnescapePath: true} is set; decode it and make sure it stays inside the directory you serve",
		"internal/mw/mw.go:4: requestid: the default ID is now a secure random token instead of a UUID; set Config.Generator to utils.UUIDv4 if something relies on the UUID format.",
		"internal/mw/mw.go:11: QueryInt was removed in Fiber v3: use fiber.Query[int](c, \"name\"); " +
			"c is not a fiber.Ctx but embeds one, so if it declares a method of the replacement's name, call the replacement on c.Ctx",
	}, notes)
}
