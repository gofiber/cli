package v3_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// the SHA-256 digests of the passwords below, computed with openssl
const (
	baHash123456 = "{SHA256}jZae727K08KaOmKSgOaGzww/XVqGr/PKEgIMkjrcbJI="
	baHashDoe    = "{SHA256}eZ75KhGvkY4/t0HfQpNPO1aO0tk6wd908bjUGieTKm8="
	baHashRaw    = "{SHA256}MZd/GJ7W/HmJcN504CeBrLMNxHy3OCVCwVHDDziaSjM="
	baHashBrace  = "{SHA256}c6suPslJhVeXTEPVNe9wBJiYWnJHsOcyc2PW62aIbe0="
	baHashQuote  = "{SHA256}azNYk5O+stoqk+Phd0aqka1lu5sH/N+e1xL2/S+TlvU="
	baHashSlash  = "{SHA256}21nfkMfrwOrH7pHHH7DjJldg1TUtcKACrg9NGbMF6E4="
	baHashS3cr3t = "{SHA256}K7gNU3sdo+OL0wNhqoVWhr3g6s1xYv72ol/pe/Unols="
)

func Test_MigrateBasicauthConfig_HashesTheUsers(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateBasicauthConfig, `package main

`+baImports+`
const adminPass = "123456"

func handler() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Users: map[string]string{
			"john":  "doe",     // john
			"admin": adminPass, // a constant
			"eve":   `+"`raw-pass`"+`,
			"bob":   "p}ss",
			"quote": "pa\"ss",
			"slash": "pa\\ss",
		},
		Realm: "Forbidden",
	})
}

func oneLine() fiber.Handler {
	return basicauth.New(basicauth.Config{Users: map[string]string{"john": "doe"}})
}
`)

	assert.Equal(t, `package main

`+baImports+`
const adminPass = "123456"

func handler() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Users: map[string]string{
			"john":  "`+baHashDoe+`", // john
			"admin": "`+baHash123456+`", // a constant
			"eve":   "`+baHashRaw+`",
			"bob":   "`+baHashBrace+`",
			"quote": "`+baHashQuote+`",
			"slash": "`+baHashSlash+`",
		},
		Realm: "Forbidden",
	})
}

func oneLine() fiber.Handler {
	return basicauth.New(basicauth.Config{Users: map[string]string{"john": "`+baHashDoe+`"}})
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating basicauth configs")
	assert.Empty(t, res.notes)
}

func Test_MigrateBasicauthConfig_KeepsWhatIsHashed(t *testing.T) {
	t.Parallel()

	src := `package main

` + baImports + `
func handler() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Users: map[string]string{
			"john":  "` + baHashDoe + `",
			"admin": "$2a$10$gTYwCN66/tBRoCr3.TXa1.v1iyvwIF7GRBqxzv7G.AHLMt/owXrp.",
			"sha":   "{SHA512}vSsar3708Jvp9Szi2NWZZ02Bqp1qRCFpbcTZPdBhnWgs5WtNZKnvCXdhztmeD2cmW192CF5bDufKRpayrW/isg==",
		},
	})
}
`
	res := mw1Run(t, v3.MigrateBasicauthConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateBasicauthConfig_PlaintextThatLooksLikeADigestIsStillPlaintext(t *testing.T) {
	t.Parallel()

	// in v2 every password is plaintext, openssl rand -base64 32 included
	password := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	res := mw1Run(t, v3.MigrateBasicauthConfig, fmt.Sprintf(`package main

`+baImports+`
func handler() fiber.Handler {
	return basicauth.New(basicauth.Config{Users: map[string]string{"svc": %q, "key": "c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0MTI="}})
}
`, password))

	assert.NotContains(t, res.file(), password)
	assert.NotContains(t, res.file(), "c2VjcmV0c2VjcmV0")
	assert.Equal(t, 2, strings.Count(res.file(), "{SHA256}"))
}

func Test_MigrateBasicauthConfig_UsersThatAreNotLiterals(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateBasicauthConfig, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
)

var users = map[string]string{"admin": "123456"}

func shared() fiber.Handler {
	return basicauth.New(basicauth.Config{Users: users})
}

func env() fiber.Handler {
	return basicauth.New(basicauth.Config{Users: map[string]string{
		"admin": os.Getenv("ADMIN_PASSWORD"),
		"john":  "doe",
	}})
}
`)

	file := res.file()
	hashing := `func(plain map[string]string) map[string]string {
		// v3 only accepts hashed passwords: they are hashed here, when the middleware is created
		hashed := make(map[string]string, len(plain))
		for user, pass := range plain {
			sum := sha256.Sum256([]byte(pass))
			hashed[user] = "{SHA256}" + base64.StdEncoding.EncodeToString(sum[:])
		}
		return hashed
	}(`
	assert.Contains(t, file, "Users: "+hashing+"users)")
	assert.Contains(t, file, hashing+"map[string]string{")
	// the plaintext of the map that is hashed at start-up is not touched
	assert.Contains(t, file, `"john":  "doe",`)
	assert.Contains(t, file, `"crypto/sha256"`)
	assert.Contains(t, file, `"encoding/base64"`)
	assert.Empty(t, res.notes)
}

func Test_MigrateBasicauthConfig_UsersNextToAnAuthorizerAreNotUsed(t *testing.T) {
	t.Parallel()

	src := `package main

` + baImports + `
func handler() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Users: map[string]string{"john": "doe"},
		Authorizer: func(user, pass string, _ fiber.Ctx) bool {
			return user == "john" && pass == "doe"
		},
	})
}
`
	res := mw1Run(t, v3.MigrateBasicauthConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateBasicauthConfig_ContextKeys(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateBasicauthConfig, `package main

`+baImports+`
func multi() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Realm:           "x",
		ContextUsername: "user", // username comment
		ContextPassword: "pass", // password comment
	})
}

func inline() fiber.Handler {
	return basicauth.New(basicauth.Config{ContextUsername: "user", ContextPassword: "pass"})
}

func assigned() fiber.Handler {
	cfg := basicauth.Config{}
	cfg.ContextUsername = "user"
	cfg.Realm = "x"
	return basicauth.New(cfg)
}
`)

	assert.Equal(t, `package main

`+baImports+`
func multi() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Realm: "x",
	})
}

func inline() fiber.Handler {
	return basicauth.New(basicauth.Config{})
}

func assigned() fiber.Handler {
	cfg := basicauth.Config{}
	cfg.Realm = "x"
	return basicauth.New(cfg)
}
`, res.file())
	// the password cannot be read back any more
	if assert.Len(t, res.notes, 2) {
		assert.Contains(t, res.notes[0], "ContextPassword was removed")
	}
}

func Test_MigrateBasicauthConfig_OnlyBasicauthConfigs(t *testing.T) {
	t.Parallel()

	// the application's own types with the field names of basicauth.Config
	src := `package main

import "github.com/gofiber/fiber/v2/middleware/basicauth"

type Directory struct {
	Users map[string]string
}

type Principal struct {
	ContextKey      string
	ContextUsername string
}

var dir = Directory{Users: map[string]string{"alice": "alice@example.com"}}

var p = Principal{
	ContextKey:      "principal",
	ContextUsername: "name",
}

var mw = basicauth.New(basicauth.Config{Realm: "x"})
`
	res := mw1Run(t, v3.MigrateBasicauthConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)

	// and a file that only has them
	other := `package main

type Directory struct {
	Users map[string]string
}

var dir = Directory{Users: map[string]string{"alice": "alice@example.com"}}
`
	res = mw1Run(t, v3.MigrateBasicauthConfig, other)
	assert.Equal(t, other, res.file())
}

func Test_MigrateBasicauthConfig_AssignedUsers(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateBasicauthConfig, `package main

`+baImports+`
func handler() fiber.Handler {
	cfg := basicauth.Config{}
	cfg.Users = map[string]string{"john": "doe"}
	return basicauth.New(cfg)
}
`)

	assert.Contains(t, res.file(), `cfg.Users = map[string]string{"john": "`+baHashDoe+`"}`)
}

func Test_MigrateBasicauthConfig_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateBasicauthConfig, `package main

`+baImports+`
var users = map[string]string{"admin": "123456"}

func handlers() []fiber.Handler {
	return []fiber.Handler{
		basicauth.New(basicauth.Config{Users: map[string]string{"john": "doe"}, ContextUsername: "u"}),
		basicauth.New(basicauth.Config{Users: users}),
	}
}
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateBasicauthConfig, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
	assert.Empty(t, second.notes)
}

func Test_MigrateBasicauthConfig_Docs(t *testing.T) {
	t.Parallel()

	// the hashes of the v3 documentation are what the migration computes
	res := mw1Run(t, v3.MigrateBasicauthConfig, `package main

`+baImports+`
var _ = basicauth.New(basicauth.Config{Users: map[string]string{"john": "doe", "x": "secret"}})
`)

	assert.Contains(t, res.file(), baHashDoe)
	assert.Contains(t, res.file(), baHashS3cr3t)
}
