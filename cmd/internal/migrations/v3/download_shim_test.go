package v3_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// Ctx.Download of v2 served the byte ranges of the file, v3 does not: a call becomes a call of
// the Download of the module package, which is Ctx.Download of v3 with the ranges.

// downloadModule makes the directory of a case a module, which can have the package.
var downloadModule = map[string]string{"go.mod": "module example.com/app\n\ngo 1.25\n"}

func Test_MigrateSendFileConfig_Download(t *testing.T) {
	t.Parallel()

	const msg = "Migrating SendFile compress flag"
	ctxRunCases(t, v3.MigrateSendFileConfig, []ctxCase{
		{
			name:   "the name is the file",
			others: downloadModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func report(c *fiber.Ctx) error {
	return c.Download("./report.pdf")
}
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func report(c *fiber.Ctx) error {
	return fibermigrate.Download(c, "./report.pdf")
}
`,
			msg: msg,
		},
		{
			name:   "an expression for the name, a chained receiver",
			others: downloadModule,
			in: `package main

import (
	"path/filepath"

	"github.com/gofiber/fiber/v2"
)

func named(c *fiber.Ctx) error {
	return c.Download(filepath.Join("./files", c.Params("name")), "download-"+c.Params("name"))
}

func chained(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).Download("./export.csv", "data.csv")
}
`,
			want: `package main

import (
	"path/filepath"

	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func named(c *fiber.Ctx) error {
	return fibermigrate.Download(c, filepath.Join("./files", c.Params("name")), "download-"+c.Params("name"))
}

func chained(c *fiber.Ctx) error {
	return fibermigrate.Download(c.Status(fiber.StatusOK), "./export.csv", "data.csv")
}
`,
			msg: msg,
		},
		{
			name:   "wherever the call is",
			others: downloadModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func statements(c *fiber.Ctx) error {
	err := c.Download("./a.txt")
	if err != nil {
		return err
	}
	if err := c.Download("./b.txt", "b"); err != nil {
		return err
	}
	defer c.Download("./c.txt")
	go c.Download("./d.txt")
	_ = c.Download(
		"./e.txt", // the file
		"e.txt",   // the name
	)
	names := []string{"n.txt"}
	return wrap(c.Download("./f.txt", names...))
}

func wrap(err error) error { return err }
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func statements(c *fiber.Ctx) error {
	err := fibermigrate.Download(c, "./a.txt")
	if err != nil {
		return err
	}
	if err := fibermigrate.Download(c, "./b.txt", "b"); err != nil {
		return err
	}
	defer fibermigrate.Download(c, "./c.txt")
	go fibermigrate.Download(c, "./d.txt")
	_ = fibermigrate.Download(c,
		"./e.txt", // the file
		"e.txt",   // the name
	)
	names := []string{"n.txt"}
	return wrap(fibermigrate.Download(c, "./f.txt", names...))
}

func wrap(err error) error { return err }
`,
			msg: msg,
		},
		{
			name:   "as a value",
			others: downloadModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

type handlers struct {
	download func(string, ...string) error
}

func values(c *fiber.Ctx) error {
	dl := c.Download
	h := handlers{download: c.Download}
	_ = h
	_ = (*fiber.Ctx).Download
	return dl("./g.txt")
}

func chained(c *fiber.Ctx) func(string, ...string) error {
	return c.Status(fiber.StatusOK).Download
}
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

type handlers struct {
	download func(string, ...string) error
}

func values(c *fiber.Ctx) error {
	dl := func(file string, filename ...string) error {
		return fibermigrate.Download(c, file, filename...)
	}
	h := handlers{download: func(file string, filename ...string) error {
		return fibermigrate.Download(c, file, filename...)
	}}
	_ = h
	_ = func(c fiber.Ctx, file string, filename ...string) error {
		return fibermigrate.Download(c, file, filename...)
	}
	return dl("./g.txt")
}

func chained(c *fiber.Ctx) func(string, ...string) error {
	return func(c fiber.Ctx) func(file string, filename ...string) error {
		return func(file string, filename ...string) error {
			return fibermigrate.Download(c, file, filename...)
		}
	}(c.Status(fiber.StatusOK))
}
`,
			msg: msg,
		},
	})
}

func Test_MigrateSendFileConfig_DownloadReceivers(t *testing.T) {
	t.Parallel()

	const msg = "Migrating SendFile compress flag"
	ctxRunCases(t, v3.MigrateSendFileConfig, []ctxCase{
		{
			name:   "a type that embeds the context hands it over",
			others: downloadModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

type wrapper struct{ *fiber.Ctx }

func (w wrapper) Send(p string) error { return w.Download(p, "w.bin") }

// own declares a Download of its own, which is no business of the migration.
type own struct{ *fiber.Ctx }

func (o own) Download(name string) error { return o.Ctx.Download(name, "own.bin") }

func use(o own) error { return o.Download("x") }
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

type wrapper struct{ *fiber.Ctx }

func (w wrapper) Send(p string) error { return fibermigrate.Download(w.Ctx, p, "w.bin") }

// own declares a Download of its own, which is no business of the migration.
type own struct{ *fiber.Ctx }

func (o own) Download(name string) error { return fibermigrate.Download(o.Ctx, name, "own.bin") }

func use(o own) error { return o.Download("x") }
`,
			msg: msg,
		},
		{
			name:   "other receivers are left alone",
			others: downloadModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

type store struct{}

func (store) Download(name string) error { return nil }

func fetch(c *fiber.Ctx) error {
	var s store
	_ = c
	return s.Download("x")
}
`,
		},
		{
			name:   "an import under another name",
			others: downloadModule,
			in: `package main

import f "github.com/gofiber/fiber/v2"

func report(c *f.Ctx) error {
	dl := c.Download
	_ = dl
	return c.Download("./report.pdf")
}
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	f "github.com/gofiber/fiber/v2"
)

func report(c *f.Ctx) error {
	dl := func(file string, filename ...string) error {
		return fibermigrate.Download(c, file, filename...)
	}
	_ = dl
	return fibermigrate.Download(c, "./report.pdf")
}
`,
			msg: msg,
		},
		{
			name:   "a dot import",
			others: downloadModule,
			in: `package main

import . "github.com/gofiber/fiber/v2"

func report(c *Ctx) func(string, ...string) error {
	_ = (*Ctx).Download
	return c.Download
}
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	. "github.com/gofiber/fiber/v2"
)

func report(c *Ctx) func(string, ...string) error {
	_ = func(c Ctx, file string, filename ...string) error {
		return fibermigrate.Download(c, file, filename...)
	}
	return func(file string, filename ...string) error {
		return fibermigrate.Download(c, file, filename...)
	}
}
`,
			msg: msg,
		},
		{
			name:   "a name of the file that means the package",
			others: downloadModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func shadow(c *fiber.Ctx) error {
	fibermigrate := 1
	_ = fibermigrate
	return c.Download("./h.txt")
}

func plain(c *fiber.Ctx) error { return c.Download("./i.txt") }
`,
			want: `package main

import (
	fibermigrateshim "example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func shadow(c *fiber.Ctx) error {
	fibermigrate := 1
	_ = fibermigrate
	return fibermigrateshim.Download(c, "./h.txt")
}

func plain(c *fiber.Ctx) error { return fibermigrateshim.Download(c, "./i.txt") }
`,
			msg: msg,
		},
	})
}

func Test_MigrateSendFileConfig_DownloadInOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias, function result and field",
			migrate: v3.MigrateSendFileConfig,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func send(c *web.Context, a *web.Ctx, r *web.Request) error {
	_ = c.Download("a.txt")
	_ = a.Download("b.txt")
	_ = web.From(a).Download("c.txt")
	_ = r.Ctx.Download("d.txt", "d")
	return web.Current().Download("e.txt")
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/fibermigrate"
	"example.com/app/internal/web"
)

func send(c *web.Context, a *web.Ctx, r *web.Request) error {
	_ = fibermigrate.Download(c.Ctx, "a.txt")
	_ = fibermigrate.Download(a, "b.txt")
	_ = fibermigrate.Download(web.From(a), "c.txt")
	_ = fibermigrate.Download(r.Ctx, "d.txt", "d")
	return fibermigrate.Download(web.Current().Ctx, "e.txt")
}
`},
			msg: "Migrating SendFile compress flag",
		},
		{
			name:    "a type that only looks like the context",
			migrate: v3.MigrateSendFileConfig,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func send(p web.Plain, h *web.Hider) error {
	_ = p.SendFile("a.txt")
	return h.Download("b.txt")
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/fibermigrate"
	"example.com/app/internal/web"
)

func send(p web.Plain, h *web.Hider) error {
	_ = p.SendFile("a.txt")
	return fibermigrate.Download(h.Ctx, "b.txt")
}
`},
			msg: "Migrating SendFile compress flag",
		},
	})
}

func Test_MigrateSendFileConfig_DownloadTheShimFile(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v2"

func report(c *fiber.Ctx) error { return c.Download("./report.pdf") }
`})
	assert.Contains(t, appRun(t, dir, v3.MigrateSendFileConfig), "Migrating SendFile compress flag")

	shim := appRead(t, dir, "internal/fibermigrate/download.go")
	assert.Contains(t, shim, `This file was added by "fiber migrate": Download with the byte ranges of Fiber v2`)
	assert.Contains(t, shim, "package fibermigrate\n")
	assert.Contains(t, shim, "func Download(c fiber.Ctx, file string, filename ...string) error {")
	assert.NotContains(t, shim, "downloadshim")
	assert.Empty(t, internal.DrainNotes(dir), "the migration says nothing about a call it migrated")

	// a second run leaves the file as it is
	before := appRead(t, dir, "main.go")
	assert.Empty(t, appRun(t, dir, v3.MigrateSendFileConfig))
	assert.Equal(t, before, appRead(t, dir, "main.go"))
	assert.Equal(t, shim, appRead(t, dir, "internal/fibermigrate/download.go"))
}

// A module cannot have the package when it is not one, or when its directory holds code of
// its own: the call stays and says what it lost.
func Test_MigrateSendFileConfig_DownloadWithoutTheShim(t *testing.T) {
	t.Parallel()

	const src = `package main

import "github.com/gofiber/fiber/v2"

func report(c *fiber.Ctx) error { return c.Download("./report.pdf") }
`
	for name, files := range map[string]map[string]string{
		"no module":            {"main.go": src, "go.mod": "this is no module"},
		"a package of its own": {"main.go": src, "internal/fibermigrate/mine.go": "package fibermigrate\n\nfunc Mine() {}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if name == "no module" {
				delete(files, "go.mod")
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600))
				assert.Empty(t, appRun(t, dir, v3.MigrateSendFileConfig))
				assert.Equal(t, src, appRead(t, dir, "main.go"))
				notes := internal.DrainNotes(dir)
				require.Len(t, notes, 1)
				assert.Contains(t, notes[0].String(), "c.Download no longer serves byte ranges")
				return
			}

			dir := appProject(t, files)
			assert.Empty(t, appRun(t, dir, v3.MigrateSendFileConfig))
			assert.Equal(t, src, appRead(t, dir, "main.go"))
			notes := internal.DrainNotes(dir)
			require.Len(t, notes, 1)
			assert.Contains(t, notes[0].String(), "c.Download no longer serves byte ranges")
		})
	}
}

// The review asks for the file name of a download that is not a literal, as it did for the
// call of the context.
func Test_Review_DownloadOfTheModulePackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v2"

func report(c *fiber.Ctx) error { return c.Download(c.Params("name")) }
`})
	appRun(t, dir, v3.MigrateSendFileConfig)
	require.Contains(t, appRead(t, dir, "main.go"), "fibermigrate.Download(c, c.Params(")

	v3.Review(dir)
	var found []string
	for _, n := range internal.DrainNotes(dir) {
		found = append(found, n.String())
	}
	require.NotEmpty(t, found)
	assert.NotContains(t, strings.Join(found, "\n"), "internal/fibermigrate", "the file the migration added is not reviewed")

	var names []string
	for _, note := range found {
		if strings.Contains(note, "takes the file name literally") {
			names = append(names, note)
		}
	}
	if assert.Len(t, names, 1, "%v", found) {
		assert.Contains(t, names[0], "main.go:8: Download takes the file name literally in Fiber v3")
	}
}
