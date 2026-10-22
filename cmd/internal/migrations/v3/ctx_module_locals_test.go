package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// xpkgRequestID is the main package of a project that uses the request ID
// middleware, which the handlers of the other packages read.
const xpkgRequestID = `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New())
}
`

func Test_MigrateMiddlewareLocals_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias, function result and field",
			migrate: v3.MigrateMiddlewareLocals,
			files: map[string]string{
				"main.go": xpkgRequestID,
				xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func ids(c *web.Context, a *web.Ctx, r *web.Request) []string {
	return []string{
		c.Locals("requestid").(string),
		a.Locals("requestid").(string),
		web.From(a).Locals("requestid").(string),
		r.Ctx.Locals("requestid").(string),
		web.Current().Locals("requestid").(string),
	}
}
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"github.com/gofiber/fiber/v3/middleware/requestid"
)

func ids(c *web.Context, a *web.Ctx, r *web.Request) []string {
	return []string{
		requestid.FromContext(c.Ctx),
		requestid.FromContext(a),
		requestid.FromContext(web.From(a)),
		requestid.FromContext(r.Ctx),
		requestid.FromContext(web.Current().Ctx),
	}
}
`},
			msg: "Migrating middleware locals",
		},
		{
			name:    "a wrapper that declares Locals itself, and a type that is no context",
			migrate: v3.MigrateMiddlewareLocals,
			files: map[string]string{
				"main.go": xpkgRequestID,
				"internal/web/scoped.go": `package web

import "github.com/gofiber/fiber/v2"

// Scoped keeps its own locals.
type Scoped struct{ *fiber.Ctx }

func (s *Scoped) Locals(key string, value ...any) any { return s.Ctx.Locals("scope:"+key, value...) }
`,
				xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func ids(s *web.Scoped, p web.Plain) (any, any) {
	return s.Locals("requestid"), p.Locals("requestid")
}
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func ids(s *web.Scoped, p web.Plain) (any, any) {
	return s.Locals("requestid"), p.Locals("requestid")
}
`},
		},
		{
			name:    "a value that is not known to be a context",
			migrate: v3.MigrateMiddlewareLocals,
			files: map[string]string{
				"main.go": xpkgRequestID,
				xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func id() string { return lib.Make().Locals("requestid").(string) }
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func id() string { return lib.Make().Locals("requestid").(string) }
`},
			notes: []string{"lib.Make().Locals(\"requestid\") was left as it is: lib.Make() is not known to be a fiber context here"},
		},
		{
			name:    "an interface that a context satisfies",
			migrate: v3.MigrateMiddlewareLocals,
			files: map[string]string{
				"main.go": xpkgRequestID,
				xpkgHandlers: `package handlers

import "example.com/app/internal/web"

// Locals is what the handlers need of a context.
type Locals interface{ Locals(key string, value ...any) any }

var _ web.Ctx

func id(l Locals) string { return l.Locals("requestid").(string) }
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

// Locals is what the handlers need of a context.
type Locals interface{ Locals(key string, value ...any) any }

var _ web.Ctx

func id(l Locals) string { return l.Locals("requestid").(string) }
`},
			notes: []string{"l.Locals(\"requestid\") was left as it is: l is not known to be a fiber context here"},
		},
		{
			name:    "a file of a package that reaches no context",
			migrate: v3.MigrateMiddlewareLocals,
			files: map[string]string{
				"main.go": xpkgRequestID,
				"internal/other/other.go": `package other

type store struct{}

func (store) Locals(key string) any { return nil }

func read(s store) any { return s.Locals("requestid") }
`,
			},
			want: map[string]string{"internal/other/other.go": `package other

type store struct{}

func (store) Locals(key string) any { return nil }

func read(s store) any { return s.Locals("requestid") }
`},
		},
		{
			name:    "the context key of the config goes once every read of it is migrated",
			migrate: v3.MigrateMiddlewareLocals,
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{ContextKey: "rid"}))
}
`,
				xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func id(c *web.Context) string { return c.Locals("rid").(string) }
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{}))
}
`,
				xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"github.com/gofiber/fiber/v3/middleware/requestid"
)

func id(c *web.Context) string { return requestid.FromContext(c.Ctx) }
`,
			},
			msg: "Migrating middleware locals",
		},
		{
			name:    "the context key of the config stays while a read of it is not known to be a context's",
			migrate: v3.MigrateMiddlewareLocals,
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{ContextKey: "rid"}))
}
`,
				xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func id() string { return lib.Make().Locals("rid").(string) }
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{ContextKey: "rid"}))
}
`,
			},
			notes: []string{
				"lib.Make().Locals(\"rid\") was left as it is: lib.Make() is not known to be a fiber context here",
				"ContextKey: \"rid\" was kept: v3 has no such field, and 1 read(s) of that key could not be migrated",
			},
		},
		{
			name:    "the value of the token kept by the jwt middleware",
			migrate: v3.MigrateMiddlewareLocals,
			files: map[string]string{
				"main.go": `package main

import (
	jwtware "github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Use(jwtware.New(jwtware.Config{SigningKey: jwtware.SigningKey{Key: []byte("secret")}}))
}
`,
				xpkgHandlers: `package handlers

import (
	"github.com/golang-jwt/jwt/v5"

	"example.com/app/internal/web"
)

func name(c *web.Context) string {
	user := c.Locals("user").(*jwt.Token)
	return user.Claims.(jwt.MapClaims)["name"].(string)
}
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"github.com/golang-jwt/jwt/v5"

	"example.com/app/internal/web"
	"github.com/gofiber/contrib/v3/jwt"
)

func name(c *web.Context) string {
	user := jwtware.FromContext(c.Ctx)
	return user.Claims.(jwt.MapClaims)["name"].(string)
}
`},
			msg: "Migrating middleware locals",
		},
	})
}
