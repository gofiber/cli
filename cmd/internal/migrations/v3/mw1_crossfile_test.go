package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// What a file does not declare, another file of the package may: a second run
// has to recognize the code a first run wrote.

func Test_MigrateCORSConfig_ListsDeclaredInAnotherFile(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"main.go": `package main

import (
	"strings"

	"github.com/gofiber/fiber/v2/middleware/cors"
)

var _ = cors.Config{AllowOrigins: strings.Join(settings.Origins, ","), AllowMethods: settings.Methods}
`,
		"settings.go": `package main

type config struct {
	Origins []string
	Methods string
}

var settings config
`,
	}
	first := mw1RunFiles(t, v3.MigrateCORSConfig, files)

	// a slice is the list as it is, a string is split
	assert.Contains(t, first.files["main.go"], `AllowOrigins: settings.Origins,`)
	assert.Contains(t, first.files["main.go"], `strings.FieldsFunc(settings.Methods,`)

	// and the migrated code is left alone the second time
	second := mw1RunFiles(t, v3.MigrateCORSConfig, first.files)
	assert.Equal(t, first.files, second.files)
	assert.Empty(t, second.stdout)
}

func Test_MigrateCORSConfig_PackageLevelSliceOfAnotherFile(t *testing.T) {
	t.Parallel()

	src := map[string]string{
		"main.go": `package main

import "github.com/gofiber/fiber/v3/middleware/cors"

var _ = cors.Config{AllowOrigins: origins, AllowHeaders: headers}
`,
		"vars.go": `package main

var origins = []string{"https://a.com"}

var headers []string
`,
	}
	res := mw1RunFiles(t, v3.MigrateCORSConfig, src)

	assert.Equal(t, src, res.files)
	assert.Empty(t, res.stdout)
}

func Test_MigrateBasicauthAuthorizer_FunctionsDeclaredInAnotherFile(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/basicauth"
)

var _ fiber.Handler

var a = basicauth.Config{Authorizer: authorize}
var b = basicauth.Config{Authorizer: legacy}
var c = basicauth.Config{Authorizer: svc.Check}
`,
		"auth.go": `package main

import "github.com/gofiber/fiber/v3"

func authorize(user, pass string, c fiber.Ctx) bool { return true }

func legacy(user, pass string) bool { return true }

type service struct{}

func (service) Check(user, pass string, c fiber.Ctx) bool { return true }

var svc service
`,
	}
	res := mw1RunFiles(t, v3.MigrateBasicauthAuthorizer, files)

	main := res.files["main.go"]
	assert.Contains(t, main, `var a = basicauth.Config{Authorizer: authorize}`)
	assert.Contains(t, main, `var b = basicauth.Config{Authorizer: func(user, pass string, _ fiber.Ctx) bool { return legacy(user, pass) }}`)
	assert.Contains(t, main, `var c = basicauth.Config{Authorizer: svc.Check}`)
}

func Test_MigrateEncryptcookieConfig_HooksDeclaredInAnotherFile(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"main.go": `package main

import "github.com/gofiber/fiber/v3/middleware/encryptcookie"

var a = encryptcookie.Config{Encryptor: seal, Decryptor: unseal}
`,
		"hooks.go": `package main

func seal(name, value, key string) (string, error) { return value, nil }

func unseal(value, key string) (string, error) { return value, nil }
`,
	}
	res := mw1RunFiles(t, v3.MigrateEncryptcookieConfig, files)

	assert.Contains(t, res.files["main.go"], `Encryptor: seal,`)
	assert.Contains(t, res.files["main.go"], `Decryptor: func(_, value, key string) (string, error) { return unseal(value, key) }`)
}

func Test_MigrateFaviconConfig_FileSystemsDeclaredInAnotherFile(t *testing.T) {
	t.Parallel()

	src := map[string]string{
		"main.go": `package main

import "github.com/gofiber/fiber/v3/middleware/favicon"

var a = favicon.Config{File: "favicon.ico", FileSystem: embedded}
var b = favicon.Config{File: "favicon.ico", FileSystem: dir}
var c = favicon.Config{File: "favicon.ico", FileSystem: site.Assets}
`,
		"assets.go": `package main

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed static
var embedded embed.FS

var dir = os.DirFS("static")

type holder struct {
	Assets fs.FS
}

var site holder
`,
	}
	res := mw1RunFiles(t, v3.MigrateFaviconConfig, src)

	assert.Equal(t, src, res.files)
	assert.Empty(t, res.notes)
	assert.Empty(t, res.stdout)
}

func Test_MigrateCSRFConfig_ExtractorsDeclaredInAnotherFile(t *testing.T) {
	t.Parallel()

	src := map[string]string{
		"main.go": `package main

import "github.com/gofiber/fiber/v3/middleware/csrf"

var a = csrf.Config{Extractor: header}
var b = csrf.Config{Extractor: typed}
var c = csrf.Config{Extractor: holder.Extract}
`,
		"extractors.go": `package main

import "github.com/gofiber/fiber/v3/extractors"

var header = extractors.FromHeader("X-Csrf-Token")

var typed extractors.Extractor

type source struct {
	Extract extractors.Extractor
}

var holder source
`,
	}
	res := mw1RunFiles(t, v3.MigrateCSRFConfig, src)

	assert.Equal(t, src, res.files)
	assert.Empty(t, res.stdout)
}
