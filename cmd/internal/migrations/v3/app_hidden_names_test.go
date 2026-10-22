package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A function can declare a variable, or take a parameter, with the name of the fiber package (func
// start(fiber *fiber.App), fiber := fiber.New()). The code a migration writes in it names the package
// by a second import, and by the name of the file where no declaration hides it.

// appRunTwice runs the migration, and again, which must change nothing.
func appRunTwice(t *testing.T, dir string, migrate appMigration) {
	t.Helper()

	appRun(t, dir, migrate)
	internal.DrainNotes(dir)
	before := appRead(t, dir, "main.go")
	assert.Empty(t, appRun(t, dir, migrate), "a second run prints nothing")
	assert.Equal(t, before, appRead(t, dir, "main.go"), "a second run changes nothing")
}

func Test_MigrateListenMethods_PackageNameHidden(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

// start runs the app; the parameter is called like the package.
func start(fiber *fiber.App, addr, cert, key string) error {
	if cert != "" {
		return fiber.ListenTLS(addr, cert, key)
	}
	return fiber.Listen(addr)
}

func plain(app *fiber.App, addr, cert, key string) error {
	return app.ListenTLS(addr, cert, key)
}

func main() {
	app := fiber.New()
	_ = start(app, ":3000", "", "")
	_ = plain(app, ":3000", "c", "k")
}
`)

	appRunTwice(t, dir, v3.MigrateListenMethods)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `fiber.Listen(addr, fiberv3.ListenConfig{CertFile: cert, CertKeyFile: key})`)
	assert.Contains(t, content, `app.Listen(addr, fiber.ListenConfig{CertFile: cert, CertKeyFile: key})`, "the name of the file where nothing hides it")
	assert.Contains(t, content, `fiberv3 "github.com/gofiber/fiber/v2"`)
	assert.Equal(t, 1, strings.Count(content, `fiberv3 "github.com/gofiber/fiber/v2"`), "the second import is added once")
}

func Test_MigrateAppTestConfig_PackageNameHidden(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http/httptest"

	"github.com/gofiber/fiber/v2"
)

func viaLocal() int {
	fiber := fiber.New()
	resp, err := fiber.Test(httptest.NewRequest("GET", "/", nil), -1)
	if err != nil {
		return 0
	}
	return resp.StatusCode
}

func viaParam(fiber *fiber.App) int {
	resp, _ := fiber.Test(httptest.NewRequest("GET", "/", nil), 2000)
	return resp.StatusCode
}

func visible(app *fiber.App) int {
	resp, _ := app.Test(httptest.NewRequest("GET", "/", nil), 500)
	return resp.StatusCode
}
`)

	appRunTwice(t, dir, v3.MigrateAppTestConfig)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `fiber.Test(httptest.NewRequest("GET", "/", nil), fiberv3.TestConfig{Timeout: 0, FailOnTimeout: false})`)
	assert.Contains(t, content, `fiber.Test(httptest.NewRequest("GET", "/", nil), fiberv3.TestConfig{Timeout: time.Duration(2000) * time.Millisecond, FailOnTimeout: true})`)
	assert.Contains(t, content, `app.Test(httptest.NewRequest("GET", "/", nil), fiber.TestConfig{Timeout: time.Duration(500) * time.Millisecond, FailOnTimeout: true})`)
	assert.Equal(t, 1, strings.Count(content, `fiberv3 "github.com/gofiber/fiber/v2"`))
}

func Test_MigrateStaticRoutes_PackageNameHidden(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func routes(fiber *fiber.App, dir string) {
	fiber.Static("/static", dir)
}

func main() {
	app := fiber.New()
	app.Static("/public", "./public", fiber.Static{Index: "index.htm"})
}
`)

	appRunTwice(t, dir, v3.MigrateStaticRoutes)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `fiber.Get("/static*", static.New(cmp.Or(dir, ".")))`, "the method of the app, not the type")
	assert.NotContains(t, content, `static.Config("/static`)
	assert.Contains(t, content, `static.Config{IndexNames: []string{"index.htm"}}`)
}
