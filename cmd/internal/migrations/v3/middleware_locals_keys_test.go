package v3_test

import (
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
			for name, content := range tt.files {
				files[name] = content
			}
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
