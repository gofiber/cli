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

func route(r fiber.Router, method, path string, handlers ...fiber.Handler) {
	r.Add(method, path, handlers...)
}

func get(r fiber.Router, path string, handlers ...fiber.Handler) fiber.Router {
	return r.Get(path, handlers...)
}

func main() {
	app := fiber.New()
	chain := []fiber.Handler{auth}
	app.Get("/a", chain...)
	g := app.Group("/x", chain...)
	_ = g
	anys := []any{auth}
	app.Group("/y", anys...)
	app.Post("/z", auth, anys...)
}
`)

	appRun(t, dir, v3.MigrateAddMethod)

	content := appRead(t, dir, "main.go")
	// the method is still wrapped, the spread cannot be rewritten by hand-free rules
	assert.Contains(t, content, "r.Add([]string{method}, path, handlers...)")
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 4)
	lines := make([]int, 0, len(notes))
	for _, n := range notes {
		lines = append(lines, n.Line)
		assert.Contains(t, n.Message, "...any")
	}
	assert.ElementsMatch(t, []int{8, 12, 18, 19}, lines)
}
