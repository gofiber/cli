package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A fiber.Config that a function returns, another one takes as a parameter and a
// third one starts the app of, is one config: every place that holds it takes the
// type of the registry, and the app is made from it by the registry.
func Test_MigrateConfigListenerFields_Flow_ParametersAndResults(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/config/config.go": `package config

import (
	"os"

	"github.com/gofiber/fiber/v2"
)

// Fiber returns the settings of the HTTP server.
func Fiber() fiber.Config {
	return fiber.Config{
		AppName:               "param",
		DisableStartupMessage: true,
		Prefork:               os.Getenv("PREFORK") == "1",
	}
}

// Admin completes the settings of the admin server.
func Admin(quiet bool) (fiber.Config, error) {
	cfg := fiber.Config{AppName: "admin"}
	cfg.DisableStartupMessage = quiet
	return cfg, nil
}
`,
		"internal/server/server.go": `package server

import "github.com/gofiber/fiber/v2"

// New builds the application from the given settings.
func New(cfg fiber.Config) *fiber.App {
	app := fiber.New(cfg)
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}
`,
		"main.go": `package main

import (
	"log"
	"os"

	"example.com/app/internal/config"
	"example.com/app/internal/server"
)

func main() {
	app := server.New(config.Fiber())
	admin, err := config.Admin(true)
	if err != nil {
		log.Fatal(err)
	}
	go server.New(admin).Listen(":3001")
	log.Fatal(app.Listen(os.Args[1]))
}
`,
	})

	out, notes := shimMigrate(t, dir)

	assert.Contains(t, out, "Migrating listener related config fields")
	assert.Equal(t, `package config

import (
	"os"

	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

// Fiber returns the settings of the HTTP server.
func Fiber() fibermigrate.Config {
	return fibermigrate.Config{
		Config: fiber.Config{
			AppName: "param",
		},
		DisableStartupMessage: true,
		Prefork:               os.Getenv("PREFORK") == "1",
	}
}

// Admin completes the settings of the admin server.
func Admin(quiet bool) (fibermigrate.Config, error) {
	cfg := fibermigrate.Config{Config: fiber.Config{AppName: "admin"}}
	cfg.DisableStartupMessage = quiet
	return cfg, nil
}
`, appRead(t, dir, "internal/config/config.go"))
	assert.Equal(t, `package server

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

// New builds the application from the given settings.
func New(cfg fibermigrate.Config) *fiber.App {
	app := fibermigrate.New(cfg)
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}
`, appRead(t, dir, "internal/server/server.go"))
	assert.Equal(t, `package main

import (
	"log"
	"os"

	"example.com/app/internal/config"
	"example.com/app/internal/fibermigrate"
	"example.com/app/internal/server"
)

func main() {
	app := server.New(config.Fiber())
	admin, err := config.Admin(true)
	if err != nil {
		log.Fatal(err)
	}
	go fibermigrate.Listen(server.New(admin), ":3001")
	log.Fatal(app.Listen(os.Args[1], fibermigrate.ListenConfig(app)))
}
`, appRead(t, dir, "main.go"))

	require.Len(t, notes, 1)
	assert.Equal(t, "internal/server/server.go", notes[0].File)
	assert.Contains(t, notes[0].Message, "config.Fiber, config.Admin now return fibermigrate.Config and server.New now takes a fibermigrate.Config")
	assert.NotContains(t, notes[0].Message, "other modules", "internal packages are not imported from outside")
}

// A config that a struct holds in a field behind a getter, built by a method and
// read as a pointer by the code that makes the app, which embeds it.
func Test_MigrateConfigListenerFields_Flow_FieldsAndGetters(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"config/config.go": `package config

import (
	"os"

	"github.com/gofiber/fiber/v2"
)

// Config is the configuration of the program.
type Config struct {
	name  string
	fiber *fiber.Config
}

// New reads the configuration.
func New() *Config {
	config := &Config{name: "boilerplate"}
	config.setFiberConfig()
	return config
}

func (config *Config) setFiberConfig() {
	config.fiber = &fiber.Config{
		AppName:               config.name,
		Prefork:               os.Getenv("PREFORK") == "1",
		DisableStartupMessage: os.Getenv("QUIET") == "1",
	}
}

// GetFiberConfig returns the settings of the app.
func (config *Config) GetFiberConfig() *fiber.Config {
	return config.fiber
}
`,
		"main.go": `package main

import (
	"example.com/app/config"
	"github.com/gofiber/fiber/v2"
)

type App struct {
	*fiber.App
}

func main() {
	config := config.New()
	app := App{App: fiber.New(*config.GetFiberConfig())}
	app.Listen(":3000")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	assert.Equal(t, `package config

import (
	"os"

	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

// Config is the configuration of the program.
type Config struct {
	name  string
	fiber *fibermigrate.Config
}

// New reads the configuration.
func New() *Config {
	config := &Config{name: "boilerplate"}
	config.setFiberConfig()
	return config
}

func (config *Config) setFiberConfig() {
	config.fiber = &fibermigrate.Config{
		Config: fiber.Config{
			AppName: config.name,
		},
		Prefork:               os.Getenv("PREFORK") == "1",
		DisableStartupMessage: os.Getenv("QUIET") == "1",
	}
}

// GetFiberConfig returns the settings of the app.
func (config *Config) GetFiberConfig() *fibermigrate.Config {
	return config.fiber
}
`, appRead(t, dir, "config/config.go"))
	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, "app := App{App: fibermigrate.New(*config.GetFiberConfig())}")
	assert.Contains(t, main, `app.Listen(":3000", fibermigrate.ListenConfig(app.App))`)

	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "config.Config.GetFiberConfig now returns fibermigrate.Config")
	assert.Contains(t, notes[0].Message, "can be called from other modules", "config is a package other modules can import")
}

// The functions that change a config, the options of a constructor, are one more
// kind of place: the functions of the type take the config of the registry.
func Test_MigrateConfigListenerFields_Flow_Options(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/app/options.go": `package app

import (
	"os"

	"github.com/gofiber/fiber/v2"
)

// Option changes the config of an app.
type Option func(*fiber.Config)

// Quiet hides the startup banner.
func Quiet() Option {
	return func(c *fiber.Config) { c.DisableStartupMessage = true }
}

// WithPrefork turns prefork on from the environment.
func WithPrefork() Option {
	return func(c *fiber.Config) { c.Prefork = os.Getenv("PREFORK") == "1" }
}

// Named sets a field that exists in v3 too.
func Named(name string) Option {
	return func(c *fiber.Config) { c.AppName = name }
}

// withNetwork is a declared function used as an option.
func withNetwork(c *fiber.Config) { c.Network = "tcp4" }

// Build applies the options to the defaults.
func Build(opts ...Option) fiber.Config {
	cfg := fiber.Config{AppName: "options"}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// New makes an app and hands its options on.
func New(opts ...Option) *fiber.App {
	return fiber.New(Build(opts...))
}

// Default is the app of the program.
func Default() *fiber.App {
	loud := func(c *fiber.Config) { c.EnablePrintRoutes = true }
	list := []Option{Quiet(), Named("x")}
	return New(append(list, WithPrefork(), withNetwork, loud)...)
}
`,
		"main.go": `package main

import (
	"log"

	"example.com/app/internal/app"
)

func main() {
	log.Fatal(app.Default().Listen(":3000"))
}
`,
	})

	_, notes := shimMigrate(t, dir)

	assert.Equal(t, `package app

import (
	"os"

	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

// Option changes the config of an app.
type Option func(*fibermigrate.Config)

// Quiet hides the startup banner.
func Quiet() Option {
	return func(c *fibermigrate.Config) { c.DisableStartupMessage = true }
}

// WithPrefork turns prefork on from the environment.
func WithPrefork() Option {
	return func(c *fibermigrate.Config) { c.Prefork = os.Getenv("PREFORK") == "1" }
}

// Named sets a field that exists in v3 too.
func Named(name string) Option {
	return func(c *fibermigrate.Config) { c.AppName = name }
}

// withNetwork is a declared function used as an option.
func withNetwork(c *fibermigrate.Config) { c.Network = "tcp4" }

// Build applies the options to the defaults.
func Build(opts ...Option) fibermigrate.Config {
	cfg := fibermigrate.Config{Config: fiber.Config{AppName: "options"}}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// New makes an app and hands its options on.
func New(opts ...Option) *fiber.App {
	return fibermigrate.New(Build(opts...))
}

// Default is the app of the program.
func Default() *fiber.App {
	loud := func(c *fibermigrate.Config) { c.EnablePrintRoutes = true }
	list := []Option{Quiet(), Named("x")}
	return New(append(list, WithPrefork(), withNetwork, loud)...)
}
`, appRead(t, dir, "internal/app/options.go"))
	assert.Contains(t, appRead(t, dir, "main.go"), `log.Fatal(fibermigrate.Listen(app.Default(), ":3000"))`)

	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "app.Build now returns fibermigrate.Config")
	assert.Contains(t, notes[0].Message, "app.Option, app.withNetwork now take a fibermigrate.Config")
}

// A literal of an alias of fiber.Config, in a file that names neither fiber nor a
// Listen call, is wrapped like the others, and so is the parameter that has the alias
// for its type.
func Test_MigrateConfigListenerFields_Flow_AliasInAnotherPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/web/web.go": `// Package web names the fiber types the program uses.
package web

import "github.com/gofiber/fiber/v2"

// Config is the fiber configuration.
type Config = fiber.Config

// App is the fiber application.
type App = fiber.App

// New builds an application from a configuration.
func New(cfg Config) *App {
	return fiber.New(cfg)
}
`,
		"helpers.go": `package main

import "example.com/app/internal/web"

// plain is a configuration without any of the settings.
func plain() web.Config { return web.Config{AppName: "plain"} }
`,
		"main.go": `package main

import (
	"log"
	"os"

	"example.com/app/internal/web"
)

func main() {
	app := web.New(web.Config{
		AppName:               "alias",
		DisableStartupMessage: true,
		Prefork:               os.Getenv("PREFORK") == "1",
	})
	other := web.New(plain())
	go log.Fatal(other.Listen(":3001"))
	log.Fatal(app.Listen(":3000"))
}
`,
	})

	_, notes := shimMigrate(t, dir)

	assert.Equal(t, `package main

import (
	"log"
	"os"

	"example.com/app/internal/fibermigrate"
	"example.com/app/internal/web"
)

func main() {
	app := web.New(fibermigrate.Config{
		Config: web.Config{
			AppName: "alias",
		},
		DisableStartupMessage: true,
		Prefork:               os.Getenv("PREFORK") == "1",
	})
	other := web.New(plain())
	go log.Fatal(other.Listen(":3001", fibermigrate.ListenConfig(other)))
	log.Fatal(app.Listen(":3000", fibermigrate.ListenConfig(app)))
}
`, appRead(t, dir, "main.go"))
	assert.Equal(t, `package main

import (
	"example.com/app/internal/fibermigrate"
	"example.com/app/internal/web"
)

// plain is a configuration without any of the settings.
func plain() fibermigrate.Config { return fibermigrate.Config{Config: web.Config{AppName: "plain"}} }
`, appRead(t, dir, "helpers.go"))
	web := appRead(t, dir, "internal/web/web.go")
	assert.Contains(t, web, "func New(cfg fibermigrate.Config) *App {\n\treturn fibermigrate.New(cfg)\n}")
	assert.Contains(t, web, "type Config = fiber.Config", "the alias stays what it was")
	require.Len(t, notes, 1)
}

// A config in a variable of the package that functions of other packages complete.
func Test_MigrateConfigListenerFields_Flow_VariableOfAnotherPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/cfg/cfg.go": `package cfg

import "github.com/gofiber/fiber/v2"

// Shared is the config of the apps of the program.
var Shared = fiber.Config{AppName: "shared"}

// Quiet hides the banner of the apps.
func Quiet() { Shared.DisableStartupMessage = true }
`,
		"internal/app/app.go": `package app

import (
	"example.com/app/internal/cfg"
	"github.com/gofiber/fiber/v2"
)

// New makes an app.
func New() *fiber.App { return fiber.New(cfg.Shared) }
`,
		"main.go": `package main

import (
	"log"

	"example.com/app/internal/app"
	"example.com/app/internal/cfg"
)

func main() {
	cfg.Quiet()
	log.Fatal(app.New().Listen(":3000"))
}
`,
	})

	_, notes := shimMigrate(t, dir)

	assert.Contains(t, appRead(t, dir, "internal/cfg/cfg.go"), "var Shared = fibermigrate.Config{Config: fiber.Config{AppName: \"shared\"}}")
	assert.Contains(t, appRead(t, dir, "internal/cfg/cfg.go"), "func Quiet() { Shared.DisableStartupMessage = true }")
	assert.Contains(t, appRead(t, dir, "internal/app/app.go"), "func New() *fiber.App { return fibermigrate.New(cfg.Shared) }")
	assert.Contains(t, appRead(t, dir, "main.go"), `log.Fatal(fibermigrate.Listen(app.New(), ":3000"))`)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "cfg.Shared now holds a fibermigrate.Config")
}

// The settings of an app that no call of the module starts have no effect, and are
// dropped from all the places that set them, with one note.
func Test_MigrateConfigListenerFields_Flow_DropsTheSettingsOfAppsNobodyStarts(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/app/app.go": `package app

import "github.com/gofiber/fiber/v2"

// New makes the app of a server.
func New(cfg fiber.Config) *fiber.App {
	app := fiber.New(cfg)
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

// Option changes a config.
type Option func(*fiber.Config)

// Quiet hides the banner.
func Quiet() Option { return func(c *fiber.Config) { c.DisableStartupMessage = true } }

// Named names the app and prints its routes.
func Named(name string) Option {
	return func(c *fiber.Config) {
		c.AppName = name
		c.EnablePrintRoutes = true
	}
}

// Make makes an app from the options.
func Make(opts ...Option) *fiber.App {
	cfg := fiber.Config{}
	for _, o := range opts {
		o(&cfg)
	}
	return New(cfg)
}
`,
		"internal/app/app_test.go": `package app

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func quiet() fiber.Config {
	return fiber.Config{AppName: "test", DisableStartupMessage: true}
}

func TestNew(t *testing.T) {
	app := New(quiet())
	if _, err := app.Test(httptest.NewRequest("GET", "/", nil), -1); err != nil {
		t.Fatal(err)
	}
	loud := Make(Quiet(), Named("x"))
	_, _ = loud.Test(httptest.NewRequest("GET", "/", nil))
}
`,
	})

	out, notes := shimMigrate(t, dir)

	assert.Contains(t, out, "Migrating listener related config fields")
	assert.Equal(t, `package app

import "github.com/gofiber/fiber/v2"

// New makes the app of a server.
func New(cfg fiber.Config) *fiber.App {
	app := fiber.New(cfg)
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

// Option changes a config.
type Option func(*fiber.Config)

// Quiet hides the banner.
func Quiet() Option { return func(c *fiber.Config) {} }

// Named names the app and prints its routes.
func Named(name string) Option {
	return func(c *fiber.Config) {
		c.AppName = name
	}
}

// Make makes an app from the options.
func Make(opts ...Option) *fiber.App {
	cfg := fiber.Config{}
	for _, o := range opts {
		o(&cfg)
	}
	return New(cfg)
}
`, appRead(t, dir, "internal/app/app.go"))
	assert.Contains(t, appRead(t, dir, "internal/app/app_test.go"), `return fiber.Config{AppName: "test"}`)
	assert.NotContains(t, projectFiles(t, dir), shimFile, "the registry is not needed")

	require.Len(t, notes, 1, "the options and the test make one config")
	assert.Equal(t, "internal/app/app_test.go", notes[0].File)
	assert.Contains(t, notes[0].Message, "has no DisableStartupMessage and EnablePrintRoutes fields in v3: dropped, because no call of this module starts the app this config makes")
}

// Where the config goes through something the migration does not follow, the
// settings stay and the note says what stopped it.
func Test_MigrateConfigListenerFields_Flow_WhatCannotBeFollowed(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"github.com/gofiber/fiber/v2"
)

type Embedded struct {
	fiber.Config
	Name string
}

type Starter interface{ Start(cfg fiber.Config) *fiber.App }

type real struct{}

func (real) Start(cfg fiber.Config) *fiber.App { return fiber.New(cfg) }

func custom(cfg fiber.Config) *fiber.App { return fiber.NewWithCustomCtx(nil, cfg) }

func variadic(cfgs ...fiber.Config) *fiber.App { return fiber.New(cfgs...) }

func handler(cfg fiber.Config) *fiber.App { return fiber.New(cfg) }

var handlers = map[string]func(fiber.Config) *fiber.App{"a": handler}

func main() {
	e := Embedded{Config: fiber.Config{Prefork: true}}
	fiber.New(e.Config).Listen(":3000")
	var s Starter = real{}
	s.Start(fiber.Config{DisableStartupMessage: true}).Listen(":3001")
	custom(fiber.Config{EnablePrintRoutes: true}).Listen(":3002")
	variadic(fiber.Config{Network: "tcp"}).Listen(":3003")
	handlers["a"](fiber.Config{Prefork: false, EnablePrintRoutes: true}).Listen(":3004")
}
`)
	before := appRead(t, dir, "main.go")

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Empty(t, out)
	assert.Equal(t, before, appRead(t, dir, "main.go"))
	var reasons []string
	for _, n := range internal.DrainNotes(dir) {
		reasons = append(reasons, n.Message[strings.LastIndex(n.Message, "(")+1:])
	}
	require.Len(t, reasons, 6)
	assert.Equal(t, "Config is embedded in Embedded, at line 8 of main.go)", reasons[0])
	assert.Equal(t, "the config is passed to s.Start, a method of an interface, at line 30 of main.go)", reasons[1])
	assert.Equal(t, "the config is passed to fiber.NewWithCustomCtx, which the registry does not stand in for, at line 18 of main.go)", reasons[2])
	assert.Equal(t, "the config is passed to variadic, which takes it in a variadic parameter, at line 32 of main.go)", reasons[3])
	assert.Equal(t, `the config is passed to handlers["a"], at line 33 of main.go)`, reasons[4])
	assert.Equal(t, `the config is passed to handlers["a"], at line 33 of main.go)`, reasons[5])
}

// A config that is made in one module and turned into an app in another cannot be
// carried: each module has a registry of its own, and two of them cannot exchange
// values. The note says so.
func Test_MigrateConfigListenerFields_Flow_AcrossModules(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod":                    "// the repository is no module\n",
		"app/go.mod":                "module example.com/mono/app\n\ngo 1.25\n",
		"shared/go.mod":             "module example.com/mono/shared\n\ngo 1.25\n",
		"shared/config/config.go":   "package config\n\nimport \"github.com/gofiber/fiber/v2\"\n\nfunc Fiber() fiber.Config {\n\treturn fiber.Config{AppName: \"mono\", DisableStartupMessage: true}\n}\n",
		"shared/server/server.go":   "package server\n\nimport \"github.com/gofiber/fiber/v2\"\n\nfunc New(cfg fiber.Config) *fiber.App { return fiber.New(cfg) }\n",
		"app/main.go":               "package main\n\nimport (\n\t\"log\"\n\n\t\"example.com/mono/shared/config\"\n\t\"example.com/mono/shared/server\"\n)\n\nfunc main() {\n\tlog.Fatal(server.New(config.Fiber()).Listen(\":3000\"))\n}\n",
		"app/other/other.go":        "package other\n\nimport (\n\t\"example.com/mono/shared/server\"\n\t\"github.com/gofiber/fiber/v2\"\n)\n\nfunc New() *fiber.App { return server.New(fiber.Config{Prefork: true}) }\n",
		"shared/server/internal.go": "package server\n\nimport \"github.com/gofiber/fiber/v2\"\n\nfunc newInternal() *fiber.App { return fiber.New(fiber.Config{EnablePrintRoutes: true}) }\n\nfunc Internal() *fiber.App { return newInternal() }\n",
	})
	before := projectFiles(t, dir)

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Empty(t, out)
	assert.Equal(t, before, projectFiles(t, dir), "nothing is carried across modules")
	var messages []string
	for _, n := range internal.DrainNotes(dir) {
		messages = append(messages, n.File+": "+n.Message)
	}
	joined := strings.Join(messages, "\n")
	assert.Contains(t, joined, "belong to different Go modules: each module gets a registry of its own, and two of them cannot exchange values")
	assert.Contains(t, joined, "shared/config/config.go: fiber.Config has no DisableStartupMessage field in v3")
	assert.Contains(t, joined, "app/other/other.go: fiber.Config has no Prefork field in v3")
}

// An app that nothing of the module starts may still be started by code the migration
// does not see: the settings of the apps that go there stay, and the note says where.
func Test_MigrateConfigListenerFields_Flow_AppsThatReachOtherCode(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http/httptest"

	"github.com/example/runner"
	"github.com/gofiber/fiber/v2"
)

type holder struct{ app *fiber.App }

func register(r fiber.Router) { r.Get("/", func(c *fiber.Ctx) error { return nil }) }

func build() *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	register(app)
	return app
}

func main() {
	build().Test(httptest.NewRequest("GET", "/", nil))

	other := fiber.New(fiber.Config{Prefork: true})
	runner.Run(other)

	third := fiber.New(fiber.Config{EnablePrintRoutes: true})
	_ = holder{app: third}
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, "app := fiber.New(fiber.Config{})", "an app that only its own module's code uses has no use for them")
	assert.Contains(t, main, "other := fiber.New(fiber.Config{Prefork: true})")
	assert.Contains(t, main, "third := fiber.New(fiber.Config{EnablePrintRoutes: true})")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 3)
	assert.Contains(t, notes[0].Message, "has no DisableStartupMessage field in v3: dropped")
	assert.Contains(t, notes[1].Message, "no Listen call of this module starts the app, but the app is passed to runner.Run, at line 24 of main.go, where code outside the migration may start it")
	assert.Contains(t, notes[2].Message, "the app is stored in the field app of holder, at line 10 of main.go")
}
