package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateMount(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

type server struct{ app *fiber.App }

func micro(name string) *fiber.App {
	m := fiber.New()
	m.Get("/ping", func(c *fiber.Ctx) error { return c.SendString(name) })
	return m
}

func (s *server) mount() {
	s.app.Mount("/field", micro("field"))
}

func register(r fiber.Router) {
	r.Mount("/iface", micro("iface"))
}

func main() {
	app := fiber.New()
	api := fiber.New()
	app.Mount("/api", api)

	grp := app.Group("/g")
	grp.Mount("/v1", micro("v1"))

	// Mount returns a router in v2, so calls chain
	app.Mount("/a", micro("a")).Mount("/b", micro("b"))

	prefix := "/dyn"
	app.Mount(prefix, micro("dyn"))
}
`)

	out := appRun(t, dir, v3.MigrateMount)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `app.Use("/api", api)`)
	assert.Contains(t, content, `grp.Use("/v1", micro("v1"))`)
	assert.Contains(t, content, `app.Use("/a", micro("a")).Use("/b", micro("b"))`)
	assert.Contains(t, content, `app.Use(prefix, micro("dyn"))`)
	assert.Contains(t, content, `s.app.Use("/field", micro("field"))`)
	assert.Contains(t, content, `r.Use("/iface", micro("iface"))`)
	assert.NotContains(t, content, "Mount(")
	assert.Contains(t, out, "Migrating Mount usages")
}

func Test_MigrateMount_LeavesOtherTypesAlone(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

// registry is not a router of Fiber, it has a Mount method of its own.
type registry struct{ mounts map[string]http.Handler }

func (r *registry) Mount(path string, h http.Handler) { r.mounts[path] = h }

func main() {
	reg := &registry{}
	reg.Mount("/legacy", http.NotFoundHandler()) // app.Mount("/x", sub) in a comment
	println("app.Mount(\"/y\", sub)")

	app := fiber.New()
	app.Mount("/sub", fiber.New())
}
`)

	appRun(t, dir, v3.MigrateMount)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `reg.Mount("/legacy", http.NotFoundHandler()) // app.Mount("/x", sub) in a comment`)
	assert.Contains(t, content, `println("app.Mount(\"/y\", sub)")`)
	assert.Contains(t, content, `app.Use("/sub", fiber.New())`)
}

func Test_MigrateMount_NoFiberNoChange(t *testing.T) {
	t.Parallel()

	src := `package main

type router struct{}

func (router) Mount(string, any) {}

func main() {
	var r router
	r.Mount("/x", nil)
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateMount)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateMount_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.Mount("/api", fiber.New())
}
`)

	appRun(t, dir, v3.MigrateMount)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateMount)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}
