package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateListenerCallbacks(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v3"
)

func main() {
	app := fiber.New()
	app.Listen(":3000", fiber.ListenConfig{
		DisableStartupMessage: true,
		OnShutdownError: func(err error) {
			log.Print(err)
		},
		OnShutdownSuccess: func() {
			log.Print("ok")
		},
	})
	app.Listen(":3001", fiber.ListenConfig{OnShutdownSuccess: func() {}, EnablePrintRoutes: true})
}
`)

	out := appRun(t, dir, v3.MigrateListenerCallbacks)

	content := appRead(t, dir, "main.go")
	assert.NotContains(t, content, "\tOnShutdownError:")
	assert.Contains(t, content, "\t\t// TODO(fiber migrate): OnShutdownError: func(err error) {\n")
	assert.Contains(t, content, "\t\t// TODO(fiber migrate): log.Print(err)\n")
	assert.Contains(t, content, "\t\t// TODO(fiber migrate): OnShutdownSuccess: func() {\n")
	assert.Contains(t, content, "\t\t// TODO(fiber migrate): },\n")
	assert.Contains(t, content, "DisableStartupMessage: true,")
	assert.Contains(t, content, `fiber.ListenConfig{ /* TODO(fiber migrate): OnShutdownSuccess: func() {} */ EnablePrintRoutes: true}`)
	assert.Contains(t, out, "Migrating listener callbacks")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 3)
	for _, n := range notes {
		assert.Contains(t, n.Message, "OnPostShutdown")
	}
}

func Test_MigrateListenerCallbacks_OtherTypes(t *testing.T) {
	t.Parallel()

	src := `package main

type handlers struct {
	OnShutdownError   func(error)
	OnShutdownSuccess func()
}

func main() {
	_ = handlers{
		OnShutdownError:   func(error) {},
		OnShutdownSuccess: func() {},
	}
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateListenerCallbacks)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateListenerCallbacks_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v3"

func main() {
	app := fiber.New()
	app.Listen(":3000", fiber.ListenConfig{
		OnShutdownSuccess: func() {},
	})
}
`)

	appRun(t, dir, v3.MigrateListenerCallbacks)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateListenerCallbacks)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}
