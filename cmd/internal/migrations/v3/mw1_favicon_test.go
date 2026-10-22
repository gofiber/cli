package v3_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateFaviconConfig(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateFaviconConfig, `package main

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/favicon"
)

//go:embed static/favicon.ico
var embedded embed.FS

func handlers() []fiber.Handler {
	return []fiber.Handler{
		favicon.New(favicon.Config{
			File:       "static/favicon.ico",
			URL:        "/favicon.ico",
			FileSystem: http.FS(embedded),
		}),
		favicon.New(favicon.Config{
			File:       "favicon.ico",
			FileSystem: http.Dir("./static"),
		}),
		favicon.New(favicon.Config{File: "/favicon.ico", FileSystem: http.Dir("./static")}),
		favicon.New(favicon.Config{File: "./static/favicon.ico", FileSystem: http.FS(embedded)}),
	}
}
`)

	assert.Equal(t, `package main

import (
	"embed"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/favicon"
)

//go:embed static/favicon.ico
var embedded embed.FS

func handlers() []fiber.Handler {
	return []fiber.Handler{
		favicon.New(favicon.Config{
			File:       "static/favicon.ico",
			URL:        "/favicon.ico",
			FileSystem: embedded,
		}),
		favicon.New(favicon.Config{
			File:       "favicon.ico",
			FileSystem: os.DirFS("./static"),
		}),
		favicon.New(favicon.Config{File: "favicon.ico", FileSystem: os.DirFS("./static")}),
		favicon.New(favicon.Config{File: "static/favicon.ico", FileSystem: embedded}),
	}
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating favicon middleware configs")
	assert.Empty(t, res.notes)
}

func Test_MigrateFaviconConfig_FileWithoutAFileSystemIsAPathOfTheDisk(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/favicon"
)

func handlers() []fiber.Handler {
	return []fiber.Handler{
		favicon.New(favicon.Config{File: "./favicon.ico"}),
		favicon.New(favicon.Config{File: "/var/www/favicon.ico", URL: "/fav.ico"}),
		favicon.New(favicon.Config{Data: []byte("ICO"), CacheControl: "public, max-age=60"}),
	}
}
`
	res := mw1Run(t, v3.MigrateFaviconConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateFaviconConfig_VariableThatOnlyFaviconUses(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateFaviconConfig, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/favicon"
)

var assets http.FileSystem = http.Dir("./static")

func variable() fiber.Handler {
	return favicon.New(favicon.Config{File: "/favicon.ico", FileSystem: assets})
}

func local() fiber.Handler {
	dir := http.Dir("./public")
	return favicon.New(favicon.Config{File: "favicon.ico", FileSystem: dir})
}
`)

	assert.Equal(t, `package main

import (
	"io/fs"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/favicon"
)

var assets fs.FS = os.DirFS("./static")

func variable() fiber.Handler {
	return favicon.New(favicon.Config{File: "favicon.ico", FileSystem: assets})
}

func local() fiber.Handler {
	dir := os.DirFS("./public")
	return favicon.New(favicon.Config{File: "favicon.ico", FileSystem: dir})
}
`, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateFaviconConfig_FileSystemsThatCannotBeConverted(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"example.com/app/assets"
)

// shared with another middleware
var shared http.FileSystem = http.Dir("./static")

func handlers(dir http.FileSystem) []fiber.Handler {
	_ = http.FileServer(shared)
	return []fiber.Handler{
		favicon.New(favicon.Config{File: "/favicon.ico", FileSystem: shared}),
		favicon.New(favicon.Config{File: "/favicon.ico", FileSystem: dir}),
		favicon.New(favicon.Config{File: "/favicon.ico", FileSystem: assets.HTTPBox()}),
	}
}
`
	res := mw1Run(t, v3.MigrateFaviconConfig, src)

	// still v2, so that the compiler points at it
	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	if assert.Len(t, res.notes, 3) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: favicon.Config.FileSystem is an fs.FS in v3", mw1LineOf(t, src, "FileSystem: shared")))
		assert.Contains(t, res.notes[2], "assets.HTTPBox() cannot be converted")
	}
}

func Test_MigrateFaviconConfig_FileSystemOfV3IsKept(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"embed"
	"io/fs"
	"os"

	"github.com/gofiber/fiber/v3/middleware/favicon"
)

//go:embed static/favicon.ico
var embedded embed.FS

var sub fs.FS = os.DirFS("static")
var dir = os.DirFS("static")

var a = favicon.Config{File: "favicon.ico", FileSystem: embedded}
var b = favicon.Config{File: "favicon.ico", FileSystem: sub}
var c = favicon.Config{File: "favicon.ico", FileSystem: dir}
var d = favicon.Config{File: "favicon.ico", FileSystem: os.DirFS("static")}
var e = favicon.Config{File: "favicon.ico", FileSystem: nil}
`
	res := mw1Run(t, v3.MigrateFaviconConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)
}

func Test_MigrateFaviconConfig_Assignment(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateFaviconConfig, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2/middleware/favicon"
)

func config() favicon.Config {
	cfg := favicon.ConfigDefault
	cfg.FileSystem = http.Dir("./static")
	cfg.File = "/favicon.ico"
	return cfg
}
`)

	assert.Contains(t, res.file(), `cfg.FileSystem = os.DirFS("./static")`)
}

func Test_MigrateFaviconConfig_NonLiteralFileIsReported(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2/middleware/favicon"
)

const name = "/favicon.ico"

var _ = favicon.Config{File: name, FileSystem: http.Dir("./static")}
`
	res := mw1Run(t, v3.MigrateFaviconConfig, src)

	assert.Contains(t, res.file(), `FileSystem: os.DirFS("./static")`)
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], "favicon.Config.File must be a relative path without a leading / or ./")
	}
}

func Test_MigrateFaviconConfig_Alias(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateFaviconConfig, `package main

import (
	h "net/http"

	fav "github.com/gofiber/fiber/v2/middleware/favicon"
)

var _ = fav.Config{File: "/favicon.ico", FileSystem: h.Dir("./static")}
`)

	assert.Contains(t, res.file(), `fav.Config{File: "favicon.ico", FileSystem: os.DirFS("./static")}`)
}

func Test_MigrateFaviconConfig_OtherConfigs(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"net/http"

	"example.com/app/favicon"
)

type site struct {
	File       string
	FileSystem http.FileSystem
}

var a = favicon.Config{File: "/favicon.ico", FileSystem: http.Dir("./static")}
var b = site{File: "/favicon.ico", FileSystem: http.Dir("./static")}
`
	res := mw1Run(t, v3.MigrateFaviconConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateFaviconConfig_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateFaviconConfig, `package main

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2/middleware/favicon"
)

//go:embed static/favicon.ico
var embedded embed.FS

var assets http.FileSystem = http.FS(embedded)

var a = favicon.Config{File: "/static/favicon.ico", FileSystem: assets}
var b = favicon.Config{File: "./favicon.ico", FileSystem: http.Dir("./static")}
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateFaviconConfig, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
	assert.Empty(t, second.notes)
}
