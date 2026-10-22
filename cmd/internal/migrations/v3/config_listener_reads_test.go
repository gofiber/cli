package v3_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The settings that v3 took out of fiber.Config are read from the config of the
// app the registry made, which gives them back with the rest of the config.
func Test_MigrateConfigListenerFields_Reads(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func build() *fiber.App {
	return fiber.New(fiber.Config{Prefork: true, AppName: "x"})
}

func main() {
	app := build()
	if app.Config().Prefork {
		fmt.Println("prefork")
	}
	cfg := app.Config()
	fmt.Println(cfg.Network, cfg.AppName)
	fmt.Println(app.Config().DisableStartupMessage, app.Config().EnablePrintRoutes)
	log.Fatal(app.Listen(":3000"))
}

func handler(c *fiber.Ctx) error {
	if c.App().Config().Prefork {
		return c.SendString("prefork")
	}
	other := c.App().Config()
	return c.SendString(other.AppName + other.Network)
}
`)

	out, notes := shimMigrate(t, dir)

	assert.Contains(t, out, "Migrating listener related config fields")
	content := appRead(t, dir, "main.go")
	// the settings of the app are recorded, though their values are static: the reads need them
	assert.Contains(t, content, "return fibermigrate.NewApp(fiber.Config{AppName: \"x\"}, fiber.ListenConfig{EnablePrefork: true})")
	assert.Contains(t, content, "if fibermigrate.ConfigOf(app).Prefork {")
	assert.Contains(t, content, "cfg := fibermigrate.ConfigOf(app)")
	assert.Contains(t, content, "fmt.Println(cfg.Network, cfg.AppName)")
	assert.Contains(t, content, "fmt.Println(fibermigrate.ConfigOf(app).DisableStartupMessage, fibermigrate.ConfigOf(app).EnablePrintRoutes)")
	assert.Contains(t, content, "log.Fatal(app.Listen(\":3000\", fibermigrate.ListenConfig(app)))")
	assert.Contains(t, content, "if fibermigrate.ConfigOf(c.App()).Prefork {")
	assert.Contains(t, content, "other := fibermigrate.ConfigOf(c.App())")
	assert.Len(t, notes, 1)
}

// A module whose apps have no settings still has code that reads them: the reads go
// through the registry, which gives the zero values v2 had.
func Test_MigrateConfigListenerFields_Reads_WithoutSettings(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	fmt.Println(app.Config().Prefork, app.Config().Network)
}
`)

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "fmt.Println(fibermigrate.ConfigOf(app).Prefork, fibermigrate.ConfigOf(app).Network)")
	assert.FileExists(t, filepath.Join(dir, shimFile))
	assert.Empty(t, notes, "nothing to tell: the registry is only there for the reads")
}

// The receiver of the config can be an expression, a type that embeds the app, or
// live in another package.
func Test_MigrateConfigListenerFields_Reads_Receivers(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/server/server.go": "package server\n\n" + moduleBuild,
		"cmd/api/main.go": `package main

import (
	"fmt"

	"example.com/app/internal/server"
	"github.com/gofiber/fiber/v2"
)

type Server struct{ *fiber.App }

func main() {
	srv := Server{App: server.New()}
	fmt.Println(srv.Config().Prefork)
	fmt.Println(server.New().Config().Network)
	srv.Listen(":3000")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "cmd/api/main.go")
	assert.Contains(t, content, "fmt.Println(fibermigrate.ConfigOf(srv.App).Prefork)")
	assert.Contains(t, content, "fmt.Println(fibermigrate.ConfigOf(server.New()).Network)")
	assert.Contains(t, content, "srv.Listen(\":3000\", fibermigrate.ListenConfig(srv.App))")
	assert.Len(t, notes, 1)
}

// The app that is made from the config of another takes the settings with it, as in v2.
func Test_MigrateConfigListenerFields_Reads_ClonedConfig(t *testing.T) {
	t.Parallel()

	t.Run("in a module whose apps have settings", func(t *testing.T) {
		t.Parallel()

		dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New(fiber.Config{Prefork: true})
	copyOf := fiber.New(app.Config())
	cfg := app.Config()
	cfg.AppName = "second"
	second := fiber.New(cfg)
	_, _ = copyOf, second
	app.Listen(":3000")
}
`)

		_, notes := shimMigrate(t, dir)

		content := appRead(t, dir, "main.go")
		assert.Contains(t, content, "copyOf := fibermigrate.New(fibermigrate.ConfigOf(app))")
		assert.Contains(t, content, "cfg := fibermigrate.ConfigOf(app)")
		assert.Contains(t, content, "second := fibermigrate.New(cfg)")
		assert.Len(t, notes, 1)
	})

	t.Run("in a module whose apps have none it stays", func(t *testing.T) {
		t.Parallel()

		dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New(fiber.Config{AppName: "x"})
	copyOf := fiber.New(app.Config())
	_ = copyOf
}
`)
		before := projectFiles(t, dir)

		out := appRun(t, dir, v3.MigrateConfigListenerFields)

		assert.Empty(t, out)
		assert.Equal(t, before, projectFiles(t, dir))
	})
}

// Where the registry cannot keep the settings of an app, the reads stay and say why;
// the build fails on them, which is better than a value that is not the app's.
func Test_MigrateConfigListenerFields_Reads_RegistryNotAvailable(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func tune(cfg *fiber.Config) { cfg.AppName = "tuned" }

func main() {
	cfg := fiber.Config{Prefork: true}
	tune(&cfg)
	app := fiber.New(cfg)
	fmt.Println(app.Config().Prefork)
	app.Listen(":3000")
}
`)
	before := projectFiles(t, dir)

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Empty(t, out)
	assert.Equal(t, before, projectFiles(t, dir), "nothing moves")
	notes := internal.DrainNotes(dir)
	var read bool
	for _, n := range notes {
		read = read || (n.Line == 15 && strings.Contains(n.Message, "has no Prefork field in v3") && strings.Contains(n.Message, "could not be used"))
	}
	assert.True(t, read, "%v", shimNotes(notes))
}

// A variable that holds the config and is used any other way cannot take its type.
func Test_MigrateConfigListenerFields_Reads_ConfigThatTravels(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func show(cfg fiber.Config) {}

func main() {
	app := fiber.New()
	cfg := app.Config()
	show(cfg)
	fmt.Println(cfg.Prefork)
}
`,
	})
	before := projectFiles(t, dir)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Equal(t, before, projectFiles(t, dir))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "has no Prefork field in v3")
	assert.Contains(t, notes[0].Message, "this fiber.Config is not one the migration follows")
}
