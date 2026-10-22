package v3_test

import (
	"strings"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// olderV2On is MigrateOlderV2Callbacks for a project that is on the release of Fiber v2.
func olderV2On(release string) appMigration {
	return func(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
		return v3.MigrateOlderV2Callbacks(cmd, cwd, semver.MustParse(release), nil)
	}
}

// Up to v2.47 the handlers of OnListen took no argument; v3 hands them the data of
// the listener.
func Test_MigrateOlderV2Callbacks_OnListen(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	f "github.com/gofiber/fiber/v2"
)

func started() error { log.Println("started"); return nil }

type server struct{ ready chan struct{} }

func (s *server) up() error { close(s.ready); return nil }

type bus struct{}

func (bus) OnListen(h func() error) {}

func main() {
	app := f.New()
	s := &server{ready: make(chan struct{})}

	app.Hooks().OnListen(func() error {
		log.Println("listening")
		return nil
	})
	app.Hooks().OnListen(started)
	app.Hooks().OnListen(s.up, func() error { return nil })

	hooks := app.Hooks()
	hooks.OnListen(func() (err error) { return })

	app.Hooks().OnListen(func(data f.ListenData) error { return nil })
	bus{}.OnListen(func() error { return nil })

	log.Fatal(app.Listen(":3000"))
}
`)

	out := appRun(t, dir, olderV2On("2.46.0"))

	assert.Equal(t, `package main

import (
	"log"

	f "github.com/gofiber/fiber/v2"
)

func started() error { log.Println("started"); return nil }

type server struct{ ready chan struct{} }

func (s *server) up() error { close(s.ready); return nil }

type bus struct{}

func (bus) OnListen(h func() error) {}

func main() {
	app := f.New()
	s := &server{ready: make(chan struct{})}

	app.Hooks().OnListen(func(f.ListenData) error {
		log.Println("listening")
		return nil
	})
	app.Hooks().OnListen(func(f.ListenData) error { return started() })
	app.Hooks().OnListen(func(f.ListenData) error { return s.up() }, func(f.ListenData) error { return nil })

	hooks := app.Hooks()
	hooks.OnListen(func(f.ListenData) (err error) { return })

	app.Hooks().OnListen(func(data f.ListenData) error { return nil })
	bus{}.OnListen(func() error { return nil })

	log.Fatal(app.Listen(":3000"))
}
`, appRead(t, dir, "main.go"))
	assert.Contains(t, out, "Migrating the callbacks of older releases of Fiber v2")
	assert.Empty(t, internal.DrainNotes(dir))

	before := appRead(t, dir, "main.go")
	again := appRun(t, dir, olderV2On("2.46.0"))
	assert.Equal(t, before, appRead(t, dir, "main.go"), "the second run changes nothing")
	assert.Empty(t, again)
	assert.Empty(t, internal.DrainNotes(dir))
}

// The app can come from another package: the file that registers the hook need not
// import Fiber, and gets the import for the data of the listener.
func Test_MigrateOlderV2Callbacks_OnListenOfAnotherPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"server/server.go": `package server

import "github.com/gofiber/fiber/v2"

func New() *fiber.App { return fiber.New() }
`,
		"main.go": `package main

import (
	"log"

	"example.com/app/server"
)

func main() {
	srv := server.New()
	srv.Hooks().OnListen(func() error {
		log.Println("listening")
		return nil
	})
	log.Fatal(srv.Listen(":3000"))
}
`,
	})

	appRun(t, dir, olderV2On("2.40.1"))

	assert.Equal(t, `package main

import (
	"log"

	"example.com/app/server"
	"github.com/gofiber/fiber/v2"
)

func main() {
	srv := server.New()
	srv.Hooks().OnListen(func(fiber.ListenData) error {
		log.Println("listening")
		return nil
	})
	log.Fatal(srv.Listen(":3000"))
}
`, appRead(t, dir, "main.go"))
}

func Test_MigrateOlderV2Callbacks_OnListenNotes(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func handlers() []func() error { return nil }

func handler() func() error { return nil }

func main() {
	app := fiber.New()
	app.Hooks().OnListen(handlers()...)
	app.Hooks().OnListen(handler())
}
`)

	appRun(t, dir, olderV2On("2.31.0"))

	assert.Equal(t, `package main

import "github.com/gofiber/fiber/v2"

func handlers() []func() error { return nil }

func handler() func() error { return nil }

func main() {
	app := fiber.New()
	app.Hooks().OnListen(handlers()...)
	app.Hooks().OnListen(handler())
}
`, appRead(t, dir, "main.go"))

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	assert.Equal(t, 11, notes[0].Line)
	assert.Contains(t, notes[0].Message, "convert the handlers you spread into it by hand")
	assert.Equal(t, 12, notes[1].Line)
	assert.Contains(t, notes[1].Message, "wrap handler() in func(fiber.ListenData) error { return ...() }")
}

// A project on v2.48 or later has the handlers of v3, and nothing is known of a
// project whose release is not.
func Test_MigrateOlderV2Callbacks_ProjectsOnTheNewSignatures(t *testing.T) {
	t.Parallel()

	source := `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.Hooks().OnListen(func() error { return nil })
}
`
	for _, release := range []string{"2.48.0", "2.52.15", "3.0.0"} {
		dir := appSingle(t, source)
		appRun(t, dir, olderV2On(release))
		assert.Equal(t, source, appRead(t, dir, "main.go"), release)
	}

	dir := appSingle(t, source)
	appRun(t, dir, v3.MigrateOlderV2Callbacks)
	assert.Equal(t, source, appRead(t, dir, "main.go"), "no release")
	assert.Empty(t, internal.DrainNotes(dir))
}

// Up to v2.22 the StackTraceHandler of the recover middleware took the recovered
// value only; v3 hands the handler the request context first.
func Test_MigrateOlderV2Callbacks_StackTraceHandler(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	mwrecover "github.com/gofiber/fiber/v2/middleware/recover"
)

func report(e interface{}) { log.Println(e) }

type mine struct {
	StackTraceHandler func(e interface{})
}

func main() {
	app := fiber.New()
	app.Use(mwrecover.New(mwrecover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(e interface{}) {
			log.Println("panic:", e)
		},
	}))
	app.Use(mwrecover.New(mwrecover.Config{StackTraceHandler: report}))
	app.Use(mwrecover.New(mwrecover.Config{StackTraceHandler: func(interface{}) {}}))
	app.Use(mwrecover.New(mwrecover.Config{StackTraceHandler: func(c *fiber.Ctx, e interface{}) {}}))
	_ = mine{StackTraceHandler: func(e interface{}) {}}
}
`)

	out := appRun(t, dir, olderV2On("2.22.0"))

	assert.Equal(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	mwrecover "github.com/gofiber/fiber/v2/middleware/recover"
)

func report(e interface{}) { log.Println(e) }

type mine struct {
	StackTraceHandler func(e interface{})
}

func main() {
	app := fiber.New()
	app.Use(mwrecover.New(mwrecover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(_ fiber.Ctx, e interface{}) {
			log.Println("panic:", e)
		},
	}))
	app.Use(mwrecover.New(mwrecover.Config{StackTraceHandler: func(_ fiber.Ctx, e any) { report(e) }}))
	app.Use(mwrecover.New(mwrecover.Config{StackTraceHandler: func(fiber.Ctx, interface{}) {}}))
	app.Use(mwrecover.New(mwrecover.Config{StackTraceHandler: func(c *fiber.Ctx, e interface{}) {}}))
	_ = mine{StackTraceHandler: func(e interface{}) {}}
}
`, appRead(t, dir, "main.go"))
	assert.Contains(t, out, "Migrating the callbacks of older releases of Fiber v2")
	assert.Empty(t, internal.DrainNotes(dir))

	before := appRead(t, dir, "main.go")
	appRun(t, dir, olderV2On("2.22.0"))
	assert.Equal(t, before, appRead(t, dir, "main.go"), "the second run changes nothing")
}

func Test_MigrateOlderV2Callbacks_StackTraceHandlerNotes(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func handler() func(e interface{}) { return nil }

func main() {
	app := fiber.New()
	app.Use(recover.New(recover.Config{StackTraceHandler: handler()}))
	cfg := recover.ConfigDefault
	cfg.StackTraceHandler = func(e interface{}) {}
	app.Use(recover.New(cfg))
}
`)

	appRun(t, dir, olderV2On("2.10.0"))

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	assert.Equal(t, 12, notes[0].Line)
	assert.Contains(t, notes[0].Message, "wrap handler() in func(_ fiber.Ctx, e any) { ...(e) }")
	assert.Equal(t, 14, notes[1].Line)
	assert.Contains(t, notes[1].Message, "adapt the handler you assign here")
}

// Between v2.23 and v2.47 the handler of the recover middleware is the one of v3.
func Test_MigrateOlderV2Callbacks_StackTraceHandlerOfALaterRelease(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	app := fiber.New()
	app.Use(recover.New(recover.Config{StackTraceHandler: func(c *fiber.Ctx, e interface{}) {}}))
}
`
	dir := appSingle(t, source)

	appRun(t, dir, olderV2On("2.40.1"))

	assert.Equal(t, source, appRead(t, dir, "main.go"))
	assert.Empty(t, internal.DrainNotes(dir))
}

// The app can be a field, a parameter, the embedded app of a struct or the result of
// a function.
func Test_MigrateOlderV2Callbacks_OnListenOfAnApp(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

type service struct {
	app *fiber.App
}

type wrapper struct {
	*fiber.App
}

func build() *fiber.App { return fiber.New() }

func register(app *fiber.App) {
	app.Hooks().OnListen(func() error { return nil })
}

func (s *service) hooks() {
	s.app.Hooks().OnListen(func() error { return nil })
}

func (w wrapper) hooks() {
	w.Hooks().OnListen(func() error { return nil })
}

func main() {
	build().Hooks().OnListen(func() error { return nil })
	register(build())
}
`)

	appRun(t, dir, olderV2On("2.45.0"))

	content := appRead(t, dir, "main.go")
	assert.Equal(t, 4, strings.Count(content, "OnListen(func(fiber.ListenData) error { return nil })"), content)
	assert.NotContains(t, content, "func() error")
	assert.Empty(t, internal.DrainNotes(dir))
}
