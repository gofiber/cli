package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The renamed methods used as values (c.Protocol, c.Hostname, c.Format) are renamed, and Range
// becomes a function literal with the signature of the v2 method.
func Test_MigrateCtxRenames_Values(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCtxRenames, []ctxCase{
		{
			name: "the renamed methods used as values",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) string {
	proto, host := c.Protocol, c.Hostname
	format := c.Format
	_ = format
	return proto() + host()
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) string {
	proto, host := c.Scheme, c.Host
	format := c.AutoFormat
	_ = format
	return proto() + host()
}
`,
			msg:   "Migrating context method renames",
			notes: []string{"main.go:7: AutoFormat negotiates the same formats as Format did"},
		},
		{
			name: "Range as a value takes an int and returns the ranges",
			in: `package main

import "github.com/gofiber/fiber/v2"

func sum(rangeOf func(int) (fiber.Range, error)) int {
	r, err := rangeOf(100)
	if err != nil {
		return 0
	}
	return len(r.Ranges)
}

func handler(c *fiber.Ctx) int {
	rangeOf := c.Range
	_, _ = rangeOf(10)
	return sum(c.Range)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func sum(rangeOf func(int) (fiber.Range, error)) int {
	r, err := rangeOf(100)
	if err != nil {
		return 0
	}
	return len(r.Ranges)
}

func handler(c *fiber.Ctx) int {
	rangeOf := func(size int) (fiber.Range, error) {
		return c.Range(int64(size))
	}
	_, _ = rangeOf(10)
	return sum(func(size int) (fiber.Range, error) {
		return c.Range(int64(size))
	})
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "Range receivers that are evaluated where the value is made",
			in: `package main

import "github.com/gofiber/fiber/v2"

type server struct{ c *fiber.Ctx }

func handler(s *server, c *fiber.Ctx) {
	rangeOf := s.c.Range
	other := c.Status(200).Range
	_, _ = rangeOf, other
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type server struct{ c *fiber.Ctx }

func handler(s *server, c *fiber.Ctx) {
	rangeOf := func(c fiber.Ctx) func(size int) (fiber.Range, error) {
		return func(size int) (fiber.Range, error) {
			return c.Range(int64(size))
		}
	}(s.c)
	other := func(c fiber.Ctx) func(size int) (fiber.Range, error) {
		return func(size int) (fiber.Range, error) {
			return c.Range(int64(size))
		}
	}(c.Status(200))
	_, _ = rangeOf, other
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "method expressions of the renamed methods and Range",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	proto := (*fiber.Ctx).Protocol
	host := (*fiber.Ctx).Hostname(c)
	rangeOf := (*fiber.Ctx).Range
	_, _ = rangeOf(c, 10)
	_, _ = proto(c), host
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	proto := (*fiber.Ctx).Scheme
	host := (*fiber.Ctx).Host(c)
	rangeOf := func(c fiber.Ctx, size int) (fiber.Range, error) {
		return c.Range(int64(size))
	}
	_, _ = rangeOf(c, 10)
	_, _ = proto(c), host
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "aliased import and dot import",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) {
	rangeOf, proto := c.Range, c.Protocol
	_, _ = rangeOf(10)
	_ = proto
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) {
	rangeOf, proto := func(size int) (Range, error) {
		return c.Range(int64(size))
	}, c.Scheme
	_, _ = rangeOf(10)
	_ = proto
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "fields and methods of other types are left alone",
			in: `package main

import "github.com/gofiber/fiber/v2"

type request struct {
	Protocol func() string
	Range    func(int) int
}

func handler(c *fiber.Ctx, r request, w unknown) {
	_, _ = r.Protocol, r.Range
	_, _ = w.Protocol, w.Range
	_ = c
}
`,
		},
	})
}

// AllParams used as a value becomes the function that builds the map; the parsers keep the
// binder they were made with, as Go binds a method value.
func Test_MigrateParserMethods_Values(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateParserMethods, []ctxCase{
		{
			name: "AllParams as a value",
			in: `package main

import "github.com/gofiber/fiber/v2"

func render(all func() map[string]string) {}

func handler(c *fiber.Ctx, other *fiber.Ctx) {
	all := c.AllParams
	render(c.AllParams)
	fromOther := other.AllParams
	other = c
	_, _ = all, fromOther
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func render(all func() map[string]string) {}

func handler(c *fiber.Ctx, other *fiber.Ctx) {
	all := func() map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	}
	render(func() map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	})
	fromOther := func(c fiber.Ctx) func() map[string]string {
		return func() map[string]string {
			params := make(map[string]string, len(c.Route().Params))
			for _, name := range c.Route().Params {
				params[name] = c.Params(name)
			}
			return params
		}
	}(other)
	other = c
	_, _ = all, fromOther
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "AllParams and the parsers as method expressions",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	all := (*fiber.Ctx).AllParams
	body := (*fiber.Ctx).BodyParser
	var v any
	_ = body(c, &v)
	_ = all(c)
	_ = (*fiber.Ctx).QueryParser(c, &v)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	all := func(c fiber.Ctx) map[string]string {
		params := make(map[string]string, len(c.Route().Params))
		for _, name := range c.Route().Params {
			params[name] = c.Params(name)
		}
		return params
	}
	body := func(c fiber.Ctx, out interface{}) error {
		return c.Bind().Body(out)
	}
	var v any
	_ = body(c, &v)
	_ = all(c)
	_ = func(c fiber.Ctx, out interface{}) error {
		return c.Bind().Query(out)
	}(c, &v)
}
`,
			msg: "Migrating parser methods",
		},
		{
			name: "the parsers as values keep the receiver they were made with",
			in: `package main

import "github.com/gofiber/fiber/v2"

type server struct{ c *fiber.Ctx }

func handler(c *fiber.Ctx, s *server) {
	parse := c.BodyParser
	query := s.c.QueryParser
	cookie := c.Status(200).CookieParser
	_, _, _ = parse, query, cookie
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type server struct{ c *fiber.Ctx }

func handler(c *fiber.Ctx, s *server) {
	parse := c.Bind().Body
	query := s.c.Bind().Query
	cookie := c.Status(200).Bind().Cookie
	_, _, _ = parse, query, cookie
}
`,
			msg: "Migrating parser methods",
		},
	})
}

// SendFile used as a value, and as a method expression, takes the config of v3.
func Test_MigrateSendFileConfig_Values(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateSendFileConfig, []ctxCase{
		{
			name: "SendFile as a value moves the compress flag into the config",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, s *struct{ c *fiber.Ctx }) error {
	send := c.SendFile
	other := s.c.SendFile
	_ = other
	return send("./a.html", true)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, s *struct{ c *fiber.Ctx }) error {
	send := func(file string, compress ...bool) error {
		cfg := fiber.SendFile{ByteRange: true}
		if len(compress) > 0 {
			cfg.Compress = compress[0]
		}
		return c.SendFile(file, cfg)
	}
	other := func(c fiber.Ctx) func(file string, compress ...bool) error {
		return func(file string, compress ...bool) error {
			cfg := fiber.SendFile{ByteRange: true}
			if len(compress) > 0 {
				cfg.Compress = compress[0]
			}
			return c.SendFile(file, cfg)
		}
	}(s.c)
	_ = other
	return send("./a.html", true)
}
`,
			msg: "Migrating SendFile compress flag",
		},
		{
			name: "SendFile as a method expression",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return (*fiber.Ctx).SendFile(c, "./a.html")
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return func(c fiber.Ctx, file string, compress ...bool) error {
		cfg := fiber.SendFile{ByteRange: true}
		if len(compress) > 0 {
			cfg.Compress = compress[0]
		}
		return c.SendFile(file, cfg)
	}(c, "./a.html")
}
`,
			msg: "Migrating SendFile compress flag",
		},
	})
}

// The accessors of the context are renamed when they are used as values too.
func Test_MigrateContextMethods_Values(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateContextMethods, []ctxCase{
		{
			name: "Context and the user context as values",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	fast := c.Context
	user, setUser := c.UserContext, c.SetUserContext
	_, _, _ = fast, user, setUser
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	fast := c.RequestCtx
	user, setUser := c.Context, c.SetContext
	_, _, _ = fast, user, setUser
}

// fiber:context-methods migrated
`,
			msg: "Migrating context methods",
		},
		{
			name: "Context and the user context as method expressions",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	fast := (*fiber.Ctx).Context
	user := (*fiber.Ctx).UserContext
	_, _ = fast(c), user(c)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) {
	fast := (*fiber.Ctx).RequestCtx
	user := (*fiber.Ctx).Context
	_, _ = fast(c), user(c)
}

// fiber:context-methods migrated
`,
			msg: "Migrating context methods",
		},
	})
}

// Bind is renamed ViewBind when it is used as a value too.
func Test_MigrateViewBind_Values(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateViewBind, []ctxCase{
		{
			name: "Bind as a value and as a method expression",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	bind := c.Bind
	other := (*fiber.Ctx).Bind
	_ = other
	return bind(fiber.Map{"a": 1})
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	bind := c.ViewBind
	other := (*fiber.Ctx).ViewBind
	_ = other
	return bind(fiber.Map{"a": 1})
}
`,
			msg: "Migrating view binding helpers",
		},
		{
			name: "the binder of v3 is not a value to rename",
			in: `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	var v any
	return c.Bind().Body(&v)
}
`,
		},
	})
}
