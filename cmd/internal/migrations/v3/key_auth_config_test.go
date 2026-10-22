package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// keyAuthSource wraps the body of a Setup function in a valid v2 file.
func keyAuthSource(body string) string {
	return `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/keyauth"
)

func validate(c *fiber.Ctx, key string) (bool, error) { return key == "secret", nil }

func Setup(app *fiber.App) {
` + body + `
}
`
}

func Test_MigrateKeyAuthConfig_Lookups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"header with scheme", `KeyLookup: "header:Authorization", AuthScheme: "Bearer"`, `Extractor: extractors.FromAuthHeader("Bearer")`},
		{"authorization is case insensitive", `KeyLookup: "header:authorization", AuthScheme: "Token"`, `Extractor: extractors.FromAuthHeader("Token")`},
		{"authorization without scheme is the raw header", `KeyLookup: "header:Authorization"`, `Extractor: extractors.FromHeader("Authorization")`},
		{"explicit empty scheme is the raw header", `KeyLookup: "header:Authorization", AuthScheme: ""`, `Extractor: extractors.FromHeader("Authorization")`},
		{"scheme without lookup", `AuthScheme: "Token"`, `Extractor: extractors.FromAuthHeader("Token")`},
		{"empty scheme without lookup defaults to Bearer", `AuthScheme: ""`, `Extractor: extractors.FromAuthHeader("Bearer")`},
		{"custom header", `KeyLookup: "header:X-API-Key"`, `Extractor: extractors.FromHeader("X-API-Key")`},
		{"query", `KeyLookup: "query:api_key"`, `Extractor: extractors.FromQuery("api_key")`},
		{"query ignores the scheme", `KeyLookup: "query:api_key", AuthScheme: "Bearer"`, `Extractor: extractors.FromQuery("api_key")`},
		{"cookie", `KeyLookup: "cookie:session"`, `Extractor: extractors.FromCookie("session")`},
		{"form", `KeyLookup: "form:key"`, `Extractor: extractors.FromForm("key")`},
		{"param", `KeyLookup: "param:key"`, `Extractor: extractors.FromParam("key")`},
		{"concatenated constants", `KeyLookup: "header:" + "X-Other"`, `Extractor: extractors.FromHeader("X-Other")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, run := mw2Single(t, v3.MigrateKeyAuthConfig, keyAuthSource(`	app.Use(keyauth.New(keyauth.Config{`+tt.config+`, Validator: validate}))`))

			assert.Contains(t, content, tt.want)
			assert.NotContains(t, content, "KeyLookup")
			assert.NotContains(t, content, "AuthScheme")
			assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
			assert.Contains(t, content, "Validator: validate")
			assert.Contains(t, run.out, "Migrating keyauth middleware configs")
			assert.Empty(t, run.notes)
		})
	}
}

func Test_MigrateKeyAuthConfig_FieldOrderAndLayout(t *testing.T) {
	t.Parallel()

	// AuthScheme first, KeyLookup last: the extractor takes the place of KeyLookup.
	content, _ := mw2Single(t, v3.MigrateKeyAuthConfig, keyAuthSource(
		`	app.Use("/b", keyauth.New(keyauth.Config{AuthScheme: "Basic", Validator: validate, KeyLookup: "header:Authorization"}))
	app.Use(keyauth.New(keyauth.Config{KeyLookup: "header:Authorization", AuthScheme: "Token"}))
	app.Use(keyauth.New(keyauth.Config{
		// where to look for the key
		KeyLookup: "header:Authorization", // default
		// scheme
		AuthScheme: "Bearer", // keep
		Validator:  validate,
		/* handled below */
		ErrorHandler: func(c *fiber.Ctx, err error) error { return c.SendStatus(401) },
	}))`))

	assert.Contains(t, content, `keyauth.Config{Validator: validate, Extractor: extractors.FromAuthHeader("Basic")}`)
	assert.Contains(t, content, `keyauth.Config{Extractor: extractors.FromAuthHeader("Token")}`)
	assert.Contains(t, content, "// where to look for the key\n\t\tExtractor: extractors.FromAuthHeader(\"Bearer\"), // default")
	assert.NotContains(t, content, "// scheme", "the comment above a removed field goes with it")
	assert.NotContains(t, content, "keep")
	assert.Contains(t, content, "/* handled below */\n\t\tErrorHandler: func(c *fiber.Ctx, err error) error { return c.SendStatus(401) },")
}

func Test_MigrateKeyAuthConfig_NestedBodies(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateKeyAuthConfig, keyAuthSource(`	app.Use(keyauth.New(keyauth.Config{
		KeyLookup: "header:X-API-Key",
		Validator: func(c *fiber.Ctx, key string) (bool, error) {
			if key == "" {
				return false, nil
			}
			return key == "secret", nil
		},
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		},
	}))
	app.Use("/inner", keyauth.New(keyauth.Config{
		Validator: func(c *fiber.Ctx, key string) (bool, error) {
			inner := keyauth.Config{KeyLookup: "query:k", Validator: validate}
			_ = inner
			return true, nil
		},
		KeyLookup: "cookie:session",
	}))`))

	assert.Contains(t, content, `Extractor: extractors.FromHeader("X-API-Key")`)
	assert.Contains(t, content, `Extractor: extractors.FromCookie("session")`)
	assert.Contains(t, content, `keyauth.Config{Extractor: extractors.FromQuery("k"), Validator: validate}`)
	assert.NotContains(t, content, "KeyLookup")
	assert.Contains(t, run.out, "Migrating keyauth middleware configs")
}

func Test_MigrateKeyAuthConfig_ResolvesPackageConstants(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateKeyAuthConfig, map[string]string{
		"consts.go": `package main

const (
	lookup     = "header:X-Token"
	authScheme = "Token"
)
`,
		"main.go": keyAuthSource(`	app.Use("/k", keyauth.New(keyauth.Config{KeyLookup: lookup, Validator: validate}))
	app.Use("/s", keyauth.New(keyauth.Config{
		KeyLookup:  "header:Authorization",
		AuthScheme: authScheme,
		Validator:  validate,
	}))`),
	})

	content := run.file(t, "main.go")
	assert.Contains(t, content, `Extractor: extractors.FromHeader("X-Token")`)
	assert.Contains(t, content, `Extractor: extractors.FromAuthHeader("Token")`)
	assert.NotContains(t, content, "KeyLookup")
	assert.Empty(t, run.notes)
}

func Test_MigrateKeyAuthConfig_SchemeOnOtherHeader(t *testing.T) {
	t.Parallel()

	// v2 stripped the scheme from any header. No stock extractor does that, so it is spelled out.
	content, run := mw2Single(t, v3.MigrateKeyAuthConfig, keyAuthSource(`	app.Use(keyauth.New(keyauth.Config{
		KeyLookup:  "header:X-Auth",
		AuthScheme: "Token",
		Validator:  validate,
	}))`))

	assert.Contains(t, content, `extractors.FromCustom("X-Auth", func(c fiber.Ctx) (string, error) {`)
	assert.Contains(t, content, `auth := c.Get("X-Auth")`)
	assert.Contains(t, content, `if len(auth) > len("Token")+1 && auth[:len("Token")] == "Token" {`)
	assert.Contains(t, content, `return auth[len("Token")+1:], nil`)
	assert.Contains(t, content, `return "", extractors.ErrNotFound`)
	assert.NotContains(t, content, "AuthScheme")
	assert.Empty(t, run.notes)
}

func Test_MigrateKeyAuthConfig_NotConstantStaysLoud(t *testing.T) {
	t.Parallel()

	src := keyAuthSource(`	app.Use(keyauth.New(keyauth.Config{
		KeyLookup: strings.Join([]string{"header", "X-Key"}, ":"),
		Validator: validate,
	}))
	app.Use(keyauth.New(keyauth.Config{KeyLookup: "header:Authorization", AuthScheme: scheme(), Validator: validate}))
	app.Use(keyauth.New(keyauth.Config{AuthScheme: scheme(), Validator: validate}))
	app.Use(keyauth.New(keyauth.Config{KeyLookup: "header:X-Auth", AuthScheme: scheme(), Validator: validate}))
	app.Use(keyauth.New(keyauth.Config{KeyLookup: "header", Validator: validate}))
	app.Use(keyauth.New(keyauth.Config{KeyLookup: "header:A, query:b", Validator: validate}))`)

	content, run := mw2Single(t, v3.MigrateKeyAuthConfig, src)

	// An unknown scheme is fine for Authorization: an empty one returns the whole header in v3 as it did in v2.
	assert.Contains(t, content, `Extractor: extractors.FromAuthHeader(scheme())`)
	assert.Contains(t, content, `KeyLookup: strings.Join(`)
	assert.Contains(t, content, `keyauth.Config{AuthScheme: scheme(), Validator: validate}`)
	assert.Contains(t, content, `keyauth.Config{KeyLookup: "header:X-Auth", AuthScheme: scheme(), Validator: validate}`)
	assert.Contains(t, content, `keyauth.Config{KeyLookup: "header", Validator: validate}`)
	assert.Contains(t, content, `keyauth.Config{KeyLookup: "header:A, query:b", Validator: validate}`)
	require.Len(t, run.notes, 5)
	assert.Contains(t, run.notes[0].Message, "not a constant string")
	assert.Contains(t, run.messages(), "not <source>:<name>")
	assert.Contains(t, run.messages(), "replaced an empty scheme by")
}

func Test_MigrateKeyAuthConfig_Alias(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateKeyAuthConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	ka "github.com/gofiber/fiber/v2/middleware/keyauth"
)

func Setup(app *fiber.App) {
	app.Use(ka.New(ka.Config{
		KeyLookup: "query:api_key",
		Validator: func(c *fiber.Ctx, key string) (bool, error) { return key == "secret", nil },
	}))
}
`)

	assert.Contains(t, content, `Extractor: extractors.FromQuery("api_key")`)
	assert.Contains(t, run.out, "Migrating keyauth middleware configs")
}

func Test_MigrateKeyAuthConfig_ReusesExtractorsImport(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateKeyAuthConfig, `package main

import (
	"github.com/gofiber/fiber/v3"
	ext "github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/keyauth"
)

func validate(c fiber.Ctx, key string) (bool, error) { return true, nil }

var _ = ext.FromCookie("x")

var h = keyauth.New(keyauth.Config{KeyLookup: "query:k", Validator: validate})
`)

	assert.Contains(t, content, `ext.FromQuery("k")`)
	assert.Equal(t, 1, strings.Count(content, "fiber/v3/extractors"))
}

func Test_MigrateKeyAuthConfig_Assignments(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateKeyAuthConfig, keyAuthSource(`	cfg := keyauth.ConfigDefault
	cfg.KeyLookup = "query:api_key"
	cfg.Validator = validate
	app.Use(keyauth.New(cfg))

	var cfg2 keyauth.Config
	cfg2.KeyLookup = "cookie:session"
	cfg2.AuthScheme = "Bearer"
	cfg2.Validator = validate
	app.Use("/c", keyauth.New(cfg2))

	var cfg3 keyauth.Config
	cfg3.AuthScheme = "Token"
	cfg3.Validator = validate
	app.Use("/t", keyauth.New(cfg3))

	cfg4 := keyauth.ConfigDefault
	cfg4.Validator = validate
	cfg4.KeyLookup = "header:X-Key"
	app.Use("/x", keyauth.New(cfg4))`))

	assert.Contains(t, content, `cfg.Extractor = extractors.FromQuery("api_key")`)
	assert.Contains(t, content, `cfg2.Extractor = extractors.FromCookie("session")`)
	assert.Contains(t, content, `cfg3.Extractor = extractors.FromAuthHeader("Token")`)
	assert.NotContains(t, content, "cfg2.AuthScheme")
	// ConfigDefault starts with the Bearer scheme, which v2 applied to the header that replaced Authorization.
	assert.Contains(t, content, `cfg4.Extractor = extractors.FromCustom("X-Key", func(c fiber.Ctx) (string, error) {`)
	assert.Contains(t, content, `auth[:len("Bearer")] == "Bearer"`)
	assert.NotContains(t, content, "KeyLookup")
	assert.Empty(t, run.notes)
}

func Test_MigrateKeyAuthConfig_IgnoresOtherPackages(t *testing.T) {
	t.Parallel()

	src := `package main

import "example.com/keyauth"

type settings struct{ KeyLookup, AuthScheme string }

func main() {
	_ = keyauth.Config{KeyLookup: "header:Authorization", AuthScheme: "Bearer"}
	_ = settings{KeyLookup: "header:X", AuthScheme: "Y"}
	var s settings
	s.KeyLookup = "query:a"
}
`
	content, run := mw2Single(t, v3.MigrateKeyAuthConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
	assert.Empty(t, run.notes)
}

func Test_MigrateKeyAuthConfig_V3ConfigIsLeftAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/keyauth"
)

var _ = keyauth.New(keyauth.Config{
	Extractor: extractors.FromAuthHeader("Bearer"),
	Validator: func(c fiber.Ctx, key string) (bool, error) { return true, nil },
})
`
	content, run := mw2Single(t, v3.MigrateKeyAuthConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateKeyAuthConfig_Idempotent(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateKeyAuthConfig, map[string]string{"main.go": keyAuthSource(`	app.Use(keyauth.New(keyauth.Config{KeyLookup: "header:Authorization", AuthScheme: "Token", Validator: validate}))
	app.Use("/x", keyauth.New(keyauth.Config{KeyLookup: "header:X-Auth", AuthScheme: "Token", Validator: validate}))
	cfg := keyauth.ConfigDefault
	cfg.KeyLookup = "query:k"`)})
	first := run.file(t, "main.go")
	require.Contains(t, run.out, "Migrating keyauth middleware configs")

	second := mw2Again(t, v3.MigrateKeyAuthConfig, run.dir)

	assert.Equal(t, first, second.file(t, "main.go"))
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

func Test_MigrateKeyAuthConfig_SkipsFilesThatDoNotParse(t *testing.T) {
	t.Parallel()

	broken := keyAuthSource(`	app.Use(keyauth.New(keyauth.Config{KeyLookup: "query:k", Validator: validate})`)
	run := mw2Apply(t, v3.MigrateKeyAuthConfig, map[string]string{
		"a_broken.go": broken,
		"b_good.go": `package main

import "github.com/gofiber/fiber/v2/middleware/keyauth"

var _ = keyauth.New(keyauth.Config{KeyLookup: "query:k"})
`,
	})

	assert.Equal(t, broken, run.file(t, "a_broken.go"))
	assert.Contains(t, run.file(t, "b_good.go"), `extractors.FromQuery("k")`)
	require.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0].Message, "does not parse")
}
