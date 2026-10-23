package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateMimeConstants_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateMimeConstants, []ctxCase{
		{
			name: "the JavaScript constants",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJavaScriptCharsetUTF8)
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJavaScript)
	return c.SendString("console.log(1)")
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextJavaScriptCharsetUTF8)
	c.Set(fiber.HeaderContentType, fiber.MIMETextJavaScript)
	return c.SendString("console.log(1)")
}
`,
			msg: "Migrating MIME constants",
			notes: []string{
				"main.go:6: fiber.MIMEApplicationJavaScriptCharsetUTF8 is now MIMETextJavaScriptCharsetUTF8: the media type is text/javascript, it was application/javascript",
				"main.go:7: fiber.MIMEApplicationJavaScript is now MIMETextJavaScript: the media type is text/javascript, it was application/javascript; " +
					`a .js file served by the static handler is "text/javascript; charset=utf-8", which this constant does not equal: compare it with MIMETextJavaScriptCharsetUTF8`,
			},
		},
		{
			name: "aliased import and a note for each kind only once",
			in: `package main

import f "github.com/gofiber/fiber/v2"

const a = f.MIMEApplicationJavaScript
const b = f.MIMEApplicationJavaScript
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

const a = f.MIMETextJavaScript
const b = f.MIMETextJavaScript
`,
			msg:   "Migrating MIME constants",
			notes: []string{"main.go:5: f.MIMEApplicationJavaScript is now MIMETextJavaScript"},
		},
		{
			name: "strings, comments and other names stay",
			in: `package main

import "github.com/gofiber/fiber/v2"

// fiber.MIMEApplicationJavaScript is the old name
const doc = "fiber.MIMEApplicationJavaScript"

const MIMEApplicationJavaScriptLegacy = "application/javascript"

var other = web.MIMEApplicationJavaScript

func use(c *fiber.Ctx) { _ = MIMEApplicationJavaScriptLegacy }
`,
		},
		{
			name: "the other media types stay",
			in: `package main

import "github.com/gofiber/fiber/v2"

const (
	a = fiber.MIMEApplicationJSON
	b = fiber.MIMETextJavaScript
)
`,
		},
	})
}
