package v3_test

import (
	"bytes"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// headerMapsOn is MigrateHeaderMaps for a project that is on the release of Fiber v2.
func headerMapsOn(release string) ctxMigrateFn {
	return func(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
		return v3.MigrateHeaderMaps(cmd, cwd, semver.MustParse(release), nil)
	}
}

const headerMapsAdapter = `func(h map[string][]string) map[string]string {
		m := make(map[string]string, len(h))
		for k, v := range h {
			m[k] = v[len(v)-1]
		}
		return m
	}(`

func Test_MigrateHeaderMaps_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, headerMapsOn("2.37.1"), []ctxCase{
		{
			name: "a header looked up by name",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	ip := c.GetReqHeaders()["X-Real-IP"]
	c.Set("X-Seen", c.GetRespHeaders()[fiber.HeaderETag])
	if c.GetReqHeaders()[c.Params("name")] == "" {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	return c.SendString(ip)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	ip := c.Get("X-Real-IP")
	c.Set("X-Seen", c.GetRespHeader(fiber.HeaderETag))
	if c.Get(c.Params("name")) == "" {
		return c.SendStatus(fiber.StatusBadRequest)
	}
	return c.SendString(ip)
}
`,
			msg:   "Migrating the header maps of the request context",
			notes: []string{"main.go:6: c.Get(name) gives the first value of a header the client sent more than once"},
		},
		{
			name: "the whole map",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func show(h map[string]string) { fmt.Println(len(h)) }

func handler(c *fiber.Ctx) error {
	headers := c.GetReqHeaders()
	for name, value := range c.GetRespHeaders() {
		fmt.Println(name, value)
	}
	show(c.GetReqHeaders())
	return c.JSON(headers)
}
`,
			want: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func show(h map[string]string) { fmt.Println(len(h)) }

func handler(c *fiber.Ctx) error {
	headers := ` + headerMapsAdapter + `c.GetReqHeaders())
	for name, value := range ` + headerMapsAdapter + `c.GetRespHeaders()) {
		fmt.Println(name, value)
	}
	show(` + headerMapsAdapter + `c.GetReqHeaders()))
	return c.JSON(headers)
}
`,
			msg: "Migrating the header maps of the request context",
		},
		{
			name: "a lookup that tells whether the header is there, and a store into the map",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	value, ok := c.GetReqHeaders()["X-Token"]
	c.GetRespHeaders()["X-Copy"] = value
	if !ok {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	return nil
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	value, ok := ` + headerMapsAdapter + `c.GetReqHeaders())["X-Token"]
	` + headerMapsAdapter + `c.GetRespHeaders())["X-Copy"] = value
	if !ok {
		return c.SendStatus(fiber.StatusUnauthorized)
	}
	return nil
}
`,
			msg: "Migrating the header maps of the request context",
		},
		{
			name: "another name for the context and for fiber, and a wrapper that has a Get of its own",
			in: `package main

import f "github.com/gofiber/fiber/v2"

type web struct{ *f.Ctx }

func (w web) Get(name string) string { return "mine " + name }

func handler(ctx *f.Ctx, w web) string {
	return ctx.GetReqHeaders()["A"] + w.GetReqHeaders()["B"] + w.GetRespHeaders()["C"]
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

type web struct{ *f.Ctx }

func (w web) Get(name string) string { return "mine " + name }

func handler(ctx *f.Ctx, w web) string {
	return ctx.Get("A") + w.Ctx.Get("B") + w.GetRespHeader("C")
}
`,
			msg:   "Migrating the header maps of the request context",
			notes: []string{"main.go:10: c.Get(name) gives the first value of a header the client sent more than once"},
		},
		{
			name: "something that is not a context",
			in: `package main

type headers struct{ values map[string]string }

func (h headers) GetReqHeaders() map[string]string { return h.values }

func use(h headers) string { return h.GetReqHeaders()["X"] }
`,
		},
	})
}

func Test_MigrateHeaderMaps_NotesAndSecondRun(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, headerMapsOn("2.37.1"), []ctxCase{
		{
			name: "a value, and a receiver the file does not tell",
			in: `package main

import (
	"github.com/gofiber/fiber/v2"
	"example.com/other"
)

func handler(c *fiber.Ctx) error {
	get := c.GetReqHeaders
	_ = get
	unknown := other.Make()
	_ = unknown.GetReqHeaders()["X"]
	return nil
}
`,
			notes: []string{
				"main.go:9: GetReqHeaders is used as a value here: it returns a map[string][]string in v3",
				"main.go:12: GetReqHeaders returns a map[string][]string in v3, and the release of Fiber v2 this project is on returned a map[string]string",
			},
		},
	})
}

// A project on v2.50 or later has the methods of v3: nothing to do.
func Test_MigrateHeaderMaps_ProjectsOnTheNewMethods(t *testing.T) {
	t.Parallel()

	for _, release := range []string{"2.50.0", "2.52.15", "3.0.0"} {
		ctxRunCases(t, headerMapsOn(release), []ctxCase{{
			name: release,
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.SendString(c.GetReqHeaders()["X-Real-IP"][0])
}
`,
		}})
	}
}

// Without a version the project is on none that is known to differ.
func Test_MigrateHeaderMaps_NoVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nfunc h(c *fiber.Ctx) string { return c.GetReqHeaders()[\"X\"] }\n"
	file := writeTempFile(t, dir, source)

	require.NoError(t, v3.MigrateHeaderMaps(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, internal.DrainNotes(dir))
}
