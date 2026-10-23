package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A module of the repository that imports a package of another module reads the
// types of that package as the project's own: the handlers of the app module use
// the wrapper of the context that the shared module declares, in files that import
// neither fiber nor the middleware, and are migrated like the handlers of a single
// module. The file gets the import of fiber, which only the shared module has.
func Test_MigrateGenericHelpers_AnotherModuleOfTheRepository(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod":        "// the repository is no module\n",
		"go.work":       "go 1.25\n\nuse (\n\t./app\n\t./shared\n)\n",
		"shared/go.mod": "module example.com/mono/shared\n\ngo 1.25\n",
		"shared/web/web.go": `package web

import "github.com/gofiber/fiber/v2"

// Ctx is the alias the project uses for the context.
type Ctx = fiber.Ctx
`,
		"app/go.mod": "module example.com/mono/app\n\ngo 1.25\n\nrequire example.com/mono/shared v0.0.0\n\nreplace example.com/mono/shared => ../shared\n",
		"app/handlers/handlers.go": `package handlers

import "example.com/mono/shared/web"

// Page answers with the page the request asks for.
func Page(c *web.Ctx) error {
	n := c.QueryInt("page", 1)
	return c.SendString(string(rune('0' + n)))
}
`,
	})

	out := appRun(t, dir, v3.MigrateGenericHelpers)

	assert.Contains(t, out, "Migrating generic helpers")
	assert.Equal(t, `package handlers

import (
	"example.com/mono/shared/web"
	"github.com/gofiber/fiber/v2"
)

// Page answers with the page the request asks for.
func Page(c *web.Ctx) error {
	n := fiber.Query[int](c, "page", 1)
	return c.SendString(string(rune('0' + n)))
}
`, appRead(t, dir, "app/handlers/handlers.go"))
}
