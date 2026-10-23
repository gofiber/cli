package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// localsSetup is a main.go that registers the middleware the handlers of a
// project read from. Each test adds the files with the reads.
const localsSetup = `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/keyauth"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New())
	app.Use(keyauth.New(keyauth.Config{
		KeyLookup: "header:X-API-Key",
		Validator: func(c *fiber.Ctx, key string) (bool, error) { return key == "k", nil },
	}))
	app.Get("/", whoami)
}
`

func Test_MigrateMiddlewareLocals(t *testing.T) {
	t.Parallel()

	// The handlers live in a file that imports no middleware, the usual layout.
	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": localsSetup,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v2"

func whoami(c *fiber.Ctx) error {
	rid := c.Locals("requestid").(string)
	tok, ok := c.Locals("token").(string)
	other, _ := c.Locals("requestid").(string)
	_, found := c.Locals("token").(string)
	_, _, _, _ = rid, tok, other, ok
	_ = found
	return c.SendString(rid + ":" + tok)
}
`,
	})

	handlers := run.file(t, "handlers.go")
	assert.Contains(t, handlers, "rid := requestid.FromContext(c)")
	assert.Contains(t, handlers, "tok, ok := keyauth.TokenFromContext(c), keyauth.TokenFromContext(c) != \"\"")
	assert.Contains(t, handlers, "other := requestid.FromContext(c)")
	assert.Contains(t, handlers, "found := keyauth.TokenFromContext(c) != \"\"")
	assert.Contains(t, handlers, `"github.com/gofiber/fiber/v3/middleware/requestid"`, "the import is added: goimports cannot find v3 on a first run")
	assert.Contains(t, handlers, `"github.com/gofiber/fiber/v3/middleware/keyauth"`)
	assert.NotContains(t, handlers, "Locals")
	assert.Contains(t, run.out, "Migrating middleware locals")
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_OwnKeysAreLeftAlone(t *testing.T) {
	t.Parallel()

	// An application with its own auth middleware and none of Fiber's: the keys
	// "token" and "username" are its own, whatever the default keys of Fiber are.
	src := `package main

import "github.com/gofiber/fiber/v2"

func authMiddleware(c *fiber.Ctx) error {
	c.Locals("token", c.Get("Authorization"))
	c.Locals("username", "bob")
	c.Locals("session", 42)
	return c.Next()
}

func me(c *fiber.Ctx) error {
	id := c.Locals("requestid")
	return c.SendString(c.Locals("username").(string) + ":" + c.Locals("token").(string) + fmt(id))
}

func fmt(any) string { return "" }
`
	content, run := mw2Single(t, v3.MigrateMiddlewareLocals, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_OwnWritesOfAMiddlewareKey(t *testing.T) {
	t.Parallel()

	// keyauth is used, but the application also stores its own "token": the reads are ambiguous.
	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": localsSetup,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v2"

func setToken(c *fiber.Ctx) { c.Locals("token", "mine") }

func whoami(c *fiber.Ctx) error {
	rid := c.Locals("requestid").(string)
	return c.SendString(rid + c.Locals("token").(string))
}
`,
	})

	handlers := run.file(t, "handlers.go")
	assert.Contains(t, handlers, `c.Locals("token").(string)`)
	assert.Contains(t, handlers, "rid := requestid.FromContext(c)")
	require.Len(t, run.notes, 1)
	assert.Equal(t, "handlers.go", run.notes[0].File)
	assert.Equal(t, 9, run.notes[0].Line)
	assert.Contains(t, run.notes[0].Message, "your own code stores a value under that key too (handlers.go:5)")
	assert.Contains(t, run.notes[0].Message, "keyauth.TokenFromContext(c)")
}

func Test_MigrateMiddlewareLocals_UserFunctionsNamedFromContext(t *testing.T) {
	t.Parallel()

	// The regexps used to append ", true" to these and to strip their type assertions.
	src := `package main

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

type User struct{ Name string }

type ctxKey struct{}

func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxKey{}).(*User)
	return u, ok
}

func TenantFromContext(ctx context.Context) interface{} { return ctx.Value("tenant") }

func handle(ctx context.Context) string {
	u, ok := UserFromContext(ctx)
	if !ok {
		return "anon"
	}
	tenant := TenantFromContext(ctx).(string)
	return fmt.Sprintf("%s@%s", u.Name, tenant)
}

var _ = requestid.New

func main() {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString(handle(c.UserContext())) })
}
`
	content, run := mw2Single(t, v3.MigrateMiddlewareLocals, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateMiddlewareLocals_Shapes(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": localsSetup,
		"handlers.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type handler struct{ c *fiber.Ctx }

func (h handler) id() string { return h.c.Locals("requestid").(string) }

func nilCheck(c *fiber.Ctx) error {
	if c.Locals("requestid") != nil {
		return c.SendString("has id")
	}
	if c.Locals("requestid") == nil {
		return c.SendString("no id")
	}
	return nil
}

func variable(c *fiber.Ctx) error {
	tok := c.Locals("token")
	if tok == nil {
		return fiber.ErrUnauthorized
	}
	v, ok := tok.(string)
	fmt.Println(v, ok, tok)
	return c.SendString(tok.(string))
}

func inIf(c *fiber.Ctx) string {
	if u := c.Locals("token"); u != nil {
		return u.(string)
	}
	var viaVar = c.Locals("requestid")
	return viaVar.(string)
}

func values(c *fiber.Ctx) error {
	fmt.Println(c.Locals("requestid"))
	_ = fiber.Map{"id": c.Locals("requestid"), "t": (c.Locals("token")).(string)}
	return c.JSON(c.Locals("token"))
}

func whoami(c *fiber.Ctx) error { return nil }
`,
	})

	handlers := run.file(t, "handlers.go")
	assert.Contains(t, handlers, "func (h handler) id() string { return requestid.FromContext(h.c) }")
	assert.Contains(t, handlers, `if requestid.FromContext(c) != "" {`)
	assert.Contains(t, handlers, `if requestid.FromContext(c) == "" {`)
	assert.Contains(t, handlers, "tok := keyauth.TokenFromContext(c)")
	assert.Contains(t, handlers, `if tok == "" {`)
	assert.Contains(t, handlers, `v, ok := tok, tok != ""`)
	assert.Contains(t, handlers, "return c.SendString(tok)")
	assert.Contains(t, handlers, `if u := keyauth.TokenFromContext(c); u != "" {`)
	assert.Contains(t, handlers, "return u\n")
	assert.Contains(t, handlers, "var viaVar = requestid.FromContext(c)")
	assert.Contains(t, handlers, "return viaVar\n")
	assert.Contains(t, handlers, "fmt.Println(requestid.FromContext(c))")
	assert.Contains(t, handlers, `fiber.Map{"id": requestid.FromContext(c), "t": keyauth.TokenFromContext(c)}`)
	assert.Contains(t, handlers, "return c.JSON(keyauth.TokenFromContext(c))")
	assert.NotContains(t, handlers, "Locals")
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_ReadsThatStay(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": localsSetup,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v2"

func typeSwitch(c *fiber.Ctx) string {
	switch v := c.Locals("requestid").(type) {
	case string:
		return v
	}
	return ""
}

func wrongType(c *fiber.Ctx) int {
	return c.Locals("requestid").(int)
}

func reassigned(c *fiber.Ctx) string {
	tok := c.Locals("token")
	tok = nil
	return ""
}

func twoValues(c *fiber.Ctx) string {
	var v, ok = c.Locals("token").(string)
	_ = ok
	return v
}

func whoami(c *fiber.Ctx) error { return nil }
`,
	})

	handlers := run.file(t, "handlers.go")
	assert.Contains(t, handlers, `switch v := c.Locals("requestid").(type) {`)
	assert.Contains(t, handlers, `c.Locals("requestid").(int)`)
	assert.Contains(t, handlers, `tok := c.Locals("token")`)
	assert.Contains(t, handlers, `var v, ok = c.Locals("token").(string)`)
	require.Len(t, run.notes, 4)
	assert.Contains(t, run.messages(), "a type switch on it would see a string")
	assert.Contains(t, run.messages(), "it is asserted to int but v3 returns a string")
	assert.Contains(t, run.messages(), "tok is assigned to")
	assert.Contains(t, run.messages(), "a two-value var declaration")
	assert.Contains(t, run.messages(), "Use requestid.FromContext(c)")
}

func Test_MigrateMiddlewareLocals_ContextKeys(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"keys.go": `package main

const requestIDKey = "request_id"

type ctxKey string

const typedKey ctxKey = "typed_rid"

type structKey struct{}
`,
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/keyauth"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

const authKey = "auth_token"

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{ContextKey: "rid"}))
	app.Use(requestid.New(requestid.Config{ContextKey: requestIDKey}))
	app.Use("/t", requestid.New(requestid.Config{
		Header:     "X-Trace",
		ContextKey: typedKey,
	}))
	app.Use(keyauth.New(keyauth.Config{
		KeyLookup:  "header:X-API-Key",
		Validator:  func(c *fiber.Ctx, key string) (bool, error) { return key == "k", nil },
		ContextKey: authKey,
	}))
	app.Use("/s", keyauth.New(keyauth.Config{
		KeyLookup:  "header:X-API-Key",
		Validator:  func(c *fiber.Ctx, key string) (bool, error) { return key == "k", nil },
		ContextKey: structKey{},
	}))
	app.Get("/", whoami)
}
`,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v2"

func whoami(c *fiber.Ctx) error {
	a := c.Locals("rid").(string)
	b, _ := c.Locals(requestIDKey).(string)
	d, _ := c.Locals(typedKey).(string)
	e := c.Locals(authKey).(string)
	f := c.Locals(structKey{}).(string)
	untyped := c.Locals("typed_rid")
	return c.SendString(a + b + d + e + f + untyped.(string))
}
`,
	})

	main := run.file(t, "main.go")
	assert.NotContains(t, main, "ContextKey")
	assert.Contains(t, main, "requestid.Config{}")
	assert.Contains(t, main, "requestid.Config{\n\t\tHeader: \"X-Trace\",\n\t})")
	assert.Contains(t, main, "KeyLookup: \"header:X-API-Key\",")

	handlers := run.file(t, "handlers.go")
	assert.Contains(t, handlers, "a := requestid.FromContext(c)")
	assert.Contains(t, handlers, "b := requestid.FromContext(c)")
	assert.Contains(t, handlers, "d := requestid.FromContext(c)")
	assert.Contains(t, handlers, "e := keyauth.TokenFromContext(c)")
	assert.Contains(t, handlers, "f := keyauth.TokenFromContext(c)")
	assert.Contains(t, handlers, `untyped := c.Locals("typed_rid")`, "a typed key is not the string it wraps")
	assert.Contains(t, run.messages(), "")
}

func Test_MigrateMiddlewareLocals_ConfigDefaultKey(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateMiddlewareLocals, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func Setup(app *fiber.App) {
	app.Use(requestid.New())
	app.Get("/", func(c *fiber.Ctx) error {
		id, _ := c.Locals(requestid.ConfigDefault.ContextKey).(string)
		return c.SendString(id)
	})
}
`)

	assert.Contains(t, content, "id := requestid.FromContext(c)")
	assert.NotContains(t, content, "ConfigDefault")
	assert.Contains(t, run.out, "Migrating middleware locals")
}

func Test_MigrateMiddlewareLocals_Alias(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateMiddlewareLocals, `package main

import (
	"github.com/gofiber/fiber/v2"
	ka "github.com/gofiber/fiber/v2/middleware/keyauth"
)

func Setup(app *fiber.App) {
	app.Use(ka.New(ka.Config{
		KeyLookup:  "query:api_key",
		Validator:  func(c *fiber.Ctx, key string) (bool, error) { return key == "secret", nil },
		ContextKey: "apikey",
	}))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(c.Locals("apikey").(string))
	})
}
`)

	assert.Contains(t, content, "return c.SendString(ka.TokenFromContext(c))")
	assert.NotContains(t, content, "ContextKey")
	assert.Equal(t, 1, countImports(content, "github.com/gofiber/fiber/v2/middleware/keyauth"))
	assert.NotContains(t, content, "fiber/v3/middleware/keyauth", "the aliased import is reused")
}

func Test_MigrateMiddlewareLocals_BasicAuth(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
)

func main() {
	app := fiber.New()
	app.Use(basicauth.New(basicauth.Config{Users: map[string]string{"admin": "x"}}))
	app.Use("/custom", basicauth.New(basicauth.Config{
		Users:           map[string]string{"admin": "x"},
		ContextUsername: "who",
		ContextPassword: "secret",
	}))
	app.Get("/", me)
}
`,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v2"

func me(c *fiber.Ctx) error {
	user := c.Locals("username").(string)
	who := c.Locals("who").(string)
	pass := c.Locals("password").(string)
	secret := c.Locals("secret").(string)
	return c.SendString(user + who + pass + secret)
}
`,
	})

	handlers := run.file(t, "handlers.go")
	assert.Contains(t, handlers, "user := basicauth.UsernameFromContext(c)")
	assert.Contains(t, handlers, "who := basicauth.UsernameFromContext(c)")
	assert.Contains(t, handlers, `pass := c.Locals("password").(string)`)
	assert.Contains(t, handlers, `secret := c.Locals("secret").(string)`)
	assert.NotContains(t, handlers, "PasswordFromContext", "v3 has no such function")
	require.Len(t, run.notes, 3)
	assert.Contains(t, run.messages(), "basicauth no longer keeps the password")

	main := run.file(t, "main.go")
	assert.Contains(t, main, "ContextPassword: \"secret\"", "the key is still read, so its field stays")
	assert.NotContains(t, main, "ContextUsername")
	assert.Contains(t, run.messages(), "was kept")
}

func Test_MigrateMiddlewareLocals_CSRF(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

func main() {
	app := fiber.New()
	app.Use(csrf.New(csrf.Config{
		CookieName:        "csrf_",
		ContextKey:        "csrf",
		HandlerContextKey: "csrfHandler",
	}))
	app.Get("/", token)
}
`,
		"handlers.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

func token(c *fiber.Ctx) error {
	t := c.Locals("csrf").(string)
	if h, ok := c.Locals("csrfHandler").(*csrf.CSRFHandler); ok {
		_ = h.DeleteToken(c)
	}
	return c.SendString(t)
}

func logout(c *fiber.Ctx) error {
	handler := c.Locals(csrf.ConfigDefault.HandlerContextKey).(*csrf.CSRFHandler)
	return handler.DeleteToken(c)
}
`,
	})

	main := run.file(t, "main.go")
	assert.Contains(t, main, `CookieName: "csrf_",`)
	assert.NotContains(t, main, "ContextKey", "HandlerContextKey was mangled to Handler by a regexp for ContextKey")
	assert.NotContains(t, main, "Handler")

	handlers := run.file(t, "handlers.go")
	assert.Contains(t, handlers, "t := csrf.TokenFromContext(c)")
	assert.Contains(t, handlers, "if h, ok := csrf.HandlerFromContext(c), csrf.HandlerFromContext(c) != nil; ok {")
	assert.Contains(t, handlers, "handler := csrf.HandlerFromContext(c)")
	assert.NotContains(t, handlers, "Locals")
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_CSRFTokenIsOnlyStoredWhenConfigured(t *testing.T) {
	t.Parallel()

	// Without ContextKey v2 keeps no token in the locals: "csrf" is the application's own key.
	src := `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

func main() {
	app := fiber.New()
	app.Use(csrf.New())
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString(c.Locals("csrf").(string)) })
}
`
	content, run := mw2Single(t, v3.MigrateMiddlewareLocals, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateMiddlewareLocals_JWTAndPaseto(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": `package main

import (
	jwtware "github.com/gofiber/contrib/jwt"
	pasetoware "github.com/gofiber/contrib/paseto"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Use(jwtware.New(jwtware.Config{SigningKey: jwtware.SigningKey{Key: []byte("secret")}}))
	app.Use("/k", jwtware.New(jwtware.Config{
		SigningKey: jwtware.SigningKey{Key: []byte("secret")},
		ContextKey: "jwt-user",
	}))
	app.Use("/p", pasetoware.New(pasetoware.Config{SymmetricKey: []byte("secret-key-of-32-bytes-length!!!")}))
	app.Use("/q", pasetoware.New(pasetoware.Config{
		SymmetricKey: []byte("secret-key-of-32-bytes-length!!!"),
		ContextKey:   "paseto-user",
	}))
}
`,
		"handlers.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func me(c *fiber.Ctx) error {
	user := c.Locals("user").(*jwt.Token)
	claims := user.Claims.(jwt.MapClaims)
	return c.SendString(claims["name"].(string))
}

func custom(c *fiber.Ctx) error {
	user := c.Locals("jwt-user").(*jwt.Token)
	return c.SendString(user.Raw)
}
`,
		"paseto.go": `package main

import (
	pasetoware "github.com/gofiber/contrib/paseto"
	"github.com/gofiber/fiber/v2"
)

func payload(c *fiber.Ctx) error {
	p := c.Locals(pasetoware.DefaultContextKey)
	n := c.Locals("paseto-user").(string)
	return c.JSON(fiber.Map{"p": p, "n": n})
}
`,
	})

	handlers := run.file(t, "handlers.go")
	assert.Contains(t, handlers, "user := jwtware.FromContext(c)")
	assert.Equal(t, 2, countOf(handlers, "jwtware.FromContext(c)"))
	assert.Contains(t, handlers, `"github.com/gofiber/contrib/v3/jwt"`, "the package is called jwtware, whatever the last element of its path")
	assert.NotContains(t, handlers, "Locals")

	paseto := run.file(t, "paseto.go")
	assert.Contains(t, paseto, "p := pasetoware.FromContext(c)")
	assert.Contains(t, paseto, "n := pasetoware.FromContext(c).(string)", "the payload is an any, the assertion stays")
	assert.NotContains(t, paseto, "DefaultContextKey")

	main := run.file(t, "main.go")
	assert.NotContains(t, main, "ContextKey")
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_SeveralMiddlewareOnOneKey(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"keyauth.go": `package main

import "github.com/gofiber/fiber/v2/middleware/keyauth"

var _ = keyauth.New(keyauth.Config{ContextKey: "token"})
`,
		"csrf.go": `package main

import "github.com/gofiber/fiber/v2/middleware/csrf"

var _ = csrf.New(csrf.Config{ContextKey: "token"})
`,
		"ka_handler.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/keyauth"
)

var _ = keyauth.ErrMissingOrMalformedAPIKey

func handlerKA(c *fiber.Ctx) error {
	token := c.Locals("token").(string)
	return c.SendString(token)
}
`,
		"csrf_handler.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.ErrTokenNotFound

func handlerCSRF(c *fiber.Ctx) error {
	token := c.Locals("token").(string)
	return c.SendString(token)
}
`,
		"neither.go": `package main

import "github.com/gofiber/fiber/v2"

func handlerNone(c *fiber.Ctx) error {
	return c.SendString(c.Locals("token").(string))
}
`,
	}
	run := mw2Apply(t, v3.MigrateMiddlewareLocals, files)

	assert.Contains(t, run.file(t, "ka_handler.go"), "token := keyauth.TokenFromContext(c)")
	assert.Contains(t, run.file(t, "csrf_handler.go"), "token := csrf.TokenFromContext(c)")
	assert.Contains(t, run.file(t, "neither.go"), `c.Locals("token").(string)`, "the file does not say which middleware it means")
	require.Len(t, run.notes, 3)
	assert.Equal(t, "csrf.go", run.notes[0].File)
	assert.Equal(t, "keyauth.go", run.notes[1].File)
	assert.Equal(t, "neither.go", run.notes[2].File)
	assert.Contains(t, run.notes[2].Message, "csrf and keyauth store a value under that key")
	assert.Contains(t, run.notes[2].Message, "csrf.TokenFromContext(c) or keyauth.TokenFromContext(c)")
	assert.Contains(t, run.file(t, "keyauth.go"), "ContextKey: \"token\"", "a read of the key was left, so the field stays")
	assert.Contains(t, run.file(t, "csrf.go"), "ContextKey: \"token\"")
	assert.Contains(t, run.notes[0].Message, "was kept")
}

func Test_MigrateMiddlewareLocals_ConfigAssignment(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateMiddlewareLocals, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func Setup(app *fiber.App) {
	cfg := requestid.ConfigDefault
	cfg.ContextKey = "rid"
	cfg.Header = "X-Trace"
	app.Use(requestid.New(cfg))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString(c.Locals("rid").(string)) })
}
`)

	assert.Contains(t, content, "return c.SendString(requestid.FromContext(c))")
	assert.NotContains(t, content, "ContextKey")
	assert.Contains(t, content, `cfg.Header = "X-Trace"`)
}

func Test_MigrateMiddlewareLocals_AppConfigWithAContextKeyField(t *testing.T) {
	t.Parallel()

	// An application type that happens to have a ContextKey field, and a key the middleware don't use.
	src := `package main

import "github.com/gofiber/fiber/v2"

type AuthConfig struct {
	ContextKey string
	Realm      string
}

func newAuth(cfg AuthConfig) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(cfg.ContextKey, "bob")
		return c.Next()
	}
}

func main() {
	app := fiber.New()
	app.Use(newAuth(AuthConfig{ContextKey: "user", Realm: "api"}))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString(c.Locals("user").(string)) })
}
`
	content, run := mw2Single(t, v3.MigrateMiddlewareLocals, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateMiddlewareLocals_SessionKeptNothingInLocals(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New()

func handler(c *fiber.Ctx) error {
	sess := c.Locals("session")
	_ = sess
	return nil
}
`
	content, _ := mw2Single(t, v3.MigrateMiddlewareLocals, src)

	assert.Equal(t, src, content)
}

func Test_MigrateMiddlewareLocals_NestedModulesAreSeparate(t *testing.T) {
	t.Parallel()

	src := `package tools

import "github.com/gofiber/fiber/v2"

func id(c *fiber.Ctx) string { return c.Locals("requestid").(string) }
`
	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"go.mod":        "module sample\n\ngo 1.25\n",
		"main.go":       localsSetup + "\nfunc whoami(c *fiber.Ctx) error { return nil }\n",
		"tools/go.mod":  "module sample/tools\n\ngo 1.25\n",
		"tools/tool.go": src,
	})

	assert.Equal(t, src, run.file(t, "tools/tool.go"), "requestid is not used by the module of the tools")
}

func Test_MigrateMiddlewareLocals_DynamicKeysAreMentioned(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func get(c *fiber.Ctx, key string) any { return c.Locals(key) }

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{ContextKey: "rid"}))
}
`,
	})

	assert.NotContains(t, run.file(t, "main.go"), "ContextKey")
	require.Len(t, run.notes, 1)
	assert.Equal(t, 12, run.notes[0].Line)
	assert.Contains(t, run.notes[0].Message, "main.go:8")
	assert.Contains(t, run.notes[0].Message, "cannot be resolved")
	assert.Contains(t, run.notes[0].Message, "requestid.FromContext(c)")
}

func Test_MigrateMiddlewareLocals_OtherReceivers(t *testing.T) {
	t.Parallel()

	// Locals of a file that does not import Fiber is not Fiber's.
	src := `package main

type store struct{}

func (store) Locals(key string) any { return nil }

func read(s store) string { return s.Locals("requestid").(string) }
`
	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{"main.go": localsSetup, "other.go": src})

	assert.Equal(t, src, run.file(t, "other.go"))
}

func Test_MigrateMiddlewareLocals_Idempotent(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go": localsSetup,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v2"

func whoami(c *fiber.Ctx) error {
	tok, ok := c.Locals("token").(string)
	if c.Locals("requestid") == nil || !ok {
		return nil
	}
	return c.SendString(tok)
}
`,
	})
	firstMain, firstHandlers := run.file(t, "main.go"), run.file(t, "handlers.go")
	assert.Contains(t, run.out, "Migrating middleware locals")

	second := mw2Again(t, v3.MigrateMiddlewareLocals, run.dir)

	assert.Equal(t, firstMain, second.file(t, "main.go"))
	assert.Equal(t, firstHandlers, second.file(t, "handlers.go"))
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

func Test_MigrateMiddlewareLocals_SkipsFilesThatDoNotParse(t *testing.T) {
	t.Parallel()

	broken := `package main

import "github.com/gofiber/fiber/v2"

func whoami(c *fiber.Ctx) error {
	return c.SendString(c.Locals("requestid").(string)
}
`
	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"main.go":     localsSetup,
		"a_broken.go": broken,
		"b_good.go": `package main

import "github.com/gofiber/fiber/v2"

func good(c *fiber.Ctx) string { return c.Locals("requestid").(string) }
`,
	})

	assert.Equal(t, broken, run.file(t, "a_broken.go"))
	assert.Contains(t, run.file(t, "b_good.go"), "requestid.FromContext(c)")
	require.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0].Message, "does not parse")
}

func Test_MigrateMiddlewareLocals_KeyOfAnotherPackage(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateMiddlewareLocals, map[string]string{
		"go.mod": "module sample\n\ngo 1.25\n",
		"keys/keys.go": `package keys

const RequestID = "rid"
`,
		"main.go": `package main

import (
	"sample/keys"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{ContextKey: keys.RequestID}))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(c.Locals(keys.RequestID).(string))
	})
}
`,
	})

	main := run.file(t, "main.go")
	assert.Contains(t, main, "return c.SendString(requestid.FromContext(c))")
	assert.NotContains(t, main, "ContextKey")
	// the constant is in the module: its value is known, and nothing is left to say
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_RemovesTheKeyFieldInEveryLayout(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateMiddlewareLocals, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

var (
	alone    = requestid.Config{ContextKey: "a"}
	trailing = requestid.Config{ContextKey: "a",}
	first    = requestid.Config{ContextKey: "a", Header: "X-One"}
	last     = requestid.Config{Header: "X-Two", ContextKey: "a"}
	middle   = requestid.Config{Header: "X-Three", ContextKey: "a", Next: nil}
	lines    = requestid.Config{
		Header: "X-Four",
		// the key under which the id is stored
		ContextKey: "a", // key comment
		Next:       nil,
	}
	lastLine = requestid.Config{
		Header:     "X-Five",
		ContextKey: "a"}
)

func main() {
	app := fiber.New()
	app.Use(requestid.New(alone), requestid.New(trailing), requestid.New(first), requestid.New(last), requestid.New(middle), requestid.New(lines), requestid.New(lastLine))
}
`)

	assert.NotContains(t, content, "ContextKey")
	assert.NotContains(t, content, "key comment")
	assert.NotContains(t, content, "the key under which")
	assert.Contains(t, content, "alone    = requestid.Config{}")
	assert.Contains(t, content, "trailing = requestid.Config{}")
	assert.Contains(t, content, `first    = requestid.Config{Header: "X-One"}`)
	assert.Contains(t, content, `last     = requestid.Config{Header: "X-Two"}`)
	assert.Contains(t, content, `middle   = requestid.Config{Header: "X-Three", Next: nil}`)
	assert.Contains(t, content, "Header: \"X-Four\",\n\t\tNext:   nil,")
	assert.Contains(t, content, `Header: "X-Five"}`)
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_CSRFHandlerAfterTheTypeWasRenamed(t *testing.T) {
	t.Parallel()

	// The csrf migration renames CSRFHandler to Handler; the read is the same either way.
	content, _ := mw2Single(t, v3.MigrateMiddlewareLocals, `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
)

func main() {
	app := fiber.New()
	app.Use(csrf.New())
	app.Post("/logout", func(c fiber.Ctx) error {
		if handler, ok := c.Locals(csrf.ConfigDefault.HandlerContextKey).(*csrf.Handler); ok {
			return handler.DeleteToken(c)
		}
		return nil
	})
}
`)

	assert.Contains(t, content, "if handler, ok := csrf.HandlerFromContext(c), csrf.HandlerFromContext(c) != nil; ok {")
}

func Test_MigrateMiddlewareLocals_V3CodeIsLeftAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/keyauth"
	"github.com/gofiber/fiber/v3/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New())
	app.Use(keyauth.New(keyauth.Config{Validator: func(c fiber.Ctx, key string) (bool, error) { return true, nil }}))
	app.Get("/", func(c fiber.Ctx) error {
		c.Locals("mine", 1)
		return c.SendString(requestid.FromContext(c) + keyauth.TokenFromContext(c))
	})
}
`
	content, run := mw2Single(t, v3.MigrateMiddlewareLocals, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
	assert.Empty(t, run.notes)
}

func Test_MigrateMiddlewareLocals_WindowsLineEndings(t *testing.T) {
	t.Parallel()

	src := strings.ReplaceAll(`package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New(requestid.Config{
		Header: "X-Trace",
		// the key
		ContextKey: "rid", // under which the id is stored
		Next:       nil,
	}))
	app.Get("/", func(c *fiber.Ctx) error {
		id, ok := c.Locals("rid").(string)
		if !ok {
			return nil
		}
		return c.SendString(id)
	})
}
`, "\n", "\r\n")
	content, _ := mw2Single(t, v3.MigrateMiddlewareLocals, src)

	assert.NotContains(t, content, "ContextKey")
	assert.NotContains(t, content, "under which")
	assert.Contains(t, content, "id, ok := requestid.FromContext(c), requestid.FromContext(c) != \"\"")
	assert.Contains(t, content, "Header: \"X-Trace\",\n\t\tNext:   nil,")
}
