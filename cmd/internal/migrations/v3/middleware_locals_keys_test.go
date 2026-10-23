package v3_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The key of a config and the key of a read are rarely the same literal: the
// constant lives in the package that builds the middleware, and the handlers that
// read the token live in another one.
const (
	localsGoMod = "module example.com/app\n\ngo 1.25\n"

	// localsAuth builds the jwt middleware with the key given by %s.
	localsAuth = `package auth

import (
	jwtware "github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v2"
)

func Protected(secret []byte) fiber.Handler {
	return jwtware.New(jwtware.Config{
		SigningKey: jwtware.SigningKey{Key: secret},
		ContextKey: %s,
	})
}
`

	// localsHandlers reads the token with the key given by %s.
	localsHandlers = `package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"

	"example.com/app/internal/auth"
)

var _ = auth.Protected

func Me(c *fiber.Ctx) error {
	token := c.Locals(%s).(*jwt.Token)
	return c.JSON(token.Claims)
}
`
)

func Test_MigrateMiddlewareLocals_ConstantOfAnotherPackage(t *testing.T) {
	t.Parallel()

	// rwx-authkit-jwt: const ContextKey = "jwt" in internal/auth, read in internal/handlers
	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"go.mod":                  localsGoMod,
		"internal/auth/keys.go":   "package auth\n\n// ContextKey is where the access token middleware stores the parsed token.\nconst ContextKey = \"jwt\"\n",
		"internal/auth/mw.go":     strings.Replace(localsAuth, "%s", "ContextKey", 1),
		"internal/handlers/me.go": strings.Replace(localsHandlers, "%s", "auth.ContextKey", 1),
	})

	handlers := run.file(t, "internal/handlers/me.go")
	assert.Contains(t, handlers, "token := jwtware.FromContext(c)")
	assert.Contains(t, handlers, `"github.com/gofiber/contrib/v3/jwt"`)
	assert.NotContains(t, handlers, "Locals")
	mw := run.file(t, "internal/auth/mw.go")
	assert.NotContains(t, mw, "ContextKey")
	assert.Contains(t, mw, "SigningKey: jwtware.SigningKey{Key: secret},")
	assert.Contains(t, run.out, "Migrating middleware locals")
	assert.Empty(t, run.notes)

	// a second run has nothing left to do
	again := mw2Again(t, v3.MigrateMiddlewareLocals, run.dir)
	assert.Empty(t, again.out)
	assert.Empty(t, again.notes)
}

func Test_MigrateMiddlewareLocals_KeysThatAreNamesOfTheModule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		files map[string]string
		name  string
		// config is the key of the config in internal/auth, read the key of the read in internal/handlers.
		config, read string
		// migrated says whether the read becomes jwtware.FromContext(c).
		migrated bool
	}{
		{
			name:     "constant of the package, in another file",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\nconst key = \"jwt\"\n\nconst Exported = key\n"},
			config:   "key",
			read:     "auth.Exported",
			migrated: true,
		},
		{
			name:     "the read spells the value",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\nconst ContextKey = \"jwt\"\n"},
			config:   "ContextKey",
			read:     `"jwt"`,
			migrated: true,
		},
		{
			name:     "the config spells the value",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\nconst ContextKey = \"jwt\"\n"},
			config:   `"jwt"`,
			read:     "auth.ContextKey",
			migrated: true,
		},
		{
			name: "alias of a constant of another package",
			files: map[string]string{
				"internal/auth/keys.go": "package auth\n\nconst ContextKey = \"jwt\"\n",
				"internal/keys/keys.go": "package keys\n\nimport \"example.com/app/internal/auth\"\n\nconst Token = auth.ContextKey\n\nconst Again = Token\n",
			},
			config:   "ContextKey",
			read:     "keys.Again",
			migrated: true,
		},
		{
			name: "concatenation of constants",
			files: map[string]string{
				"internal/auth/keys.go": "package auth\n\nconst Prefix = \"app.\"\n\nconst ContextKey = Prefix + \"jwt\"\n",
			},
			config:   "ContextKey",
			read:     `auth.Prefix + "jwt"`,
			migrated: true,
		},
		{
			name:     "variable that nothing assigns to",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\nvar ContextKey = \"jwt\"\n"},
			config:   "ContextKey",
			read:     `"jwt"`,
			migrated: true,
		},
		{
			name:     "variable that something assigns to is the same key wherever it is named",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\nimport \"os\"\n\nvar ContextKey = \"jwt\"\n\nfunc init() { ContextKey = os.Getenv(\"JWT_KEY\") }\n"},
			config:   "ContextKey",
			read:     "auth.ContextKey",
			migrated: true,
		},
		{
			name:     "variable that something assigns to is not its initializer",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\nimport \"os\"\n\nvar ContextKey = \"jwt\"\n\nfunc init() { ContextKey = os.Getenv(\"JWT_KEY\") }\n"},
			config:   "ContextKey",
			read:     `"jwt"`,
			migrated: false,
		},
		{
			name:     "variable whose address is taken",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\nvar ContextKey = \"jwt\"\n\nvar _ = &ContextKey\n"},
			config:   "ContextKey",
			read:     `"jwt"`,
			migrated: false,
		},
		{
			name:     "a typed constant of another package is another key",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\ntype ctxKey string\n\nconst Typed ctxKey = \"jwt\"\n\nconst ContextKey = \"jwt\"\n"},
			config:   "ContextKey",
			read:     "auth.Typed",
			migrated: false,
		},
		{
			name: "constant that two files declare under build constraints",
			files: map[string]string{
				"internal/auth/keys_a.go": "//go:build linux\n\npackage auth\n\nconst ContextKey = \"jwt\"\n",
				"internal/auth/keys_b.go": "//go:build !linux\n\npackage auth\n\nconst ContextKey = \"token\"\n",
			},
			config:   "ContextKey",
			read:     `"jwt"`,
			migrated: false,
		},
		{
			name: "constant that two files declare, read by name",
			files: map[string]string{
				"internal/auth/keys_a.go": "//go:build linux\n\npackage auth\n\nconst ContextKey = \"jwt\"\n",
				"internal/auth/keys_b.go": "//go:build !linux\n\npackage auth\n\nconst ContextKey = \"token\"\n",
			},
			config:   "ContextKey",
			read:     "auth.ContextKey",
			migrated: true,
		},
		{
			name:     "import with a name of its own",
			files:    map[string]string{"internal/auth/keys.go": "package auth\n\nconst ContextKey = \"jwt\"\n"},
			config:   "ContextKey",
			read:     "authn.ContextKey",
			migrated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{
				"go.mod":                  localsGoMod,
				"internal/auth/mw.go":     strings.Replace(localsAuth, "%s", tt.config, 1),
				"internal/handlers/me.go": strings.Replace(localsHandlers, "%s", tt.read, 1),
			}
			maps.Copy(files, tt.files)
			// the same package, imported by the name a project may give it
			if strings.Contains(tt.read, "authn.") {
				files["internal/handlers/me.go"] = strings.Replace(files["internal/handlers/me.go"], `"example.com/app/internal/auth"`, `authn "example.com/app/internal/auth"`, 1)
				files["internal/handlers/me.go"] = strings.Replace(files["internal/handlers/me.go"], "var _ = auth.Protected", "var _ = authn.Protected", 1)
			}
			// a package that is named by an import of the module that other packages use
			if strings.Contains(tt.read, "keys.") {
				files["internal/handlers/me.go"] = strings.Replace(files["internal/handlers/me.go"], `"example.com/app/internal/auth"`, "\"example.com/app/internal/auth\"\n\t\"example.com/app/internal/keys\"", 1)
			}
			if strings.Contains(tt.read, "auth.") || strings.Contains(tt.read, "authn.") || strings.Contains(tt.read, "keys.") {
				// the file imports auth for Protected
				assert.Contains(t, files["internal/handlers/me.go"], "example.com/app/internal/auth")
			}

			run := mw2Apply(t, v3.MigrateMiddlewareLocals, files)

			handlers := run.file(t, "internal/handlers/me.go")
			if tt.migrated {
				assert.Contains(t, handlers, "token := jwtware.FromContext(c)")
				assert.NotContains(t, handlers, "Locals")
				assert.NotContains(t, run.file(t, "internal/auth/mw.go"), "ContextKey:")
				assert.Empty(t, run.notes)
				return
			}
			assert.Contains(t, handlers, ".Locals(", "the read is not the key of the middleware")
			assert.NotContains(t, handlers, "FromContext")
		})
	}
}

func Test_MigrateMiddlewareLocals_KeysOfOtherPackages_OtherMiddleware(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"go.mod": localsGoMod,
		"internal/keys/keys.go": `package keys

const (
	RequestID = "rid"
	Token     = "api_token"
	Paseto    = "paseto_token"
)
`,
		"internal/server/server.go": `package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/keyauth"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	pasetoware "github.com/gofiber/contrib/paseto"

	"example.com/app/internal/keys"
)

func New(secret []byte) *fiber.App {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{ContextKey: keys.RequestID}))
	app.Use(keyauth.New(keyauth.Config{
		Validator:  func(c *fiber.Ctx, key string) (bool, error) { return key == "k", nil },
		ContextKey: keys.Token,
	}))
	app.Use(pasetoware.New(pasetoware.Config{SymmetricKey: secret, ContextKey: keys.Paseto}))
	return app
}
`,
		"internal/handlers/whoami.go": `package handlers

import (
	"github.com/gofiber/fiber/v2"

	"example.com/app/internal/keys"
)

func Whoami(c *fiber.Ctx) error {
	rid := c.Locals(keys.RequestID).(string)
	tok := c.Locals(keys.Token).(string)
	user := c.Locals(keys.Paseto)
	return c.SendString(rid + tok + user.(string))
}

func ByValue(c *fiber.Ctx) string {
	return c.Locals("rid").(string) + c.Locals("api_token").(string)
}
`,
	})

	handlers := run.file(t, "internal/handlers/whoami.go")
	assert.Contains(t, handlers, "rid := requestid.FromContext(c)")
	assert.Contains(t, handlers, "tok := keyauth.TokenFromContext(c)")
	assert.Contains(t, handlers, "user := pasetoware.FromContext(c)")
	assert.Contains(t, handlers, "return requestid.FromContext(c) + keyauth.TokenFromContext(c)")
	assert.NotContains(t, handlers, "Locals")
	server := run.file(t, "internal/server/server.go")
	assert.NotContains(t, server, "ContextKey")
	assert.Empty(t, run.notes)
}

// A module of its own is another module: its packages are not read, and a key
// that both sides spell with the same name keeps being the same key, as before.
func Test_MigrateMiddlewareLocals_KeyOfAnotherModule(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"go.mod":         localsGoMod,
		"keys/go.mod":    "module example.com/keys\n\ngo 1.25\n",
		"keys/keys.go":   "package keys\n\nconst RequestID = \"rid\"\n",
		"main.go":        "package main\n\nimport (\n\t\"github.com/gofiber/fiber/v2\"\n\t\"github.com/gofiber/fiber/v2/middleware/requestid\"\n\n\t\"example.com/keys\"\n)\n\nfunc main() {\n\tapp := fiber.New()\n\tapp.Use(requestid.New(requestid.Config{ContextKey: keys.RequestID}))\n\tapp.Get(\"/\", func(c *fiber.Ctx) error {\n\t\treturn c.SendString(c.Locals(keys.RequestID).(string) + c.Locals(\"rid\").(string))\n\t})\n}\n",
		"other/other.go": "package other\n",
	})

	main := run.file(t, "main.go")
	// the read that names the key as the config does is migrated; the value of the key is not known
	assert.Contains(t, main, `requestid.FromContext(c) + c.Locals("rid").(string)`)
	if assert.Len(t, run.notes, 1) {
		assert.Contains(t, run.notes[0].Message, "The value of its key could not be resolved")
	}
}

// The middleware for sentry keeps the hub of the request under "sentry-hub" at v2,
// where code that does not call GetHubFromContext reads it from; v3 keeps it under
// a key of a private type, so the read finds nothing.
func Test_MigrateMiddlewareLocals_SentryHub(t *testing.T) {
	t.Parallel()

	const reader = `package httpserver

import (
	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v2"
)

var sentryHubKey = "sentry-hub"

func gentlelyGetHubFromContext(ctx *fiber.Ctx) *sentry.Hub {
	if hub, ok := ctx.Locals(sentryHubKey).(*sentry.Hub); ok {
		return hub
	}
	return nil
}

func byName(ctx *fiber.Ctx) *sentry.Hub {
	return ctx.Locals("sentry-hub").(*sentry.Hub)
}
`

	tests := []struct {
		name   string
		server string
	}{
		{
			name: "the import of v2",
			server: `package httpserver

import (
	"github.com/gofiber/contrib/fibersentry"
	"github.com/gofiber/fiber/v2"
)

func New() *fiber.App {
	app := fiber.New()
	app.Use(fibersentry.New(fibersentry.Config{Repanic: true}))
	return app
}
`,
		},
		{
			name: "the import after the module moved",
			server: `package httpserver

import (
	fibersentry "github.com/gofiber/contrib/v3/sentry"
	"github.com/gofiber/fiber/v2"
)

func New() *fiber.App {
	app := fiber.New()
	app.Use(fibersentry.New(fibersentry.Config{Repanic: true}))
	return app
}
`,
		},
		{
			name: "the import of v3 under its own name",
			server: `package httpserver

import (
	"github.com/gofiber/contrib/v3/sentry"
	"github.com/gofiber/fiber/v2"
)

func New() *fiber.App {
	app := fiber.New()
	app.Use(sentry.New())
	return app
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
				"go.mod":                   localsGoMod,
				"internal/server/mw.go":    strings.Replace(tt.server, "package httpserver", "package server", 1),
				"internal/httpserver/h.go": reader,
			})

			// the file reads the hub where sentry-go is called sentry: the middleware is imported under a name of its own
			h := run.file(t, "internal/httpserver/h.go")
			assert.Contains(t, h, `fibersentry "github.com/gofiber/contrib/v3/sentry"`)
			assert.Contains(t, h, "hub, ok := fibersentry.GetHubFromContext(ctx), fibersentry.GetHubFromContext(ctx) != nil; ok {")
			assert.Contains(t, h, "return fibersentry.GetHubFromContext(ctx)")
			assert.NotContains(t, h, "Locals")
			assert.Contains(t, h, `"github.com/getsentry/sentry-go"`)
			assert.Empty(t, run.notes)
		})
	}
}

func Test_MigrateMiddlewareLocals_SentryHubIsLeftAloneWithoutTheMiddleware(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v2"
)

func hub(c *fiber.Ctx) *sentry.Hub {
	// the application keeps a hub of its own under that key
	return c.Locals("sentry-hub").(*sentry.Hub)
}
`
	content, run := mw2Single(t, v3.MigrateMiddlewareLocals, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_SentryHubStoredByTheApplication(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": `package main

import (
	"github.com/getsentry/sentry-go"
	"github.com/gofiber/contrib/fibersentry"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Use(fibersentry.New())
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("sentry-hub", sentry.CurrentHub().Clone())
		return c.Next()
	})
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(c.Locals("sentry-hub").(*sentry.Hub).LastEventID().String())
	})
}
`,
	})

	// the application writes that key too: it is not clear whose hub the read wants
	assert.Contains(t, run.file(t, "main.go"), `c.Locals("sentry-hub").(*sentry.Hub)`)
	if assert.Len(t, run.notes, 1) {
		assert.Contains(t, run.notes[0].Message, "your own code stores a value under that key too")
		assert.Contains(t, run.notes[0].Message, "sentry.GetHubFromContext(c)")
	}
}

// A name that a function declares is not the package level name that is spelled
// like it: the parameter key of a helper that reads any key of the locals is not
// the variable key of the package.
func Test_MigrateMiddlewareLocals_LocalNamesShadowTheKeys(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"go.mod":  localsGoMod,
		"main.go": localsSetup,
		"keys.go": `package main

import "github.com/gofiber/fiber/v2"

var key = "token"

const rid = "requestid"

func named(c *fiber.Ctx) any { return c.Locals(key) }

func any2(c *fiber.Ctx, key string) any { return c.Locals(key) }

func inLoop(c *fiber.Ctx) (out []any) {
	for _, key := range []string{"a", "b"} {
		out = append(out, c.Locals(key))
	}
	return out
}

func shadowedConstant(c *fiber.Ctx) any {
	rid := "mine"
	return c.Locals(rid)
}

func whoami(c *fiber.Ctx) error {
	return c.SendString(c.Locals(rid).(string))
}
`,
	})

	keys := run.file(t, "keys.go")
	// the package level names are the keys of the middleware
	assert.Contains(t, keys, "func named(c *fiber.Ctx) any { return keyauth.TokenFromContext(c) }")
	assert.Contains(t, keys, "return c.SendString(requestid.FromContext(c))")
	// the names that a function declares are not
	assert.Contains(t, keys, "func any2(c *fiber.Ctx, key string) any { return c.Locals(key) }")
	assert.Contains(t, keys, "out = append(out, c.Locals(key))")
	assert.Contains(t, keys, "return c.Locals(rid)")
}

// The package that names a key goes with the config and the read that used it: the
// import is not used any more, and the tidying of the imports cannot tell, since
// the name of the package of the module is its own business.
func Test_MigrateMiddlewareLocals_DropsTheImportOfThePackageOfTheKey(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"go.mod":                localsGoMod,
		"internal/keys/keys.go": "package keys\n\nconst RequestID = \"rid\"\n\nconst Header = \"X-Trace\"\n",
		// the directory is not the name of the package
		"internal/ids/ids.go": "package idkeys\n\nconst Other = \"other\"\n",
		"internal/web/web.go": `package web

import (
	"github.com/gofiber/fiber/v2"

	"example.com/app/internal/keys"
)

func Who(c *fiber.Ctx) error { return c.SendString(c.Locals(keys.RequestID).(string)) }

func Trace(c *fiber.Ctx) string { return c.Get(keys.Header) }
`,
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	"example.com/app/internal/ids"
	"example.com/app/internal/keys"
	"example.com/app/internal/web"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{ContextKey: keys.RequestID}))
	app.Use(requestid.New(requestid.Config{ContextKey: idkeys.Other}))
	app.Get("/", web.Who)
}
`,
	})

	main := run.file(t, "main.go")
	assert.NotContains(t, main, "ContextKey")
	// both packages were there for the keys only; web is not, and its name is not guessed
	assert.NotContains(t, main, "internal/keys")
	assert.NotContains(t, main, "internal/ids")
	assert.Contains(t, main, `"example.com/app/internal/web"`)
	assert.Contains(t, main, `"github.com/gofiber/fiber/v2"`)

	web := run.file(t, "internal/web/web.go")
	assert.Contains(t, web, "requestid.FromContext(c)")
	// keys is used for the header there too
	assert.Contains(t, web, `"example.com/app/internal/keys"`)
	assert.Empty(t, run.notes)
}
