package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A typed getter used as a value, without being called, becomes a function literal with the
// signature of the v2 method (see ctxValueBuilder). ParamsInt keeps the error v2 returned.
func Test_MigrateGenericHelpers_Values(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name: "the typed getters as values",
			in: `package main

import "github.com/gofiber/fiber/v2"

type parser struct {
	id    func(string, ...int) (int, error)
	page  func(string, ...int) int
	ratio func(string, ...float64) float64
	flag  func(string, ...bool) bool
}

func parserOf(c *fiber.Ctx) parser {
	return parser{id: c.ParamsInt, page: c.QueryInt, ratio: c.QueryFloat, flag: c.QueryBool}
}
`,
			want: `package main

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type parser struct {
	id    func(string, ...int) (int, error)
	page  func(string, ...int) int
	ratio func(string, ...float64) float64
	flag  func(string, ...bool) bool
}

func parserOf(c *fiber.Ctx) parser {
	return parser{id: func(key string, defaultValue ...int) (int, error) {
		value, err := strconv.Atoi(c.Params(key))
		if err != nil {
			if len(defaultValue) > 0 {
				return defaultValue[0], nil
			}
			return 0, fmt.Errorf("failed to convert: %w", err)
		}
		return value, nil
	}, page: func(key string, defaultValue ...int) int {
		return fiber.Query[int](c, key, defaultValue...)
	}, ratio: func(key string, defaultValue ...float64) float64 {
		return fiber.Query[float64](c, key, defaultValue...)
	}, flag: func(key string, defaultValue ...bool) bool {
		return fiber.Query[bool](c, key, defaultValue...)
	}}
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"QueryFloat is fiber.Query[float64] now"},
		},
		{
			name: "ParamsInt keeps the error of v2, with the text in front of it",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	id := c.ParamsInt
	n, err := id("id")
	m, _ := id("id", 99)
	fmt.Println(n, m)
	return err
}
`,
			want: `package main

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	id := func(key string, defaultValue ...int) (int, error) {
		value, err := strconv.Atoi(c.Params(key))
		if err != nil {
			if len(defaultValue) > 0 {
				return defaultValue[0], nil
			}
			return 0, fmt.Errorf("failed to convert: %w", err)
		}
		return value, nil
	}
	n, err := id("id")
	m, _ := id("id", 99)
	fmt.Println(n, m)
	return err
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "aliased strconv and fmt, a multi-line value",
			in: `package main

import (
	f "fmt"
	sc "strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	get := c.
		ParamsInt
	n, _ := get("id")
	return n + len(sc.Itoa(n)) + len(f.Sprint(n))
}
`,
			want: `package main

import (
	f "fmt"
	sc "strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	get := func(key string, defaultValue ...int) (int, error) {
		value, err := sc.Atoi(c.Params(key))
		if err != nil {
			if len(defaultValue) > 0 {
				return defaultValue[0], nil
			}
			return 0, f.Errorf("failed to convert: %w", err)
		}
		return value, nil
	}
	n, _ := get("id")
	return n + len(sc.Itoa(n)) + len(f.Sprint(n))
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "dot import",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) int {
	page, id := c.QueryInt, c.ParamsInt
	n, _ := id("id")
	return page("page", 1) + n
}
`,
			want: `package main

import (
	"fmt"
	"strconv"

	. "github.com/gofiber/fiber/v2"
)

func handler(c *Ctx) int {
	page, id := func(key string, defaultValue ...int) int {
		return Query[int](c, key, defaultValue...)
	}, func(key string, defaultValue ...int) (int, error) {
		value, err := strconv.Atoi(c.Params(key))
		if err != nil {
			if len(defaultValue) > 0 {
				return defaultValue[0], nil
			}
			return 0, fmt.Errorf("failed to convert: %w", err)
		}
		return value, nil
	}
	n, _ := id("id")
	return page("page", 1) + n
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "a package name that a variable hides is imported a second time under another name",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, strconv string) {
	get := c.ParamsInt
	page := c.QueryInt
	_, _ = get, page
}
`,
			want: `package main

import (
	"fmt"
	stdstrconv "strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx, strconv string) {
	get := func(key string, defaultValue ...int) (int, error) {
		value, err := stdstrconv.Atoi(c.Params(key))
		if err != nil {
			if len(defaultValue) > 0 {
				return defaultValue[0], nil
			}
			return 0, fmt.Errorf("failed to convert: %w", err)
		}
		return value, nil
	}
	page := func(key string, defaultValue ...int) int {
		return fiber.Query[int](c, key, defaultValue...)
	}
	_, _ = get, page
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "receivers that are evaluated where the value is made",
			in: `package main

import "github.com/gofiber/fiber/v2"

type server struct{ c *fiber.Ctx }

func ctxOf(c *fiber.Ctx) *fiber.Ctx { return c }

func handler(c *fiber.Ctx, s *server) {
	page := s.c.QueryInt
	ratio := ctxOf(c).QueryFloat
	flag := c.Status(200).QueryBool
	_, _, _ = page, ratio, flag
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type server struct{ c *fiber.Ctx }

func ctxOf(c *fiber.Ctx) *fiber.Ctx { return c }

func handler(c *fiber.Ctx, s *server) {
	page := func(c fiber.Ctx) func(key string, defaultValue ...int) int {
		return func(key string, defaultValue ...int) int {
			return fiber.Query[int](c, key, defaultValue...)
		}
	}(s.c)
	ratio := func(c fiber.Ctx) func(key string, defaultValue ...float64) float64 {
		return func(key string, defaultValue ...float64) float64 {
			return fiber.Query[float64](c, key, defaultValue...)
		}
	}(ctxOf(c))
	flag := func(c fiber.Ctx) func(key string, defaultValue ...bool) bool {
		return func(key string, defaultValue ...bool) bool {
			return fiber.Query[bool](c, key, defaultValue...)
		}
	}(c.Status(200))
	_, _, _ = page, ratio, flag
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"QueryFloat is fiber.Query[float64] now"},
		},
		{
			name: "receivers that are calls of function values",
			in: `package main

import "github.com/gofiber/fiber/v2"

type getter func() *fiber.Ctx

type server struct {
	get   func() *fiber.Ctx
	named getter
}

func handler(c *fiber.Ctx, s *server, param func() *fiber.Ctx) int {
	get := func() *fiber.Ctx { return c }
	page := get().QueryInt
	ratio := s.get().QueryFloat
	flag := s.named().QueryBool
	other := param().QueryInt
	once := func() *fiber.Ctx { return c }().QueryInt
	_, _, _, _, _ = page, ratio, flag, other, once
	return get().QueryInt("page", 1)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type getter func() *fiber.Ctx

type server struct {
	get   func() *fiber.Ctx
	named getter
}

func handler(c *fiber.Ctx, s *server, param func() *fiber.Ctx) int {
	get := func() *fiber.Ctx { return c }
	page := func(c fiber.Ctx) func(key string, defaultValue ...int) int {
		return func(key string, defaultValue ...int) int {
			return fiber.Query[int](c, key, defaultValue...)
		}
	}(get())
	ratio := func(c fiber.Ctx) func(key string, defaultValue ...float64) float64 {
		return func(key string, defaultValue ...float64) float64 {
			return fiber.Query[float64](c, key, defaultValue...)
		}
	}(s.get())
	flag := func(c fiber.Ctx) func(key string, defaultValue ...bool) bool {
		return func(key string, defaultValue ...bool) bool {
			return fiber.Query[bool](c, key, defaultValue...)
		}
	}(s.named())
	other := func(c fiber.Ctx) func(key string, defaultValue ...int) int {
		return func(key string, defaultValue ...int) int {
			return fiber.Query[int](c, key, defaultValue...)
		}
	}(param())
	once := func(c fiber.Ctx) func(key string, defaultValue ...int) int {
		return func(key string, defaultValue ...int) int {
			return fiber.Query[int](c, key, defaultValue...)
		}
	}(func() *fiber.Ctx { return c }())
	_, _, _, _, _ = page, ratio, flag, other, once
	return fiber.Query[int](get(), "page", 1)
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"QueryFloat is fiber.Query[float64] now"},
		},
		{
			name: "receivers whose type comes from the results of a call, an interface or a generic function",
			in: `package main

import "github.com/gofiber/fiber/v2"

type provider interface{ Ctx() *fiber.Ctx }

type box[T any] struct{ v T }

func (b box[T]) get() T { return b.v }

func two(c *fiber.Ctx) (int, *fiber.Ctx) { return 1, c }

func first[T any](xs []T) T { return xs[0] }

func handler(c *fiber.Ctx, p provider, ch chan *fiber.Ctx, b box[*fiber.Ctx]) int {
	_, cc := two(c)
	total := cc.QueryInt("a") + p.Ctx().QueryInt("b") + b.get().QueryInt("c") + b.v.QueryInt("d")
	total += first([]*fiber.Ctx{c}).QueryInt("e")
	for x := range ch {
		total += x.QueryInt("f")
	}
	return total
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type provider interface{ Ctx() *fiber.Ctx }

type box[T any] struct{ v T }

func (b box[T]) get() T { return b.v }

func two(c *fiber.Ctx) (int, *fiber.Ctx) { return 1, c }

func first[T any](xs []T) T { return xs[0] }

func handler(c *fiber.Ctx, p provider, ch chan *fiber.Ctx, b box[*fiber.Ctx]) int {
	_, cc := two(c)
	total := fiber.Query[int](cc, "a") + fiber.Query[int](p.Ctx(), "b") + fiber.Query[int](b.get(), "c") + fiber.Query[int](b.v, "d")
	total += fiber.Query[int](first([]*fiber.Ctx{c}), "e")
	for x := range ch {
		total += fiber.Query[int](x, "f")
	}
	return total
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "method expressions take the context as their first argument",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) int {
	page := (*fiber.Ctx).QueryInt
	n, _ := (*fiber.Ctx).ParamsInt(c, "id")
	return page(c, "page") + n
}
`,
			want: `package main

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	page := func(c fiber.Ctx, key string, defaultValue ...int) int {
		return fiber.Query[int](c, key, defaultValue...)
	}
	n, _ := func(c fiber.Ctx, key string, defaultValue ...int) (int, error) {
		value, err := strconv.Atoi(c.Params(key))
		if err != nil {
			if len(defaultValue) > 0 {
				return defaultValue[0], nil
			}
			return 0, fmt.Errorf("failed to convert: %w", err)
		}
		return value, nil
	}(c, "id")
	return page(c, "page") + n
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "a wrapper that hides members hands the embedded context to the helpers",
			in: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type Context struct{ *fiber.Ctx }

func (c *Context) Params(key string, defaultValue ...string) string { return "hidden" }

func (c *Context) Query(key string, defaultValue ...string) string { return "hidden" }

func (c *Context) Redirect(to string) error { return nil }

type Page struct{ *Context }

func calls(c *Context, p *Page) (int, int, float64, bool, error) {
	id, err := c.ParamsInt("id")
	n, _ := p.ParamsInt("id", 3)
	return id + n, p.QueryInt("n", 1), c.QueryFloat("f"), p.QueryBool("b"), err
}

func values(c *Context, p *Page) (func(string, ...int) (int, error), func(string, ...int) int, int) {
	return c.ParamsInt, p.QueryInt, len(strconv.Itoa(1))
}
`,
			want: `package main

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type Context struct{ *fiber.Ctx }

func (c *Context) Params(key string, defaultValue ...string) string { return "hidden" }

func (c *Context) Query(key string, defaultValue ...string) string { return "hidden" }

func (c *Context) Redirect(to string) error { return nil }

type Page struct{ *Context }

func calls(c *Context, p *Page) (int, int, float64, bool, error) {
	id, err := strconv.Atoi(c.Ctx.Params("id"))
	if err != nil {
		id = 0
	}
	n, _ := fiber.Params[int](p.Context.Ctx, "id", 3), error(nil)
	return id + n, fiber.Query[int](p.Context.Ctx, "n", 1), fiber.Query[float64](c.Ctx, "f"), fiber.Query[bool](p.Context.Ctx, "b"), err
}

func values(c *Context, p *Page) (func(string, ...int) (int, error), func(string, ...int) int, int) {
	return func(c fiber.Ctx) func(key string, defaultValue ...int) (int, error) {
			return func(key string, defaultValue ...int) (int, error) {
				value, err := strconv.Atoi(c.Params(key))
				if err != nil {
					if len(defaultValue) > 0 {
						return defaultValue[0], nil
					}
					return 0, fmt.Errorf("failed to convert: %w", err)
				}
				return value, nil
			}
		}(c.Ctx), func(c fiber.Ctx) func(key string, defaultValue ...int) int {
			return func(key string, defaultValue ...int) int {
				return fiber.Query[int](c, key, defaultValue...)
			}
		}(p.Context.Ctx), len(strconv.Itoa(1))
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"main.go:20: ParamsInt without a default value is strconv.Atoi(c.Params(key)) now", "QueryFloat is fiber.Query[float64] now"},
		},
		{
			name: "a wrapper that declares the getter itself keeps it",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }

func (c *Context) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(c *Context) int {
	page := c.QueryInt
	return page("page") + c.QueryInt("page")
}
`,
		},
		{
			name: "a receiver that is not known is reported, a field of another type is left alone",
			in: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

type reader struct {
	QueryInt func(string) int
}

func handler(c *fiber.Ctx, r reader) {
	ctx := web.Current(c)
	page := ctx.QueryInt
	own := r.QueryInt
	_, _ = page, own
}
`,
			notes: []string{"main.go:15: ctx.QueryInt was removed in Fiber v3 and is used as a value, but ctx is not known to be a fiber context here"},
		},
	})
}
