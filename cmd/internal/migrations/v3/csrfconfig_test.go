package v3_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const csrfImports = `import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)
`

const csrfExtractorsImport = `"github.com/gofiber/fiber/v3/extractors"`

func Test_MigrateCSRFConfig(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.New(csrf.Config{
	Expiration: 10 * time.Minute,
	SessionKey: "csrf", // session comment
	CookieName: "csrf_",
})
`)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.New(csrf.Config{
	IdleTimeout: 10 * time.Minute,
	CookieName:  "csrf_",
})
`, res.file())
	assert.Contains(t, res.stdout, "Migrating CSRF middleware configs")
	assert.Empty(t, res.notes)
}

func Test_MigrateCSRFConfig_KeyLookup(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

`+csrfImports+`
const lookupName = "X-Token"

func configs() []fiber.Handler {
	return []fiber.Handler{
		csrf.New(csrf.Config{KeyLookup: "header:X-CSRF-Token"}),
		csrf.New(csrf.Config{KeyLookup: "form:_csrf", CookieName: "c"}),
		csrf.New(csrf.Config{KeyLookup: "query:csrf"}),
		csrf.New(csrf.Config{KeyLookup: "param:csrf"}),
		csrf.New(csrf.Config{TokenLookup: "header:X-XSRF-TOKEN"}),
		csrf.New(csrf.Config{KeyLookup: "header:" + csrf.HeaderName}),
		csrf.New(csrf.Config{KeyLookup: "form:" + lookupName}),
		csrf.New(csrf.Config{KeyLookup: "header:X-" + lookupName}),
		csrf.New(csrf.Config{KeyLookup: ""}),
		csrf.New(csrf.Config{
			KeyLookup:  "header:X-Csrf-Token", // where the token is sent
			CookieName: "csrf_",
		}),
	}
}
`)

	file := res.file()
	for _, want := range []string{
		`csrf.Config{Extractor: extractors.FromHeader("X-CSRF-Token")}`,
		`csrf.Config{Extractor: extractors.FromForm("_csrf"), CookieName: "c"}`,
		`csrf.Config{Extractor: extractors.FromQuery("csrf")}`,
		`csrf.Config{Extractor: extractors.FromParam("csrf")}`,
		`csrf.Config{Extractor: extractors.FromHeader("X-XSRF-TOKEN")}`,
		`csrf.Config{Extractor: extractors.FromHeader(csrf.HeaderName)}`,
		`csrf.Config{Extractor: extractors.FromForm(lookupName)}`,
		`csrf.Config{Extractor: extractors.FromHeader("X-" + lookupName)}`,
		"\t\tcsrf.New(csrf.Config{}),\n",
		"Extractor:  extractors.FromHeader(\"X-Csrf-Token\"), // where the token is sent",
	} {
		assert.Contains(t, file, want)
	}
	assert.NotContains(t, file, "KeyLookup")
	assert.NotContains(t, file, "TokenLookup")
	assert.Contains(t, file, csrfExtractorsImport)
	assert.Empty(t, res.notes)
}

func Test_MigrateCSRFConfig_LookupWithoutAnEquivalentStaysAndIsReported(t *testing.T) {
	t.Parallel()

	src := `package main

` + csrfImports + `
func configs(lookup string) []fiber.Handler {
	return []fiber.Handler{
		csrf.New(csrf.Config{KeyLookup: "cookie:csrf_"}),
		csrf.New(csrf.Config{KeyLookup: lookup, CookieName: "c"}),
		csrf.New(csrf.Config{KeyLookup: "body:token"}),
		csrf.New(csrf.Config{
			KeyLookup:   "header:X-A",
			TokenLookup: "header:X-B",
		}),
	}
}
`
	res := mw1Run(t, v3.MigrateCSRFConfig, src)

	// no silent fall back to the default header extractor: the compiler points at the field
	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	if assert.Len(t, res.notes, 4) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: csrf.Config has no KeyLookup in v3", mw1LineOf(t, src, `"cookie:csrf_"`)))
		assert.Contains(t, res.notes[0], "v3 panics when the extractor reads the CSRF cookie")
	}
}

func Test_MigrateCSRFConfig_OneLineConfigsStayValid(t *testing.T) {
	t.Parallel()

	// the comment of a field that cannot be migrated used to swallow the rest of the line
	res := mw1RunFiles(t, v3.MigrateCSRFConfig, map[string]string{
		"main.go": `package main

` + csrfImports + `
func a() fiber.Handler { return csrf.New(csrf.Config{KeyLookup: "param:csrf"}) }
func b() fiber.Handler { return csrf.New(csrf.Config{KeyLookup: "cookie:csrf_"}) }
`,
		"z.go": `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.New(csrf.Config{KeyLookup: "header:X-Csrf-Token", Expiration: time.Hour})
`,
	})

	assert.Contains(t, res.files["main.go"], `csrf.New(csrf.Config{Extractor: extractors.FromParam("csrf")})`)
	assert.Contains(t, res.files["main.go"], `csrf.Config{KeyLookup: "cookie:csrf_"}`)
	assert.Contains(t, res.files["z.go"], `csrf.Config{Extractor: extractors.FromHeader("X-Csrf-Token"), IdleTimeout: time.Hour}`)
}

func Test_MigrateCSRFConfig_ExtractorHelpers(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

import (
	cs "github.com/gofiber/fiber/v2/middleware/csrf"
)

var a = cs.Config{Extractor: cs.CsrfFromHeader(cs.HeaderName)}
var b = cs.Config{Extractor: cs.CsrfFromForm("_csrf")}
var c = cs.Config{Extractor: cs.CsrfFromQuery("csrf")}
var d = cs.Config{Extractor: cs.CsrfFromParam("csrf")}
`)

	file := res.file()
	assert.Contains(t, file, `var a = cs.Config{Extractor: extractors.FromHeader(cs.HeaderName)}`)
	assert.Contains(t, file, `var b = cs.Config{Extractor: extractors.FromForm("_csrf")}`)
	assert.Contains(t, file, `var c = cs.Config{Extractor: extractors.FromQuery("csrf")}`)
	assert.Contains(t, file, `var d = cs.Config{Extractor: extractors.FromParam("csrf")}`)
	assert.Contains(t, file, csrfExtractorsImport)
}

func Test_MigrateCSRFConfig_ExtractorHelperThatIsCalled(t *testing.T) {
	t.Parallel()

	// v2 returned a function, v3 an extractor
	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

`+csrfImports+`
func token(c *fiber.Ctx) (string, error) {
	return csrf.CsrfFromHeader("X-Token")(c)
}
`)

	assert.Contains(t, res.file(), `return extractors.FromHeader("X-Token").Extract(c)`)
}

func Test_MigrateCSRFConfig_CookieExtractorIsReported(t *testing.T) {
	t.Parallel()

	src := `package main

` + csrfImports + `
var _ = csrf.New(csrf.Config{Extractor: csrf.CsrfFromCookie("csrf_")})
`
	res := mw1Run(t, v3.MigrateCSRFConfig, src)

	assert.Equal(t, src, res.file())
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], "csrf.CsrfFromCookie is gone in v3, which panics when the extractor reads the CSRF cookie")
	}
}

func Test_MigrateCSRFConfig_CustomExtractor(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

`+csrfImports+`
func fromBody(c *fiber.Ctx) (string, error) {
	return c.FormValue("token"), nil
}

type extractor struct{}

func (extractor) Extract(c *fiber.Ctx) (string, error) { return "", csrf.ErrMissingForm }

func configs(e extractor) []fiber.Handler {
	return []fiber.Handler{
		csrf.New(csrf.Config{
			Extractor: func(c *fiber.Ctx) (string, error) {
				if v := c.Get("X-Token"); v != "" {
					return v, nil
				}
				return "", csrf.ErrMissingHeader
			},
		}),
		csrf.New(csrf.Config{Extractor: func(c *fiber.Ctx) (string, error) { return c.Get("a") + c.Get("b"), nil }}),
		csrf.New(csrf.Config{Extractor: fromBody}),
		csrf.New(csrf.Config{Extractor: e.Extract}),
	}
}
`)

	file := res.file()
	assert.Contains(t, file, `Extractor: extractors.FromCustom("X-Token", func(c *fiber.Ctx) (string, error) {`)
	// inside the extractor the missing token is the error of the extractors
	assert.Contains(t, file, `return "", extractors.ErrNotFound`)
	assert.Contains(t, file, `Extractor: extractors.FromCustom("csrf", func(c *fiber.Ctx) (string, error) { return c.Get("a") + c.Get("b"), nil })`)
	assert.Contains(t, file, `Extractor: extractors.FromCustom("csrf", fromBody)`)
	assert.Contains(t, file, `Extractor: extractors.FromCustom("csrf", e.Extract)`)
	// outside of it the error handler gets csrf.ErrTokenNotFound
	assert.Contains(t, file, `return "", csrf.ErrTokenNotFound }`)
	assert.Contains(t, file, csrfExtractorsImport)
}

func Test_MigrateCSRFConfig_ExtractorOfV3IsKept(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/csrf"
)

var header = extractors.FromHeader("X-Csrf-Token")

var a = csrf.Config{Extractor: header}
var b = csrf.Config{Extractor: extractors.Chain(extractors.FromHeader("a"), extractors.FromForm("b"))}
var c = csrf.Config{Extractor: extractors.FromCustom("x", nil)}
var d = csrf.Config{IdleTimeout: 1, Extractor: extractors.FromParam("csrf")}
`
	res := mw1Run(t, v3.MigrateCSRFConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateCSRFConfig_ExtractorWinsOverTheLookup(t *testing.T) {
	t.Parallel()

	// v2 ignored the lookup when an extractor was given
	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

`+csrfImports+`
var _ = csrf.New(csrf.Config{
	KeyLookup: "cookie:csrf_",
	Extractor: csrf.CsrfFromHeader("X-A"),
})
`)

	assert.Contains(t, res.file(), `Extractor: extractors.FromHeader("X-A"),`)
	assert.NotContains(t, res.file(), "KeyLookup")
	assert.Empty(t, res.notes)
}

func Test_MigrateCSRFConfig_DeprecatedFields(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.New(csrf.Config{
	TokenLookup:   "header:X-XSRF-TOKEN",
	CookieExpires: 2 * time.Hour,
	Cookie: &fiber.Cookie{
		Name:     "XSRF-TOKEN",
		Domain:   "example.com",
		Path:     "/",
		Secure:   true,
		HTTPOnly: false,
		SameSite: "Strict",
		Expires:  time.Now(),
	},
})

var _ = csrf.New(csrf.Config{Cookie: &fiber.Cookie{Name: "c", Secure: true}})
var _ = csrf.New(csrf.Config{Cookie: &fiber.Cookie{Value: "ignored"}})
`)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/csrf"
	`+csrfExtractorsImport+`
)

var _ = csrf.New(csrf.Config{
	Extractor:      extractors.FromHeader("X-XSRF-TOKEN"),
	IdleTimeout:    2 * time.Hour,
	CookieName:     "XSRF-TOKEN",
	CookieDomain:   "example.com",
	CookiePath:     "/",
	CookieSecure:   true,
	CookieHTTPOnly: false,
	CookieSameSite: "Strict",
})

var _ = csrf.New(csrf.Config{CookieName: "c", CookieSecure: true})
var _ = csrf.New(csrf.Config{})
`, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateCSRFConfig_DeprecatedFieldsThatConflict(t *testing.T) {
	t.Parallel()

	src := `package main

` + csrfImports + `
var cookie = &fiber.Cookie{Name: "c"}

var a = csrf.Config{Cookie: cookie}
var b = csrf.Config{Cookie: &fiber.Cookie{Name: "c"}, CookieName: "other"}
var c = csrf.Config{Cookie: &fiber.Cookie{Name: "c"}, CookieSecure: true}
var d = csrf.Config{CookieExpires: 1, Expiration: 2}
`
	res := mw1Run(t, v3.MigrateCSRFConfig, src)

	assert.Contains(t, res.file(), `var a = csrf.Config{Cookie: cookie}`)
	assert.Contains(t, res.file(), `var b = csrf.Config{Cookie: &fiber.Cookie{Name: "c"}, CookieName: "other"}`)
	assert.Contains(t, res.file(), `var c = csrf.Config{Cookie: &fiber.Cookie{Name: "c"}, CookieSecure: true}`)
	assert.Contains(t, res.file(), `var d = csrf.Config{CookieExpires: 1, IdleTimeout: 2}`)
	assert.Len(t, res.notes, 4)
}

func Test_MigrateCSRFConfig_Errors(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

func handler(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, csrf.ErrNoReferer), errors.Is(err, csrf.ErrBadReferer):
		return fiber.ErrForbidden
	case err == csrf.ErrBadReferer, csrf.ErrBadReferer == err:
		return fiber.ErrForbidden
	case err != csrf.ErrBadReferer:
		return nil
	case errors.Is(err, csrf.ErrMissingHeader), errors.Is(err, csrf.ErrMissingForm), err == csrf.ErrMissingQuery:
		return fiber.ErrBadRequest
	case errors.Is(err, csrf.ErrMissingParam), errors.Is(err, csrf.ErrMissingCookie):
		return fiber.ErrBadRequest
	}
	switch err {
	case csrf.ErrTokenNotFound, csrf.ErrBadReferer:
		return nil
	}
	return err
}
`)

	file := res.file()
	for _, want := range []string{
		`errors.Is(err, csrf.ErrRefererNotFound), (errors.Is(err, csrf.ErrRefererInvalid) || errors.Is(err, csrf.ErrRefererNoMatch))`,
		`case (err == csrf.ErrRefererInvalid || err == csrf.ErrRefererNoMatch), (err == csrf.ErrRefererInvalid || err == csrf.ErrRefererNoMatch):`,
		`case (err != csrf.ErrRefererInvalid && err != csrf.ErrRefererNoMatch):`,
		`errors.Is(err, csrf.ErrTokenNotFound), errors.Is(err, csrf.ErrTokenNotFound), err == csrf.ErrTokenNotFound`,
		`case csrf.ErrTokenNotFound, csrf.ErrRefererInvalid, csrf.ErrRefererNoMatch:`,
	} {
		assert.Contains(t, file, want)
	}
	assert.NotContains(t, file, "ErrMissing")
	assert.NotContains(t, file, "ErrBadReferer")
	assert.NotContains(t, file, "ErrNoReferer")
	assert.Empty(t, res.notes)
}

func Test_MigrateCSRFConfig_BadRefererThatCannotBeSplit(t *testing.T) {
	t.Parallel()

	src := `package main

` + csrfImports + `
var errs = []error{csrf.ErrBadReferer}

func is(err error) bool {
	return errors.Is(wrap(err), csrf.ErrBadReferer)
}
`
	res := mw1Run(t, v3.MigrateCSRFConfig, src)

	assert.Equal(t, src, res.file())
	if assert.Len(t, res.notes, 2) {
		assert.Contains(t, res.notes[0], "csrf.ErrBadReferer is split in v3 into csrf.ErrRefererInvalid")
	}
}

func Test_MigrateCSRFConfig_Handler(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

`+csrfImports+`
func mw() fiber.Handler {
	return csrf.New(csrf.Config{
		CookieName:        "csrf_",
		HandlerContextKey: "csrfHandler",
	})
}

func single(c *fiber.Ctx) error {
	handler := c.Locals(csrf.ConfigDefault.HandlerContextKey).(*csrf.CSRFHandler)
	return handler.DeleteToken(c)
}

func fieldAndDocs(c *fiber.Ctx) error {
	if handler, ok := c.Locals(csrf.ConfigDefault.HandlerContextKey).(*csrf.CSRFHandler); ok {
		if err := handler.DeleteToken(c); err != nil {
			return err
		}
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func negated(c *fiber.Ctx) error {
	if h, ok := c.Locals("fiber.csrf.handler").(*csrf.CSRFHandler); !ok {
		return fiber.ErrInternalServerError
	} else {
		return h.DeleteToken(c)
	}
}

func block(c *fiber.Ctx) error {
	handler, ok := c.Locals("csrfHandler").(*csrf.CSRFHandler)
	if !ok {
		return fiber.ErrInternalServerError
	}
	return handler.DeleteToken(c)
}

func blank(c *fiber.Ctx) bool {
	if _, ok := c.Locals("csrfHandler").(*csrf.CSRFHandler); ok {
		return true
	}
	return false
}

func typed(h *csrf.CSRFHandler, c *fiber.Ctx) error { return h.DeleteToken(c) }
`)

	assert.Equal(t, `package main

`+csrfImports+`
func mw() fiber.Handler {
	return csrf.New(csrf.Config{
		CookieName: "csrf_",
	})
}

func single(c *fiber.Ctx) error {
	handler := csrf.HandlerFromContext(c)
	return handler.DeleteToken(c)
}

func fieldAndDocs(c *fiber.Ctx) error {
	if handler := csrf.HandlerFromContext(c); handler != nil {
		if err := handler.DeleteToken(c); err != nil {
			return err
		}
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func negated(c *fiber.Ctx) error {
	if h := csrf.HandlerFromContext(c); h == nil {
		return fiber.ErrInternalServerError
	} else {
		return h.DeleteToken(c)
	}
}

func block(c *fiber.Ctx) error {
	handler := csrf.HandlerFromContext(c)
	ok := handler != nil
	if !ok {
		return fiber.ErrInternalServerError
	}
	return handler.DeleteToken(c)
}

func blank(c *fiber.Ctx) bool {
	if csrf.HandlerFromContext(c) != nil {
		return true
	}
	return false
}

func typed(h *csrf.Handler, c *fiber.Ctx) error { return h.DeleteToken(c) }
`, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateCSRFConfig_HandlerLookupsThatAreNotRewritten(t *testing.T) {
	t.Parallel()

	src := `package main

` + csrfImports + `
func lookups(c *fiber.Ctx, v any) {
	stored := c.Locals("csrfHandler")
	_ = stored.(*csrf.CSRFHandler)
	var handler, ok = c.Locals("csrfHandler").(*csrf.CSRFHandler)
	_, _ = handler, ok
	_ = csrf.ConfigDefault.HandlerContextKey
}
`
	res := mw1Run(t, v3.MigrateCSRFConfig, src)

	// still v2, so that the compiler points at it
	assert.Equal(t, src, res.file())
	assert.Len(t, res.notes, 3)
	assert.Contains(t, strings.Join(res.notes, "\n"), "csrf.HandlerFromContext(c)")
}

func Test_MigrateCSRFConfig_Assignments(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/csrf"
)

func config() csrf.Config {
	cfg := csrf.ConfigDefault
	cfg.Expiration = 5 * time.Minute
	cfg.KeyLookup = "form:_csrf"
	var other csrf.Config
	other.CookieExpires = time.Hour
	other.TokenLookup = ""
	return cfg
}
`)

	// the extractors import is the migration's, not goimports': an empty module cache does not have it
	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v3/extractors"
)

func config() csrf.Config {
	cfg := csrf.ConfigDefault
	cfg.IdleTimeout = 5 * time.Minute
	cfg.Extractor = extractors.FromForm("_csrf")
	var other csrf.Config
	other.IdleTimeout = time.Hour
	other.Extractor = extractors.FromHeader("X-Csrf-Token")
	return cfg
}
`, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateCSRFConfig_AssignmentsOfFieldsThatAreGone(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

func config() csrf.Config {
	cfg := csrf.Config{}
	cfg.SessionKey = "csrf"
	cfg.HandlerContextKey = "handler" // the key
	cfg.Cookie = &fiber.Cookie{Name: "csrf_", Secure: true}
	cfg.CookieName = "other"
	return cfg
}
`)

	// the keys have no effect in v3 and go, the cookie is the user's to spread over the fields
	assert.Equal(t, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

func config() csrf.Config {
	cfg := csrf.Config{}
	cfg.Cookie = &fiber.Cookie{Name: "csrf_", Secure: true}
	cfg.CookieName = "other"
	return cfg
}
`, res.file())
	require.Len(t, res.notes, 1)
	assert.Contains(t, res.notes[0], "main.go:10: ")
	assert.Contains(t, res.notes[0], "csrf.Config has no Cookie in v3: set CookieName")
}

func Test_MigrateCSRFConfig_Alias(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

import (
	"time"

	f "github.com/gofiber/fiber/v2"
	cs "github.com/gofiber/fiber/v2/middleware/csrf"
)

func handler() f.Handler {
	return cs.New(cs.Config{KeyLookup: "header:X-Token", Expiration: time.Hour})
}
`)

	assert.Contains(t, res.file(), `cs.Config{Extractor: extractors.FromHeader("X-Token"), IdleTimeout: time.Hour}`)
}

func Test_MigrateCSRFConfig_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateCSRFConfig, `package main

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.New(csrf.Config{
	KeyLookup:  "header:X-Token",
	Expiration: time.Hour,
	SessionKey: "k",
	Cookie:     &fiber.Cookie{Name: "c"},
	Extractor:  nil,
})

var _ = csrf.New(csrf.Config{Extractor: func(c *fiber.Ctx) (string, error) { return "", csrf.ErrMissingForm }})

func handler(c *fiber.Ctx, err error) error {
	if h, ok := c.Locals("k").(*csrf.CSRFHandler); ok && errors.Is(err, csrf.ErrBadReferer) {
		return h.DeleteToken(c)
	}
	return nil
}
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateCSRFConfig, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
	assert.Empty(t, second.notes)
}

func Test_MigrateCSRFConfig_IgnoresOtherConfigs(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/keyauth"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/o1egl/paseto"
)

var _ = keyauth.New(keyauth.Config{
	KeyLookup: "header:Authorization",
})

var _ = session.New(session.Config{
	KeyLookup:  "cookie:__Host-session",
	Expiration: time.Hour,
})

var payload = &paseto.JSONToken{
	Expiration: time.Now(),
	NotBefore:  time.Now(),
}

type settings struct {
	Expiration time.Duration
	SessionKey string
	Cookie     string
}

var s = settings{Expiration: time.Hour, SessionKey: "k", Cookie: "c"}
`
	res := mw1Run(t, v3.MigrateCSRFConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)
}

func Test_MigrateCSRFConfig_NotesCarryTheLineOfTheOutput(t *testing.T) {
	t.Parallel()

	// the migration adds an import and removes lines above the construct it reports
	res := mw1Run(t, v3.MigrateCSRFConfig, `package main

import "github.com/gofiber/fiber/v2/middleware/csrf"

var a = csrf.Config{
	KeyLookup: "param:csrf",
	SessionKey: "k",
	HandlerContextKey: "h",
}

var b = csrf.Config{KeyLookup: "cookie:csrf_"}
`)

	line := mw1LineOf(t, res.file(), `KeyLookup: "cookie:csrf_"`)
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: ", line))
	}
}
