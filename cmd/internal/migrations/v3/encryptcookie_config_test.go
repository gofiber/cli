package v3_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const ecImports = `import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/encryptcookie"
)
`

func Test_MigrateEncryptcookieConfig(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateEncryptcookieConfig, `package main

import "github.com/gofiber/fiber/v2/middleware/encryptcookie"

var _ = encryptcookie.New(encryptcookie.Config{
	Key:       "0123456789abcdef0123456789abcdef",
	Encryptor: func(value, key string) (string, error) { return "", nil },
	Decryptor: func(value string, key string) (string, error) { return "", nil },
})

var typeless = encryptcookie.Config{
	Encryptor: func(string, string) (string, error) { return "", nil },
	Decryptor: func(_, _ string) (string, error) { return "", nil },
}

var multiLine = encryptcookie.Config{
	Encryptor: func(
		value string,
		key string,
	) (string, error) {
		return "", nil
	},
}
`)

	assert.Equal(t, `package main

import "github.com/gofiber/fiber/v2/middleware/encryptcookie"

var _ = encryptcookie.New(encryptcookie.Config{
	Key:       "0123456789abcdef0123456789abcdef",
	Encryptor: func(_ string, value, key string) (string, error) { return "", nil },
	Decryptor: func(_ string, value string, key string) (string, error) { return "", nil },
})

var typeless = encryptcookie.Config{
	Encryptor: func(string, string, string) (string, error) { return "", nil },
	Decryptor: func(_ string, _, _ string) (string, error) { return "", nil },
}

var multiLine = encryptcookie.Config{
	Encryptor: func(
		_ string,
		value string,
		key string,
	) (string, error) {
		return "", nil
	},
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating encryptcookie middleware configs")
	assert.Empty(t, res.notes)
}

func Test_MigrateEncryptcookieConfig_GenerateKey(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateEncryptcookieConfig, `package main

`+ecImports+`
var generated = encryptcookie.GenerateKey()

func handler() fiber.Handler {
	return encryptcookie.New(encryptcookie.Config{Key: encryptcookie.GenerateKey()})
}
`)

	assert.Contains(t, res.file(), `var generated = encryptcookie.GenerateKey(32)`)
	assert.Contains(t, res.file(), `Key: encryptcookie.GenerateKey(32)`)
}

func Test_MigrateEncryptcookieConfig_HelpersTakeTheNameOfTheCookie(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateEncryptcookieConfig, `package main

`+ecImports+`
func direct(key string) (string, error) {
	enc, err := encryptcookie.EncryptCookie("secret-value", key)
	if err != nil {
		return "", err
	}
	return encryptcookie.DecryptCookie(enc, key)
}

func wrapping() fiber.Handler {
	return encryptcookie.New(encryptcookie.Config{
		Key: "0123456789abcdef0123456789abcdef",
		Encryptor: func(value, key string) (string, error) {
			return encryptcookie.EncryptCookie(value, key)
		},
		Decryptor: func(value, key string) (string, error) {
			return encryptcookie.DecryptCookie(value, key)
		},
	})
}
`)

	file := res.file()
	// an empty name reads and writes what v2 did
	assert.Contains(t, file, `encryptcookie.EncryptCookie("", "secret-value", key)`)
	assert.Contains(t, file, `encryptcookie.DecryptCookie("", enc, key)`)
	assert.Contains(t, file, `Encryptor: func(_ string, value, key string) (string, error) {`)
	assert.Contains(t, file, `return encryptcookie.EncryptCookie("", value, key)`)
	assert.Contains(t, file, `return encryptcookie.DecryptCookie("", value, key)`)
}

func Test_MigrateEncryptcookieConfig_NamedHooks(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateEncryptcookieConfig, `package main

`+ecImports+`
func myEncrypt(value, key string) (string, error) { return value + key, nil }
func myDecrypt(value, key string) (string, error) { return value, nil }

type codec struct{}

func (codec) Seal(value, key string) (string, error) { return value, nil }

func hooks(c codec, key string, value codec) []fiber.Handler {
	return []fiber.Handler{
		encryptcookie.New(encryptcookie.Config{Key: key, Encryptor: myEncrypt, Decryptor: myDecrypt}),
		encryptcookie.New(encryptcookie.Config{Key: key, Encryptor: c.Seal}),
		encryptcookie.New(encryptcookie.Config{Key: key, Encryptor: value.Seal}),
		encryptcookie.New(encryptcookie.Config{Key: key, Encryptor: encryptcookie.EncryptCookie, Decryptor: encryptcookie.DecryptCookie}),
	}
}

func assigned(key string) encryptcookie.Config {
	var cfg encryptcookie.Config
	cfg.Encryptor = myEncrypt
	return cfg
}
`)

	file := res.file()
	assert.Contains(t, file, `Encryptor: func(_, value, key string) (string, error) { return myEncrypt(value, key) }`)
	assert.Contains(t, file, `Decryptor: func(_, value, key string) (string, error) { return myDecrypt(value, key) }`)
	assert.Contains(t, file, `Encryptor: func(_, value, key string) (string, error) { return c.Seal(value, key) }`)
	// the receiver is called value, which the parameter must not hide
	assert.Contains(t, file, `Encryptor: func(_, valueArg, keyArg string) (string, error) { return value.Seal(valueArg, keyArg) }`)
	// the stock functions have the signature already
	assert.Contains(t, file, `Encryptor: encryptcookie.EncryptCookie, Decryptor: encryptcookie.DecryptCookie`)
	assert.Contains(t, file, `cfg.Encryptor = func(_, value, key string) (string, error) { return myEncrypt(value, key) }`)
}

func Test_MigrateEncryptcookieConfig_NamedHookThatTakesTheNameAlready(t *testing.T) {
	t.Parallel()

	src := `package main

` + ecImports + `
func myEncrypt(name, value, key string) (string, error) { return value + key, nil }

var _ = encryptcookie.New(encryptcookie.Config{Key: "0123456789abcdef0123456789abcdef", Encryptor: myEncrypt, Decryptor: nil})
`
	res := mw1Run(t, v3.MigrateEncryptcookieConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateEncryptcookieConfig_ComputedHookIsReported(t *testing.T) {
	t.Parallel()

	src := `package main

` + ecImports + `
var _ = encryptcookie.New(encryptcookie.Config{Key: "0123456789abcdef0123456789abcdef", Encryptor: build()})
`
	res := mw1Run(t, v3.MigrateEncryptcookieConfig, src)

	assert.Equal(t, src, res.file())
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: the Encryptor of encryptcookie.Config takes the name of the cookie", mw1LineOf(t, src, "build()")))
	}
}

func Test_MigrateEncryptcookieConfig_KeyOfHooksIsChecked(t *testing.T) {
	t.Parallel()

	// v3 validates the key when the middleware is created, v2 only needed it for
	// the stock functions
	src := `package main

` + ecImports + `
var custom = encryptcookie.Config{
	Key:       "secret-thirty-2-character-string",
	Encryptor: func(_ string, value, key string) (string, error) { return value, nil },
}

var stock = encryptcookie.Config{Key: "secret-thirty-2-character-string"}

var valid = encryptcookie.Config{
	Key:       "0123456789abcdef0123456789abcdef",
	Encryptor: func(_ string, value, key string) (string, error) { return value, nil },
}
`
	res := mw1Run(t, v3.MigrateEncryptcookieConfig, src)

	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: v3 panics at start-up unless the Key", mw1LineOf(t, src, `Key:       "secret-thirty-2-character-string"`)))
	}
}

func Test_MigrateEncryptcookieConfig_Alias(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateEncryptcookieConfig, `package main

import enc "github.com/gofiber/fiber/v2/middleware/encryptcookie"

var key = enc.GenerateKey()

var cfg = enc.Config{
	Key:       key,
	Encryptor: func(value, key string) (string, error) { return enc.EncryptCookie(value, key) },
}
`)

	assert.Contains(t, res.file(), `var key = enc.GenerateKey(32)`)
	assert.Contains(t, res.file(), `func(_ string, value, key string) (string, error) { return enc.EncryptCookie("", value, key) }`)
}

func Test_MigrateEncryptcookieConfig_OtherPackages(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"example.com/app/cookies"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.New

type codec struct {
	Encryptor func(value, key string) (string, error)
}

var c = codec{Encryptor: func(value, key string) (string, error) { return value, nil }}
var k = cookies.GenerateKey()
var e, _ = cookies.EncryptCookie("v", "k")
`
	res := mw1Run(t, v3.MigrateEncryptcookieConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateEncryptcookieConfig_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateEncryptcookieConfig, `package main

`+ecImports+`
func myEncrypt(value, key string) (string, error) { return value + key, nil }

var _ = encryptcookie.New(encryptcookie.Config{
	Key:       encryptcookie.GenerateKey(),
	Encryptor: myEncrypt,
	Decryptor: func(value, key string) (string, error) { return encryptcookie.DecryptCookie(value, key) },
})
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateEncryptcookieConfig, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
	assert.Empty(t, second.notes)
}

func Test_MigrateEncryptcookieConfig_V3CodeIsLeftAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v3/middleware/encryptcookie"

var _ = encryptcookie.New(encryptcookie.Config{
	Key:       encryptcookie.GenerateKey(32),
	Encryptor: func(name, value, key string) (string, error) { return encryptcookie.EncryptCookie(name, value, key) },
	Decryptor: encryptcookie.DecryptCookie,
})
`
	res := mw1Run(t, v3.MigrateEncryptcookieConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}
