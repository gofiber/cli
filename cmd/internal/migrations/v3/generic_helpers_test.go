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
	value, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return 0, err
	}
	return value, nil
}

func pair(a int, err error) {}

func handler(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid id")
	}
	var other int
	other, err = strconv.Atoi(c.Params("other"))
	if err != nil {
		other = 0
	}
	_, err = strconv.Atoi(c.Params(strings.ToLower("ID")))
	n := fiber.Params[int](c, "n")
	var x, y = strconv.Atoi(c.Params("x"))
	if y != nil {
		x = 0
	}
	pair(func() (int, error) {
		value, err := strconv.Atoi(c.Params("p"))
		if err != nil {
			return 0, err
		}
		return value, nil
	}())
	if v, err := strconv.Atoi(c.Params("v")); err != nil {
		return err
	} else if v < 0 {
		return fiber.ErrBadRequest
	}
	_, _, _, _, _ = id, other, n, x, y
	return err
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"ParamsInt without a default value"},
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
			msg:   "Migrating generic helpers",
			notes: []string{"QueryFloat is fiber.Query[float64] now"},
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
	if err != nil {
		id = 0
	}
	page := f.Query[int](c, "page", 1)
	_, _, _ = n, id, page
	return err
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"ParamsInt without a default value"},
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
			msg:   "Migrating generic helpers",
			notes: []string{"ParamsInt without a default value"},
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
			name: "default values from a slice: the getters of one value take them as they are, ParamsInt becomes a literal",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, defaults []int, fs []float64) {
	_ = c.QueryInt("id", defaults...)
	_ = c.QueryFloat("ratio", fs...)
	_, _ = c.ParamsInt("id", defaults...)
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx, defaults []int, fs []float64) {
	_ = fiber.Query[int](c, "id", defaults...)
	_ = fiber.Query[float64](c, "ratio", fs...)
	_, _ = func() (int, error) {
		value, err := strconv.Atoi(c.Params("id"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}()
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"ParamsInt without a default value", "QueryFloat is fiber.Query[float64] now"},
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

// ParamsInt without a default value keeps its error, but the text v2 put in
// front of it is gone: the project is told once, with the number of other places.
func Test_MigrateGenericHelpers_ErrorTextNote(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name: "one place",
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
			msg:   "Migrating generic helpers",
			notes: []string{`v2 put "failed to convert: " in front of it`},
		},
		{
			name: "the other places are counted",
			in: `package main

import "github.com/gofiber/fiber/v2"

func a(c *fiber.Ctx) error {
	_, err := c.ParamsInt("id")
	return err
}

func b(c *fiber.Ctx, defaults ...int) error {
	_, err := c.ParamsInt("page", defaults...)
	return err
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func a(c *fiber.Ctx) error {
	_, err := strconv.Atoi(c.Params("id"))
	return err
}

func b(c *fiber.Ctx, defaults ...int) error {
	_, err := func() (int, error) {
		value, err := strconv.Atoi(c.Params("page"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}()
	return err
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"(1 more in this project)"},
		},
		{
			name: "a default value leaves no error to change",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	_, err := c.ParamsInt("id", 7)
	return err
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	_, err := fiber.Params[int](c, "id", 7), error(nil)
	return err
}
`,
			msg: "Migrating generic helpers",
		},
	})
}
