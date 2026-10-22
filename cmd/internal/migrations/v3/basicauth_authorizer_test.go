package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const baImports = `import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
)
`

func Test_MigrateBasicauthAuthorizer(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateBasicauthAuthorizer, `package main

`+baImports+`
func typed() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Authorizer: func(user, pass string) bool {
			return user == "john" && pass == "doe"
		},
	})
}

func separate() fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: func(username string, password string) bool { return username != "" }})
}

func blank() fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: func(_, _ string) bool { return true }})
}

func unnamed() fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: func(string, string) bool { return true }})
}

func multiLine() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Authorizer: func(
			user string,
			pass string,
		) bool {
			return user == pass
		},
	})
}
`)

	assert.Equal(t, `package main

`+baImports+`
func typed() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Authorizer: func(user, pass string, _ fiber.Ctx) bool {
			return user == "john" && pass == "doe"
		},
	})
}

func separate() fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: func(username string, password string, _ fiber.Ctx) bool { return username != "" }})
}

func blank() fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: func(_, _ string, _ fiber.Ctx) bool { return true }})
}

func unnamed() fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: func(string, string, fiber.Ctx) bool { return true }})
}

func multiLine() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Authorizer: func(
			user string,
			pass string,
			_ fiber.Ctx,
		) bool {
			return user == pass
		},
	})
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating basicauth authorizer")
	assert.Empty(t, res.notes)
}

func Test_MigrateBasicauthAuthorizer_NamedFunctions(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateBasicauthAuthorizer, `package main

`+baImports+`
func checkCreds(user, pass string) bool { return user == pass }

type authService struct{}

func (a *authService) Authorize(user, pass string) bool { return user == pass }

func named() fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: checkCreds})
}

func method(svc *authService) fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: svc.Authorize})
}

func variable() fiber.Handler {
	authFn := func(user, pass string) bool { return user == "a" }
	cfg := basicauth.Config{}
	cfg.Authorizer = authFn
	return basicauth.New(cfg)
}

func shadowing(user *authService) fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: user.Authorize})
}
`)

	file := res.file()
	assert.Contains(t, file, `Authorizer: func(user, pass string, _ fiber.Ctx) bool { return checkCreds(user, pass) }`)
	assert.Contains(t, file, `Authorizer: func(user, pass string, _ fiber.Ctx) bool { return svc.Authorize(user, pass) }`)
	assert.Contains(t, file, `cfg.Authorizer = func(user, pass string, _ fiber.Ctx) bool { return authFn(user, pass) }`)
	// a parameter called user must not hide the receiver
	assert.Contains(t, file, `Authorizer: func(userArg, passArg string, _ fiber.Ctx) bool { return user.Authorize(userArg, passArg) }`)
	assert.Empty(t, res.notes)
}

func Test_MigrateBasicauthAuthorizer_AlreadyTakesTheContext(t *testing.T) {
	t.Parallel()

	src := `package main

` + baImports + `
func checkCreds(user, pass string, c fiber.Ctx) bool { return user == pass }

type authService struct{}

func (a *authService) Authorize(user, pass string, c fiber.Ctx) bool { return user == pass }

func named(svc *authService) []fiber.Handler {
	return []fiber.Handler{
		basicauth.New(basicauth.Config{Authorizer: checkCreds}),
		basicauth.New(basicauth.Config{Authorizer: svc.Authorize}),
		basicauth.New(basicauth.Config{Authorizer: func(user, pass string, _ fiber.Ctx) bool { return true }}),
		basicauth.New(basicauth.Config{Authorizer: nil}),
	}
}
`
	res := mw1Run(t, v3.MigrateBasicauthAuthorizer, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)
}

func Test_MigrateBasicauthAuthorizer_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateBasicauthAuthorizer, `package main

`+baImports+`
func checkCreds(user, pass string) bool { return user == pass }

func handlers() []fiber.Handler {
	return []fiber.Handler{
		basicauth.New(basicauth.Config{Authorizer: checkCreds}),
		basicauth.New(basicauth.Config{Authorizer: func(user, pass string) bool { return true }}),
	}
}
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateBasicauthAuthorizer, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
}

func Test_MigrateBasicauthAuthorizer_OnlyBasicauthConfigs(t *testing.T) {
	t.Parallel()

	// the application's own type with an Authorizer field
	src := `package main

` + baImports + `
type gate struct {
	Authorizer func(user, token string) bool
}

func newGate() gate {
	return gate{Authorizer: func(user, token string) bool { return user == token }}
}

func handler(g gate) fiber.Handler {
	return basicauth.New(basicauth.Config{Realm: "x"})
}
`
	res := mw1Run(t, v3.MigrateBasicauthAuthorizer, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateBasicauthAuthorizer_ComputedAuthorizerIsReported(t *testing.T) {
	t.Parallel()

	src := `package main

` + baImports + `
func handler(db *DB) fiber.Handler {
	return basicauth.New(basicauth.Config{Authorizer: db.Authorizer()})
}
`
	res := mw1Run(t, v3.MigrateBasicauthAuthorizer, src)

	assert.Equal(t, src, res.file())
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], "takes the request context as a third argument in v3: wrap db.Authorizer()")
	}
}
