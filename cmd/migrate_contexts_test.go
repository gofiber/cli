package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The whole migration follows the request context into the packages that import
// neither Fiber nor the middleware: a handler that takes the wrapper or the alias a
// project declares in another package is migrated like one that takes *fiber.Ctx,
// including the read of what the request ID middleware keeps in the locals.
func Test_Migrate_V2_to_V3_ContextsOfOtherPackages(t *testing.T) {
	dir, err := os.MkdirTemp("", "migrate_v2_v3_contexts")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	files := map[string]string{
		"go.mod": goModV2,
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	"example.com/demo/internal/handlers"
	"example.com/demo/internal/web"
)

func main() {
	app := fiber.New()
	app.Use(requestid.New())
	app.Get("/", web.Wrap(handlers.Page))
	app.Get("/alias", web.WrapAlias(handlers.Alias))
	_ = app.Listen(":3000")
}
`,
		"internal/web/web.go": `package web

import "github.com/gofiber/fiber/v2"

// Ctx is the context of the project.
type Ctx = fiber.Ctx

// Context adds the user to the context.
type Context struct {
	*fiber.Ctx
	User string
}

// Wrap makes a fiber handler of a handler of the project.
func Wrap(h func(*Context) error) fiber.Handler {
	return func(c *fiber.Ctx) error { return h(&Context{Ctx: c}) }
}

// WrapAlias makes a fiber handler of a handler that takes the alias.
func WrapAlias(h func(*Ctx) error) fiber.Handler { return h }
`,
		"internal/handlers/handlers.go": `package handlers

import "example.com/demo/internal/web"

// Page reads through the wrapper.
func Page(c *web.Context) error {
	rid := c.Locals("requestid").(string)
	if c.QueryInt("page", 1) < 0 {
		return c.Redirect("/" + rid)
	}
	return c.SendString(rid)
}

// Alias takes the alias.
func Alias(c *web.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return err
	}
	return c.SendString(string(rune(id)))
}
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { require.NoError(t, os.Chdir(cwd)) }()

	cmd := newMigrateCmd()
	setupCmd()
	defer teardownCmd()
	out, err := runCobraCmd(cmd, "-t=3.0.0")
	require.NoError(t, err)
	assert.Contains(t, out, "Migrating generic helpers")
	assert.Contains(t, out, "Migrating middleware locals")

	handlers := readFileTB(t, filepath.Join(dir, "internal", "handlers", "handlers.go"))
	at := assert.New(t)
	at.Contains(handlers, `"github.com/gofiber/fiber/v3"`)
	at.Contains(handlers, `"github.com/gofiber/fiber/v3/middleware/requestid"`)
	at.Contains(handlers, "rid := requestid.FromContext(c.Ctx)")
	at.Contains(handlers, `fiber.Query[int](c.Ctx, "page", 1)`)
	at.Contains(handlers, `c.Redirect().Status(fiber.StatusFound).To("/" + rid)`)
	at.Contains(handlers, "func Alias(c web.Ctx) error")
	at.Contains(handlers, `strconv.Atoi(c.Params("id"))`)
	at.NotContains(handlers, "Locals")
	at.NotContains(handlers, "QueryInt")
	at.NotContains(handlers, "ParamsInt")
	at.NotContains(handlers, "*web.Ctx")

	web := readFileTB(t, filepath.Join(dir, "internal", "web", "web.go"))
	at.Contains(web, "fiber.Ctx\n\tUser string")
	at.NotContains(web, "*fiber.Ctx")

	// the migration is idempotent
	cmd = newMigrateCmd()
	_, err = runCobraCmd(cmd, "-t=3.0.0", "-f")
	require.NoError(t, err)
	assert.Equal(t, handlers, readFileTB(t, filepath.Join(dir, "internal", "handlers", "handlers.go")))
	assert.Equal(t, web, readFileTB(t, filepath.Join(dir, "internal", "web", "web.go")))
}
