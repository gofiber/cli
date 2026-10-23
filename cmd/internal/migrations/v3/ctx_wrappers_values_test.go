package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A type that embeds the context hands the embedded one to the helpers of v3 and to the literals
// of the method values: the helpers take a fiber.Ctx, the type may declare methods that make it none, and the
// getters of v2 used the Params and Query of the context and not the ones of the type.
func Test_MigrateGenericHelpers_Wrappers(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name: "ParamsInt through a wrapper reads the parameter of the embedded context",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep) (total int, err error) {
	a, errA := p.ParamsInt("a")
	b, _ := h.ParamsInt("b")
	c, _ := d.ParamsInt("c", 3)
	return a + b + c, errA
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep) (total int, err error) {
	a, errA := strconv.Atoi(p.Params("a"))
	if errA != nil {
		a = 0
	}
	b := fiber.Params[int](h.Ctx, "b")
	c, _ := fiber.Params[int](d.Hiding.Ctx, "c", 3), error(nil)
	return a + b + c, errA
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"ParamsInt without a default value is strconv.Atoi"},
		},
		{
			name: "the typed query getters through a wrapper take the embedded context",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep) (int, int, float64, bool) {
	return p.QueryInt("a"), h.QueryInt("b", 2), d.QueryFloat("c"), d.QueryBool("d", true)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep) (int, int, float64, bool) {
	return fiber.Query[int](p.Ctx, "a"), fiber.Query[int](h.Ctx, "b", 2), fiber.Query[float64](d.Hiding.Ctx, "c"), fiber.Query[bool](d.Hiding.Ctx, "d", true)
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"QueryFloat is fiber.Query[float64] now"},
		},
		{
			name: "the getters through a wrapper that has them itself are its own",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(o *Own) (int, int) {
	n, _ := o.ParamsInt("a")
	return n, o.QueryInt("b")
}
`,
		},
		{
			name: "the getters as values through a wrapper",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep, o *Own) {
	a, b, c := p.ParamsInt, h.QueryInt, d.QueryFloat
	e, f := d.QueryBool, o.ParamsInt
	_, _, _, _, _ = a, b, c, e, f
}
`,
			want: `package main

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep, o *Own) {
	a, b, c := func(key string, defaultValue ...int) (int, error) {
		value, err := strconv.Atoi(p.Params(key))
		if err != nil {
			if len(defaultValue) > 0 {
				return defaultValue[0], nil
			}
			return 0, fmt.Errorf("failed to convert: %w", err)
		}
		return value, nil
	}, func(c fiber.Ctx) func(key string, defaultValue ...int) int {
		return func(key string, defaultValue ...int) int {
			return fiber.Query[int](c, key, defaultValue...)
		}
	}(h.Ctx), func(c fiber.Ctx) func(key string, defaultValue ...float64) float64 {
		return func(key string, defaultValue ...float64) float64 {
			return fiber.Query[float64](c, key, defaultValue...)
		}
	}(d.Hiding.Ctx)
	e, f := func(c fiber.Ctx) func(key string, defaultValue ...bool) bool {
		return func(key string, defaultValue ...bool) bool {
			return fiber.Query[bool](c, key, defaultValue...)
		}
	}(d.Hiding.Ctx), o.ParamsInt
	_, _, _, _, _ = a, b, c, e, f
}
`,
			msg:   "Migrating generic helpers",
			notes: []string{"QueryFloat is fiber.Query[float64] now"},
		},
		{
			name: "the getters through a wrapper in a value that is called at once",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(h *Hiding, defaults []int) (int, error) {
	return h.ParamsInt("id", defaults...)
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(h *Hiding, defaults []int) (int, error) {
	return func() (int, error) {
		value, err := strconv.Atoi(h.Ctx.Params("id"))
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
			notes: []string{"ParamsInt without a default value is strconv.Atoi"},
		},
	})
}

func Test_MigrateRedirectMethods_WrapperValues(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name: "Redirect, Range and AllParams through wrappers",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep, o *Own) {
	a, b := p.RedirectBack, h.RedirectBack
	c, e := d.RedirectToRoute, o.RedirectBack
	_, _, _, _ = a, b, c, e
	_ = h.RedirectBack("/x")
	_ = d.RedirectBack("/x", 301)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep, o *Own) {
	a, b := func(fallback string, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		return p.Redirect().Status(code).Back(fallback)
	}, func(c fiber.Ctx) func(fallback string, status ...int) error {
		return func(fallback string, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			return c.Redirect().Status(code).Back(fallback)
		}
	}(h.Ctx)
	c, e := func(c fiber.Ctx) func(routeName string, params fiber.Map, status ...int) error {
		return func(routeName string, params fiber.Map, status ...int) error {
			code := fiber.StatusFound
			if len(status) > 0 {
				code = status[0]
			}
			cfg := fiber.RedirectConfig{Params: params}
			if queries, ok := params["queries"].(map[string]string); ok {
				cfg.Queries = queries
			}
			return c.Redirect().Status(code).Route(routeName, cfg)
		}
	}(d.Hiding.Ctx), func(fallback string, status ...int) error {
		code := fiber.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		return o.Redirect().Status(code).Back(fallback)
	}
	_, _, _, _ = a, b, c, e
	_ = h.Ctx.Redirect().Status(fiber.StatusFound).Back("/x")
	_ = d.Hiding.Ctx.Redirect().Status(301).Back("/x")
}
`,
			msg: "Migrating redirect methods",
		},
	})
}

func Test_MigrateCtxRenames_WrapperValues(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCtxRenames, []ctxCase{
		{
			name: "Range and the parsers through wrappers",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep) {
	a, b, c := p.Range, h.Range, d.Range
	_, _, _ = a, b, c
	_, _ = h.Range(10)
	proto, host := h.Protocol, d.Hostname
	_, _ = proto, host
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep) {
	a, b, c := func(size int) (fiber.Range, error) {
		return p.Range(int64(size))
	}, func(size int) (fiber.Range, error) {
		return h.Range(int64(size))
	}, func(size int) (fiber.Range, error) {
		return d.Range(int64(size))
	}
	_, _, _ = a, b, c
	_, _ = h.Range(10)
	proto, host := h.Scheme, d.Host
	_, _ = proto, host
}
`,
			msg: "Migrating context method renames",
		},
	})
}

func Test_MigrateParserMethods_WrapperValues(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateParserMethods, []ctxCase{
		{
			name: "AllParams and the parsers as values through wrappers",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep) {
	a, b, c := p.AllParams, h.AllParams, d.AllParams
	parse, query := h.BodyParser, d.QueryParser
	_, _, _, _, _ = a, b, c, parse, query
	_ = h.AllParams()
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Plain struct {
	*fiber.Ctx
	user string
}

type Hiding struct{ *fiber.Ctx }

func (h *Hiding) Params(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) Query(key string, defaultValue ...string) string { return "hidden" }

func (h *Hiding) App() *fiber.App { return nil }

func (h *Hiding) Redirect(to string) error { return nil }

func (h *Hiding) Bind(v any) error { return nil }

type Deep struct{ *Hiding }

type Own struct{ *fiber.Ctx }

func (o *Own) ParamsInt(key string, defaultValue ...int) (int, error) { return 0, nil }

func (o *Own) QueryInt(key string, defaultValue ...int) int { return 0 }

func handler(p *Plain, h *Hiding, d *Deep) {
	a, b, c := func(c fiber.Ctx) func() map[string]string {
		return func() map[string]string {
			params := make(map[string]string, len(c.Route().Params))
			for _, name := range c.Route().Params {
				params[name] = c.Params(name)
			}
			return params
		}
	}(p.Ctx), func(c fiber.Ctx) func() map[string]string {
		return func() map[string]string {
			params := make(map[string]string, len(c.Route().Params))
			for _, name := range c.Route().Params {
				params[name] = c.Params(name)
			}
			return params
		}
	}(h.Ctx), func(c fiber.Ctx) func() map[string]string {
		return func() map[string]string {
			params := make(map[string]string, len(c.Route().Params))
			for _, name := range c.Route().Params {
				params[name] = c.Params(name)
			}
			return params
		}
	}(d.Hiding.Ctx)
	parse, query := h.Ctx.Bind().Body, d.Hiding.Ctx.Bind().Query
	_, _, _, _, _ = a, b, c, parse, query
	_ = func(c fiber.Ctx) map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	}(h.Ctx)
}
`,
			msg: "Migrating parser methods",
		},
	})
}
