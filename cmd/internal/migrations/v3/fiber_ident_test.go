package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_isFiberApp(t *testing.T) {
	t.Parallel()

	src := `package main
import "github.com/gofiber/fiber/v2"
func main() {
        app := fiber.New()
        _ = app
}`
	if !isFiberApp(src, "app") {
		t.Fatal("expected app to be recognized")
	}
	if isFiberApp(src, "main") {
		t.Fatal("unexpected match")
	}
}

func Test_isFiberRouter(t *testing.T) {
	t.Parallel()

	src := `package main
import "github.com/gofiber/fiber/v2"
func main() {
        app := fiber.New()
        grp := app.Group("/api")
        var r fiber.Router
        r = app
        _ = grp
        _ = r
}`
	if !isFiberRouter(src, "app") || !isFiberRouter(src, "grp") || !isFiberRouter(src, "r") {
		t.Fatal("router identifiers not detected")
	}
	if isFiberRouter(src, "main") {
		t.Fatal("unexpected match")
	}
}

func Test_isFiberCtx(t *testing.T) {
	t.Parallel()

	src := `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error { return nil }`
	if !isFiberCtx(src, "c") {
		t.Fatal("expected context to be recognized")
	}
	if isFiberCtx(src, "handler") {
		t.Fatal("unexpected match")
	}
}

func Test_isFiber_Aliased(t *testing.T) {
	t.Parallel()

	src := `package main

import f "github.com/gofiber/fiber/v2"

type server struct {
	app *f.App
	rt  f.Router
}

func newApp() *f.App { return f.New() }

func handler(c *f.Ctx, grp f.Group, reg f.Registering) error {
	app := f.New()
	other := newApp()
	api := app.Group("/api")
	return nil
}
`
	assert.True(t, isFiberCtx(src, "c"))
	assert.True(t, isFiberApp(src, "app"))
	assert.True(t, isFiberApp(src, "other"), "an app that a function of the file returns")
	assert.True(t, isFiberGroup(src, "grp"))
	assert.True(t, isFiberGroup(src, "api"))
	assert.True(t, isFiberRouter(src, "api"))
	assert.True(t, isFiberRouter(src, "rt"))
	assert.True(t, isFiberRouter(src, "reg"))
	assert.False(t, isFiberCtx(src, "app"))
	assert.False(t, isFiberApp(src, "c"))
	assert.False(t, isFiberApp(src, "api"))
}

func Test_isFiber_OnlyTheImportedPackage(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"example.com/web"

	fb "github.com/gofiber/fiber/v2"
)

func handler(c *web.Ctx, d *fb.Ctx, e *fiber.Ctx) {}
`
	assert.False(t, isFiberCtx(src, "c"), "another package's Ctx")
	assert.True(t, isFiberCtx(src, "d"))
	assert.False(t, isFiberCtx(src, "e"), "fiber is not the name the package is imported by")
}

func Test_isFiber_StructFieldsAndAssignments(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

type s struct{ app *fiber.App }

func (x *s) init() {
	x.app = fiber.New()
}

func run(h *s) {
	var local = fiber.New()
	var typed *fiber.App
	_, _ = local, typed
}
`
	assert.True(t, isFiberApp(src, "app"))
	assert.True(t, isFiberApp(src, "local"))
	assert.True(t, isFiberApp(src, "typed"))
	assert.False(t, isFiberApp(src, "h"))
}

func Test_isFiber_WithoutImport(t *testing.T) {
	t.Parallel()

	// Snippets without an import clause are taken to import fiber as "fiber".
	assert.True(t, isFiberCtx("func handler(c *fiber.Ctx) error { return nil }", "c"))
	assert.True(t, isFiberApp("var app = fiber.New()", "app"))
	assert.False(t, isFiberCtx("func handler(c *other.Ctx) error { return nil }", "c"))
}

func Test_isFiber_UnparsableSource(t *testing.T) {
	t.Parallel()

	assert.False(t, isFiberCtx("func (", "c"))
	assert.False(t, isFiberApp("not go at all", "app"))
}

func Test_isFiber_EmptyIdentifier(t *testing.T) {
	t.Parallel()

	// The unnamed parameter of a struct type is indexed under no name at all,
	// which must not make every lookup of an empty identifier succeed.
	src := `package main

import "github.com/gofiber/fiber/v2"

type hook func(struct{ *fiber.Ctx }, int)
`
	assert.False(t, isFiberCtx(src, ""))
	assert.False(t, isFiberApp(src, ""))
	assert.False(t, identHasType(src, "", "fiber.Ctx"))
	assert.False(t, identAssignedFrom(src, "", `fiber\.New\(`))
}

func Test_identHasType(t *testing.T) {
	t.Parallel()

	src := `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx, n int, a, b *f.App) {}
`
	assert.True(t, identHasType(src, "c", "fiber.Ctx"))
	assert.True(t, identHasType(src, "c", "*fiber.Ctx"))
	assert.True(t, identHasType(src, "a", "fiber.App"), "the first name of a group")
	assert.True(t, identHasType(src, "b", "fiber.App"))
	assert.False(t, identHasType(src, "c", "fiber.App"))
	assert.True(t, identHasType(src, "n", "int"))
	assert.False(t, identHasType(src, "missing", "int"))
}

func Test_identAssignedFrom(t *testing.T) {
	t.Parallel()

	src := `package main

import f "github.com/gofiber/fiber/v2"

func main() {
	app := f.New()
	api := app.Group("/api")
	var plain = other()
	_, _, _ = app, api, plain
}
`
	assert.True(t, identAssignedFrom(src, "app", `fiber\.New\(`), "fiber stands for the name of the package")
	assert.True(t, identAssignedFrom(src, "api", `\w+\.Group\(`))
	assert.False(t, identAssignedFrom(src, "plain", `fiber\.New\(`))
	assert.False(t, identAssignedFrom(src, "app", `\w+\.Group\(`))
}
