package v3_test

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The migrations of the request context follow the context into the other
// packages of the module: a value of a type that another package declares as a
// wrapper of the context, as an alias of it, or as a function result or a field
// that holds it, is a context. The files that use such values import neither the
// fiber package nor the middleware.

// xpkgCase is a migration run on a module. The files are valid v2 code.
type xpkgCase struct {
	migrate ctxMigrateFn
	// files are added to the packages of xpkgBase (a path of it is replaced), by path.
	files map[string]string
	// want are the migrated files (the complete content) by path; a file that is
	// not listed is not looked at.
	want map[string]string
	name string
	// msg is what the migration prints; the other files of the module are migrated
	// too, so that it prints for a case that leaves the file it looks at alone.
	msg string
	// notes are substrings of the notes the run leaves, one for each of them.
	notes []string
}

// xpkgBase is the package every case builds on: the types a project builds around
// the context.
var xpkgBase = map[string]string{
	"internal/web/web.go": ctxSrc(`package web

import "github.com/gofiber/fiber/v2"

// Ctx is the alias the project uses for the context.
type Ctx = fiber.Ctx

// Vars are the variables of a view.
type Vars = fiber.Map

// Context adds the user to the context.
type Context struct {
	*fiber.Ctx
	User string
}

// Hider declares a Redirect of its own.
type Hider struct{ *fiber.Ctx }

func (h *Hider) Redirect(to string, status ...int) error { return h.Ctx.Redirect(to, status...) }

// Request keeps the context.
type Request struct {
	Ctx *fiber.Ctx
}

// Plain has the methods of the context, and nothing to do with it.
type Plain struct{}

func (Plain) QueryInt(string, ...int) int                  { return 0 }
func (Plain) ParamsInt(string, ...int) (int, error)        { return 0, nil }
func (Plain) BodyParser(any) error                         { return nil }
func (Plain) Redirect(string, ...int) error                { return nil }
func (Plain) RedirectBack(string, ...int) error            { return nil }
func (Plain) SendFile(string, ...bool) error               { return nil }
func (Plain) UserContext() string                          { return "" }
func (Plain) Bind(map[string]any) error                    { return nil }
func (Plain) Format(any) error                             { return nil }
func (Plain) Protocol() string                             { return "" }
func (Plain) Locals(string, ...any) any                    { return nil }

// From returns the context it was given.
func From(c *fiber.Ctx) *fiber.Ctx { return c }

// Current returns the context of the request in progress.
func Current() *Context { return nil }

// HandlerFunc is a handler of the project.
type HandlerFunc[T any] func(c *Context, in T) error
`),
}

// xpkgRunCases runs the cases, and runs the migration once more over what it left,
// which must not change a file.
func xpkgRunCases(t *testing.T, cases []xpkgCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files := maps.Clone(xpkgBase)
			for path, src := range tc.files {
				files[path] = ctxSrc(src)
			}
			if _, ok := files["go.mod"]; !ok {
				files["go.mod"] = "module example.com/app\n\ngo 1.25\n"
			}
			dir := t.TempDir()
			for path, src := range files {
				full := filepath.Join(dir, filepath.FromSlash(path))
				require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
				require.NoError(t, os.WriteFile(full, []byte(src), 0o600))
			}

			var out bytes.Buffer
			require.NoError(t, tc.migrate(newCmd(&out), dir, nil, nil))
			after := xpkgReadAll(t, dir)
			notes := internal.DrainNotes(dir)

			for path, want := range tc.want {
				assert.Equal(t, ctxSrc(want), after[path], path)
			}
			if tc.msg != "" {
				assert.Contains(t, out.String(), tc.msg)
			}
			if assert.Len(t, notes, len(tc.notes), "notes: %v", notes) {
				for _, want := range tc.notes {
					assert.True(t, slices.ContainsFunc(notes, func(n internal.Note) bool {
						return strings.Contains(n.String(), want)
					}), "no note contains %q: %v", want, notes)
				}
			}

			var again bytes.Buffer
			require.NoError(t, tc.migrate(newCmd(&again), dir, nil, nil))
			internal.DrainNotes(dir)
			assert.Equal(t, after, xpkgReadAll(t, dir), "the second run changed a file")
			assert.Empty(t, again.String(), "the second run printed")
		})
	}
}

// xpkgReadAll returns the Go files below dir by path.
func xpkgReadAll(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		require.NoError(t, err)
		data, err := os.ReadFile(path) // #nosec G304 -- a file the test wrote
		require.NoError(t, err)
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	}))
	return files
}

const xpkgHandlers = "internal/handlers/handlers.go"

func Test_MigrateGenericHelpers_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias, function result and field",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func page(c *web.Context, a *web.Ctx, r *web.Request) int {
	n := c.QueryInt("page", 1)
	n += a.QueryInt("size", 2)
	n += web.From(a).QueryInt("from", 3)
	n += web.Current().QueryInt("current", 4)
	return n + r.Ctx.QueryInt("req")
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"github.com/gofiber/fiber/v2"
)

func page(c *web.Context, a *web.Ctx, r *web.Request) int {
	n := fiber.Query[int](c.Ctx, "page", 1)
	n += fiber.Query[int](a, "size", 2)
	n += fiber.Query[int](web.From(a), "from", 3)
	n += fiber.Query[int](web.Current().Ctx, "current", 4)
	return n + fiber.Query[int](r.Ctx, "req")
}
`},
			msg: "Migrating generic",
		},
		{
			name:    "ParamsInt and the strconv import",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func id(c *web.Context) (int, error) {
	return c.ParamsInt("id")
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"strconv"

	"example.com/app/internal/web"
)

func id(c *web.Context) (int, error) {
	return strconv.Atoi(c.Params("id"))
}
`},
			notes: []string{"ParamsInt without a default value"},
			msg:   "Migrating generic",
		},
		{
			name:    "the package under another name, and a dot import",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{
				xpkgHandlers: `package handlers

import w "example.com/app/internal/web"

func a(c *w.Context) int { return c.QueryInt("a") }
`,
				"internal/handlers/dot.go": `package handlers

import . "example.com/app/internal/web"

func b(c *Context, d Ctx) int { return c.QueryInt("b") + d.QueryInt("d") }
`,
			},
			want: map[string]string{
				xpkgHandlers: `package handlers

import (
	w "example.com/app/internal/web"
	"github.com/gofiber/fiber/v2"
)

func a(c *w.Context) int { return fiber.Query[int](c.Ctx, "a") }
`,
				"internal/handlers/dot.go": `package handlers

import (
	. "example.com/app/internal/web"
	"github.com/gofiber/fiber/v2"
)

func b(c *Context, d Ctx) int { return fiber.Query[int](c.Ctx, "b") + fiber.Query[int](d, "d") }
`,
			},
			msg: "Migrating generic",
		},
		{
			name:    "the file imports the package of fiber under another name already",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import (
	f "github.com/gofiber/fiber/v2"

	"example.com/app/internal/web"
)

func a(c *web.Context, d *f.Ctx) int { return c.QueryInt("a") + d.QueryInt("d") }
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	f "github.com/gofiber/fiber/v2"

	"example.com/app/internal/web"
)

func a(c *web.Context, d *f.Ctx) int { return f.Query[int](c.Ctx, "a") + f.Query[int](d, "d") }
`},
			msg: "Migrating generic",
		},
		{
			name:    "the file uses the name fiber for something else",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func a(c *web.Context, fiber int) int { return c.QueryInt("a") + fiber }
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	fiberv3 "github.com/gofiber/fiber/v2"
)

func a(c *web.Context, fiber int) int { return fiberv3.Query[int](c.Ctx, "a") + fiber }
`},
			msg: "Migrating generic",
		},
		{
			name:    "a package of the project is called fiber",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{
				"internal/fiber/fiber.go": `package fiber

// Thing is a type of the package.
type Thing struct{}
`,
				xpkgHandlers: `package handlers

import (
	"example.com/app/internal/fiber"
	"example.com/app/internal/web"
)

var _ fiber.Thing

func a(c *web.Context) int { return c.QueryInt("a") }
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/fiber"
	"example.com/app/internal/web"
	fiberv3 "github.com/gofiber/fiber/v2"
)

var _ fiber.Thing

func a(c *web.Context) int { return fiberv3.Query[int](c.Ctx, "a") }
`},
			msg: "Migrating generic",
		},
		{
			name:    "the package that returns the context imports a package of the module that declares it",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{
				"internal/mid/mid.go": `package mid

import "example.com/app/internal/web"

// Make returns a context.
func Make() *web.Context { return nil }
`,
				xpkgHandlers: `package handlers

import "example.com/app/internal/mid"

func a() int { return mid.Make().QueryInt("a") }
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/mid"
	"github.com/gofiber/fiber/v2"
)

func a() int { return fiber.Query[int](mid.Make().Ctx, "a") }
`},
			msg: "Migrating generic",
		},
		{
			name:    "a type with the methods of the context that is none",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func plain(p web.Plain, h func() web.Plain) int {
	id, _ := p.ParamsInt("id")
	return p.QueryInt("a") + h().QueryInt("b") + id
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func plain(p web.Plain, h func() web.Plain) int {
	id, _ := p.ParamsInt("id")
	return p.QueryInt("a") + h().QueryInt("b") + id
}
`},
		},
		{
			name:    "a value that is not known to be a context",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func unknown() int {
	x := lib.Make()
	return x.QueryInt("n")
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func unknown() int {
	x := lib.Make()
	return x.QueryInt("n")
}
`},
			notes: []string{"QueryInt was removed in Fiber v3, but x is not known to be a fiber context here"},
		},
		{
			name:    "a file of a package that imports no package of the module is not looked at",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{"internal/other/other.go": `package other

type reader struct{}

func (reader) QueryInt(string) int { return 0 }

func use(r reader, x interface{ QueryInt(string) int }) int { return r.QueryInt("a") + x.QueryInt("b") }
`},
			want: map[string]string{"internal/other/other.go": `package other

type reader struct{}

func (reader) QueryInt(string) int { return 0 }

func use(r reader, x interface{ QueryInt(string) int }) int { return r.QueryInt("a") + x.QueryInt("b") }
`},
		},
		{
			name:    "a local that is called like the package",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

type holder struct{ Ctx reader }

type reader struct{}

func (reader) QueryInt(string) int { return 0 }

func local(web holder, c *web.Context) int {
	return web.Ctx.QueryInt("a")
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

type holder struct{ Ctx reader }

type reader struct{}

func (reader) QueryInt(string) int { return 0 }

func local(web holder, c *web.Context) int {
	return web.Ctx.QueryInt("a")
}
`},
		},
		{
			name:    "the call written over several lines",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func a(c *web.Context) int {
	return c.
		QueryInt("a",
			1)
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"github.com/gofiber/fiber/v2"
)

func a(c *web.Context) int {
	return fiber.Query[int](c.Ctx, "a",
		1)
}
`},
			msg: "Migrating generic",
		},
		{
			name:    "a literal of a generic func type",
			migrate: v3.MigrateGenericHelpers,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

var typed web.HandlerFunc[string] = func(c *web.Context, in string) error {
	_ = c.QueryInt("n")
	return nil
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"github.com/gofiber/fiber/v2"
)

var typed web.HandlerFunc[string] = func(c *web.Context, in string) error {
	_ = fiber.Query[int](c.Ctx, "n")
	return nil
}
`},
			msg: "Migrating generic",
		},
	})
}

func Test_MigrateParserMethods_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias, function result and field",
			migrate: v3.MigrateParserMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

type form struct{ Name string }

func parse(c *web.Context, a *web.Ctx, r *web.Request) error {
	var f form
	if err := c.BodyParser(&f); err != nil {
		return err
	}
	if err := a.QueryParser(&f); err != nil {
		return err
	}
	if err := web.From(a).CookieParser(&f); err != nil {
		return err
	}
	return r.Ctx.ParamsParser(&f)
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

type form struct{ Name string }

func parse(c *web.Context, a *web.Ctx, r *web.Request) error {
	var f form
	if err := c.Bind().Body(&f); err != nil {
		return err
	}
	if err := a.Bind().Query(&f); err != nil {
		return err
	}
	if err := web.From(a).Bind().Cookie(&f); err != nil {
		return err
	}
	return r.Ctx.Bind().URI(&f)
}
`},
			msg: "Migrating parser methods",
		},
		{
			name:    "a type with the methods of the context that is none",
			migrate: v3.MigrateParserMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func parse(p web.Plain) error { return p.BodyParser(nil) }
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func parse(p web.Plain) error { return p.BodyParser(nil) }
`},
		},
		{
			name:    "a value that is not known to be a context",
			migrate: v3.MigrateParserMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func parse() error { return lib.Make().BodyParser(nil) }
`},
			notes: []string{"BodyParser looks like a Fiber v2 context method"},
		},
	})
}

func Test_MigrateRedirectMethods_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias, function result and field",
			migrate: v3.MigrateRedirectMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func redirect(c *web.Context, a *web.Ctx, r *web.Request) error {
	if c.Query("a") != "" {
		return c.Redirect("/a")
	}
	if a.Query("b") != "" {
		return a.Redirect("/b", 301)
	}
	if r.Ctx.Query("c") != "" {
		return web.From(a).RedirectToRoute("home", web.Vars{"id": 1})
	}
	return web.Current().RedirectBack("/")
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"github.com/gofiber/fiber/v2"
)

func redirect(c *web.Context, a *web.Ctx, r *web.Request) error {
	if c.Query("a") != "" {
		return c.Redirect().Status(fiber.StatusFound).To("/a")
	}
	if a.Query("b") != "" {
		return a.Redirect().Status(301).To("/b")
	}
	if r.Ctx.Query("c") != "" {
		return web.From(a).Redirect().Status(fiber.StatusFound).Route("home", fiber.RedirectConfig{Params: web.Vars{"id": 1}})
	}
	return web.Current().Redirect().Status(fiber.StatusFound).Back("/")
}
`},
			msg: "Migrating redirect methods",
		},
		{
			name:    "a wrapper with a Redirect of its own",
			migrate: v3.MigrateRedirectMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func redirect(h *web.Hider) error {
	if h.Query("a") != "" {
		return h.Redirect("/own")
	}
	return h.RedirectToRoute("home", nil)
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"github.com/gofiber/fiber/v2"
)

func redirect(h *web.Hider) error {
	if h.Query("a") != "" {
		return h.Redirect("/own")
	}
	return h.Ctx.Redirect().Status(fiber.StatusFound).Route("home")
}
`},
			msg: "Migrating redirect methods",
		},
		{
			name:    "a type with the methods of the context that is none",
			migrate: v3.MigrateRedirectMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func redirect(p web.Plain) error {
	_ = p.Redirect("/x")
	return p.RedirectBack("/")
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func redirect(p web.Plain) error {
	_ = p.Redirect("/x")
	return p.RedirectBack("/")
}
`},
		},
		{
			name:    "a value that is not known to be a context",
			migrate: v3.MigrateRedirectMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func redirect() error {
	_ = lib.Make().Redirect("/x")
	return lib.Make().RedirectBack("/")
}
`},
			notes: []string{"RedirectBack looks like a Fiber v2 context method"},
		},
	})
}

func Test_MigrateSendFileConfig_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias and field",
			migrate: v3.MigrateSendFileConfig,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func send(c *web.Context, a *web.Ctx, r *web.Request) error {
	if err := c.SendFile("./a.txt", true); err != nil {
		return err
	}
	if err := a.SendFile("./b.txt"); err != nil {
		return err
	}
	return r.Ctx.SendFile("./c.txt", false)
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"github.com/gofiber/fiber/v2"
)

func send(c *web.Context, a *web.Ctx, r *web.Request) error {
	if err := c.SendFile("./a.txt", fiber.SendFile{Compress: true, ByteRange: true}); err != nil {
		return err
	}
	if err := a.SendFile("./b.txt", fiber.SendFile{ByteRange: true}); err != nil {
		return err
	}
	return r.Ctx.SendFile("./c.txt", fiber.SendFile{ByteRange: true})
}
`},
			msg: "Migrating SendFile",
		},
		{
			name:    "a type with a SendFile of its own that is no context",
			migrate: v3.MigrateSendFileConfig,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func send(p web.Plain) error { return p.SendFile("./a.txt", true) }
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func send(p web.Plain) error { return p.SendFile("./a.txt", true) }
`},
		},
		{
			name:    "a value that is not known to be a context",
			migrate: v3.MigrateSendFileConfig,
			files: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func send() error { return lib.Make().SendFile("./a.txt", true) }
`},
			notes: []string{"if the receiver is a fiber context, the flag is now fiber.SendFile{Compress: true, ByteRange: true}"},
		},
	})
}

func Test_MigrateViewBind_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias, field and a wrapper with a Bind of its own",
			migrate: v3.MigrateViewBind,
			files: map[string]string{
				"internal/web/viewer.go": `package web

import "github.com/gofiber/fiber/v2"

// Viewer declares a Bind of its own.
type Viewer struct{ *fiber.Ctx }

func (v *Viewer) Bind(vars fiber.Map) error { return v.Ctx.Bind(vars) }
`,
				xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func view(c *web.Context, a *web.Ctx, r *web.Request, v *web.Viewer) error {
	_ = c.Bind(web.Vars{"a": 1})
	_ = a.Bind(web.Vars{"b": 2})
	_ = r.Ctx.Bind(web.Vars{"c": 3})
	return v.Bind(web.Vars{"d": 4})
}
`,
			},
			want: map[string]string{
				"internal/web/viewer.go": `package web

import "github.com/gofiber/fiber/v2"

// Viewer declares a Bind of its own.
type Viewer struct{ *fiber.Ctx }

func (v *Viewer) Bind(vars fiber.Map) error { return v.Ctx.ViewBind(vars) }
`,
				xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func view(c *web.Context, a *web.Ctx, r *web.Request, v *web.Viewer) error {
	_ = c.ViewBind(web.Vars{"a": 1})
	_ = a.ViewBind(web.Vars{"b": 2})
	_ = r.Ctx.ViewBind(web.Vars{"c": 3})
	return v.Bind(web.Vars{"d": 4})
}
`,
			},
			msg: "Migrating view binding",
		},
		{
			name:    "a type with a Bind of its own that is no context",
			migrate: v3.MigrateViewBind,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func view(p web.Plain) error { return p.Bind(map[string]any{"a": 1}) }
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func view(p web.Plain) error { return p.Bind(map[string]any{"a": 1}) }
`},
		},
	})
}

func Test_MigrateCtxRenames_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias and field",
			migrate: v3.MigrateCtxRenames,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func names(c *web.Context, a *web.Ctx, r *web.Request) string {
	_ = c.Format("body")
	return c.Protocol() + a.Hostname() + r.Ctx.Protocol()
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func names(c *web.Context, a *web.Ctx, r *web.Request) string {
	_ = c.AutoFormat("body")
	return c.Scheme() + a.Host() + r.Ctx.Scheme()
}
`},
			notes: []string{"AutoFormat negotiates the same formats"},
			msg:   "Migrating context method renames",
		},
		{
			name:    "a type with the same method names that is no context",
			migrate: v3.MigrateCtxRenames,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func names(p web.Plain) string {
	_ = p.Format("body")
	return p.Protocol()
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func names(p web.Plain) string {
	_ = p.Format("body")
	return p.Protocol()
}
`},
		},
		{
			name:    "the ranges that a function of another package returns",
			migrate: v3.MigrateCtxRenames,
			files: map[string]string{
				"internal/rng/rng.go": `package rng

import "github.com/gofiber/fiber/v2"

// Asked is what a request asked for.
type Asked struct {
	Range fiber.Range
}

// Of reads the range a request asked for.
func Of(c *fiber.Ctx, size int) (fiber.Range, error) { return c.Range(size) }

// Ask keeps the range.
func Ask(c *fiber.Ctx, size int) *Asked {
	r, _ := c.Range(size)
	return &Asked{Range: r}
}
`,
				xpkgHandlers: `package handlers

import (
	"example.com/app/internal/rng"
	"example.com/app/internal/web"
)

func ranges(c *web.Context) int {
	r, _ := rng.Of(c.Ctx, 10)
	total := 0
	for _, s := range r.Ranges {
		total += s.End - s.Start
	}
	asked := rng.Ask(c.Ctx, 10)
	first := asked.Range.Ranges[0]
	return total + first.End + asked.Range.Ranges[0].Start
}
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/rng"
	"example.com/app/internal/web"
)

func ranges(c *web.Context) int {
	r, _ := rng.Of(c.Ctx, 10)
	total := 0
	for _, s := range r.Ranges {
		total += int(s.End) - int(s.Start)
	}
	asked := rng.Ask(c.Ctx, 10)
	first := asked.Range.Ranges[0]
	return total + int(first.End) + int(asked.Range.Ranges[0].Start)
}
`},
			msg: "Migrating context method renames",
		},
	})
}

func Test_MigrateContextMethods_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "wrapper, alias and field",
			migrate: v3.MigrateContextMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import (
	"context"

	"example.com/app/internal/web"
)

func ctx(c *web.Context, a *web.Ctx, r *web.Request) context.Context {
	base := c.UserContext()
	a.SetUserContext(context.WithValue(base, "k", "v"))
	_ = r.Ctx.Context()
	return r.Ctx.UserContext()
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"context"

	"example.com/app/internal/web"
)

func ctx(c *web.Context, a *web.Ctx, r *web.Request) context.Context {
	base := c.Context()
	a.SetContext(context.WithValue(base, "k", "v"))
	_ = r.Ctx.RequestCtx()
	return r.Ctx.Context()
}

// fiber:context-methods migrated
`},
			msg: "Migrating context methods",
		},
		{
			name:    "a type with a UserContext of its own that is no context",
			migrate: v3.MigrateContextMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func ctx(p web.Plain) string { return p.UserContext() }
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

func ctx(p web.Plain) string { return p.UserContext() }
`},
		},
		{
			name:    "a value that is not known to be a context",
			migrate: v3.MigrateContextMethods,
			files: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/web"
	"example.org/lib"
)

var _ web.Ctx

func ctx() any { return lib.Make().UserContext() }
`},
			notes: []string{"UserContext was renamed Context in Fiber v3, but lib.Make() is not known to be a fiber context here"},
		},
	})
}
