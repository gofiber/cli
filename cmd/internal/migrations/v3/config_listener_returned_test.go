package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A function that builds the config with the arguments it gets cannot hand the
// settings to the call that starts the app. It returns the config of v2 instead, and
// the app is made from it the way the registry makes apps.
func Test_MigrateConfigListenerFields_Returned_ValuesFromTheArguments(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/config/config.go": `package config

import "github.com/gofiber/fiber/v2"

// ForServer is the config of an instance of the server.
func ForServer(name string, prefork bool) fiber.Config {
	return fiber.Config{
		AppName: name,
		Prefork: prefork,
		Network: "tcp4",
	}
}
`,
		"cmd/api/main.go": `package main

import (
	"log"

	"example.com/app/internal/config"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New(config.ForServer("api", true))
	log.Fatal(app.Listen(":3000"))
}
`,
	})

	out, notes := shimMigrate(t, dir)

	assert.Contains(t, out, "Migrating listener related config fields")
	assert.Equal(t, `package config

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

// ForServer is the config of an instance of the server.
func ForServer(name string, prefork bool) fibermigrate.Config {
	return fibermigrate.Config{
		Config: fiber.Config{
			AppName: name,
		},
		Prefork: prefork,
		Network: "tcp4",
	}
}
`, appRead(t, dir, "internal/config/config.go"))
	assert.Equal(t, `package main

import (
	"log"

	"example.com/app/internal/config"
	"example.com/app/internal/fibermigrate"
)

func main() {
	app := fibermigrate.New(config.ForServer("api", true))
	log.Fatal(app.Listen(":3000", fibermigrate.ListenConfig(app)))
}
`, appRead(t, dir, "cmd/api/main.go"))

	require.Len(t, notes, 1)
	assert.Equal(t, "cmd/api/main.go", notes[0].File)
	assert.Contains(t, notes[0].Message, "config.ForServer now returns fibermigrate.Config")
	assert.NotContains(t, notes[0].Message, "other modules", "internal packages are not imported from outside")
}

// Several returns, a config that a function completes in a variable, and one that
// is a variable declared without a value are all the same config to the callers.
func Test_MigrateConfigListenerFields_Returned_BranchesAndVariables(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"config/config.go": `package config

import "github.com/gofiber/fiber/v2"

// ForEnv builds the config of an environment.
func ForEnv(env string) fiber.Config {
	cfg := fiber.Config{AppName: "api-" + env}
	if env == "prod" {
		cfg.Prefork = true
		cfg.DisableStartupMessage = true
	}
	return cfg
}

func Dev(quiet bool) fiber.Config {
	if quiet {
		return fiber.Config{AppName: "dev", DisableStartupMessage: true}
	}
	return fiber.Config{AppName: "dev"}
}

func Plain() fiber.Config {
	var cfg fiber.Config
	cfg.Network = "tcp4"
	return cfg
}
`,
		"main.go": `package main

import (
	"fmt"
	"os"

	"example.com/app/config"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New(config.ForEnv(os.Getenv("ENV")))
	go app.Listen(":3000")

	dev := config.Dev(true)
	dev.EnablePrintRoutes = true
	fmt.Println(dev.Prefork)
	second := fiber.New(dev)
	go second.Listen(":3001")

	third := fiber.New(config.Plain())
	third.Listen(":3002")
	fmt.Println(config.Plain().Network)
}
`,
	})

	_, notes := shimMigrate(t, dir)

	cfg := appRead(t, dir, "config/config.go")
	assert.Contains(t, cfg, "func ForEnv(env string) fibermigrate.Config {\n\tcfg := fibermigrate.Config{Config: fiber.Config{AppName: \"api-\" + env}}\n")
	assert.Contains(t, cfg, "\t\tcfg.Prefork = true\n\t\tcfg.DisableStartupMessage = true\n", "the assignments are the ones of v2")
	assert.Contains(t, cfg, "return fibermigrate.Config{Config: fiber.Config{AppName: \"dev\"}, DisableStartupMessage: true}")
	assert.Contains(t, cfg, "return fibermigrate.Config{Config: fiber.Config{AppName: \"dev\"}}", "a return that sets none of them is the config of v2 too")
	assert.Contains(t, cfg, "func Plain() fibermigrate.Config {\n\tvar cfg fibermigrate.Config\n\tcfg.Network = \"tcp4\"\n")

	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, "app := fibermigrate.New(config.ForEnv(os.Getenv(\"ENV\")))")
	assert.Contains(t, main, `go app.Listen(":3000", fibermigrate.ListenConfig(app))`)
	assert.Contains(t, main, "dev := config.Dev(true)\n\tdev.EnablePrintRoutes = true\n\tfmt.Println(dev.Prefork)\n\tsecond := fibermigrate.New(dev)")
	assert.Contains(t, main, `go second.Listen(":3001", fibermigrate.ListenConfig(second))`)
	assert.Contains(t, main, `third.Listen(":3002", fibermigrate.ListenConfig(third))`)
	assert.Contains(t, main, "fmt.Println(config.Plain().Network)")

	require.Len(t, notes, 1, "the settings that the code reads or assigns are the ones the migration follows")
	assert.Contains(t, notes[0].Message, "config.ForEnv, config.Dev, config.Plain now return fibermigrate.Config")
	assert.Contains(t, notes[0].Message, "can be called from other modules", "config is a package other modules can import")
}

// A method that builds the config is followed to the calls on its type; the
// settings that are static still move to the call that starts the app.
func Test_MigrateConfigListenerFields_Returned_Methods(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/settings/settings.go": `package settings

import "github.com/gofiber/fiber/v2"

type Settings struct {
	Quiet   bool
	Workers int
}

func (s *Settings) Fiber() fiber.Config {
	return fiber.Config{EnablePrintRoutes: !s.Quiet, DisableStartupMessage: s.Quiet}
}

func Static() fiber.Config {
	return fiber.Config{AppName: "static", DisableStartupMessage: true}
}
`,
		"main.go": `package main

import (
	"example.com/app/internal/settings"
	"github.com/gofiber/fiber/v2"
)

func main() {
	s := &settings.Settings{Quiet: true}
	app := fiber.New(s.Fiber())
	go app.Listen(":3000")

	static := fiber.New(settings.Static())
	static.Listen(":3001")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	settings := appRead(t, dir, "internal/settings/settings.go")
	assert.Contains(t, settings, "func (s *Settings) Fiber() fibermigrate.Config {\n\treturn fibermigrate.Config{EnablePrintRoutes: !s.Quiet, DisableStartupMessage: s.Quiet}\n}")
	assert.Contains(t, settings, "func Static() fiber.Config {\n\treturn fiber.Config{AppName: \"static\"}\n}")
	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, "app := fibermigrate.New(s.Fiber())")
	assert.Contains(t, main, `go app.Listen(":3000", fibermigrate.ListenConfig(app))`)
	assert.Contains(t, main, "static := fiber.New(settings.Static())")
	assert.Contains(t, main, `static.Listen(":3001", fiber.ListenConfig{DisableStartupMessage: true})`)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "settings.Settings.Fiber now returns fibermigrate.Config")
}

// The result of the function has to be handed to fiber.New, and the function has to
// be called: where it is not, the settings stay with a note that says why.
func Test_MigrateConfigListenerFields_Returned_WhatCannotBeFollowed(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"config/a.go": `package config

import "github.com/gofiber/fiber/v2"

func A(p bool) fiber.Config { return fiber.Config{Prefork: p} }

var ByName = map[string]func(bool) fiber.Config{"a": A}
`,
		"config/b.go": `package config

import "github.com/gofiber/fiber/v2"

func Base() fiber.Config { return fiber.Config{AppName: "b"} }

func B(p bool) fiber.Config {
	cfg := Base()
	cfg.Prefork = p
	return cfg
}
`,
		"config/c.go": `package config

import "github.com/gofiber/fiber/v2"

func C(p bool) fiber.Config { return fiber.Config{Prefork: p} }
`,
		"config/d.go": `package config

import "github.com/gofiber/fiber/v2"

type Provider interface{ Config() fiber.Config }

type ProdProvider struct{ Prefork bool }

func (p ProdProvider) Config() fiber.Config { return fiber.Config{Prefork: p.Prefork} }
`,
		"main.go": `package main

import (
	"fmt"

	"example.com/app/config"
	"github.com/gofiber/fiber/v2"
)

func main() {
	fmt.Println(config.C(true))

	var p config.Provider = config.ProdProvider{}
	app := fiber.New(p.Config())
	app.Listen(":3000")

	b := fiber.New(config.B(true))
	b.Listen(":3001")

	a := fiber.New(config.ByName["a"](true))
	a.Listen(":3002")
	fmt.Println(fiber.New(config.C(false)))
}
`,
	})
	before := projectFiles(t, dir)

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Empty(t, out)
	assert.Equal(t, before, projectFiles(t, dir), "nothing is changed")
	byFile := map[string]string{}
	for _, n := range internal.DrainNotes(dir) {
		byFile[n.File] = n.Message
	}
	assert.Contains(t, byFile["config/a.go"], "A is used as a value, at line 7 of config/a.go")
	assert.Contains(t, byFile["config/b.go"], "this fiber.Config is not one the migration follows")
	assert.Contains(t, byFile["config/c.go"], "the result of C is passed to another function, at line 11 of main.go")
	assert.Contains(t, byFile["config/d.go"], "a call of Config whose receiver could not be traced may hand its config to fiber.New, at line 14 of main.go")
	assert.Len(t, byFile, 4)
}

// The apps made from what the function returns are started anywhere in the module,
// also by code that only gets the app.
func Test_MigrateConfigListenerFields_Returned_StartedByAnotherPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/config/config.go": `package config

import "github.com/gofiber/fiber/v2"

func ForServer(prefork bool) fiber.Config { return fiber.Config{Prefork: prefork, AppName: "x"} }
`,
		"internal/serve/serve.go": `package serve

import "github.com/gofiber/fiber/v2"

func Run(app *fiber.App, addr string) error { return app.Listen(addr) }
`,
		"cmd/api/main.go": `package main

import (
	"log"

	"example.com/app/internal/config"
	"example.com/app/internal/serve"
	"github.com/gofiber/fiber/v2"
)

func main() {
	log.Fatal(serve.Run(fiber.New(config.ForServer(false)), ":3000"))
}
`,
	})

	_, notes := shimMigrate(t, dir)

	assert.Contains(t, appRead(t, dir, "internal/serve/serve.go"), "return app.Listen(addr, fibermigrate.ListenConfig(app))")
	main := appRead(t, dir, "cmd/api/main.go")
	assert.Contains(t, main, `serve.Run(fibermigrate.New(config.ForServer(false)), ":3000")`)
	assert.Len(t, notes, 1)
}

// A field that has the name of the method that builds the config is not the method.
func Test_MigrateConfigListenerFields_Returned_FieldWithTheNameOfTheMethod(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"main.go": `package main

import "github.com/gofiber/fiber/v2"

type builder struct{ quiet bool }

func (b builder) Config() fiber.Config { return fiber.Config{DisableStartupMessage: b.quiet} }

type holder struct{ Config fiber.Config }

func main() {
	h := holder{Config: fiber.Config{AppName: "held"}}
	_ = h.Config
	app := fiber.New(builder{quiet: true}.Config())
	app.Listen(":3000")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "func (b builder) Config() fibermigrate.Config {")
	assert.Contains(t, content, "app := fibermigrate.New(builder{quiet: true}.Config())")
	assert.Contains(t, content, "h := holder{Config: fiber.Config{AppName: \"held\"}}")
	assert.Len(t, notes, 1)
}

// A public package gets the note that other modules cannot use the result.
func Test_MigrateConfigListenerFields_Returned_PublicPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"config/config.go": `package config

import "github.com/gofiber/fiber/v2"

func ForServer(prefork bool) fiber.Config { return fiber.Config{Prefork: prefork} }

func forTests(prefork bool) fiber.Config { return fiber.Config{Prefork: prefork} }

func NewTest() *fiber.App { return fiber.New(forTests(true)) }
`,
		"main.go": `package main

import (
	"example.com/app/config"
	"github.com/gofiber/fiber/v2"
)

func main() {
	config.NewTest().Listen(":3001")
	fiber.New(config.ForServer(true)).Listen(":3000")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	require.Len(t, notes, 1)
	msg := notes[0].Message
	assert.Contains(t, msg, "config.ForServer, config.forTests now return fibermigrate.Config")
	assert.Contains(t, msg, "config.ForServer can be called from other modules, which cannot hand the result to fiber.New")
	assert.NotContains(t, msg, "config.forTests can be called")
}

// The fields that are evaluated before the others in v2 stay before them when the
// order of the calls they make matters; when it cannot be kept the note says so.
func Test_MigrateConfigListenerFields_Returned_EvaluationOrder(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/config/config.go": `package config

import "github.com/gofiber/fiber/v2"

var calls []string

func note(s string) string { calls = append(calls, s); return s }

func notePrefork(s string) bool { calls = append(calls, s); return true }

// Before has the setting first, so that it can stay first.
func Before(p string) fiber.Config {
	return fiber.Config{Prefork: notePrefork(p), AppName: note("name")}
}

// After has it last: the order is the one of the registry's config.
func After(p string) fiber.Config {
	return fiber.Config{AppName: note("name"), Prefork: notePrefork(p)}
}

func width() int { calls = append(calls, "width"); return 1 }

// Between has it in the middle, which the config of v2 cannot keep.
func Between(p string) fiber.Config {
	return fiber.Config{AppName: note("name"), Prefork: notePrefork(p), Network: "tcp", Concurrency: width()}
}

// Pure values do not care.
func Pure(p bool) fiber.Config {
	return fiber.Config{AppName: note("name"), Prefork: p, Network: "tcp"}
}
`,
		"cmd/api/main.go": `package main

import (
	"example.com/app/internal/config"
	"github.com/gofiber/fiber/v2"
)

func main() {
	fiber.New(config.Before("a")).Listen(":3000")
	fiber.New(config.After("b")).Listen(":3001")
	fiber.New(config.Between("c")).Listen(":3002")
	fiber.New(config.Pure(true)).Listen(":3003")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	cfg := appRead(t, dir, "internal/config/config.go")
	assert.Contains(t, cfg, "return fibermigrate.Config{Prefork: notePrefork(p), Config: fiber.Config{AppName: note(\"name\")}}")
	assert.Contains(t, cfg, "return fibermigrate.Config{Config: fiber.Config{AppName: note(\"name\")}, Prefork: notePrefork(p)}")
	assert.Contains(t, cfg, "return fibermigrate.Config{Config: fiber.Config{AppName: note(\"name\"), Concurrency: width()}, Prefork: notePrefork(p), Network: \"tcp\"}")
	assert.Contains(t, cfg, "return fibermigrate.Config{Config: fiber.Config{AppName: note(\"name\")}, Prefork: p, Network: \"tcp\"}")

	var order []string
	for _, n := range notes {
		if strings.Contains(n.Message, "evaluated") {
			order = append(order, n.Message)
		}
	}
	require.Len(t, order, 1, "only the config that cannot keep the order says so")
	assert.Contains(t, order[0], "Prefork of this fiber.Config is now evaluated after Concurrency, where v2 evaluated the fields in the order they are written")
}

// A literal handed to fiber.New takes its settings to the registry's NewApp, which
// evaluates them after the rest of the config; the note tells when that matters.
func Test_MigrateConfigListenerFields_EvaluationOrder_NewApp(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/server/server.go": `package server

import "github.com/gofiber/fiber/v2"

func pick() bool { return true }

func name() string { return "api" }

// New has a setting that calls a function before a field that does.
func New() *fiber.App {
	return fiber.New(fiber.Config{Prefork: pick(), AppName: name()})
}

// Quiet has one before a field that is a constant, which does nothing.
func Quiet() *fiber.App {
	return fiber.New(fiber.Config{DisableStartupMessage: pick(), AppName: "quiet"})
}

// Last has the setting after the field, which is the order of NewApp.
func Last() *fiber.App {
	return fiber.New(fiber.Config{AppName: name(), Prefork: pick()})
}
`,
		"cmd/api/main.go": `package main

import "example.com/app/internal/server"

func main() {
	server.New().Listen(":3000")
	server.Quiet().Listen(":3001")
	server.Last().Listen(":3002")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	server := appRead(t, dir, "internal/server/server.go")
	assert.Contains(t, server, "return fibermigrate.NewApp(fiber.Config{AppName: name()}, fiber.ListenConfig{EnablePrefork: pick()})\n}\n\n// Quiet")
	assert.Contains(t, server, "fibermigrate.NewApp(fiber.Config{AppName: \"quiet\"}, fiber.ListenConfig{DisableStartupMessage: pick()})")

	var order []internal.Note
	for _, n := range notes {
		if strings.Contains(n.Message, "evaluated") {
			order = append(order, n)
		}
	}
	require.Len(t, order, 1, "only New puts a setting that calls a function before a field that does")
	assert.Equal(t, 11, order[0].Line)
	assert.Contains(t, order[0].Message, "Prefork of this fiber.Config is now evaluated after AppName")
}
