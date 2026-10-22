package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateGenericHelpers_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name: "ParamsInt keeps its error",
			in: `package main

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

func userID(c *fiber.Ctx) (int, error) {
	return c.ParamsInt("id")
}

func pair(a int, err error) {}

func handler(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid id")
	}
	var other int
	other, err = c.ParamsInt("other")
	_, err = c.ParamsInt(strings.ToLower("ID"))
	n, _ := c.ParamsInt("n")
	var x, y = c.ParamsInt("x")
	pair(c.ParamsInt("p"))
	if v, err := c.ParamsInt("v"); err != nil {
		return err
	} else if v < 0 {
		return fiber.ErrBadRequest
	}
	_, _, _, _, _ = id, other, n, x, y
	return err
}
`,
			want: `package main

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func userID(c *fiber.Ctx) (int, error) {
	return strconv.Atoi(c.Params("id"))
}

func pair(a int, err error) {}

func handler(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid id")
	}
	var other int
	other, err = strconv.Atoi(c.Params("other"))
	_, err = strconv.Atoi(c.Params(strings.ToLower("ID")))
	n, _ := strconv.Atoi(c.Params("n"))
	var x, y = strconv.Atoi(c.Params("x"))
	pair(strconv.Atoi(c.Params("p")))
	if v, err := strconv.Atoi(c.Params("v")); err != nil {
		return err
	} else if v < 0 {
		return fiber.ErrBadRequest
	}
	_, _, _, _, _ = id, other, n, x, y
	return err
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "ParamsInt with a default has no error",
			in: `package main

import "github.com/gofiber/fiber/v2"

func userID(c *fiber.Ctx) (int, error) {
	return c.ParamsInt("id", 7)
}

func pair(a int, err error) {}

func handler(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id", 7)
	pair(c.ParamsInt("p", 1, 2))
	_ = id
	return err
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func userID(c *fiber.Ctx) (int, error) {
	return fiber.Params[int](c, "id", 7), error(nil)
}

func pair(a int, err error) {}

func handler(c *fiber.Ctx) error {
	id, err := fiber.Params[int](c, "id", 7), error(nil)
	pair(fiber.Params[int](c, "p", 1, 2), error(nil))
	_ = id
	return err
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "Query helpers",
			in: `package main

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

type handler struct{ c *fiber.Ctx }

func (h *handler) page() int { return h.c.QueryInt("page", 1) }

func (h *handler) limit() (int, bool, float64) {
	return h.c.QueryInt("limit"), h.c.QueryBool("all", true), h.c.QueryFloat("ratio")
}

func list(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	n := c.QueryInt(strings.TrimSpace(" n "), 3)
	ok := c.Status(fiber.StatusOK).QueryBool("ok")
	score := c.QueryFloat("score", 1.5)
	_, _, _, _ = page, n, ok, score
	return nil
}
`,
			want: `package main

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

type handler struct{ c *fiber.Ctx }

func (h *handler) page() int { return fiber.Query[int](h.c, "page", 1) }

func (h *handler) limit() (int, bool, float64) {
	return fiber.Query[int](h.c, "limit"), fiber.Query[bool](h.c, "all", true), fiber.Query[float64](h.c, "ratio")
}

func list(c *fiber.Ctx) error {
	page := fiber.Query[int](c, "page", 1)
	n := fiber.Query[int](c, strings.TrimSpace(" n "), 3)
	ok := fiber.Query[bool](c.Status(fiber.StatusOK), "ok")
	score := fiber.Query[float64](c, "score", 1.5)
	_, _, _, _ = page, n, ok, score
	return nil
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "calls whose results are not both used",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	c.ParamsInt("id", 0)
	_ = c.ParamsInt("id", 0)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	fiber.Params[int](c, "id", 0)
	_ = fiber.Params[int](c, "id", 0)
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "aliased imports",
			in: `package main

import (
	sc "strconv"

	f "github.com/gofiber/fiber/v2"
)

func handler(c *f.Ctx) error {
	n := sc.Itoa(1)
	id, err := c.ParamsInt("id")
	page := c.QueryInt("page", 1)
	_, _, _ = n, id, page
	return err
}
`,
			want: `package main

import (
	sc "strconv"

	f "github.com/gofiber/fiber/v2"
)

func handler(c *f.Ctx) error {
	n := sc.Itoa(1)
	id, err := sc.Atoi(c.Params("id"))
	page := f.Query[int](c, "page", 1)
	_, _, _ = n, id, page
	return err
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "the strconv import is added",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	_, err := c.ParamsInt("id")
	return err
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	_, err := strconv.Atoi(c.Params("id"))
	return err
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "the strconv import is not needed",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) int {
	return c.QueryInt("id")
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) int {
	return fiber.Query[int](c, "id")
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "default values from a slice are reported",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, defaults []int) {
	_ = c.QueryInt("id", defaults...)
	_, _ = c.ParamsInt("id", defaults...)
}
`,
			notes: []string{"main.go:6: c.QueryInt was removed in Fiber v3", "main.go:7: c.ParamsInt was removed in Fiber v3"},
		},
		{
			name: "other receivers stay",
			in: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

type reader struct{}

func (reader) QueryInt(string, ...int) int { return 0 }

func handler(c *fiber.Ctx) int {
	var r reader
	// c.QueryInt("page") was v2
	_ = "c.ParamsInt(id)"
	ctx := web.Current(c)
	return r.QueryInt("page") + ctx.QueryInt("page")
}
`,
			notes: []string{"main.go:18: ctx.QueryInt was removed in Fiber v3, but ctx is not known"},
		},
	})
}
