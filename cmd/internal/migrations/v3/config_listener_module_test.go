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

// moduleBuild is a constructor whose settings come from the environment, so that
// they cannot move to the call that starts the app. It is declared in the package
// the test files put it in.
const moduleBuild = `import (
	"os"

	"github.com/gofiber/fiber/v2"
)

// New builds the app.
func New() *fiber.App {
	cfg := fiber.Config{AppName: "api"}
	if os.Getenv("PREFORK") == "1" {
		cfg.Prefork = true
	}
	cfg.DisableStartupMessage = os.Getenv("QUIET") == "1"
	return fiber.New(cfg)
}
`

// The layout of most programs: an internal package builds the app and a command
// that does not import fiber starts it. The settings travel with the app, and the
// command asks for them.
func Test_MigrateConfigListenerFields_Module_CommandThatDoesNotImportFiber(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/server/server.go": "package server\n\n" + moduleBuild,
		"cmd/api/main.go": `package main

import (
	"log"

	"example.com/app/internal/server"
)

func main() {
	log.Fatal(server.New().Listen(":3000"))
}
`,
		"cmd/api/second.go": `package main

import "example.com/app/internal/server"

func second() error {
	app := server.New()
	return app.Listen(":3001")
}
`,
	})

	out, notes := shimMigrate(t, dir)

	assert.Contains(t, out, "Migrating listener related config fields")
	assert.Equal(t, `package main

import (
	"log"

	"example.com/app/internal/fibermigrate"
	"example.com/app/internal/server"
)

func main() {
	log.Fatal(fibermigrate.Listen(server.New(), ":3000"))
}
`, appRead(t, dir, "cmd/api/main.go"))
	assert.Equal(t, `package main

import (
	"example.com/app/internal/fibermigrate"
	"example.com/app/internal/server"
)

func second() error {
	app := server.New()
	return app.Listen(":3001", fibermigrate.ListenConfig(app))
}
`, appRead(t, dir, "cmd/api/second.go"))

	server := appRead(t, dir, "internal/server/server.go")
	assert.Contains(t, server, `"example.com/app/internal/fibermigrate"`)
	assert.Contains(t, server, "cfgListen := fiber.ListenConfig{}")
	assert.Contains(t, server, "return fibermigrate.NewApp(cfg, cfgListen)")
	assert.FileExists(t, filepath.Join(dir, shimFile))

	// one note, in the package that makes the app; internal packages are not imported from outside
	require.Len(t, notes, 1)
	assert.Equal(t, "internal/server/server.go", filepath.ToSlash(notes[0].File))
	assert.NotContains(t, notes[0].Message, "export")
}

// A helper of another package that starts the app it is given asks for the
// settings itself; the package that made the app needs nothing more.
func Test_MigrateConfigListenerFields_Module_RunHelperOfAnotherPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/serve/serve.go": `package serve

import "github.com/gofiber/fiber/v2"

// Run starts the app.
func Run(app *fiber.App, addr string) error {
	return app.Listen(addr)
}

// RunTLS starts the app with a certificate.
func RunTLS(app *fiber.App, addr string) error {
	return app.ListenTLS(addr, "cert.pem", "key.pem")
}

// Register adds the routes of the program.
func Register(app *fiber.App) {
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
}
`,
		"main.go": `package main

import (
	"log"
	"os"

	"example.com/app/internal/serve"
	"github.com/gofiber/fiber/v2"
)

func build() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.DisableStartupMessage = true
	}
	return fiber.New(cfg)
}

func main() {
	app := build()
	serve.Register(app)
	log.Fatal(serve.Run(app, ":3000"))
}
`,
	})

	_, notes := shimMigrate(t, dir)

	serve := appRead(t, dir, "internal/serve/serve.go")
	assert.Contains(t, serve, "return app.Listen(addr, fibermigrate.ListenConfig(app))")
	assert.Contains(t, serve, `return app.Listen(addr, fibermigrate.ListenConfig(app, fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"}))`)
	assert.Contains(t, serve, `"example.com/app/internal/fibermigrate"`)
	assert.Contains(t, appRead(t, dir, "main.go"), "return fibermigrate.NewApp(cfg, cfgListen)")
	assert.Len(t, notes, 1)
}

// The packages other modules can import export apps that start without the settings
// of the registry in another module's code, and the note says which.
func Test_MigrateConfigListenerFields_Module_ExportedAPI(t *testing.T) {
	t.Parallel()

	files := func(dir string) map[string]string {
		return map[string]string{
			dir + "/server.go": "package server\n\n" + moduleBuild + `
// Default is the app of the program.
var Default = New()

// Server keeps an app.
type Server struct{ App *fiber.App }

// NewServer makes a server.
func NewServer() *Server { return &Server{App: New()} }
`,
			"cmd/api/main.go": `package main

import (
	"log"

	"example.com/app/` + dir + `"
)

func main() {
	log.Fatal(server.New().Listen(":3000"))
}
`,
		}
	}

	t.Run("a package other modules can import", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, files("pkg/server"))

		_, notes := shimMigrate(t, dir)

		require.Len(t, notes, 1)
		msg := notes[0].Message
		assert.Contains(t, msg, "example.com/app/pkg/server.Default")
		assert.Contains(t, msg, "example.com/app/pkg/server.New")
		assert.Contains(t, msg, "example.com/app/pkg/server.Server.App")
		assert.Contains(t, msg, "from a package other modules can import")
		assert.Contains(t, msg, "pass them in the fiber.ListenConfig of those calls")
		assert.Contains(t, appRead(t, dir, "cmd/api/main.go"), `fibermigrate.Listen(server.New(), ":3000")`, "the calls of this module are exact")
	})

	t.Run("a package under internal", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, files("internal/server"))

		_, notes := shimMigrate(t, dir)

		require.Len(t, notes, 1)
		assert.NotContains(t, notes[0].Message, "other modules can import")
	})

	t.Run("a main package exports nothing", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, map[string]string{
			"main.go": "package main\n\n" + moduleBuild + `
func main() {
	New().Listen(":3000")
}
`,
		})

		_, notes := shimMigrate(t, dir)

		require.Len(t, notes, 1)
		assert.NotContains(t, notes[0].Message, "other modules can import")
	})

	t.Run("more names than the note shows", func(t *testing.T) {
		t.Parallel()

		parts := []string{"package server\n\n" + moduleBuild}
		for _, name := range []string{"A", "B", "C", "D", "E"} {
			parts = append(parts, "\n// "+name+" makes an app.\nfunc "+name+"() *fiber.App { return New() }\n")
		}
		dir := appProject(t, map[string]string{
			"server/server.go": strings.Join(parts, ""),
			"cmd/api/main.go":  "package main\n\nimport \"example.com/app/server\"\n\nfunc main() { server.New().Listen(\":3000\") }\n",
		})

		_, notes := shimMigrate(t, dir)

		require.Len(t, notes, 1)
		assert.Contains(t, notes[0].Message, "and 3 more export")
	})
}

// Files that start an app with a TLS method and do not import fiber get the
// import of the config they need.
func Test_MigrateConfigListenerFields_Module_TLSInAFileWithoutFiber(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/server/server.go": "package server\n\n" + moduleBuild,
		"cmd/api/main.go": `package main

import "example.com/app/internal/server"

func main() {
	app := server.New()
	_ = app.ListenTLS(":8443", "cert.pem", "key.pem")
	_ = server.New().ListenMutualTLS(":8444", "cert.pem", "key.pem", "ca.pem")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	main := appRead(t, dir, "cmd/api/main.go")
	assert.Contains(t, main, `"github.com/gofiber/fiber/v2"`)
	assert.Contains(t, main, `"example.com/app/internal/fibermigrate"`)
	assert.Contains(t, main, `_ = app.Listen(":8443", fibermigrate.ListenConfig(app, fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"}))`)
	assert.Contains(t, main, `_ = fibermigrate.Listen(server.New(), ":8444", fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem", CertClientFile: "ca.pem"})`)
	assert.Len(t, notes, 1)
}

// The tests of the package, and the external ones, create and start apps like
// the rest of the program; the registry is a package they all import.
func Test_MigrateConfigListenerFields_Module_ExternalTestPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"app_test.go": `package main_test

import (
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func newApp() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.Network = "tcp4"
	}
	return fiber.New(cfg)
}

func TestServe(t *testing.T) {
	app := newApp()
	go app.Listen("127.0.0.1:0")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	test := appRead(t, dir, "app_test.go")
	assert.Contains(t, test, "return fibermigrate.NewApp(cfg, cfgListen)")
	assert.Contains(t, test, `go app.Listen("127.0.0.1:0", fibermigrate.ListenConfig(app))`)
	assert.Contains(t, test, `"example.com/app/internal/fibermigrate"`)
	assert.FileExists(t, filepath.Join(dir, shimFile))
	assert.Len(t, notes, 1)
}

// Every module has a registry of its own. A call of one module cannot be asked for
// the settings of an app of another.
func Test_MigrateConfigListenerFields_Module_SeveralModules(t *testing.T) {
	t.Parallel()

	t.Run("each module gets its own registry", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, map[string]string{
			"go.mod":      "module example.com/one\n\ngo 1.25\n",
			"main.go":     "package main\n\n" + moduleBuild + "\nfunc main() { New().Listen(\":3000\") }\n",
			"two/go.mod":  "module example.com/two\n\ngo 1.25\n",
			"two/main.go": "package main\n\n" + moduleBuild + "\nfunc main() { New().Listen(\":4000\") }\n",
		})

		_, notes := shimMigrate(t, dir)

		assert.FileExists(t, filepath.Join(dir, shimFile))
		assert.FileExists(t, filepath.Join(dir, "two", shimFile))
		assert.Contains(t, appRead(t, dir, "main.go"), `"example.com/one/internal/fibermigrate"`)
		assert.Contains(t, appRead(t, dir, "two/main.go"), `"example.com/two/internal/fibermigrate"`)
		assert.Len(t, notes, 2, "one note for each module")
	})

	t.Run("the app is started by another module", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, map[string]string{
			"go.mod":     "module example.com/one\n\ngo 1.25\n",
			"lib.go":     "package one\n\n" + strings.Replace(moduleBuild, "cfg.DisableStartupMessage = os.Getenv(\"QUIET\") == \"1\"", "if os.Getenv(\"QUIET\") == \"1\" {\n\t\tcfg.DisableStartupMessage = true\n\t}", 1),
			"cmd/go.mod": "module example.com/two\n\ngo 1.25\n",
			"cmd/main.go": `package main

import "example.com/one"

func main() {
	one.New().Listen(":3000")
}
`,
		})
		before := projectFiles(t, dir)

		appRun(t, dir, v3.MigrateConfigListenerFields)

		assert.Equal(t, before, projectFiles(t, dir))
		notes := internal.DrainNotes(dir)
		require.Len(t, notes, 2, "one for each setting")
		for _, n := range notes {
			assert.Contains(t, n.Message, "the app is created in lib.go and started in cmd/main.go, which belong to different Go modules")
			assert.Contains(t, n.Message, "two of them cannot exchange values")
		}
	})
}

// A method that starts an app, used as a value, has the type it had in v2 when it
// goes through the registry.
func Test_MigrateConfigListenerFields_Module_MethodValues(t *testing.T) {
	t.Parallel()

	t.Run("Listen and Listener", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, map[string]string{
			"internal/server/server.go": "package server\n\n" + moduleBuild,
			"cmd/api/main.go": `package main

import (
	"net"

	"example.com/app/internal/server"
)

func run(start func(string) error, serve func(net.Listener) error) {}

func main() {
	app := server.New()
	start := app.Listen
	run(app.Listen, server.New().Listener)
	_ = start
}
`,
		})

		_, notes := shimMigrate(t, dir)

		main := appRead(t, dir, "cmd/api/main.go")
		assert.Contains(t, main, "start := fibermigrate.ListenFunc(app)")
		assert.Contains(t, main, "run(fibermigrate.ListenFunc(app), fibermigrate.ListenerFunc(server.New()))")
		assert.Len(t, notes, 1)
	})

	t.Run("a TLS method has no function in the registry", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, map[string]string{
			"main.go": "package main\n\n" + moduleBuild + `
func main() {
	app := New()
	start := app.ListenTLS
	_ = start
}
`,
		})
		before := projectFiles(t, dir)

		appRun(t, dir, v3.MigrateConfigListenerFields)

		assert.Equal(t, before, projectFiles(t, dir))
		notes := internal.DrainNotes(dir)
		var found bool
		for _, n := range notes {
			found = found || strings.Contains(n.Message, "uses the method ListenTLS that starts an app as a value")
		}
		assert.True(t, found, "%v", shimNotes(notes))
	})
}

// The name of the import is free in the files that use the registry.
func Test_MigrateConfigListenerFields_Module_NameOfTheImport(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"main.go": "package main\n\n" + moduleBuild + `
func main() {
	fibermigrate := 1
	_ = fibermigrate
	New().Listen(":3000")
}
`,
	})

	shimMigrate(t, dir)

	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, `fibermigratemw "example.com/app/internal/fibermigrate"`)
	assert.Contains(t, main, `fibermigratemw.Listen(New(), ":3000")`)
	assert.Contains(t, main, "fibermigratemw.NewApp(cfg, cfgListen)")
}

// Apps that are held in slices, maps and fields, and taken out of them by index, by
// range or by a type switch, are started with the settings they were made with.
func Test_MigrateConfigListenerFields_Module_AppsInCollections(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"internal/server/server.go": "package server\n\n" + moduleBuild,
		"cmd/api/main.go": `package main

import (
	"example.com/app/internal/server"
	"github.com/gofiber/fiber/v2"
)

type Fleet struct {
	apps   []*fiber.App
	byName map[string]*fiber.App
}

func (f *Fleet) start() {
	f.apps = append(f.apps, server.New())
	f.byName = map[string]*fiber.App{"api": server.New()}
	for _, app := range f.apps {
		app.Listen(":3000")
	}
	for name, app := range f.byName {
		_ = name
		app.ListenTLS(":4000", "c", "k")
	}
	f.apps[0].Listen(":5000")
	f.byName["api"].Listen(":6000")
}

func serveAny(v any) {
	switch app := v.(type) {
	case *fiber.App:
		app.Listen(":7000")
	}
}

func handler(c *fiber.Ctx) error {
	return c.App().Listen(":8000")
}
`,
	})

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "cmd/api/main.go")
	assert.Contains(t, content, `app.Listen(":3000", fibermigrate.ListenConfig(app))`)
	assert.Contains(t, content, `app.Listen(":4000", fibermigrate.ListenConfig(app, fiber.ListenConfig{CertFile: "c", CertKeyFile: "k"}))`)
	assert.Contains(t, content, `f.apps[0].Listen(":5000", fibermigrate.ListenConfig(f.apps[0]))`)
	assert.Contains(t, content, `f.byName["api"].Listen(":6000", fibermigrate.ListenConfig(f.byName["api"]))`)
	assert.Contains(t, content, `app.Listen(":7000", fibermigrate.ListenConfig(app))`)
	assert.Contains(t, content, `fibermigrate.Listen(c.App(), ":8000")`, "the app of the context is an expression, which is evaluated once")
	assert.NotContains(t, content, "ListenTLS")
	require.Len(t, notes, 1)
	assert.Equal(t, "internal/server/server.go", filepath.ToSlash(notes[0].File))

	// nothing is left to do for a second run
	out := appRun(t, dir, v3.MigrateConfigListenerFields)
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}
