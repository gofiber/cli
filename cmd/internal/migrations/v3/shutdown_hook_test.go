package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateShutdownHook(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

func cleanup() error { return nil }

type resource struct{}

func (r *resource) Close() error { return nil }

func main() {
	app := fiber.New()
	res := &resource{}

	app.Hooks().OnShutdown(func() error {
		log.Println("shutting down")
		return nil
	})
	app.Hooks().OnShutdown(cleanup)
	app.Hooks().OnShutdown(res.Close)
	app.Hooks().OnShutdown(cleanup, res.Close)

	hooks := app.Hooks()
	hooks.OnShutdown(func() error { return nil })

	app.Hooks().OnListen(func(fiber.ListenData) error { return nil })
}
`)

	out := appRun(t, dir, v3.MigrateShutdownHook)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "app.Hooks().OnPostShutdown(func(error) error {\n\t\tlog.Println(\"shutting down\")")
	assert.Contains(t, content, "app.Hooks().OnPostShutdown(func(error) error { return cleanup() })")
	assert.Contains(t, content, "app.Hooks().OnPostShutdown(func(error) error { return res.Close() })")
	assert.Contains(t, content, "app.Hooks().OnPostShutdown(func(error) error { return cleanup() }, func(error) error { return res.Close() })")
	assert.Contains(t, content, "hooks.OnPostShutdown(func(error) error { return nil })")
	assert.Contains(t, content, "app.Hooks().OnListen(")
	assert.NotContains(t, content, "OnShutdown")
	assert.Contains(t, out, "Migrating shutdown hooks")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateShutdownHook_KeepsOuterVariables(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func setup() error { return nil }

func main() {
	app := fiber.New()
	err := setup()
	app.Hooks().OnShutdown(func() error { return err })
}
`)

	appRun(t, dir, v3.MigrateShutdownHook)

	// a parameter named err would shadow the variable the handler returns
	assert.Contains(t, appRead(t, dir, "main.go"), "app.Hooks().OnPostShutdown(func(error) error { return err })")
}

func Test_MigrateShutdownHook_Notes(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func hook() func() error { return nil }

func main() {
	app := fiber.New()
	handlers := []func() error{hook()}
	app.Hooks().OnShutdown(hook())
	app.Hooks().OnShutdown(handlers...)
}
`)

	appRun(t, dir, v3.MigrateShutdownHook)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "app.Hooks().OnPostShutdown(hook())")
	assert.Contains(t, content, "app.Hooks().OnPostShutdown(handlers...)")
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	assert.Equal(t, 10, notes[0].Line)
	assert.Contains(t, notes[0].Message, "wrap hook()")
	assert.Equal(t, 11, notes[1].Line)
}

func Test_MigrateShutdownHook_OtherTypes(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

type lifecycle struct{}

func (lifecycle) OnShutdown(func() error) {}

func main() {
	var l lifecycle
	l.OnShutdown(func() error { return nil })
	println("app.Hooks().OnShutdown(cleanup)") // app.Hooks().OnShutdown(cleanup)
	_ = fiber.New()
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateShutdownHook)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateShutdownHook_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func cleanup() error { return nil }

func main() {
	app := fiber.New()
	app.Hooks().OnShutdown(cleanup)
	app.Hooks().OnShutdown(func() error { return nil })
}
`)

	appRun(t, dir, v3.MigrateShutdownHook)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateShutdownHook)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}
