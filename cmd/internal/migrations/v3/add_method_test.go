package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateAddMethod(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

type server struct{ app *fiber.App }

func register(r fiber.Router) {
	r.Add(fiber.MethodGet, "/r", func(c *fiber.Ctx) error { return nil })
}

func merge(a, b string) string { return a + b }

func main() {
	h := func(c *fiber.Ctx) error { return c.SendString("ok") }

	app := fiber.New()
	app.Add(fiber.MethodGet, "/a", h)
	app.Add("POST", "/b", h)
	app.Add(http.MethodPut, "/c", h, h)
	app.Add(merge("GE", "T"), "/nested", h)

	grp := app.Group("/g")
	grp.Add(fiber.MethodGet, "/d", h)

	s := &server{app: fiber.New()}
	s.app.Add(fiber.MethodGet, "/e", h)

	app.Add(
		fiber.MethodDelete,
		"/multi",
		h,
	)
}
`)

	out := appRun(t, dir, v3.MigrateAddMethod)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `r.Add([]string{fiber.MethodGet}, "/r"`)
	assert.Contains(t, content, `app.Add([]string{fiber.MethodGet}, "/a", h)`)
	assert.Contains(t, content, `app.Add([]string{"POST"}, "/b", h)`)
	assert.Contains(t, content, `app.Add([]string{http.MethodPut}, "/c", h, h)`)
	assert.Contains(t, content, `app.Add([]string{merge("GE", "T")}, "/nested", h)`)
	assert.Contains(t, content, `grp.Add([]string{fiber.MethodGet}, "/d", h)`)
	assert.Contains(t, content, `s.app.Add([]string{fiber.MethodGet}, "/e", h)`)
	assert.Contains(t, content, "[]string{fiber.MethodDelete},\n\t\t\"/multi\",")
	assert.Contains(t, out, "Migrating Add method calls")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateAddMethod_SkipUnrelated(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"net/http"
	"sync/atomic"

	"github.com/gofiber/fiber/v2"
)

var httpServerActiveRequests atomic.Int64

func handler(c *fiber.Ctx, req *http.Request) error {
	req.Header.Add("Authorization", "Bearer token")
	c.Response().Header.Add("X-Key", "Value")
	httpServerActiveRequests.Add(1)
	return nil
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateAddMethod)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateAddMethod_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.Add(fiber.MethodGet, "/foo", func(c *fiber.Ctx) error { return nil })
}
`)

	appRun(t, dir, v3.MigrateAddMethod)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateAddMethod)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateAddMethod_HandlerSlices(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func auth(c *fiber.Ctx) error { return c.Next() }

func chain() []fiber.Handler { return []fiber.Handler{auth} }

func route(r fiber.Router, method, path string, handlers ...fiber.Handler) {
	r.Add(method, path, handlers...)
}

func get(r fiber.Router, path string, handlers ...fiber.Handler) fiber.Router {
	return r.Get(path, handlers...)
}

func main() {
	app := fiber.New()
	mws := []fiber.Handler{auth}
	app.Get("/a", mws...)
	g := app.Group("/x", mws...)
	_ = g
	anys := []any{auth}
	app.Group("/y", anys...)
	app.Post("/z", auth, anys...)
	app.Put("/w", chain()...)
}
`)

	appRun(t, dir, v3.MigrateAddMethod)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "func route(r fiber.Router, method, path string, handlers ...fiber.Handler) {\n"+
		"\thandlerArgs := make([]any, len(handlers))\n"+
		"\tfor i, h := range handlers {\n"+
		"\t\thandlerArgs[i] = h\n"+
		"\t}\n"+
		"\tr.Add([]string{method}, path, handlerArgs[0], handlerArgs[1:]...)\n}")
	assert.Contains(t, content, "\thandlerArgs := make([]any, len(handlers))\n\tfor i, h := range handlers {\n\t\thandlerArgs[i] = h\n\t}\n\treturn r.Get(path, handlerArgs[0], handlerArgs[1:]...)")
	assert.Contains(t, content, "app.Get(\"/a\", handlerArgs[0], handlerArgs[1:]...)")
	assert.Contains(t, content, "g := app.Group(\"/x\", handlerArgs2...)")
	// a slice of any is what v3 takes
	assert.Contains(t, content, `app.Group("/y", anys...)`)
	assert.Contains(t, content, `app.Post("/z", auth, anys...)`)
	// a call is not evaluated twice: left for the user
	assert.Contains(t, content, `app.Put("/w", chain()...)`)

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "...any")
}

func Test_MigrateAddMethod_HandlerSlicesIdempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func get(r fiber.Router, path string, handlers ...fiber.Handler) fiber.Router {
	return r.Get(path, handlers...)
}
`)

	appRun(t, dir, v3.MigrateAddMethod)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateAddMethod)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}
