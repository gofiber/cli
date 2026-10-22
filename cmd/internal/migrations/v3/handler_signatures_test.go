package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateHandlerSignatures(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhstest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c *fiber.Ctx) error { return nil }
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateHandlerSignatures(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "*fiber.Ctx")
	assert.Contains(t, content, "fiber.Ctx")
	assert.Contains(t, buf.String(), "Migrating handler signatures")
}

func Test_MigrateHandlerSignatures_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateHandlerSignatures, []ctxCase{
		{
			name: "functions, receivers and closures",
			in: `package main

import "github.com/gofiber/fiber/v2"

type server struct{}

func (s *server) handle(c *fiber.Ctx) error { return c.SendString("ok") }

func (s server) value(c *fiber.Ctx, err error) error { return err }

func handler(c *fiber.Ctx) error { return nil }

func main() {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Next()
	})
	app.Use(func(a, b *fiber.Ctx) (ctx *fiber.Ctx, err error) { return })
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type server struct{}

func (s *server) handle(c fiber.Ctx) error { return c.SendString("ok") }

func (s server) value(c fiber.Ctx, err error) error { return err }

func handler(c fiber.Ctx) error { return nil }

func main() {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return c.Next()
	})
	app.Use(func(a, b fiber.Ctx) (ctx fiber.Ctx, err error) { return })
}
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "aliased import",
			in: `package main

import (
	"log"

	f "github.com/gofiber/fiber/v2"
)

type Handler func(*f.Ctx) error

func hello(c *f.Ctx) error { return c.SendString("hello") }

func main() {
	app := f.New()
	app.Get("/", hello)
	log.Fatal(app.Listen(":3000"))
}
`,
			want: `package main

import (
	"log"

	f "github.com/gofiber/fiber/v2"
)

type Handler func(f.Ctx) error

func hello(c f.Ctx) error { return c.SendString("hello") }

func main() {
	app := f.New()
	app.Get("/", hello)
	log.Fatal(app.Listen(":3000"))
}
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "dot import",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func hello(c *Ctx) error { return c.SendString("hello") }

func local(x int) int {
	Ctx := &x
	return *Ctx
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func hello(c Ctx) error { return c.SendString("hello") }

func local(x int) int {
	Ctx := &x
	return *Ctx
}
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "strings and comments stay",
			in: `package main

import "github.com/gofiber/fiber/v2"

// handlers take *fiber.Ctx, the comment stays.
var doc = "handlers take *fiber.Ctx"
var raw = §*fiber.Ctx§

/* block: *fiber.Ctx */
func handler(c *fiber.Ctx) error { return nil } // returns *fiber.Ctx
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

// handlers take *fiber.Ctx, the comment stays.
var doc = "handlers take *fiber.Ctx"
var raw = §*fiber.Ctx§

/* block: *fiber.Ctx */
func handler(c fiber.Ctx) error { return nil } // returns *fiber.Ctx
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "composite types and assertions",
			in: `package main

import "github.com/gofiber/fiber/v2"

type S struct {
	*fiber.Ctx
	list []*fiber.Ctx
	byID map[string]*fiber.Ctx
	ch   chan *fiber.Ctx
	arr  [2]*fiber.Ctx
	fn   func(*fiber.Ctx) (*fiber.Ctx, error)
}

type Getter interface {
	Get(c *fiber.Ctx) *fiber.Ctx
}

func generic[T any](c *fiber.Ctx, v T) {}

func assert(v any) {
	if c, ok := v.(*fiber.Ctx); ok {
		generic[*fiber.Ctx](c, c)
	}
	switch c := v.(type) {
	case *fiber.Ctx:
		_ = c
	}
	_ = []*fiber.Ctx{}
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type S struct {
	fiber.Ctx
	list []fiber.Ctx
	byID map[string]fiber.Ctx
	ch   chan fiber.Ctx
	arr  [2]fiber.Ctx
	fn   func(fiber.Ctx) (fiber.Ctx, error)
}

type Getter interface {
	Get(c fiber.Ctx) fiber.Ctx
}

func generic[T any](c fiber.Ctx, v T) {}

func assert(v any) {
	if c, ok := v.(fiber.Ctx); ok {
		generic[fiber.Ctx](c, c)
	}
	switch c := v.(type) {
	case fiber.Ctx:
		_ = c
	}
	_ = []fiber.Ctx{}
}
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "reflect.TypeOf keeps a type",
			in: `package main

import (
	"reflect"

	"github.com/gofiber/fiber/v2"
)

var ctxType = reflect.TypeOf((*fiber.Ctx)(nil))

var already = reflect.TypeOf((*fiber.Ctx)(nil)).Elem()

func use(t reflect.Type) bool {
	return t == reflect.TypeOf((*fiber.Ctx)(nil))
}
`,
			want: `package main

import (
	"reflect"

	"github.com/gofiber/fiber/v2"
)

var ctxType = reflect.TypeOf((*fiber.Ctx)(nil)).Elem()

var already = reflect.TypeOf((*fiber.Ctx)(nil)).Elem()

func use(t reflect.Type) bool {
	return t == reflect.TypeOf((*fiber.Ctx)(nil)).Elem()
}
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "aliased reflect",
			in: `package main

import (
	rf "reflect"

	"github.com/gofiber/fiber/v2"
)

var ctxType = rf.TypeOf((*fiber.Ctx)(nil))
`,
			want: `package main

import (
	rf "reflect"

	"github.com/gofiber/fiber/v2"
)

var ctxType = rf.TypeOf((*fiber.Ctx)(nil)).Elem()
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "interface assertion",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

var _ fmt.Stringer = (*fiber.Ctx)(nil)
`,
			want: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

var _ fmt.Stringer = (fiber.Ctx)(nil)
`,
			msg: "Migrating handler signatures",
		},
		{
			name: "other conversion is reported",
			in: `package main

import "github.com/gofiber/fiber/v2"

var nilCtx = (*fiber.Ctx)(nil)
`,
			notes: []string{"main.go:5: (*fiber.Ctx)(nil) is a nil pointer to the Ctx interface"},
		},
		{
			name: "other packages Ctx",
			in: `package main

import (
	"example.com/fiber"
	"example.com/web"
)

func a(c *fiber.Ctx) {}
func b(c *web.Ctx)   {}
`,
		},
		{
			name: "no fiber import",
			in: `package main

type Ctx struct{}

func a(c *Ctx) {}
`,
		},
		{
			name: "v3 import path",
			in: `package main

import "github.com/gofiber/fiber/v3"

func a(c *fiber.Ctx) error { return nil }
`,
			want: `package main

import "github.com/gofiber/fiber/v3"

func a(c fiber.Ctx) error { return nil }
`,
			msg: "Migrating handler signatures",
		},
	})
}
