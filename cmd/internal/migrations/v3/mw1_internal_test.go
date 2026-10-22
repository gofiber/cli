package v3

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// These tests look at the text a migration produces before goimports has had
// its say: on a first run the packages of v3 cannot be resolved from the module
// cache, so every import the output needs has to be written by the migration.

func mw1FileInfo(t *testing.T) internal.FileInfo {
	t.Helper()
	dir := t.TempDir()
	return internal.FileInfo{Root: dir, Path: dir + "/main.go", Rel: "main.go"}
}

func Test_migrateCORSContent_AddsTheStringsImport(t *testing.T) {
	t.Parallel()

	out := migrateCORSContent(`package main

import (
	"os"

	"github.com/gofiber/fiber/v3/middleware/cors"
)

var _ = cors.Config{AllowOrigins: os.Getenv("ORIGINS")}
`, mw1FileInfo(t))

	assert.Contains(t, out, "\t\"strings\"\n")
	assert.Contains(t, out, `strings.FieldsFunc(os.Getenv("ORIGINS")`)
}

func Test_migrateCORSContent_UsesTheAliasOfTheStringsImport(t *testing.T) {
	t.Parallel()

	out := migrateCORSContent(`package main

import (
	"os"
	str "strings"

	"github.com/gofiber/fiber/v3/middleware/cors"
)

var _ = cors.Config{AllowOrigins: os.Getenv("ORIGINS")}
`, mw1FileInfo(t))

	assert.Contains(t, out, `str.FieldsFunc(os.Getenv("ORIGINS")`)
	assert.Equal(t, 1, strings.Count(out, `"strings"`))
}

func Test_migrateCORSContent_NoImportForLiterals(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v3/middleware/cors"

var _ = cors.Config{AllowOrigins: "https://a.com"}
`
	out := migrateCORSContent(src, mw1FileInfo(t))

	assert.NotContains(t, out, `"strings"`)
	assert.Contains(t, out, `[]string{"https://a.com"}`)
}

func Test_migrateCORSContent_SkipsAFileThatDoesNotParse(t *testing.T) {
	t.Parallel()

	fi := mw1FileInfo(t)
	src := `package main

import "github.com/gofiber/fiber/v3/middleware/cors"

var _ = cors.Config{AllowOrigins: "a"
`
	assert.Equal(t, src, migrateCORSContent(src, fi))
	notes := internal.DrainNotes(fi.Root)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "does not parse")
}

func Test_migrateBasicauthAuthorizerContent_AddsTheFiberImport(t *testing.T) {
	t.Parallel()

	// a file that never mentions the fiber package itself
	out := migrateBasicauthAuthorizerContent(`package main

import "github.com/gofiber/fiber/v3/middleware/basicauth"

func authConfig() basicauth.Config {
	return basicauth.Config{
		Authorizer: func(user, pass string) bool {
			return user == "john" && pass == "doe"
		},
	}
}
`, mw1FileInfo(t))

	assert.Contains(t, out, "\t\"github.com/gofiber/fiber/v3\"\n")
	assert.Contains(t, out, "func(user, pass string, _ fiber.Ctx) bool")
}

func Test_migrateBasicauthAuthorizerContent_UsesTheAliasOfFiber(t *testing.T) {
	t.Parallel()

	out := migrateBasicauthAuthorizerContent(`package main

import (
	f "github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/basicauth"
)

var _ f.Handler

var cfg = basicauth.Config{Authorizer: check}

func check(user, pass string) bool { return true }
`, mw1FileInfo(t))

	assert.Contains(t, out, "func(user, pass string, _ f.Ctx) bool { return check(user, pass) }")
	assert.Equal(t, 1, strings.Count(out, "gofiber/fiber/v3\""))
}

func Test_migrateBasicauthConfigContent_AddsTheHashingImports(t *testing.T) {
	t.Parallel()

	out := migrateBasicauthConfigContent(`package main

import "github.com/gofiber/fiber/v3/middleware/basicauth"

var users = map[string]string{"admin": "123456"}

var _ = basicauth.Config{Users: users}
`, mw1FileInfo(t))

	assert.Contains(t, out, "\t\"crypto/sha256\"\n")
	assert.Contains(t, out, "\t\"encoding/base64\"\n")
	assert.Contains(t, out, "sha256.Sum256")
}

func Test_migrateBasicauthConfigContent_NoImportsForLiterals(t *testing.T) {
	t.Parallel()

	out := migrateBasicauthConfigContent(`package main

import "github.com/gofiber/fiber/v3/middleware/basicauth"

var _ = basicauth.Config{Users: map[string]string{"admin": "123456"}}
`, mw1FileInfo(t))

	assert.NotContains(t, out, "crypto/sha256")
	assert.NotContains(t, out, "encoding/base64")
}
