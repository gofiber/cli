package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateRedirectMethods_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name: "Redirect",
			in: `package main

import (
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

const permanent = 301

func handler(c *fiber.Ctx) error {
	if c.Query("a") == "1" {
		return c.Redirect("/a")
	}
	if c.Query("a") == "2" {
		return c.Redirect("/b", fiber.StatusMovedPermanently)
	}
	if c.Query("a") == "3" {
		code := 307
		return c.Redirect("/c", code)
	}
	if c.Query("a") == "4" {
		return c.Redirect(fmt.Sprintf("/u/%s", c.Params("id")), http.StatusSeeOther)
	}
	return c.Redirect("/d", permanent)
}
`,
			want: `package main

import (
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

const permanent = 301

func handler(c *fiber.Ctx) error {
	if c.Query("a") == "1" {
		return c.Redirect().To("/a")
	}
	if c.Query("a") == "2" {
		return c.Redirect().Status(fiber.StatusMovedPermanently).To("/b")
	}
	if c.Query("a") == "3" {
		code := 307
		return c.Redirect().Status(code).To("/c")
	}
	if c.Query("a") == "4" {
		return c.Redirect().Status(http.StatusSeeOther).To(fmt.Sprintf("/u/%s", c.Params("id")))
	}
	return c.Redirect().Status(permanent).To("/d")
}
`,
			msg:   "Migrating redirect methods",
			notes: []string{"main.go:14: A redirect without a status answers 303 See Other"},
		},
		{
			name: "RedirectBack",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	if c.Query("a") == "1" {
		return c.RedirectBack("/")
	}
	code := 307
	if c.Query("a") == "2" {
		return c.RedirectBack("/fallback", code)
	}
	return c.RedirectBack("/", fiber.StatusFound)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	if c.Query("a") == "1" {
		return c.Redirect().Back("/")
	}
	code := 307
	if c.Query("a") == "2" {
		return c.Redirect().Status(code).Back("/fallback")
	}
	return c.Redirect().Status(fiber.StatusFound).Back("/")
}
`,
			msg:   "Migrating redirect methods",
			notes: []string{"main.go:7: A redirect without a status answers 303 See Other"},
		},
		{
			name: "RedirectToRoute",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	switch c.Query("a") {
	case "1":
		return c.RedirectToRoute("home", fiber.Map{"id": 1})
	case "2":
		return c.RedirectToRoute("home", fiber.Map{"id": 1}, fiber.StatusSeeOther)
	case "3":
		code := 307
		return c.RedirectToRoute("home", fiber.Map{}, code)
	case "4":
		return c.RedirectToRoute("home", nil)
	case "5":
		return c.RedirectToRoute("home", map[string]interface{}{"id": 1, "x": "y"}, 301)
	}
	return c.RedirectToRoute("home", fiber.Map{"id": 1, "queries": map[string]string{"a": "b"}})
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	switch c.Query("a") {
	case "1":
		return c.Redirect().Route("home", fiber.RedirectConfig{Params: fiber.Map{"id": 1}})
	case "2":
		return c.Redirect().Status(fiber.StatusSeeOther).Route("home", fiber.RedirectConfig{Params: fiber.Map{"id": 1}})
	case "3":
		code := 307
		return c.Redirect().Status(code).Route("home", fiber.RedirectConfig{Params: fiber.Map{}})
	case "4":
		return c.Redirect().Route("home")
	case "5":
		return c.Redirect().Status(301).Route("home", fiber.RedirectConfig{Params: map[string]interface{}{"id": 1, "x": "y"}})
	}
	return c.Redirect().Route("home", fiber.RedirectConfig{Params: fiber.Map{"id": 1}, Queries: map[string]string{"a": "b"}})
}
`,
			msg:   "Migrating redirect methods",
			notes: []string{"main.go:8: A redirect without a status answers 303 See Other"},
		},
		{
			name: "params that are not a literal may hold queries",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, params fiber.Map) error {
	return c.RedirectToRoute("home", params)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, params fiber.Map) error {
	return c.Redirect().Route("home", fiber.RedirectConfig{Params: params})
}
`,
			msg:   "Migrating redirect methods",
			notes: []string{"main.go:6: RedirectToRoute took a \"queries\" entry", "main.go:6: A redirect without a status answers 303"},
		},
		{
			name: "aliased import",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	return c.RedirectToRoute("home", f.Map{"id": 1}, f.StatusFound)
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	return c.Redirect().Status(f.StatusFound).Route("home", f.RedirectConfig{Params: f.Map{"id": 1}})
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "struct fields and acquired contexts",
			in: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type req struct{ c *fiber.Ctx }

func (r *req) redirect() error { return r.c.Redirect("/x", 301) }
func (r *req) back() error     { return r.c.RedirectBack("/", 302) }

func TestRedirect(t *testing.T) {
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	_ = ctx.Redirect("/x", fiber.StatusFound)
}
`,
			want: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type req struct{ c *fiber.Ctx }

func (r *req) redirect() error { return r.c.Redirect().Status(301).To("/x") }
func (r *req) back() error     { return r.c.Redirect().Status(302).Back("/") }

func TestRedirect(t *testing.T) {
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	_ = ctx.Redirect().Status(fiber.StatusFound).To("/x")
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "comments between the arguments are kept",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.Redirect( // where to
		"/new", // the target
		fiber.StatusMovedPermanently, // the status
	)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.Redirect().Status(fiber.StatusMovedPermanently).To("/new") /* where to */ /* the target */ /* the status */
}
`,
			msg: "Migrating redirect methods",
		},
		{
			name: "other receivers stay",
			in: `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

type router struct{}

func (router) Redirect(to string, code int) {}
func (router) RedirectBack(fallback string) {}

func handler(c *fiber.Ctx, w http.ResponseWriter, r *http.Request) error {
	var rt router
	rt.Redirect("/x", 301)
	rt.RedirectBack("/")
	http.Redirect(w, r, "/x", 301)
	// c.Redirect("/x") in a comment
	_ = "c.RedirectBack(\"/\")"
	return c.Redirect().To("/already")
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
	if err := ctx.Redirect("/x"); err != nil {
		return err
	}
	return ctx.RedirectBack("/")
}
`,
			notes: []string{"main.go:14: ctx.RedirectBack looks like a Fiber v2 context method"},
		},
		{
			name: "calls that cannot be rewritten are reported",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, codes []int) error {
	if c.Query("a") == "1" {
		return c.Redirect("/x", codes...)
	}
	return c.Redirect("/x", 301, 302)
}
`,
			notes: []string{"main.go:7: c.Redirect was called with more arguments", "main.go:9: c.Redirect was called with more arguments"},
		},
	})
}
