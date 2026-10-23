package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The store calls that take a context pass the one of the request. A handler that
// takes the request context under another name, an alias of fiber.Ctx or a struct
// that embeds it, has it all the same, and so does a method of such a struct.
func Test_MigrateSessionMethods_RequestContextUnderAnotherName(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

type Ctx = fiber.Ctx

type web struct {
	fiber.Ctx
	user string
}

type plain struct {
	ctx fiber.Ctx
}

func alias(c *Ctx) error {
	return store.Reset()
}

func wrapped(w *web) error {
	return store.Delete(w.user)
}

func (w web) logout() error {
	return store.Reset()
}

func notEmbedded(p plain) error {
	return store.Reset()
}

func closure(w web) {
	go func() { _ = store.Reset() }()
}
`, v3.MigrateSessionMethods)

	assert.Equal(t, `package main

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

type Ctx = fiber.Ctx

type web struct {
	fiber.Ctx
	user string
}

type plain struct {
	ctx fiber.Ctx
}

func alias(c *Ctx) error {
	return store.Reset(c)
}

func wrapped(w *web) error {
	return store.Delete(w, w.user)
}

func (w web) logout() error {
	return store.Reset(w)
}

func notEmbedded(p plain) error {
	return store.Reset(context.Background())
}

func closure(w web) {
	go func() { _ = store.Reset(context.Background()) }()
}
`, got)
	assert.Empty(t, res.notes)
}

// The type can be declared in another package of the module, and the file that
// uses it need not import Fiber.
func Test_MigrateSessionMethods_RequestContextOfAnotherPackage(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"web/web.go": `package web

import "github.com/gofiber/fiber/v3"

// Ctx is the context of a request.
type Ctx = fiber.Ctx

// Context adds what the handlers of the service need.
type Context struct {
	fiber.Ctx
	User string
}
`,
		"handlers/handlers.go": `package handlers

import (
	"github.com/gofiber/fiber/v3/middleware/session"

	"example.com/app/web"
)

var store = session.NewStore()

func Logout(c *web.Ctx) error {
	return store.Reset()
}

func Purge(c *web.Context) error {
	return store.Delete(c.User)
}

func Startup() error {
	return store.Reset()
}
`,
	}, v3.MigrateSessionMethods)

	got := res.files["handlers/handlers.go"]
	assert.Contains(t, got, "func Logout(c *web.Ctx) error {\n\treturn store.Reset(c)\n}")
	assert.Contains(t, got, "func Purge(c *web.Context) error {\n\treturn store.Delete(c, c.User)\n}")
	assert.Contains(t, got, "func Startup() error {\n\treturn store.Reset(context.Background())\n}")
	assert.Empty(t, res.notes)

	again := mw3RunModule(t, res.files, v3.MigrateSessionMethods)
	assert.Equal(t, res.files, again.files)
}
