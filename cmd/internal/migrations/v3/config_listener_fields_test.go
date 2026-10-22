package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateConfigListenerFields(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New(fiber.Config{
		Prefork:               true,
		Network:               "tcp",
		DisableStartupMessage: true,
		EnablePrintRoutes:     true,
	})
	app.Listen(":3000")
}
`)

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "fiber.New(fiber.Config{})")
	assert.Contains(t, content, `app.Listen(":3000", fiber.ListenConfig{EnablePrefork: true, ListenerNetwork: "tcp", DisableStartupMessage: true, EnablePrintRoutes: true})`)
	assert.Contains(t, out, "Migrating listener related config fields")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateConfigListenerFields_PreservesTheOtherFields(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

var prod = true

func main() {
	app := fiber.New(fiber.Config{
		// the name of the app
		AppName:     "demo",
		Prefork:     prod, // needs SO_REUSEPORT
		ServerHeader: "x",
	})
	app.Listen(":3000")
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "\t\t// the name of the app\n\t\tAppName:      \"demo\",\n\t\tServerHeader: \"x\",\n\t})")
	assert.Contains(t, content, `app.Listen(":3000", fiber.ListenConfig{EnablePrefork: prod})`)
}

func Test_MigrateConfigListenerFields_ConfigAndListenInDifferentFiles(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"app.go": `package main

import "github.com/gofiber/fiber/v2"

func newApp() *fiber.App {
	return fiber.New(fiber.Config{
		Prefork:               true,
		DisableStartupMessage: true,
	})
}

type Server struct {
	app *fiber.App
}

func NewServer() *Server {
	return &Server{app: fiber.New(fiber.Config{EnablePrintRoutes: true})}
}

func (s *Server) Run(addr string) error {
	return s.app.Listen(addr)
}
`,
		"main.go": `package main

func main() {
	app := newApp()
	go NewServer().Run(":4000")
	app.Listen(":3000")
}
`,
	})

	appRun(t, dir, v3.MigrateConfigListenerFields)

	app := appRead(t, dir, "app.go")
	assert.NotContains(t, app, "Prefork")
	assert.NotContains(t, app, "EnablePrintRoutes: true})}")
	assert.Contains(t, app, `s.app.Listen(addr, fiber.ListenConfig{EnablePrintRoutes: true})`)

	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, `app.Listen(":3000", fiber.ListenConfig{EnablePrefork: true, DisableStartupMessage: true})`)
	assert.Contains(t, main, `"github.com/gofiber/fiber/v2"`)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateConfigListenerFields_TwoApps(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

func main() {
	api := fiber.New(fiber.Config{Prefork: true, EnablePrintRoutes: true})
	admin := fiber.New(fiber.Config{DisableStartupMessage: true})
	plain := fiber.New()

	go func() { log.Fatal(admin.Listen("127.0.0.1:4000")) }()
	go plain.Listen(":5000")
	log.Fatal(api.Listen(":3000"))
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `admin.Listen("127.0.0.1:4000", fiber.ListenConfig{DisableStartupMessage: true})`)
	assert.Contains(t, content, `api.Listen(":3000", fiber.ListenConfig{EnablePrefork: true, EnablePrintRoutes: true})`)
	assert.Contains(t, content, `go plain.Listen(":5000")`)
	assert.Contains(t, content, `api := fiber.New(fiber.Config{})`)
	assert.Contains(t, content, `admin := fiber.New(fiber.Config{})`)
}

func Test_MigrateConfigListenerFields_ListenerAndTLS(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"net"

	"github.com/gofiber/fiber/v2"
)

func main() {
	a := fiber.New(fiber.Config{DisableStartupMessage: true, EnablePrintRoutes: true})
	ln, err := net.Listen("tcp", "127.0.0.1:3000")
	if err != nil {
		log.Fatal(err)
	}
	go a.Listener(ln)

	b := fiber.New(fiber.Config{Prefork: true, DisableStartupMessage: true})
	log.Fatal(b.ListenTLS(":443", "cert.pem", "key.pem"))
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `go a.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true, EnablePrintRoutes: true})`)
	assert.Contains(t, content, `b.Listen(":443", fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem", EnablePrefork: true, DisableStartupMessage: true})`)
	assert.NotContains(t, content, "ListenTLS")
}

func Test_MigrateConfigListenerFields_ValuesWithCalls(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func main() {
	admin := fiber.New(fiber.Config{
		Network:               fmt.Sprintf("%s%d", "tcp", 4),
		DisableStartupMessage: strings.Contains(strings.Join(os.Args, ","), "-q"),
	})
	log.Fatal(admin.Listen(":4000"))
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `admin.Listen(":4000", fiber.ListenConfig{ListenerNetwork: fmt.Sprintf("%s%d", "tcp", 4), DisableStartupMessage: strings.Contains(strings.Join(os.Args, ","), "-q")})`)
	assert.Contains(t, content, "admin := fiber.New(fiber.Config{})")
}

func Test_MigrateConfigListenerFields_Assignments(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func main() {
	cfg := fiber.Config{AppName: "svc"}
	cfg.Prefork = true
	cfg.DisableStartupMessage = os.Getenv("QUIET") != ""
	cfg.Network = "tcp4"
	app := fiber.New(cfg)
	log.Fatal(app.Listen(":3000"))
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.NotContains(t, content, "cfg.Prefork")
	assert.NotContains(t, content, "cfg.Network")
	assert.Contains(t, content, `app.Listen(":3000", fiber.ListenConfig{EnablePrefork: true, ListenerNetwork: "tcp4", DisableStartupMessage: os.Getenv("QUIET") != ""})`)
	assert.Contains(t, content, `cfg := fiber.Config{AppName: "svc"}`)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateConfigListenerFields_ConditionalAssignmentStays(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func main() {
	cfg := fiber.Config{DisableStartupMessage: true}
	if os.Getenv("ENV") == "prod" {
		cfg.Prefork = true
	}
	app := fiber.New(cfg)
	log.Fatal(app.Listen(":3000"))
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "cfg.Prefork = true")
	assert.Contains(t, content, `app.Listen(":3000", fiber.ListenConfig{DisableStartupMessage: true})`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, 13, notes[0].Line)
	assert.Contains(t, notes[0].Message, "EnablePrefork")
	assert.Contains(t, notes[0].Message, "conditionally")
}

func Test_MigrateConfigListenerFields_UnlinkedAppKeepsItsFields(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

func run(app *fiber.App) error {
	return app.Listen(":3000")
}

func main() {
	app := fiber.New(fiber.Config{
		Prefork:   true,
		AppName: "x",
	})
	log.Fatal(run(app))
}
`)
	before := appRead(t, dir, "main.go")

	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Equal(t, before, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, 15, notes[0].Line)
	assert.Contains(t, notes[0].Message, "EnablePrefork")
	assert.Contains(t, notes[0].Message, "no Listen call")
}

func Test_MigrateConfigListenerFields_OtherTypesAreNotApps(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

type tunnel struct{}

func (tunnel) Listen(addr string) error { return nil }

func main() {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var t tunnel
	go func() { _ = t.Listen("localhost:9999") }()
	_ = app.Listen(":3000")
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `t.Listen("localhost:9999")`)
	assert.NotContains(t, content, `t.Listen("localhost:9999", `)
	assert.Contains(t, content, `app.Listen(":3000", fiber.ListenConfig{DisableStartupMessage: true})`)
}

func Test_MigrateConfigListenerFields_ExistingListenConfig(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New(fiber.Config{Prefork: true, DisableStartupMessage: true})
	app.Listen(":3000", fiber.ListenConfig{ShutdownTimeout: 5})
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `app.Listen(":3000", fiber.ListenConfig{ShutdownTimeout: 5, EnablePrefork: true, DisableStartupMessage: true})`)
}

func Test_MigrateConfigListenerFields_LocalValuesStayInTheirFunction(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"app.go": `package main

import "github.com/gofiber/fiber/v2"

func newApp(prefork bool) *fiber.App {
	return fiber.New(fiber.Config{Prefork: prefork})
}
`,
		"main.go": `package main

func main() {
	app := newApp(true)
	app.Listen(":3000")
}
`,
	})
	before := appRead(t, dir, "app.go")

	appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Equal(t, before, appRead(t, dir, "app.go"))
	assert.Equal(t, "package main\n\nfunc main() {\n\tapp := newApp(true)\n\tapp.Listen(\":3000\")\n}\n", appRead(t, dir, "main.go"))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "app.go", notes[0].File)
	assert.Contains(t, notes[0].Message, "not in scope")
}

func Test_MigrateConfigListenerFields_OtherPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"internal/server/server.go": `package server

import "github.com/gofiber/fiber/v2"

func New() *fiber.App {
	return fiber.New(fiber.Config{
		Prefork:   true,
		AppName:   "demo",
		Network:   fiber.NetworkTCP4,
	})
}
`,
		"cmd/main.go": `package main

import (
	"log"

	"example.com/demo/internal/server"
)

func main() {
	log.Fatal(server.New().Listen(":3000"))
}
`,
	})

	appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.NotContains(t, appRead(t, dir, "internal/server/server.go"), "Prefork")
	main := appRead(t, dir, "cmd/main.go")
	assert.Contains(t, main, `server.New().Listen(":3000", fiber.ListenConfig{EnablePrefork: true, ListenerNetwork: fiber.NetworkTCP4})`)
	assert.Contains(t, main, `"github.com/gofiber/fiber/v2"`)
}

func Test_MigrateConfigListenerFields_SharedConfig(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	cfg := fiber.Config{Prefork: true}
	a := fiber.New(cfg)
	b := fiber.New(cfg)
	go a.Listen(":3000")
	b.Listen(":3001")
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `go a.Listen(":3000", fiber.ListenConfig{EnablePrefork: true})`)
	assert.Contains(t, content, `b.Listen(":3001", fiber.ListenConfig{EnablePrefork: true})`)
	assert.Contains(t, content, "cfg := fiber.Config{}")
}

func Test_MigrateConfigListenerFields_AliasedImport(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import f "github.com/gofiber/fiber/v2"

func main() {
	app := f.New(f.Config{Prefork: true})
	app.Listen(":3000")
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Contains(t, appRead(t, dir, "main.go"), `app.Listen(":3000", f.ListenConfig{EnablePrefork: true})`)
}

func Test_MigrateConfigListenerFields_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New(fiber.Config{Prefork: true, Network: "tcp"})
	app.Listen(":3000")
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateConfigListenerFields_BrokenFileDoesNotStopTheRest(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"a_broken.go": "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ = fiber.New\n\nfunc broken( {\n",
		"b_good.go": `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New(fiber.Config{Prefork: true})
	app.Listen(":3000")
}
`,
	})

	appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Contains(t, appRead(t, dir, "b_good.go"), `app.Listen(":3000", fiber.ListenConfig{EnablePrefork: true})`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "a_broken.go", notes[0].File)
}

func Test_MigrateConfigListenerFields_TestAppsThatNeverListen(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"app_test.go": `package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestQuiet(t *testing.T) {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		EnablePrintRoutes:     true,
		Prefork:               true,
		AppName:               "test",
	})
	_, _ = app.Test(httptest.NewRequest("GET", "/", nil))
}
`,
		"app.go": `package main

import "github.com/gofiber/fiber/v2"

func newApp() *fiber.App {
	return fiber.New(fiber.Config{DisableStartupMessage: true})
}
`,
	})

	appRun(t, dir, v3.MigrateConfigListenerFields)

	// printing is all those two settings change, and the app is never started
	test := appRead(t, dir, "app_test.go")
	assert.NotContains(t, test, "DisableStartupMessage")
	assert.NotContains(t, test, "EnablePrintRoutes")
	assert.Contains(t, test, "Prefork:")
	assert.Contains(t, test, `AppName: "test"`)
	// an app of the program itself may be started from anywhere
	assert.Contains(t, appRead(t, dir, "app.go"), "DisableStartupMessage: true")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	assert.Equal(t, "app.go", notes[0].File)
	assert.Equal(t, "app_test.go", notes[1].File)
	assert.Contains(t, notes[1].Message, "EnablePrefork")
}

func Test_MigrateConfigListenerFields_ValuesWithSideEffectsStay(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"flag"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New(fiber.Config{Prefork: *flag.Bool("prefork", false, "prefork"), DisableStartupMessage: true})
	flag.Parse()
	app.Listen(":3000")
}
`)

	appRun(t, dir, v3.MigrateConfigListenerFields)

	// the setting that can move does, the one that would be evaluated later stays
	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `fiber.New(fiber.Config{Prefork: *flag.Bool("prefork", false, "prefork")})`)
	assert.Contains(t, content, `app.Listen(":3000", fiber.ListenConfig{DisableStartupMessage: true})`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "side effects")
}

func Test_MigrateConfigListenerFields_ConfigReturnedByAFunction(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"config/config.go": `package config

import (
	"os"

	"github.com/gofiber/fiber/v2"
)

type Settings struct{ Quiet bool }

// Fiber is the config of the program's app.
func Fiber() fiber.Config {
	return fiber.Config{
		AppName:               "demo",
		Prefork:               os.Getenv("PREFORK") == "1",
		DisableStartupMessage: true,
	}
}

func (s Settings) Fiber() fiber.Config {
	return fiber.Config{EnablePrintRoutes: !s.Quiet}
}

func ForServer(prefork bool) fiber.Config {
	return fiber.Config{Network: "tcp", Prefork: prefork}
}
`,
		"main.go": `package main

import (
	"log"

	"example.com/demo/config"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New(config.Fiber())
	go app.Listen(":3000")

	cfg := config.Fiber()
	second := fiber.New(cfg)
	go second.Listen(":3001")

	s := config.Settings{}
	third := fiber.New(s.Fiber())
	go third.Listen(":3002")

	fourth := fiber.New(config.ForServer(true))
	log.Fatal(fourth.Listen(":3003"))
}
`,
	})

	appRun(t, dir, v3.MigrateConfigListenerFields)

	cfg := appRead(t, dir, "config/config.go")
	assert.Contains(t, cfg, "return fiber.Config{\n\t\tAppName: \"demo\",\n\t}")
	// the value that reads the environment has to be written where the app starts
	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, `go app.Listen(":3000", fiber.ListenConfig{EnablePrefork: os.Getenv("PREFORK") == "1", DisableStartupMessage: true})`)
	assert.Contains(t, main, `go second.Listen(":3001", fiber.ListenConfig{EnablePrefork: os.Getenv("PREFORK") == "1", DisableStartupMessage: true})`)
	assert.Contains(t, main, `"os"`)

	// the settings that depend on the arguments of the function cannot move, the others do
	assert.Contains(t, cfg, `fiber.Config{Prefork: prefork}`)
	assert.Contains(t, main, `fourth.Listen(":3003", fiber.ListenConfig{ListenerNetwork: "tcp"})`)
	// a method whose receiver is not in scope where the app starts, either
	assert.Contains(t, cfg, "EnablePrintRoutes: !s.Quiet")
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	for _, n := range notes {
		assert.Equal(t, "config/config.go", n.File)
	}
}

func Test_MigrateConfigListenerFields_ConfigThatEscapes(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func other(cfg fiber.Config) *fiber.App { return fiber.New(cfg) }

func main() {
	cfg := fiber.Config{Prefork: true}
	app := fiber.New(cfg)
	app.Listen(":3000")
	other(cfg).Listen(":3001")
}
`)
	before := appRead(t, dir, "main.go")

	appRun(t, dir, v3.MigrateConfigListenerFields)

	assert.Equal(t, before, appRead(t, dir, "main.go"))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "passed on")
}
