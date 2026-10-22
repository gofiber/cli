package v3

import (
	"os"
	"path/filepath"
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
`, mw1FileInfo(t), nil)

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
`, mw1FileInfo(t), nil)

	assert.Contains(t, out, `str.FieldsFunc(os.Getenv("ORIGINS")`)
	assert.Equal(t, 1, strings.Count(out, `"strings"`))
}

func Test_migrateCORSContent_NoImportForLiterals(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v3/middleware/cors"

var _ = cors.Config{AllowOrigins: "https://a.com"}
`
	out := migrateCORSContent(src, mw1FileInfo(t), nil)

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
	assert.Equal(t, src, migrateCORSContent(src, fi, nil))
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
`, mw1FileInfo(t), nil)

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
`, mw1FileInfo(t), nil)

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

func Test_migrateCSRFContent_AddsTheExtractorsImportNextToFiber(t *testing.T) {
	t.Parallel()

	out := migrateCSRFContent(`package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
)

var _ fiber.Handler
var _ = csrf.Config{KeyLookup: "header:X-Token", Expiration: time.Hour}
`, mw1FileInfo(t), nil)

	assert.Contains(t, out, `"github.com/gofiber/fiber/v3/middleware/csrf"`+"\n\t"+`"github.com/gofiber/fiber/v3/extractors"`)
}

func Test_migrateCSRFContent_UsesTheAliasOfExtractors(t *testing.T) {
	t.Parallel()

	out := migrateCSRFContent(`package main

import (
	ex "github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/csrf"
)

var _ = ex.FromHeader
var _ = csrf.Config{KeyLookup: "header:X-Token"}
`, mw1FileInfo(t), nil)

	assert.Contains(t, out, `Extractor: ex.FromHeader("X-Token")`)
	assert.Equal(t, 1, strings.Count(out, "fiber/v3/extractors"))
}

func Test_migrateCSRFContent_NoImportWhenNothingUsesExtractors(t *testing.T) {
	t.Parallel()

	out := migrateCSRFContent(`package main

import (
	"time"

	"github.com/gofiber/fiber/v3/middleware/csrf"
)

var _ = csrf.Config{Expiration: time.Hour}
`, mw1FileInfo(t), nil)

	assert.NotContains(t, out, "extractors")
}

func Test_mw1AddImport(t *testing.T) {
	t.Parallel()

	const fiber = `"github.com/gofiber/fiber/v3"`
	tests := []struct {
		name string
		src  string
		path string
		want string
	}{
		{
			name: "next to the other Fiber imports",
			src:  "package p\n\nimport (\n\t\"fmt\"\n\n\t" + fiber + " // core\n\t\"github.com/gofiber/utils/v2\"\n\n\t\"example.com/x\"\n)\n",
			path: "github.com/gofiber/fiber/v3/extractors",
			want: "package p\n\nimport (\n\t\"fmt\"\n\n\t" + fiber + " // core\n\t\"github.com/gofiber/utils/v2\"\n\t\"github.com/gofiber/fiber/v3/extractors\"\n\n\t\"example.com/x\"\n)\n",
		},
		{
			name: "next to the standard library",
			src:  "package p\n\nimport (\n\t\"fmt\"\n\n\t" + fiber + "\n)\n",
			path: "strings",
			want: "package p\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n\n\t" + fiber + "\n)\n",
		},
		{
			name: "a group of its own for the standard library",
			src:  "package p\n\nimport (\n\t" + fiber + "\n)\n",
			path: "strings",
			want: "package p\n\nimport (\n\t\"strings\"\n\n\t" + fiber + "\n)\n",
		},
		{
			name: "a group of its own for Fiber",
			src:  "package p\n\nimport (\n\t\"fmt\"\n)\n",
			path: "github.com/gofiber/fiber/v3",
			want: "package p\n\nimport (\n\t\"fmt\"\n\n\t" + fiber + "\n)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, mw1AddImport(tt.src, tt.path))
		})
	}
}

func Test_migrateCacheContent_AddsTheTimeImport(t *testing.T) {
	t.Parallel()

	out := migrateCacheContent(`package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3/middleware/cache"
)

var _ = fmt.Sprint
var _ = cache.New()
`, mw1FileInfo(t))

	assert.Contains(t, out, "\t\"fmt\"\n\t\"time\"\n")
	assert.Contains(t, out, "cache.Config{DisableCacheControl: true, Expiration: time.Minute}")
}

func Test_migrateCacheContent_UsesTheAliasOfTime(t *testing.T) {
	t.Parallel()

	out := migrateCacheContent(`package main

import (
	t "time"

	"github.com/gofiber/fiber/v3/middleware/cache"
)

var _ = t.Second
var _ = cache.Config{}
`, mw1FileInfo(t))

	assert.Contains(t, out, "Expiration: t.Minute")
	assert.Equal(t, 1, strings.Count(out, `"time"`))
}

func Test_migrateCacheContent_NoTimeImportWhenTheExpirationIsSet(t *testing.T) {
	t.Parallel()

	out := migrateCacheContent(`package main

import (
	"github.com/gofiber/fiber/v3/middleware/cache"
)

var _ = cache.Config{Expiration: 5, CacheControl: true}
`, mw1FileInfo(t))

	assert.NotContains(t, out, `"time"`)
}

func Test_migrateFaviconContent_AddsTheImports(t *testing.T) {
	t.Parallel()

	out := migrateFaviconContent(`package main

import (
	"net/http"

	"github.com/gofiber/fiber/v3/middleware/favicon"
)

var assets http.FileSystem = http.Dir("./static")

var _ = favicon.Config{File: "favicon.ico", FileSystem: assets}
`, mw1FileInfo(t), nil)

	assert.Contains(t, out, "\t\"os\"\n")
	assert.Contains(t, out, "\t\"io/fs\"\n")
	assert.Contains(t, out, `var assets fs.FS = os.DirFS("./static")`)
}

func Test_migrateFaviconContent_UsesTheAliasOfOS(t *testing.T) {
	t.Parallel()

	out := migrateFaviconContent(`package main

import (
	"net/http"
	system "os"

	"github.com/gofiber/fiber/v3/middleware/favicon"
)

var _ = system.Exit
var _ = favicon.Config{File: "favicon.ico", FileSystem: http.Dir("./static")}
`, mw1FileInfo(t), nil)

	assert.Contains(t, out, `system.DirFS("./static")`)
	assert.Equal(t, 1, strings.Count(out, `"os"`))
}

func Test_mw1Index(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	}
	write("a.go", `package a

type config struct {
	Origins []string
	Name, Title string
}

var origins = []string{"a"}

const prefix = "/p"

func check(user, pass string) bool { return true }

func (config) Check(user, pass string, c any) bool { return true }
`)
	write("sub/b.go", "package b\n\nvar origins string\n\nfunc check(a int) {}\n")
	write("vendor/c.go", "package c\n\nvar vendored int\n")
	write("broken.go", "package broken\n\nfunc (")

	idx := newMW1Index(dir)

	assert.ElementsMatch(t, []int{2, 1}, idx.funcParams("check"))
	assert.Equal(t, []int{3}, idx.funcParams("Check"))
	assert.Nil(t, idx.funcParams("missing"))
	assert.Equal(t, []string{"[]string"}, idx.fieldTypes("Origins"))
	assert.Equal(t, []string{"string"}, idx.fieldTypes("Title"))

	v, ok := idx.pkgVar(filepath.Join(dir, "a.go"), "origins")
	assert.True(t, ok)
	assert.Equal(t, mw1VarDecl{init: "[]string{…}"}, v)
	v, ok = idx.pkgVar(filepath.Join(dir, "x.go"), "prefix")
	assert.True(t, ok)
	assert.Equal(t, `"/p"`, v.init)
	// declared in another package
	v, ok = idx.pkgVar(filepath.Join(dir, "sub", "x.go"), "origins")
	assert.True(t, ok)
	assert.Equal(t, "string", v.typ)
	_, ok = idx.pkgVar(filepath.Join(dir, "vendor", "x.go"), "vendored")
	assert.False(t, ok)

	// a missing index knows nothing
	var none *mw1Index
	assert.Nil(t, none.funcParams("check"))
	assert.Nil(t, none.fieldTypes("Origins"))
	_, ok = none.pkgVar("a.go", "origins")
	assert.False(t, ok)
}

func Test_allEqual(t *testing.T) {
	t.Parallel()

	assert.True(t, allEqual([]int{3, 3}, 3))
	assert.False(t, allEqual([]int{3, 2}, 3))
	assert.False(t, allEqual([]int(nil), 3))
}
