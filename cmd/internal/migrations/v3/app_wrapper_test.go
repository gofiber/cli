package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A type that embeds an app can have methods of its own with the names of the
// app's. Calling one of them is not calling fiber, so nothing is rewritten, while
// the calls of the promoted methods and of the embedded app still are.
func Test_AppMigrations_MethodsOfTheProjectAreNotFibers(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

type Server struct {
	*fiber.App
}

func (s *Server) Listen(addr string) error { return s.App.Listen(addr) }

func (s *Server) Static(prefix, root string) { s.App.Static(prefix, root) }

func (s *Server) Mount(prefix string, other *Server) {}

func (s *Server) Test(req *http.Request, ms int) {}

func (s *Server) Add(method, path string) {}

func main() {
	srv := &Server{App: fiber.New(fiber.Config{DisableStartupMessage: true})}
	srv.Static("/own", "./own")
	srv.Mount("/m", srv)
	srv.Test(nil, 5)
	srv.Add("GET", "/x")
	srv.Get("/", func(c *fiber.Ctx) error { return nil })
	srv.Static("/", "./public")
	srv.Listen(":3000")
	srv.App.Listen(":3001")
}
`)

	for _, migrate := range []appMigration{
		v3.MigrateStaticRoutes, v3.MigrateMount, v3.MigrateAppTestConfig, v3.MigrateAddMethod,
		v3.MigrateConfigListenerFields, v3.MigrateListenMethods,
	} {
		appRun(t, dir, migrate)
	}

	content := appRead(t, dir, "main.go")
	for _, own := range []string{
		`srv.Static("/own", "./own")`, `srv.Mount("/m", srv)`, `srv.Test(nil, 5)`, `srv.Add("GET", "/x")`,
		`srv.Static("/", "./public")`, `srv.Listen(":3000")`,
	} {
		assert.Contains(t, content, own, "a method of the project")
	}
	// the embedded app is fiber's, and so is what the type promotes
	assert.Contains(t, content, `s.App.Get(prefix+"*", static.New(cmp.Or(root, ".")))`)
	assert.Contains(t, content, `return s.App.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true})`)
	assert.Contains(t, content, `srv.App.Listen(":3001", fiber.ListenConfig{DisableStartupMessage: true})`)
	assert.Empty(t, internal.DrainNotes(dir))
}
