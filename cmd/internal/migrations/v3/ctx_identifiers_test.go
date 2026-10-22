package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateCoreIdentifiers_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCoreIdentifiers, []ctxCase{
		{
			name: "removed constants",
			in: `package main

import "github.com/gofiber/fiber/v3"

const guidConstraint = fiber.ConstraintGuid

func main() {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderFeaturePolicy, "geolocation 'none'")
		return c.Next()
	})
	app.Get("/x/:id<"+guidConstraint+">", func(c *fiber.Ctx) error { return nil })
	_ = map[string]string{fiber.HeaderFeaturePolicy: "none"}
}
`,
			want: `package main

import "github.com/gofiber/fiber/v3"

const guidConstraint = fiber.ConstraintGUID

func main() {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Set("Feature-Policy", "geolocation 'none'")
		return c.Next()
	})
	app.Get("/x/:id<"+guidConstraint+">", func(c *fiber.Ctx) error { return nil })
	_ = map[string]string{"Feature-Policy": "none"}
}
`,
			msg: "Migrating removed fiber identifiers",
		},
		{
			name: "IndexRune",
			in: `package main

import "github.com/gofiber/fiber/v3"

func hasSlash(s string) bool { return fiber.IndexRune(s, '/') }

var contains = fiber.IndexRune
`,
			want: `package main

import (
	"strings"
)

func hasSlash(s string) bool { return strings.ContainsRune(s, '/') }

var contains = strings.ContainsRune
`,
			msg: "Migrating removed fiber identifiers",
		},
		{
			name: "IndexRune with the strings import already there",
			in: `package main

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

func hasSlash(s string) bool { return strings.TrimSpace(s) != "" && fiber.IndexRune(s, '/') }
`,
			want: `package main

import (
	"strings"
)

func hasSlash(s string) bool { return strings.TrimSpace(s) != "" && strings.ContainsRune(s, '/') }
`,
			msg: "Migrating removed fiber identifiers",
		},
		{
			name: "IndexRune with an aliased strings import",
			in: `package main

import (
	str "strings"

	"github.com/gofiber/fiber/v3"
)

func hasSlash(s string) bool { return str.TrimSpace(s) != "" && fiber.IndexRune(s, '/') }
`,
			want: `package main

import (
	str "strings"
)

func hasSlash(s string) bool { return str.TrimSpace(s) != "" && str.ContainsRune(s, '/') }
`,
			msg: "Migrating removed fiber identifiers",
		},
		{
			name: "OnShutdownHandler",
			in: `package main

import "github.com/gofiber/fiber/v3"

var shutdownHooks []fiber.OnShutdownHandler

func register(h fiber.OnShutdownHandler) { shutdownHooks = append(shutdownHooks, h) }
`,
			want: `package main

var shutdownHooks []func() error

func register(h func() error) { shutdownHooks = append(shutdownHooks, h) }
`,
			msg:   "Migrating removed fiber identifiers",
			notes: []string{"fiber.OnShutdownHandler no longer exists in Fiber v3 and was replaced by func() error; Hooks().OnShutdown is OnPostShutdown(func(error) error) now, so adapt the code that registers these handlers (1 more in this project)"},
		},
		{
			name: "parser decoder",
			in: `package main

import (
	"reflect"
	"time"

	"github.com/gofiber/fiber/v3"
)

type CustomTime time.Time

func main() {
	customTime := fiber.ParserType{
		Customtype: CustomTime{},
		Converter: func(value string) reflect.Value {
			if v, err := time.Parse("2006-01-02", value); err == nil {
				return reflect.ValueOf(CustomTime(v))
			}
			return reflect.Value{}
		},
	}

	fiber.SetParserDecoder(fiber.ParserConfig{
		IgnoreUnknownKeys: true,
		ParserType:        []fiber.ParserType{customTime},
		ZeroEmpty:         true,
	})

	fiber.SetParserDecoder(fiber.ParserConfig{
		ParserType: []fiber.ParserType{{Customtype: CustomTime{}, Converter: nil}},
	})
	_ = fiber.New()
}
`,
			want: `package main

import (
	"reflect"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/binder"
)

type CustomTime time.Time

func main() {
	customTime := binder.ParserType{
		CustomType: CustomTime{},
		Converter: func(value string) reflect.Value {
			if v, err := time.Parse("2006-01-02", value); err == nil {
				return reflect.ValueOf(CustomTime(v))
			}
			return reflect.Value{}
		},
	}

	binder.SetParserDecoder(binder.ParserConfig{
		IgnoreUnknownKeys: true,
		ParserType:        []binder.ParserType{customTime},
		ZeroEmpty:         true,
	})

	binder.SetParserDecoder(binder.ParserConfig{
		ParserType: []binder.ParserType{{CustomType: CustomTime{}, Converter: nil}},
	})
	_ = fiber.New()
}
`,
			msg: "Migrating removed fiber identifiers",
		},
		{
			name: "parser decoder only",
			in: `package main

import "github.com/gofiber/fiber/v3"

func init() {
	fiber.SetParserDecoder(fiber.ParserConfig{IgnoreUnknownKeys: true})
}
`,
			want: `package main

import (
	"github.com/gofiber/fiber/v3/binder"
)

func init() {
	binder.SetParserDecoder(binder.ParserConfig{IgnoreUnknownKeys: true})
}
`,
			msg: "Migrating removed fiber identifiers",
		},
		{
			name: "aliased imports",
			in: `package main

import f "github.com/gofiber/fiber/v3"

func init() {
	f.SetParserDecoder(f.ParserConfig{ParserType: []f.ParserType{{Customtype: 1}}})
}

const guid = f.ConstraintGuid
`,
			want: `package main

import (
	f "github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/binder"
)

func init() {
	binder.SetParserDecoder(binder.ParserConfig{ParserType: []binder.ParserType{{CustomType: 1}}})
}

const guid = f.ConstraintGUID
`,
			msg: "Migrating removed fiber identifiers",
		},
		{
			name: "binder is imported already",
			in: `package main

import (
	bnd "github.com/gofiber/fiber/v3/binder"
	"github.com/gofiber/fiber/v3"
)

func init() {
	bnd.SetParserDecoder(bnd.ParserConfig{})
	fiber.SetParserDecoder(fiber.ParserConfig{ZeroEmpty: true})
}
`,
			want: `package main

import (
	bnd "github.com/gofiber/fiber/v3/binder"
)

func init() {
	bnd.SetParserDecoder(bnd.ParserConfig{})
	bnd.SetParserDecoder(bnd.ParserConfig{ZeroEmpty: true})
}
`,
			msg: "Migrating removed fiber identifiers",
		},
		{
			name: "other packages and fields stay",
			in: `package main

import (
	"example.com/fiber"
	"example.com/web"
)

type Type struct{ Customtype int }

func main() {
	fiber.SetParserDecoder(fiber.ParserConfig{})
	_ = web.ConstraintGuid
	_ = Type{Customtype: 1}
}
`,
		},
	})
}
