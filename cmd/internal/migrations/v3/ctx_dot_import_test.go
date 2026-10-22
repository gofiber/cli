package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSendFileConfig_DotImport(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateSendFileConfig, []ctxCase{
		{
			name: "the config is spelled without a qualifier",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	if err := c.SendFile("./a.html", true); err != nil {
		return err
	}
	return c.SendFile("./b.html")
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	if err := c.SendFile("./a.html", SendFile{Compress: true, ByteRange: true}); err != nil {
		return err
	}
	return c.SendFile("./b.html", SendFile{ByteRange: true})
}
`,
			msg: "Migrating SendFile compress flag",
		},
	})
}

func Test_MigrateGenericHelpers_DotImport(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name: "the helpers are spelled without a qualifier",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	id, err := c.ParamsInt("id", 3)
	page := c.QueryInt("page", 1)
	_, _ = id, page
	return err
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	id, err := Params[int](c, "id", 3), error(nil)
	page := Query[int](c, "page", 1)
	_, _ = id, page
	return err
}
`,
			msg: "Migrating generic helpers",
		},
	})
}

func Test_MigrateRedirectMethods_DotImport(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name: "the config is spelled without a qualifier",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	if c.Query("a") == "1" {
		return c.RedirectToRoute("home", Map{"id": 1})
	}
	return c.Redirect("/x", StatusFound)
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	if c.Query("a") == "1" {
		return c.Redirect().Status(StatusFound).Route("home", RedirectConfig{Params: Map{"id": 1}})
	}
	return c.Redirect().Status(StatusFound).To("/x")
}
`,
			msg: "Migrating redirect methods",
		},
	})
}

func Test_MigrateViewBind_DotImport(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateViewBind, []ctxCase{
		{
			name: "a view map of the dot import",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	return c.Bind(Map{"a": 1})
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx) error {
	return c.ViewBind(Map{"a": 1})
}
`,
			msg: "Migrating view binding helpers",
		},
	})
}
