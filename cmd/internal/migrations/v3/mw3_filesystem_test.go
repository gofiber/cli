package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// Only the files that import the filesystem middleware are touched, and in
// them only what the middleware owns: another struct with a Root or an Index,
// a package of the project with the name filesystem and text stay as they are.
func Test_MigrateFilesystemMiddleware_OnlyTheMiddleware(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

type TreeNode struct {
	Root  string
	Index int
}

type Page struct {
	PageIndex    int
	DocRoot      string
	NotFoundFile string
}

// filesystem.New(filesystem.Config{Root: http.Dir("x")})
var doc = "filesystem.New(filesystem.Config{Root: http.Dir(x)})"

func main() {
	n := TreeNode{Root: "/srv", Index: 7}
	p := Page{PageIndex: 2, DocRoot: "/var/www", NotFoundFile: "x"}
	app := fiber.New()
	app.Use(filesystem.New(filesystem.Config{Root: http.Dir("./public")}))
	_, _ = n, p
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
)

type TreeNode struct {
	Root  string
	Index int
}

type Page struct {
	PageIndex    int
	DocRoot      string
	NotFoundFile string
}

// filesystem.New(filesystem.Config{Root: http.Dir("x")})
var doc = "filesystem.New(filesystem.Config{Root: http.Dir(x)})"

func main() {
	n := TreeNode{Root: "/srv", Index: 7}
	p := Page{PageIndex: 2, DocRoot: "/var/www", NotFoundFile: "x"}
	app := fiber.New()
	app.Use(static.New("", static.Config{FS: os.DirFS("./public")}))
	_, _ = n, p
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateFilesystemMiddleware_LeavesOtherPackages(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, src string }{
		{
			name: "a package of the project that is called filesystem",
			src: `package main

import (
	"github.com/gofiber/fiber/v2"

	"example.com/project/filesystem"
)

func main() {
	store := filesystem.New(filesystem.Config{Base: "/var/data", Root: "x", Index: 1})
	_, _ = store, fiber.New()
}
`,
		},
		{
			name: "a file without the import",
			src: `package main

type options struct {
	Root         string
	Index        string
	NotFoundFile string
}

var o = options{Root: "/", Index: "i", NotFoundFile: "n"}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, res := mw3RunOne(t, tc.src, v3.MigrateFilesystemMiddleware)
			assert.Equal(t, tc.src, got)
			assert.Empty(t, res.out)
			assert.Empty(t, res.notes)
		})
	}
}

// The values are read as expressions, not up to the next comma or bracket.
func Test_MigrateFilesystemMiddleware_ReadsTheValuesAsExpressions(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"embed"
	"net/http"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed dist
var dist embed.FS

type site struct{ files embed.FS }

func (s site) FS() embed.FS { return s.files }

func current() site { return site{files: dist} }

func main() {
	app := fiber.New()
	app.Use("/one", filesystem.New(filesystem.Config{Root: http.Dir("./one")}))
	app.Use("/two", filesystem.New(filesystem.Config{Root: http.Dir("./two"), Index: "home.html"}))
	app.Use("/three", filesystem.New(filesystem.Config{Root: http.Dir("./three"), Browse: true}))
	app.Use("/four", filesystem.New(filesystem.Config{
		Root: http.Dir(filepath.Join(".", "four")),
	}))
	app.Use("/five", filesystem.New(filesystem.Config{
		Root: http.FS(current().FS()),
	}))
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
)

//go:embed dist
var dist embed.FS

type site struct{ files embed.FS }

func (s site) FS() embed.FS { return s.files }

func current() site { return site{files: dist} }

func main() {
	app := fiber.New()
	app.Use("/one", static.New("", static.Config{FS: os.DirFS("./one")}))
	app.Use("/two", static.New("", static.Config{FS: os.DirFS("./two"), IndexNames: []string{"home.html"}}))
	app.Use("/three", static.New("", static.Config{FS: os.DirFS("./three"), Browse: true}))
	app.Use("/four", static.New("", static.Config{
		FS: os.DirFS(filepath.Join(".", "four")),
	}))
	app.Use("/five", static.New("", static.Config{
		FS: current().FS(),
	}))
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateFilesystemMiddleware_ImportsTheFileNeeds(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, in, want string }{
		{
			name: "net/http is dropped once nothing uses it",
			in: `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

func main() {
	app := fiber.New()
	app.Use(filesystem.New(filesystem.Config{Root: http.Dir("./public")}))
}
`,
			want: `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	app.Use(static.New("", static.Config{FS: os.DirFS("./public")}))
}
`,
		},
		{
			name: "net/http stays when something else uses it, os is reused under its name",
			in: `package main

import (
	"net/http"
	stdos "os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

var _ = http.StatusOK

func main() {
	app := fiber.New()
	app.Use(filesystem.New(filesystem.Config{Root: http.Dir(stdos.TempDir())}))
}
`,
			want: `package main

import (
	"net/http"
	stdos "os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
)

var _ = http.StatusOK

func main() {
	app := fiber.New()
	app.Use(static.New("", static.Config{FS: stdos.DirFS(stdos.TempDir())}))
}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, res := mw3RunOne(t, tc.in, v3.MigrateFilesystemMiddleware)
			assert.Equal(t, tc.want, got)
			assert.Empty(t, res.notes)
		})
	}
}

// PathPrefix is the directory of the file system that static serves, the first
// argument of New.
func Test_MigrateFilesystemMiddleware_PathPrefix(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed dist
var distFS embed.FS

func main() {
	app := fiber.New()

	app.Use("/", filesystem.New(filesystem.Config{
		Root:       http.FS(distFS),
		PathPrefix: "dist",
		Index:      "index.html",
	}))

	cfg := filesystem.Config{
		Root:       http.FS(distFS),
		PathPrefix: "/other",
	}
	app.Use("/other", filesystem.New(cfg))

	app.Use("/empty", filesystem.New(filesystem.Config{Root: http.FS(distFS), PathPrefix: ""}))
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"embed"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
)

//go:embed dist
var distFS embed.FS

func main() {
	app := fiber.New()

	app.Use("/", static.New("dist", static.Config{
		FS:         distFS,
		IndexNames: []string{"index.html"},
	}))

	cfg := static.Config{
		FS: distFS,
	}
	app.Use("/other", static.New("/other", cfg))

	app.Use("/empty", static.New("", static.Config{FS: distFS}))
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateFilesystemMiddleware_PathPrefixOfAConfigItCannotFollow(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed dist
var distFS embed.FS

func config() filesystem.Config {
	return filesystem.Config{Root: http.FS(distFS), PathPrefix: "dist"}
}
`, v3.MigrateFilesystemMiddleware)

	// The field has no place in static.Config: it stays, so that the build says
	// where it still is, and is reported.
	assert.Contains(t, got, `PathPrefix: "dist"`)
	require.Len(t, res.notes, 1)
	assert.Equal(t, 14, res.notes[0].Line)
	assert.Contains(t, res.notes[0].Message, "PathPrefix is the first argument of static.New")
}

func Test_MigrateFilesystemMiddleware_SendFile(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed web
var webFS embed.FS

func main() {
	app := fiber.New()
	app.Get("/*", func(c *fiber.Ctx) error {
		return filesystem.SendFile(c, http.FS(webFS), "web/index.html")
	})
	app.Get("/download/:name", func(ctx *fiber.Ctx) error {
		if err := filesystem.SendFile(ctx, http.Dir("./files"), "/"+ctx.Params("name")); err != nil {
			return err
		}
		return nil
	})
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"embed"
	"os"

	"github.com/gofiber/fiber/v2"
)

//go:embed web
var webFS embed.FS

func main() {
	app := fiber.New()
	app.Get("/*", func(c *fiber.Ctx) error {
		return c.SendFile("web/index.html", fiber.SendFile{FS: webFS})
	})
	app.Get("/download/:name", func(ctx *fiber.Ctx) error {
		if err := ctx.SendFile("/"+ctx.Params("name"), fiber.SendFile{FS: os.DirFS("./files")}); err != nil {
			return err
		}
		return nil
	})
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateFilesystemMiddleware_SendFileInAFileWithoutFiber(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

func serve(c Ctx, dir string) error {
	return filesystem.SendFile(c, http.Dir(dir), "index.html")
}
`, v3.MigrateFilesystemMiddleware)

	assert.Contains(t, got, `"github.com/gofiber/fiber/v3"`)
	assert.Contains(t, got, `return c.SendFile("index.html", fiber.SendFile{FS: os.DirFS(dir)})`)
}

// Any http.FileSystem that is not http.Dir or http.FS(fs.FS) has no fs.FS
// counterpart in the standard library: the value stays where it is, which the
// build refuses, and the user is told.
func Test_MigrateFilesystemMiddleware_RootsWithoutACounterpart(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"net/http"

	"github.com/gobuffalo/packr/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/markbates/pkger"
)

var fsys http.FileSystem = http.Dir("/tmp")

func main() {
	app := fiber.New()
	app.Use("/pkger", filesystem.New(filesystem.Config{Root: pkger.Dir("/assets")}))
	app.Use("/packr", filesystem.New(filesystem.Config{Root: packr.New("Assets Box", "/assets"), Browse: true}))
	app.Get("/f", func(c *fiber.Ctx) error {
		return filesystem.SendFile(c, fsys, "x.txt")
	})
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"net/http"

	"github.com/gobuffalo/packr/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/markbates/pkger"
)

var fsys http.FileSystem = http.Dir("/tmp")

func main() {
	app := fiber.New()
	app.Use("/pkger", static.New("", static.Config{FS: pkger.Dir("/assets")}))
	app.Use("/packr", static.New("", static.Config{FS: packr.New("Assets Box", "/assets"), Browse: true}))
	app.Get("/f", func(c *fiber.Ctx) error {
		return c.SendFile("x.txt", fiber.SendFile{FS: fsys})
	})
}
`, got)

	require.Len(t, res.notes, 3)
	assert.Equal(t, []int{16, 17, 19}, []int{res.notes[0].Line, res.notes[1].Line, res.notes[2].Line})
	assert.Contains(t, res.notes[0].Message, `Root pkger.Dir("/assets") is an http.FileSystem`)
	assert.Contains(t, res.notes[1].Message, `Root packr.New("Assets Box", "/assets") is an http.FileSystem`)
	assert.Contains(t, res.notes[2].Message, "the file system fsys is an http.FileSystem")
}

func Test_MigrateFilesystemMiddleware_ContentTypeCharset(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

func main() {
	app := fiber.New()
	app.Use("/utf8", filesystem.New(filesystem.Config{
		Root:               http.Dir("./assets"),
		ContentTypeCharset: "utf-8",
		Browse:             true,
	}))
	app.Use("/iso", filesystem.New(filesystem.Config{
		Root:               http.Dir("./legacy"),
		ContentTypeCharset: "iso-8859-1",
	}))
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	app.Use("/utf8", static.New("", static.Config{
		FS:     os.DirFS("./assets"),
		Browse: true,
	}))
	app.Use("/iso", static.New("", static.Config{
		FS:                 os.DirFS("./legacy"),
		ContentTypeCharset: "iso-8859-1",
	}))
}
`, got)

	// utf-8 is what static does anyway; any other charset is reported.
	require.Len(t, res.notes, 1)
	assert.Equal(t, 19, res.notes[0].Line)
	assert.Contains(t, res.notes[0].Message, `static.Config has no ContentTypeCharset`)
}

func Test_MigrateFilesystemMiddleware_Index(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

const home = "/home.html"

var indexFile = "index.htm"

func main() {
	app := fiber.New()
	// v2 put a "/" in front of an index name that had none, v3 refuses one that has it.
	app.Use("/a", filesystem.New(filesystem.Config{Root: http.Dir("./a"), Index: "/index.html"}))
	app.Use("/b", filesystem.New(filesystem.Config{Root: http.Dir("./b"), Index: "index.html"}))
	app.Use("/c", filesystem.New(filesystem.Config{Root: http.Dir("./c"), Index: home}))
	app.Use("/d", filesystem.New(filesystem.Config{Root: http.Dir("./d"), Index: ""}))
	app.Use("/e", filesystem.New(filesystem.Config{Root: http.Dir("./e"), Index: indexFile}))
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
)

const home = "/home.html"

var indexFile = "index.htm"

func main() {
	app := fiber.New()
	// v2 put a "/" in front of an index name that had none, v3 refuses one that has it.
	app.Use("/a", static.New("", static.Config{FS: os.DirFS("./a"), IndexNames: []string{"index.html"}}))
	app.Use("/b", static.New("", static.Config{FS: os.DirFS("./b"), IndexNames: []string{"index.html"}}))
	app.Use("/c", static.New("", static.Config{FS: os.DirFS("./c"), IndexNames: []string{"home.html"}}))
	app.Use("/d", static.New("", static.Config{FS: os.DirFS("./d")}))
	app.Use("/e", static.New("", static.Config{FS: os.DirFS("./e"), IndexNames: []string{"index.htm"}}))
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateFilesystemMiddleware_IndexOfUnknownValue(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import (
	"net/http"
	"os"

	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

var _ = filesystem.New(filesystem.Config{Root: http.Dir("."), Index: os.Getenv("INDEX")})
`, v3.MigrateFilesystemMiddleware)

	assert.Contains(t, got, `IndexNames: []string{strings.TrimPrefix(os.Getenv("INDEX"), "/")}`)
	assert.Contains(t, got, `"strings"`)
}

func Test_MigrateFilesystemMiddleware_NotFoundFileHandler(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed dist
var distFS embed.FS

const fallback = "/dist/index.html"

func main() {
	app := fiber.New()

	// v2 opens NotFoundFile from the root, not below PathPrefix.
	app.Use("/", filesystem.New(filesystem.Config{
		Root:         http.FS(distFS),
		PathPrefix:   "dist",
		NotFoundFile: fallback,
	}))

	app.Use("/files", filesystem.New(filesystem.Config{
		Root:         http.Dir("./files"),
		NotFoundFile: "404.html",
		Index:        "index.html",
	}))
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"embed"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v3/middleware/static"
)

//go:embed dist
var distFS embed.FS

const fallback = "/dist/index.html"

func main() {
	app := fiber.New()

	// v2 opens NotFoundFile from the root, not below PathPrefix.
	app.Use("/", static.New("dist", static.Config{
		FS: distFS,
		NotFoundHandler: func(c fiber.Ctx) error {
			c.Status(fiber.StatusOK)
			return c.SendFile("dist/index.html", fiber.SendFile{FS: distFS})
		},
	}))

	app.Use("/files", static.New("", static.Config{
		FS: os.DirFS("./files"),
		NotFoundHandler: func(c fiber.Ctx) error {
			c.Status(fiber.StatusOK)
			return c.SendFile("404.html", fiber.SendFile{FS: os.DirFS("./files")})
		},
		IndexNames: []string{"index.html"},
	}))
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateFilesystemMiddleware_NotFoundFileWithoutARoot(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/filesystem"

var _ = filesystem.New(filesystem.Config{NotFoundFile: "index.html"})
`, v3.MigrateFilesystemMiddleware)

	assert.Contains(t, got, `NotFoundFile: "index.html"`)
	require.Len(t, res.notes, 1)
	assert.Contains(t, res.notes[0].Message, "this config has no Root")
}

func Test_MigrateFilesystemMiddleware_ConfigTypeAndFields(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"net/http"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	fs "github.com/gofiber/fiber/v2/middleware/filesystem"
)

func staticConfig(dir string) fs.Config {
	return fs.Config{Root: http.Dir(dir), Browse: true, Index: "/index.html"}
}

type Options struct{ FS fs.Config }

func handler(dir string) fiber.Handler {
	cfg := fs.ConfigDefault
	cfg.Root = http.Dir(filepath.Join(dir, "x"))
	cfg.Index = "index.htm"
	cfg.MaxAge = 60
	cfg.PathPrefix = "sub"
	return fs.New(cfg)
}

func fromOptions(opts Options, extra *fs.Config) fiber.Handler {
	extra.Root = http.Dir("./extra")
	return fs.New(opts.FS)
}

func fromFunction() fiber.Handler {
	cfg := staticConfig("./public")
	cfg.Root = http.Dir("./other")
	return fs.New(cfg)
}

func main() {
	app := fiber.New()
	opts := Options{FS: staticConfig("./public")}
	app.Use("/static", fs.New(opts.FS))
	app.Use("/two", fs.New(fs.Config{Root: http.Dir("./two"), Index: "home.html"}))
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"os"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	fs "github.com/gofiber/fiber/v3/middleware/static"
)

func staticConfig(dir string) fs.Config {
	return fs.Config{FS: os.DirFS(dir), Browse: true, IndexNames: []string{"index.html"}}
}

type Options struct{ FS fs.Config }

func handler(dir string) fiber.Handler {
	cfg := fs.ConfigDefault
	cfg.FS = os.DirFS(filepath.Join(dir, "x"))
	cfg.IndexNames = []string{"index.htm"}
	cfg.MaxAge = 60
	cfg.PathPrefix = "sub"
	return fs.New("", cfg)
}

func fromOptions(opts Options, extra *fs.Config) fiber.Handler {
	extra.FS = os.DirFS("./extra")
	return fs.New("", opts.FS)
}

func fromFunction() fiber.Handler {
	cfg := staticConfig("./public")
	cfg.FS = os.DirFS("./other")
	return fs.New("", cfg)
}

func main() {
	app := fiber.New()
	opts := Options{FS: staticConfig("./public")}
	app.Use("/static", fs.New("", opts.FS))
	app.Use("/two", fs.New("", fs.Config{FS: os.DirFS("./two"), IndexNames: []string{"home.html"}}))
}
`, got)

	// cfg.PathPrefix has no field to go to; it stays for the build to find.
	require.Len(t, res.notes, 1)
	assert.Contains(t, res.notes[0].Message, "cfg.PathPrefix has no field in static.Config")
}

func Test_MigrateFilesystemMiddleware_DotImport(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	. "github.com/gofiber/fiber/v2/middleware/filesystem"
)

func main() {
	app := fiber.New()
	cfg := ConfigDefault
	cfg.Root = http.Dir("./a")
	app.Use("/a", New(cfg))
	app.Use("/b", New(Config{Root: http.Dir("./b"), Index: "b.html"}))
}
`, v3.MigrateFilesystemMiddleware)

	assert.Equal(t, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	. "github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	cfg := ConfigDefault
	cfg.FS = os.DirFS("./a")
	app.Use("/a", New("", cfg))
	app.Use("/b", New("", Config{FS: os.DirFS("./b"), IndexNames: []string{"b.html"}}))
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateFilesystemMiddleware_ChoosesANameThatIsFree(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, in, want string }{
		{
			name: "a local variable called static",
			in: `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

func main() {
	app := fiber.New()
	static := "public"
	app.Use("/a", filesystem.New(filesystem.Config{Root: http.Dir(static)}))
}
`,
			want: `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	staticmw "github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	static := "public"
	app.Use("/a", staticmw.New("", staticmw.Config{FS: os.DirFS(static)}))
}
`,
		},
		{
			name: "the static middleware is imported already",
			in: `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/filesystem"
	"github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	app.Get("/x*", static.New("./x"))
	app.Use("/a", filesystem.New(filesystem.Config{Root: http.Dir("./a"), Browse: true}))
}
`,
			want: `package main

import (
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	app.Get("/x*", static.New("./x"))
	app.Use("/a", static.New("", static.Config{FS: os.DirFS("./a"), Browse: true}))
}
`,
		},
		{
			name: "the static middleware is imported under another name",
			in: `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/filesystem"
	st "github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	app.Get("/x*", st.New("./x"))
	app.Use("/a", filesystem.New(filesystem.Config{Root: http.Dir("./a")}))
}
`,
			want: `package main

import (
	"os"

	"github.com/gofiber/fiber/v3"
	st "github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	app.Get("/x*", st.New("./x"))
	app.Use("/a", st.New("", st.Config{FS: os.DirFS("./a")}))
}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, res := mw3RunOne(t, tc.in, v3.MigrateFilesystemMiddleware)
			assert.Equal(t, tc.want, got)
			assert.Empty(t, res.notes)
		})
	}
}

func Test_MigrateFilesystemMiddleware_NameOfAnotherFile(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"a.go": `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

var _ = filesystem.New(filesystem.Config{Root: http.Dir(".")})
`,
		// static is declared in the package, so it cannot also be an import name
		// of a file of it.
		"b.go": `package main

var static = 1
`,
	}, v3.MigrateFilesystemMiddleware)

	assert.Contains(t, res.files["a.go"], `staticmw "github.com/gofiber/fiber/v3/middleware/static"`)
}

func Test_MigrateFilesystemMiddleware_IdempotentAfterTheRewrite(t *testing.T) {
	t.Parallel()

	got, first := mw3RunOne(t, `package main

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed dist
var distFS embed.FS

func main() {
	app := fiber.New()
	app.Use("/", filesystem.New(filesystem.Config{
		Root:         http.FS(distFS),
		PathPrefix:   "dist",
		NotFoundFile: "dist/index.html",
	}))
	app.Get("/x", func(c *fiber.Ctx) error {
		return filesystem.SendFile(c, http.FS(distFS), "dist/x.html")
	})
}
`, v3.MigrateFilesystemMiddleware)
	require.NotEmpty(t, first.out)

	again, second := mw3RunOne(t, got, v3.MigrateFilesystemMiddleware)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}
