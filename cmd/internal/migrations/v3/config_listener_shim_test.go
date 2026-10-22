package v3_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// shimFile and shimCallsFile are the files the migration adds to a package whose
// apps get their listener settings away from the call that starts them.
const (
	shimFile      = "fiber_migrate_listen.go"
	shimCallsFile = "fiber_migrate_listen_calls.go"
)

// shimMigrate runs the migration and checks what every run of it must: a second
// run changes nothing, prints nothing and has no note that the first did not have.
func shimMigrate(t *testing.T, dir string) (out string, notes []internal.Note) {
	t.Helper()

	out = appRun(t, dir, v3.MigrateConfigListenerFields)
	notes = internal.DrainNotes(dir)

	before := projectFiles(t, dir)
	again := appRun(t, dir, v3.MigrateConfigListenerFields)
	assert.Empty(t, again, "a second run prints nothing")
	assert.Equal(t, before, projectFiles(t, dir), "a second run changes nothing")
	for _, n := range internal.DrainNotes(dir) {
		assert.Contains(t, shimNotes(notes), n.String(), "a second run has nothing new to say")
	}
	return out, notes
}

// projectFiles returns the Go files of the project by path.
func projectFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	require.NoError(t, filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		b, readErr := os.ReadFile(path) // #nosec G304 -- test file
		require.NoError(t, readErr)
		rel, relErr := filepath.Rel(dir, path)
		require.NoError(t, relErr)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	}))
	return files
}

func shimNotes(notes []internal.Note) []string {
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = n.String()
	}
	return out
}

// A config that is completed away from the app it creates keeps its settings in a
// fiber.ListenConfig next to it, and the app takes them to the call that starts it.
func Test_MigrateConfigListenerFields_Shim(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func buildApp() *fiber.App {
	// config assigned field by field after the literal
	cfg := fiber.Config{AppName: "svc"}
	if os.Getenv("ENV") == "prod" {
		cfg.Prefork = true
		cfg.DisableStartupMessage = true
	}
	cfg.EnablePrintRoutes = os.Getenv("ROUTES") != ""
	cfg.Network = "tcp4"
	return fiber.New(cfg)
}

func main() {
	app := buildApp()
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	log.Fatal(app.Listen(":3000"))
}
`)

	out, notes := shimMigrate(t, dir)

	assert.Equal(t, `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func buildApp() *fiber.App {
	// config assigned field by field after the literal
	cfg := fiber.Config{AppName: "svc"}
	cfgListen := fiber.ListenConfig{}
	if os.Getenv("ENV") == "prod" {
		cfgListen.EnablePrefork = true
		cfgListen.DisableStartupMessage = true
	}
	cfgListen.EnablePrintRoutes = os.Getenv("ROUTES") != ""
	cfgListen.ListenerNetwork = "tcp4"
	return fiberMigrateNew(cfg, cfgListen)
}

func main() {
	app := buildApp()
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	log.Fatal(app.Listen(":3000", fiberMigrateListenConfig(app)))
}
`, appRead(t, dir, "main.go"))

	assert.Contains(t, out, "Migrating listener related config fields")

	// the shim: the package name is the package's, the file says what it is for
	shim := appRead(t, dir, shimFile)
	assert.Contains(t, shim, "\npackage main\n")
	assert.Contains(t, shim, `This file was added by "fiber migrate"`)
	assert.Contains(t, shim, `"github.com/gofiber/fiber/v2"`)
	assert.NotContains(t, shim, "{{pkg}}")
	assert.NoFileExists(t, filepath.Join(dir, shimCallsFile), "nothing starts an app through an expression")

	// one note for the package, at the first app that uses the shim
	require.Len(t, notes, 1)
	assert.Equal(t, "main.go", notes[0].File)
	assert.Equal(t, 19, notes[0].Line)
	assert.Contains(t, notes[0].Message, shimFile)
	assert.Contains(t, notes[0].Message, "fiber.ListenConfig to Listen yourself")
}

func Test_MigrateConfigListenerFields_Shim_AliasedImportAndTLS(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"os"

	f "github.com/gofiber/fiber/v2"
)

func build() *f.App {
	cfg := f.Config{
		AppName:           "svc", // the name
		EnablePrintRoutes: os.Getenv("ROUTES") != "",
	}
	if os.Getenv("ENV") == "prod" {
		cfg.Prefork = true
	}
	return f.New(cfg)
}

func main() {
	app := build()
	log.Fatal(app.ListenTLS(":8443", "cert.pem", "key.pem"))
}
`)

	_, notes := shimMigrate(t, dir)

	assert.Equal(t, `package main

import (
	"log"
	"os"

	f "github.com/gofiber/fiber/v2"
)

func build() *f.App {
	cfg := f.Config{
		AppName: "svc", // the name
	}
	cfgListen := f.ListenConfig{EnablePrintRoutes: os.Getenv("ROUTES") != ""}
	if os.Getenv("ENV") == "prod" {
		cfgListen.EnablePrefork = true
	}
	return fiberMigrateNew(cfg, cfgListen)
}

func main() {
	app := build()
	log.Fatal(app.Listen(":8443", fiberMigrateListenConfig(app, f.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"})))
}
`, appRead(t, dir, "main.go"))
	assert.Len(t, notes, 1)
}

// A literal handed to fiber.New whose values are only known to the function that
// creates the app: they become the second argument of fiberMigrateNew.
func Test_MigrateConfigListenerFields_Shim_LiteralWithLocalValues(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

func newApp(prefork bool, network string) *fiber.App {
	return fiber.New(fiber.Config{
		AppName: "x",
		Prefork: prefork,
		Network: network,
	})
}

func newQuiet(quiet bool) *fiber.App {
	return fiber.New(fiber.Config{DisableStartupMessage: quiet, AppName: "y"})
}

func main() {
	app := newApp(true, "tcp4")
	go newQuiet(true).Listen(":3001")
	log.Fatal(app.Listen(":3000"))
}
`)

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `return fiberMigrateNew(fiber.Config{
		AppName: "x",
	}, fiber.ListenConfig{EnablePrefork: prefork, ListenerNetwork: network})`)
	assert.Contains(t, content, `return fiberMigrateNew(fiber.Config{AppName: "y"}, fiber.ListenConfig{DisableStartupMessage: quiet})`)
	assert.Contains(t, content, `log.Fatal(app.Listen(":3000", fiberMigrateListenConfig(app)))`)
	// the receiver of this call is a call: it goes to a function that takes the app
	assert.Contains(t, content, `go fiberMigrateListen(newQuiet(true), ":3001")`)
	assert.FileExists(t, filepath.Join(dir, shimCallsFile))
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, shimCallsFile)
}

func Test_MigrateConfigListenerFields_Shim_PackageLevelConfig(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"config.go": `package main

import "github.com/gofiber/fiber/v2"

var cfg = fiber.Config{AppName: "pkg", EnablePrintRoutes: true} // the config

var plain fiber.Config

func configure(n int) {
	if n > 3 {
		cfg.Prefork = true
	}
	plain.DisableStartupMessage = n > 5
}
`,
		"main.go": `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func main() {
	configure(len(os.Args))
	app := fiber.New(cfg)
	log.Fatal(app.Listen(":3000"))
}

func admin() {
	app := fiber.New(plain)
	log.Fatal(app.Listen(":3001"))
}
`,
	})

	_, notes := shimMigrate(t, dir)

	config := appRead(t, dir, "config.go")
	assert.Contains(t, config, `var cfg = fiber.Config{AppName: "pkg"} // the config
var cfgListen = fiber.ListenConfig{EnablePrintRoutes: true}
`)
	assert.Contains(t, config, "var plain fiber.Config\nvar plainListen = fiber.ListenConfig{}\n")
	assert.Contains(t, config, "cfgListen.EnablePrefork = true")
	assert.Contains(t, config, "plainListen.DisableStartupMessage = n > 5")

	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, "app := fiberMigrateNew(cfg, cfgListen)")
	assert.Contains(t, main, "app := fiberMigrateNew(plain, plainListen)")
	assert.Contains(t, main, `app.Listen(":3000", fiberMigrateListenConfig(app))`)
	assert.Contains(t, main, `app.Listen(":3001", fiberMigrateListenConfig(app))`)
	assert.Len(t, notes, 1, "one note for the package")
}

// Whatever the code does with the config before the app exists, the settings it
// sets are the settings the app has: v2 copied the config when it made the app.
func Test_MigrateConfigListenerFields_Shim_ConditionalAssignments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "the app is started by another function",
			src: `func build() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("ENV") == "prod" {
		cfg.Prefork = true
	}
	return fiber.New(cfg)
}

func main() {
	app := build()
	log.Fatal(app.Listen(":3000"))
}`,
			want: []string{
				"cfg := fiber.Config{}\n\tcfgListen := fiber.ListenConfig{}\n\tif os.Getenv(\"ENV\") == \"prod\" {\n\t\tcfgListen.EnablePrefork = true\n\t}\n\treturn fiberMigrateNew(cfg, cfgListen)",
				`log.Fatal(app.Listen(":3000", fiberMigrateListenConfig(app)))`,
			},
		},
		{
			name: "the assignment follows fiber.New, which copied the config",
			src: `func main() {
	cfg := fiber.Config{}
	app := fiber.New(cfg)
	if os.Getenv("ENV") == "prod" {
		cfg.Prefork = true
	}
	log.Fatal(app.Listen(":3000"))
}`,
			want: []string{
				"cfg := fiber.Config{}\n\tcfgListen := fiber.ListenConfig{}\n\tapp := fiberMigrateNew(cfg, cfgListen)",
				"cfgListen.EnablePrefork = true",
				`app.Listen(":3000", fiberMigrateListenConfig(app))`,
			},
		},
		{
			name: "the assignment is made by a function literal",
			src: `func main() {
	cfg := fiber.Config{}
	enable := func() { cfg.Prefork = true }
	enable()
	app := fiber.New(cfg)
	log.Fatal(app.Listen(":3000"))
}`,
			want: []string{
				"enable := func() { cfgListen.EnablePrefork = true }",
				"app := fiberMigrateNew(cfg, cfgListen)",
			},
		},
		{
			name: "the setting is read from the config",
			src: `func main() {
	cfg := fiber.Config{}
	if os.Getenv("ENV") == "prod" {
		cfg.Prefork = true
	}
	if cfg.Prefork {
		log.Println("prefork")
	}
	cfg.Network += "4"
	app := fiber.New(cfg)
	log.Fatal(app.Listen(":3000"))
}`,
			want: []string{
				"if cfgListen.EnablePrefork {",
				`cfgListen.ListenerNetwork += "4"`,
				"app := fiberMigrateNew(cfg, cfgListen)",
			},
		},
		{
			name: "the app is started in another function",
			src: `func run(app *fiber.App) { log.Fatal(app.Listen(":3000")) }

func main() {
	cfg := fiber.Config{}
	if os.Getenv("ENV") == "prod" {
		cfg.Prefork = true
	}
	run(fiber.New(cfg))
}`,
			want: []string{
				"run(fiberMigrateNew(cfg, cfgListen))",
				`func run(app *fiber.App) { log.Fatal(app.Listen(":3000", fiberMigrateListenConfig(app))) }`,
			},
		},
		{
			name: "a variable without a literal, and two apps from the same config",
			src: `func start(app *fiber.App, addr string) { log.Fatal(app.Listen(addr)) }

func main() {
	var cfg fiber.Config
	if os.Getenv("ENV") == "prod" {
		cfg.EnablePrintRoutes = true
	}
	first, second := fiber.New(cfg), fiber.New(cfg)
	go start(first, ":3000")
	start(second, ":3001")
}`,
			want: []string{
				"var cfg fiber.Config\n\tcfgListen := fiber.ListenConfig{}\n",
				"first, second := fiberMigrateNew(cfg, cfgListen), fiberMigrateNew(cfg, cfgListen)",
				`log.Fatal(app.Listen(addr, fiberMigrateListenConfig(app)))`,
			},
		},
		{
			name: "the name of the listen config is free",
			src: `func start(app *fiber.App) { log.Fatal(app.Listen(":3000")) }

func main() {
	cfgListen := 1
	cfg := fiber.Config{}
	if os.Getenv("ENV") == "prod" {
		cfg.Prefork = cfgListen > 0
	}
	start(fiber.New(cfg))
}`,
			want: []string{
				"cfgListen2 := fiber.ListenConfig{}",
				"cfgListen2.EnablePrefork = cfgListen > 0",
				"start(fiberMigrateNew(cfg, cfgListen2))",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := appSingle(t, `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

`+tt.src+"\n")

			out, notes := shimMigrate(t, dir)

			content := appRead(t, dir, "main.go")
			for _, want := range tt.want {
				assert.Contains(t, content, want)
			}
			assert.NotContains(t, content, "cfg.Prefork")
			assert.NotContains(t, content, "cfg.Network")
			assert.NotContains(t, content, "cfg.EnablePrintRoutes")
			assert.Contains(t, out, "Migrating listener related config fields")
			require.Len(t, notes, 1)
			assert.Contains(t, notes[0].Message, shimFile)
		})
	}
}

// An app started through a method of a type that embeds it or through a field is
// named in the call that asks for its settings; a call is not, it is evaluated once.
func Test_MigrateConfigListenerFields_Shim_Receivers(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"net"
	"os"

	"github.com/gofiber/fiber/v2"
)

type Server struct {
	*fiber.App
	other *fiber.App
}

func build() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.DisableStartupMessage = true
	}
	return fiber.New(cfg)
}

func main() {
	srv := &Server{App: build(), other: build()}

	var ln net.Listener
	go srv.Listen(":3000")
	go srv.other.Listen(":3001")
	go (srv.other).Listener(ln)
	go build().Listener(ln)
	log.Fatal(srv.App.Listen(":3002"))
}
`)

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `go srv.Listen(":3000", fiberMigrateListenConfig(srv.App))`)
	assert.Contains(t, content, `go srv.other.Listen(":3001", fiberMigrateListenConfig(srv.other))`)
	assert.Contains(t, content, `go (srv.other).Listener(ln, fiberMigrateListenConfig((srv.other)))`)
	assert.Contains(t, content, `go fiberMigrateListener(build(), ln)`)
	assert.Contains(t, content, `log.Fatal(srv.App.Listen(":3002", fiberMigrateListenConfig(srv.App)))`)
	assert.Len(t, notes, 1)
}

// Calls that cannot be traced to the apps of the shim ask for the settings too,
// since they may start one; a call that starts an app of known origin does not.
func Test_MigrateConfigListenerFields_Shim_UnknownStarts(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func build() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.DisableStartupMessage = true
	}
	return fiber.New(cfg)
}

func serve(app *fiber.App, addr string) error {
	return app.Listen(addr)
}

func main() {
	other := fiber.New()
	go other.Listen(":4000")
	log.Fatal(serve(build(), ":3000"))
}
`)

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `return app.Listen(addr, fiberMigrateListenConfig(app))`)
	assert.Contains(t, content, `go other.Listen(":4000")`, "an app that comes from another fiber.New is not the shim's")
	assert.Len(t, notes, 1)
}

// An app in an unexported field of a type that only the package builds stays in
// the package, whichever functions of it are exported.
func Test_MigrateConfigListenerFields_Shim_LibraryPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"server.go": `package server

import (
	"os"

	"github.com/gofiber/fiber/v2"
)

type Server struct{ app *fiber.App }

func build() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.DisableStartupMessage = true
	}
	return fiber.New(cfg)
}

func New() *Server { return &Server{app: build()} }

func (s *Server) Run(addr string) error { return s.app.Listen(addr) }
`,
	})

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "server.go")
	assert.Contains(t, content, "return fiberMigrateNew(cfg, cfgListen)")
	assert.Contains(t, content, `return s.app.Listen(addr, fiberMigrateListenConfig(s.app))`)
	assert.Contains(t, appRead(t, dir, shimFile), "\npackage server\n")
	assert.Len(t, notes, 1)
}

// What the shim cannot carry stays as it is, with a note that says why.
func Test_MigrateConfigListenerFields_Shim_LeftAlone(t *testing.T) {
	t.Parallel()

	const header = `import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

`
	// the setting is assigned on line 13
	const build = `func build() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.DisableStartupMessage = true
	}
	return fiber.New(cfg)
}
`

	tests := []struct {
		name  string
		files map[string]string
		// the file and the line of the setting, and what the note says about the shim
		file   string
		reason string
		line   int
	}{
		{
			name: "an exported function returns the app",
			files: map[string]string{
				"go.mod": "module example.com/demo\n",
				"server.go": "package server\n\n" + header + `// New builds the app.
func New() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.DisableStartupMessage = true
	}
	return fiber.New(cfg)
}

var _ = log.Println
`,
			},
			file:   "server.go",
			line:   14,
			reason: "New is exported and returns an app",
		},
		{
			name: "an exported variable holds the app",
			files: map[string]string{
				"go.mod":    "module example.com/demo\n",
				"server.go": "package server\n\n" + header + build + "\nvar App = build()\n\nvar _ = log.Println\n",
			},
			file:   "server.go",
			line:   13,
			reason: "App is an exported variable",
		},
		{
			name: "an exported field holds the app",
			files: map[string]string{
				"go.mod":    "module example.com/demo\n",
				"server.go": "package server\n\n" + header + build + "\ntype Server struct{ App *fiber.App }\n\nfunc New() *Server { return &Server{App: build()} }\n\nvar _ = log.Println\n",
			},
			file:   "server.go",
			line:   13,
			reason: "Server.App is an exported field",
		},
		{
			name: "a call on a value of a type that is not known",
			files: map[string]string{
				"main.go": "package main\n\n" + header + build + `
func main() {
	apps := []*fiber.App{build(), build()}
	for _, app := range apps {
		go app.Listen(":3000")
	}
	log.Println(apps)
}
`,
			},
			file:   "main.go",
			line:   13,
			reason: "the type of its receiver is unknown",
		},
		{
			name: "a method that starts the app is used as a value",
			files: map[string]string{
				"main.go": "package main\n\n" + header + build + `
func main() {
	app := build()
	start := app.Listen
	log.Fatal(start(":3000"))
}
`,
			},
			file:   "main.go",
			line:   13,
			reason: "uses a method that starts an app as a value, at line 20",
		},
		{
			name: "the call sets the same setting itself",
			files: map[string]string{
				"main.go": "package main\n\n" + header + build + `
func main() {
	app := build()
	log.Fatal(app.Listen(":3000", fiber.ListenConfig{DisableStartupMessage: false}))
}
`,
			},
			file:   "main.go",
			line:   13,
			reason: "already sets DisableStartupMessage",
		},
		{
			name: "the app is created in an external test package",
			files: map[string]string{
				"main.go": "package main\n\nfunc main() {}\n",
				"app_test.go": "package main_test\n\n" + header + build + `
func Serve() {
	log.Fatal(build().Listen(":3000"))
}
`,
			},
			file:   "app_test.go",
			line:   13,
			reason: "external test package",
		},
		{
			name: "a file of the project has the name of the shim",
			files: map[string]string{
				"main.go": "package main\n\n" + header + build + `
func main() {
	log.Fatal(build().Listen(":3000"))
}
`,
				shimFile: "package main\n\n// mine\n",
			},
			file:   "main.go",
			line:   13,
			reason: "a file or a name of the project is in the way",
		},
		{
			name: "the project declares a name of the shim",
			files: map[string]string{
				"main.go": "package main\n\n" + header + build + `
func main() {
	log.Fatal(build().Listen(":3000"))
}

func fiberMigrateNew() {}
`,
			},
			file:   "main.go",
			line:   13,
			reason: "a file or a name of the project is in the way",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := appProject(t, tt.files)
			before := projectFiles(t, dir)

			out := appRun(t, dir, v3.MigrateConfigListenerFields)

			assert.Empty(t, out)
			assert.Equal(t, before, projectFiles(t, dir), "nothing moves")
			notes := internal.DrainNotes(dir)
			require.NotEmpty(t, notes)
			var found bool
			for _, n := range notes {
				if n.File == tt.file && strings.Contains(n.Message, "DisableStartupMessage") && strings.Contains(n.Message, tt.reason) {
					found = true
					assert.Equal(t, tt.line, n.Line, n.Message)
				}
			}
			assert.True(t, found, "a note about %q: %v", tt.reason, shimNotes(notes))
		})
	}
}

func Test_MigrateConfigListenerFields_Shim_DotImport(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"os"

	. "github.com/gofiber/fiber/v2"
)

func build() *App {
	cfg := Config{AppName: "dot"}
	if os.Getenv("Q") != "" {
		cfg.DisableStartupMessage = true
	}
	return New(cfg)
}

func main() {
	log.Fatal(build().ListenTLS(":8443", "cert.pem", "key.pem"))
}
`)

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "cfgListen := ListenConfig{}")
	assert.Contains(t, content, "return fiberMigrateNew(cfg, cfgListen)")
	assert.Contains(t, content, `log.Fatal(fiberMigrateListen(build(), ":8443", ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"}))`)
	assert.Len(t, notes, 1)
}

// The shim of an earlier run is reused by the code that needs it later: it is not
// written again, and the calls it holds are not changed.
func Test_MigrateConfigListenerFields_Shim_SecondRunWithNewCode(t *testing.T) {
	t.Parallel()

	const first = `package main

import (
	"log"
	"os"

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
	go app.Listen(":3001")
	log.Fatal(build().Listen(":3000"))
}
`
	dir := appProject(t, map[string]string{"main.go": first})
	appRun(t, dir, v3.MigrateConfigListenerFields)
	internal.DrainNotes(dir)
	shim, calls := appRead(t, dir, shimFile), appRead(t, dir, shimCallsFile)
	migrated := appRead(t, dir, "main.go")
	require.Contains(t, migrated, `go app.Listen(":3001", fiberMigrateListenConfig(app))`)

	// code written later, in the style of v2 again
	require.NoError(t, os.WriteFile(filepath.Join(dir, "admin.go"), []byte(`package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func admin() *fiber.App {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.EnablePrintRoutes = true
	}
	return fiber.New(cfg)
}

func serveAdmin() {
	app := admin()
	log.Fatal(app.Listen(":4000"))
}
`), 0o600))

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Contains(t, out, "Migrating listener related config fields")
	assert.Contains(t, appRead(t, dir, "admin.go"), `log.Fatal(app.Listen(":4000", fiberMigrateListenConfig(app)))`)
	assert.Equal(t, shim, appRead(t, dir, shimFile), "the shim is not written again")
	assert.Equal(t, calls, appRead(t, dir, shimCallsFile))
	assert.Equal(t, migrated, appRead(t, dir, "main.go"), "what the first run did is not done twice")
	assert.Contains(t, appRead(t, dir, "main.go"), `fiberMigrateListen(build(), ":3000")`)
	assert.NotContains(t, appRead(t, dir, shimFile), "fiberMigrateListenConfig(fiberMigrateListenConfig")
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "admin.go", notes[0].File)
}

// The files of the shim are not the project's code: what starts an app in them
// does not ask the registry for settings twice.
func Test_MigrateConfigListenerFields_Shim_FilesAreNotTheProjects(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"main.go": `package main

import (
	"log"
	"os"

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
	log.Fatal(build().Listen(":3000"))
}
`,
	})
	appRun(t, dir, v3.MigrateConfigListenerFields)
	internal.DrainNotes(dir)
	calls := appRead(t, dir, shimCallsFile)
	assert.Contains(t, calls, "return app.Listen(addr, fiberMigrateListenConfig(app, lc...))")

	// a package that has the files and no source of its own is left alone
	appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Equal(t, calls, appRead(t, dir, shimCallsFile))
	assert.Empty(t, internal.DrainNotes(dir))
}

// The edits of a literal that cannot be rebuilt stay out: nothing is changed.
func Test_MigrateConfigListenerFields_Shim_LiteralThatCannotBeEdited(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

func newApp(prefork bool) *fiber.App {
	return fiber.New(fiber.Config{AppName: "x", /* the setting */ Prefork: prefork})
}

func main() {
	log.Fatal(newApp(true).Listen(":3000"))
}
`)
	before := appRead(t, dir, "main.go")

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Empty(t, out)
	assert.Equal(t, before, appRead(t, dir, "main.go"))
	assert.NoFileExists(t, filepath.Join(dir, shimFile))
	notes := internal.DrainNotes(dir)
	require.NotEmpty(t, notes)
	assert.Contains(t, notes[0].Message, "laid out in a way that cannot be edited safely")
}

// A call that starts an app which another edit makes, as fiber.New(cfg).Listen(addr)
// does, takes the edited expression for its receiver.
func Test_MigrateConfigListenerFields_Shim_AppMadeWhereItIsStarted(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

var cfg = fiber.Config{AppName: "x"}

func init() {
	cfg.DisableStartupMessage = len(os.Args) > 1
}

func main() {
	go fiber.New(cfg).ListenTLS(":8443", "cert.pem", "key.pem")
	log.Fatal(fiber.New(cfg).Listen(":3000"))
}
`)

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `go fiberMigrateListen(fiberMigrateNew(cfg, cfgListen), ":8443", fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"})`)
	assert.Contains(t, content, `log.Fatal(fiberMigrateListen(fiberMigrateNew(cfg, cfgListen), ":3000"))`)
	assert.Len(t, notes, 1)
}

// A test of the package that makes an app with values of its own and starts it
// through a helper gets the shim too, which is a file of the package.
func Test_MigrateConfigListenerFields_Shim_TestFiles(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"app_test.go": `package main

import (
	"net"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func newTestApp(network string) *fiber.App {
	return fiber.New(fiber.Config{AppName: "test", Network: network})
}

func serve(app *fiber.App, ln net.Listener) {
	go app.Listener(ln)
}

func TestServe(t *testing.T) {
	var ln net.Listener
	serve(newTestApp("tcp4"), ln)
}
`,
	})

	_, notes := shimMigrate(t, dir)

	content := appRead(t, dir, "app_test.go")
	assert.Contains(t, content, `return fiberMigrateNew(fiber.Config{AppName: "test"}, fiber.ListenConfig{ListenerNetwork: network})`)
	assert.Contains(t, content, `go app.Listener(ln, fiberMigrateListenConfig(app))`)
	assert.Contains(t, appRead(t, dir, shimFile), "\npackage main\n")
	require.Len(t, notes, 1)
	assert.Equal(t, "app_test.go", notes[0].File)
}

// A file that starts an app without importing fiber gets the import when the
// call needs the fiber.ListenConfig of a converted TLS call.
func Test_MigrateConfigListenerFields_Shim_AddsTheImportItNeeds(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"app.go": `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
)

type Server struct{ app *fiber.App }

func NewServer() *Server {
	cfg := fiber.Config{}
	if os.Getenv("Q") != "" {
		cfg.DisableStartupMessage = true
	}
	return &Server{app: fiber.New(cfg)}
}
`,
		"main.go": `package main

import "log"

func (s *Server) run() error {
	return s.app.ListenTLS(":8443", "cert.pem", "key.pem")
}

func main() {
	log.Fatal(NewServer().run())
}
`,
	})

	_, notes := shimMigrate(t, dir)

	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, `"github.com/gofiber/fiber/v2"`)
	assert.Contains(t, main, `return s.app.Listen(":8443", fiberMigrateListenConfig(s.app, fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"}))`)
	assert.Len(t, notes, 1)
}

// An app that is handed to a function of another package which starts apps would
// start without its settings, so they stay where they are; a function that only
// registers routes does not start anything.
func Test_MigrateConfigListenerFields_Shim_AppHandedToAnotherPackage(t *testing.T) {
	t.Parallel()

	files := func(callee string) map[string]string {
		return map[string]string{
			"go.mod": "module example.com/demo\n",
			"serve/serve.go": `package serve

import "github.com/gofiber/fiber/v2"

// Run starts the app.
func Run(app *fiber.App, addr string) error {
	return app.Listen(addr)
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

	"example.com/demo/serve"
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
	log.Fatal(` + callee + `)
}
`,
		}
	}

	t.Run("the other package starts it", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, files(`serve.Run(app, ":3000")`))
		before := projectFiles(t, dir)

		appRun(t, dir, v3.MigrateConfigListenerFields)

		assert.Equal(t, before, projectFiles(t, dir))
		notes := internal.DrainNotes(dir)
		require.Len(t, notes, 1)
		assert.Contains(t, notes[0].Message, "hands an app to Run, which starts apps in another package")
	})

	t.Run("the other package hands it on to a function that starts it", func(t *testing.T) {
		t.Parallel()

		fs := files(`serve.Setup(app, ":3000")`)
		fs["serve/serve.go"] += `
// Setup prepares the app and runs it.
func Setup(app *fiber.App, addr string) error {
	Register(app)
	return start(app, addr)
}

func start(app *fiber.App, addr string) error { return app.Listen(addr) }
`
		dir := appProject(t, fs)
		before := projectFiles(t, dir)

		appRun(t, dir, v3.MigrateConfigListenerFields)

		assert.Equal(t, before, projectFiles(t, dir))
		notes := internal.DrainNotes(dir)
		require.Len(t, notes, 1)
		assert.Contains(t, notes[0].Message, "hands an app to Setup")
	})

	t.Run("the other package does not", func(t *testing.T) {
		t.Parallel()

		dir := appProject(t, files(`app.Listen(":3000")`))
		src := appRead(t, dir, "main.go")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(strings.Replace(src, "app := build()\n", "app := build()\n\tserve.Register(app)\n", 1)), 0o600))

		_, notes := shimMigrate(t, dir)

		main := appRead(t, dir, "main.go")
		assert.Contains(t, main, "serve.Register(app)")
		assert.Contains(t, main, `log.Fatal(app.Listen(":3000", fiberMigrateListenConfig(app)))`)
		assert.Len(t, notes, 1)
	})
}
