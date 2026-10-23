package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// An alias of fiber.Config is a fiber.Config, in the package that declares it and in the
// ones that use it.
func Test_MigrateConfigListenerFields_AliasOfTheConfig(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/web/web.go": `package web

import "github.com/gofiber/fiber/v2"

// Config is the fiber configuration.
type Config = fiber.Config

// Settings is another name of it.
type Settings = Config
`,
		"main.go": `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"

	"example.com/app/internal/web"
)

type Local = fiber.Config

func main() {
	app := fiber.New(web.Config{AppName: "a", DisableStartupMessage: true, Prefork: os.Getenv("PREFORK") == "1"})
	go app.Listen(":3000")

	second := fiber.New(Local{EnablePrintRoutes: true})
	go second.Listen(":3001")

	var cfg web.Settings
	cfg.Network = "tcp4"
	third := fiber.New(cfg)
	log.Fatal(third.Listen(":3002"))
}
`,
	})

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Contains(t, out, "Migrating listener related config fields")
	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, `app := fiber.New(web.Config{AppName: "a"})`)
	assert.Contains(t, main, `go app.Listen(":3000", fiber.ListenConfig{EnablePrefork: os.Getenv("PREFORK") == "1", DisableStartupMessage: true})`)
	assert.Contains(t, main, "second := fiber.New(Local{})")
	assert.Contains(t, main, `go second.Listen(":3001", fiber.ListenConfig{EnablePrintRoutes: true})`)
	assert.NotContains(t, main, "cfg.Network")
	assert.Contains(t, main, `third.Listen(":3002", fiber.ListenConfig{ListenerNetwork: "tcp4"})`)
	assert.Empty(t, internal.DrainNotes(dir))
	assert.Empty(t, appRun(t, dir, v3.MigrateConfigListenerFields), "a second run changes nothing")
}
