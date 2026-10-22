package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateAppConfig_ETag(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func newApp() *fiber.App {
	return fiber.New(fiber.Config{
		AppName: "etag-demo",
		ETag:    true,
	})
}

func newPair() (*fiber.App, error) {
	return fiber.New(fiber.Config{ETag: true}), nil
}

func main() {
	app := fiber.New(fiber.Config{
		AppName: "demo",
		ETag:    true,
	})
	conditional := fiber.New(fiber.Config{ETag: os.Getenv("ETAG") != ""})
	off := fiber.New(fiber.Config{ETag: false, AppName: "off"})

	cfg := fiber.Config{ETag: true}
	viaVar := fiber.New(cfg)
	_, _, _, _ = conditional, off, viaVar, app
	log.Println(newApp(), newPair)
}
`)

	out := appRun(t, dir, v3.MigrateAppConfig)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/middleware/etag"`)
	assert.Contains(t, content, "func newApp() *fiber.App {\n\tapp := fiber.New(fiber.Config{\n\t\tAppName: \"etag-demo\",\n\t})\n\tapp.Use(etag.New())\n\treturn app\n}")
	assert.Contains(t, content, "\tapp := fiber.New(fiber.Config{})\n\tapp.Use(etag.New())\n\treturn app, nil")
	assert.Contains(t, content, "\tapp := fiber.New(fiber.Config{\n\t\tAppName: \"demo\",\n\t})\n\tapp.Use(etag.New())\n")
	assert.Contains(t, content, "conditional := fiber.New(fiber.Config{})\n\tif os.Getenv(\"ETAG\") != \"\" {\n\t\tconditional.Use(etag.New())\n\t}")
	assert.Contains(t, content, "off := fiber.New(fiber.Config{AppName: \"off\"})\n")
	assert.Contains(t, content, "cfg := fiber.Config{}\n\tviaVar := fiber.New(cfg)\n\tviaVar.Use(etag.New())")
	assert.NotContains(t, content, "ETag:")
	assert.Contains(t, out, "Migrating app config fields")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateAppConfig_ETagWithoutAnAppToAttachTo(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

type server struct{ app *fiber.App }

var global = fiber.New(fiber.Config{ETag: true})

func newServer() *server {
	return &server{app: fiber.New(fiber.Config{ETag: true})}
}

func configOnly() fiber.Config {
	return fiber.Config{ETag: true}
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateAppConfig)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 3)
	for _, n := range notes {
		assert.Contains(t, n.Message, "etag.New()")
	}
}

func Test_MigrateAppConfig_CompressedFileSuffix(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New(fiber.Config{CompressedFileSuffix: ".gz", AppName: "x"})
	cfg := fiber.Config{
		AppName:              "y",
		CompressedFileSuffix: suffix(),
	}
	cfg.CompressedFileSuffix = ".br"
	log.Println(fiber.DefaultCompressedFileSuffix, app, cfg)
}

func suffix() string { return ".gz" }
`)

	appRun(t, dir, v3.MigrateAppConfig)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `fiber.Config{CompressedFileSuffixes: map[string]string{"gzip": ".gz"}, AppName: "x"}`)
	assert.Contains(t, content, `CompressedFileSuffixes: map[string]string{"gzip": suffix()},`)
	assert.Contains(t, content, `cfg.CompressedFileSuffixes = map[string]string{"gzip": ".br"}`)
	assert.Contains(t, content, `log.Println(".fiber.gz", app, cfg)`)
	assert.NotContains(t, content, "CompressedFileSuffix:")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateAppConfig_OtherTypes(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

// options has fields called like the removed ones of fiber.Config.
type options struct {
	ETag                 bool
	CompressedFileSuffix string
}

func main() {
	o := options{ETag: true, CompressedFileSuffix: ".gz"}
	o.CompressedFileSuffix = ".br"
	_ = o
	_ = fiber.New()
	println("fiber.DefaultCompressedFileSuffix, ETag: true")
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateAppConfig)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateAppConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New(fiber.Config{ETag: true, CompressedFileSuffix: ".gz"})
	_ = app
}
`)

	appRun(t, dir, v3.MigrateAppConfig)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateAppConfig)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}
