package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3CheckImports fails when the imports of src are not exactly the packages it
// uses, among the ones a migration of this area writes. goimports settles that
// for a project whose module cache it can read; a user's first run has none, and
// then the migration has to get it right on its own.
func mw3CheckImports(t *testing.T, src string) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ParseComments)
	require.NoError(t, err, src)

	// imported holds every import by the name it most likely has, known those
	// whose name the path or an alias says for sure.
	imported, known := map[string]string{}, map[string]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		require.NoError(t, err)
		guess, sure := defaultImportName(path), mw3KnownPackageName(path)
		if spec.Name != nil {
			guess, sure = spec.Name.Name, spec.Name.Name
		}
		if guess != "_" && guess != "." {
			imported[guess] = path
		}
		if sure != "" && sure != "_" && sure != "." {
			known[sure] = path
		}
	}

	used := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
				used[id.Name] = true
			}
		}
		return true
	})

	for name, path := range known {
		assert.True(t, used[name], "%s (%s) is imported and not used in:\n%s", name, path, src)
	}
	for _, name := range []string{"os", "strings", "time", "context", "fmt", "http", "filepath", "embed", "fiber", "extractors", "static", "staticmw", "session", "packr"} {
		if used[name] {
			_, ok := imported[name]
			assert.True(t, ok, "%s is used and not imported in:\n%s", name, src)
		}
	}
}

// mw3Direct runs a content rewrite on src as it leaves the migration, before
// goimports and gofmt, in a directory of its own.
func mw3Direct(t *testing.T, src string, rewrite func(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	fi := internal.FileInfo{Root: dir, Path: path, Rel: "main.go"}
	return rewrite(src, fi, newMw3Pkgs())
}

func Test_mw3ImportEdits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		edit  func(e *mw3ImportEdits)
		wants []string
		wantN []string
	}{
		{
			name: "a standard package goes to a block that has none",
			src:  "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n\nvar _ = fiber.New\nvar _ = os.Args\n",
			edit: func(e *mw3ImportEdits) { e.require("os", "") },
			wants: []string{
				"\"os\"",
				"\"github.com/gofiber/fiber/v3\"",
			},
		},
		{
			name: "a single import becomes a block",
			src:  "package p\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\nvar _ = time.Now\n",
			edit: func(e *mw3ImportEdits) { e.require("time", "") },
			wants: []string{
				"\"fmt\"",
				"\"time\"",
			},
		},
		{
			name: "no imports at all",
			src:  "package p\n\nvar _ = time.Now\n",
			edit: func(e *mw3ImportEdits) { e.require("time", "") },
			wants: []string{
				"\"time\"",
			},
		},
		{
			name:  "an alias",
			src:   "package p\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\nvar _ = fiberextractors.FromCookie\n",
			edit:  func(e *mw3ImportEdits) { e.require(mw3ExtractorsPath, "fiberextractors") },
			wants: []string{"fiberextractors \"github.com/gofiber/fiber/v3/extractors\""},
		},
		{
			name:  "a package that is imported already, under another name",
			src:   "package p\n\nimport t \"time\"\n\nvar _ = t.Now\n",
			edit:  func(e *mw3ImportEdits) { e.require("time", "") },
			wantN: []string{"\"time\"\n\t\"time\""},
		},
		{
			name:  "an import that is not used any more goes",
			src:   "package p\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n)\n\nvar _ = fmt.Sprint\n",
			edit:  func(e *mw3ImportEdits) { e.dropIfUnused("net/http") },
			wantN: []string{"net/http"},
		},
		{
			name:  "an import that is used stays",
			src:   "package p\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n)\n\nvar _ = fmt.Sprint\nvar _ = http.StatusOK\n",
			edit:  func(e *mw3ImportEdits) { e.dropIfUnused("net/http") },
			wants: []string{"net/http"},
		},
		{
			name:  "a variable of the same name does not use it",
			src:   "package p\n\nimport (\n\t\"net/http\"\n)\n\nfunc f() { http := struct{ X int }{}; _ = http.X }\n",
			edit:  func(e *mw3ImportEdits) { e.dropIfUnused("net/http") },
			wantN: []string{"net/http"},
		},
		{
			name: "an import whose name the path does not say stays",
			src:  "package p\n\nimport (\n\t\"github.com/gobuffalo/packr/v2\"\n\t\"github.com/GeertJohan/go.rice\"\n)\n\nvar _ = packr.New\nvar _ = rice.MustFindBox\n",
			edit: func(e *mw3ImportEdits) {
				e.dropIfUnused("github.com/gobuffalo/packr/v2")
				e.dropIfUnused("github.com/GeertJohan/go.rice")
			},
			wants: []string{"packr/v2", "go.rice"},
		},
		{
			name: "an aliased import is known by its alias",
			src:  "package p\n\nimport (\n\tpk \"github.com/gobuffalo/packr/v2\"\n\tun \"github.com/example/unused/v2\"\n)\n\nvar _ = pk.New\n",
			edit: func(e *mw3ImportEdits) {
				e.dropIfUnused("github.com/gobuffalo/packr/v2")
				e.dropIfUnused("github.com/example/unused/v2")
			},
			wants: []string{"packr/v2"},
			wantN: []string{"example/unused"},
		},
		{
			name:  "blank and dot imports stay",
			src:   "package p\n\nimport (\n\t_ \"net/http\"\n\t. \"strings\"\n)\n",
			edit:  func(e *mw3ImportEdits) { e.dropIfUnused("net/http"); e.dropIfUnused("strings") },
			wants: []string{"_ \"net/http\"", ". \"strings\""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var edits mw3ImportEdits
			tc.edit(&edits)
			got := edits.apply(tc.src)
			for _, want := range tc.wants {
				assert.Contains(t, got, want)
			}
			for _, not := range tc.wantN {
				assert.NotContains(t, got, not)
			}
		})
	}
}

func Test_mw3ImportEdits_ReleaseIn(t *testing.T) {
	t.Parallel()

	src := "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n\nvar a = fmt.Sprintf(\"%s\", os.Args[0])\n\nvar b = strings.ToUpper(\"x\")\n"
	g, ok := parseSource(src)
	require.True(t, ok)

	var decl ast.Node
	for _, d := range g.file.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.VAR {
			decl = gd
			break
		}
	}
	require.NotNil(t, decl)

	var edits mw3ImportEdits
	edits.releaseIn(g, decl)
	// The declaration is still in the source, so both stay; once it is gone
	// they do not.
	assert.Equal(t, src, edits.apply(src))

	gone := "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n\nvar b = strings.ToUpper(\"x\")\n"
	out := edits.apply(gone)
	assert.NotContains(t, out, `"fmt"`)
	assert.NotContains(t, out, `"os"`)
	assert.Contains(t, out, `"strings"`)
}

func Test_mw3FreeName(t *testing.T) {
	t.Parallel()

	taken := func(names ...string) func(string) bool {
		return func(name string) bool { return slices.Contains(names, name) }
	}
	assert.Equal(t, "static", mw3FreeName(taken(), "static", "staticmw"))
	assert.Equal(t, "staticmw", mw3FreeName(taken("static"), "static", "staticmw"))
	assert.Equal(t, "staticmw2", mw3FreeName(taken("static", "staticmw"), "static", "staticmw"))
}

// The migrations of this area write their own imports.
func Test_Mw3MigrationsWriteTheirImports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rewrite func(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string
		name    string
		src     string
	}{
		{
			name: "filesystem: http.Dir and the fiber package",
			src: `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v3/middleware/filesystem"
)

func main() {
	_ = filesystem.New(filesystem.Config{Root: http.Dir("./public"), NotFoundFile: "index.html"})
}
`,
			rewrite: mw3FilesystemContent,
		},
		{
			name: "filesystem: http.FS, SendFile and an index in a variable",
			src: `package main

import (
	"embed"
	"net/http"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/filesystem"
)

//go:embed dist
var dist embed.FS

func main() {
	_ = filesystem.New(filesystem.Config{Root: http.FS(dist), Index: os.Getenv("INDEX")})
	_ = func(c fiber.Ctx) error { return filesystem.SendFile(c, http.Dir("."), "x") }
}
`,
			rewrite: mw3FilesystemContent,
		},
		{
			name: "filesystem: the name static is taken",
			src: `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v3/middleware/filesystem"
)

func main() {
	static := "x"
	_ = filesystem.New(filesystem.Config{Root: http.Dir(static)})
}
`,
			rewrite: mw3FilesystemContent,
		},
		{
			name: "filesystem: static is imported already, filesystem is aliased",
			src: `package main

import (
	"net/http"

	fs "github.com/gofiber/fiber/v3/middleware/filesystem"
	"github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	_ = static.New("x")
	_ = fs.New(fs.Config{Root: http.Dir(".")})
}
`,
			rewrite: mw3FilesystemContent,
		},
		{
			name: "filesystem: only SendFile is used",
			src: `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/filesystem"
)

func serve(c fiber.Ctx) error { return filesystem.SendFile(c, http.Dir("."), "x") }
`,
			rewrite: mw3FilesystemContent,
		},
		{
			name: "filesystem: third party roots keep their imports",
			src: `package main

import (
	"github.com/GeertJohan/go.rice"
	"github.com/gobuffalo/packr/v2"
	"github.com/gofiber/fiber/v3/middleware/filesystem"
)

func main() {
	_ = filesystem.New(filesystem.Config{Root: packr.New("Assets Box", "/assets")})
	_ = filesystem.New(filesystem.Config{Root: rice.MustFindBox("assets").HTTPBox()})
}
`,
			rewrite: mw3FilesystemContent,
		},
		{
			name: "session config: a lifetime and no time import",
			src: `package main

import "github.com/gofiber/fiber/v3/middleware/session"

var a = session.New()

var b = session.New(session.Config{CookieSecure: true})
`,
			rewrite: mw3SessionConfigContent,
		},
		{
			name: "session extractor: fmt only built the lookup",
			src: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3/middleware/session"
)

const name = "sid"

var s = session.New(session.Config{KeyLookup: fmt.Sprintf("cookie:%s", name)})
`,
			rewrite: mw3SessionExtractorContent,
		},
		{
			name: "session methods: a context",
			src: `package main

import "github.com/gofiber/fiber/v3/middleware/session"

var store = session.NewStore()

func purge() error { return store.Reset() }
`,
			rewrite: mw3SessionMethodsContent,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := mw3Direct(t, tc.src, tc.rewrite)
			assert.NotEqual(t, tc.src, got)
			mw3CheckImports(t, got)
		})
	}
}

// However the code is laid out, what a migration writes parses and has the
// imports it needs.
func Test_Mw3MigrationsWriteValidGoForAnyLayout(t *testing.T) {
	t.Parallel()

	const sessionHeader = `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

var _ = fmt.Sprint
var _ = time.Now
var _ = fiber.New

`
	const fsHeader = `package main

import (
	"net/http"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

var _ = http.StatusOK
var _ = filepath.Join
var _ = fiber.New

`
	sessionBodies := []string{
		"var s = session.New(\n\tsession.Config{\n\t\tCookieSecure: true,\n\t},\n)",
		"var s = session.New(session.Config{\n\t// only a comment\n})",
		"var s = session.New(session.Config{CookieSecure: true /* inline */, KeyLookup: \"cookie:a\"})",
		"var s = session.New(session.Config{\n\tKeyLookup: \"cookie:a\", // the cookie\n\tCookieSecure: true, // only over https\n})",
		"var s = session.New(session.Config{KeyLookup: `header:X-Token`,\n\tExpiration: time.Hour})",
		"func f(cfgs ...session.Config) *session.Store { return session.New(cfgs...) }",
		"var f = session.New\nvar s = f()",
		"func f[T any](v T) *session.Store {\n\treturn session.New(session.Config{CookieName: fmt.Sprint(v)})\n}",
		"func f() {\n\tgo func() {\n\t\ts := session.New()\n\t\t_ = s\n\t}()\n}",
		"var s = []*session.Store{\n\tsession.New(),\n\tsession.New(session.Config{KeyLookup: \"query:q\"}),\n}",
		"var s = map[string]*session.Store{\"a\": session.New()}",
		"func f(c *fiber.Ctx) error {\n\tstore := session.New()\n\tsess, err := store.Get(c)\n\tif err != nil {\n\t\treturn err\n\t}\n\tsess.SetExpiry(time.Minute)\n\tstore.Delete(sess.ID())\n\treturn store.Reset()\n}",
	}
	fsBodies := []string{
		"var _ = filesystem.New(\n\tfilesystem.Config{\n\t\tRoot: http.Dir(\n\t\t\t\"./x\",\n\t\t),\n\t},\n)",
		"var _ = filesystem.New(filesystem.Config{\n\tRoot: http.Dir(\".\"), // the root\n\tIndex: \"i.html\", // the index\n\tBrowse: true,\n})",
		"var _ = filesystem.New(filesystem.Config{Root: /* c */ http.Dir(\".\"), Index: \"i.html\" /* d */})",
		"func f(cfgs ...filesystem.Config) fiber.Handler { return filesystem.New(cfgs...) }",
		"var cfg = filesystem.Config{\n\tRoot:         http.Dir(filepath.Join(\".\", \"x\")),\n\tNotFoundFile: \"index.html\",\n\tMaxAge:       1,\n}",
		"var h = filesystem.New",
		"func f(c *fiber.Ctx) error {\n\treturn filesystem.SendFile(\n\t\tc,\n\t\thttp.Dir(\".\"),\n\t\t\"x\",\n\t)\n}",
		"func f() filesystem.Config {\n\tvar cfg filesystem.Config\n\tcfg.Root = http.Dir(\".\")\n\tcfg.Index = \"/x.html\"\n\treturn cfg\n}",
		"var _ = filesystem.New(filesystem.Config{Root: http.Dir(\".\"), ContentTypeCharset: \"utf-8\"})",
		"var _ = filesystem.New(filesystem.Config{\n\tContentTypeCharset: \"utf-8\",\n\tRoot:               http.Dir(\".\"),\n})",
	}

	run := func(name, header string, bodies []string, rewrite func(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string) {
		for i, body := range bodies {
			t.Run(name+"/"+strconv.Itoa(i), func(t *testing.T) {
				t.Parallel()

				src := header + body + "\n"
				got := mw3Direct(t, src, rewrite)
				_, err := parser.ParseFile(token.NewFileSet(), "", got, parser.ParseComments)
				require.NoError(t, err, "input:\n%s\noutput:\n%s", src, got)
				mw3CheckImports(t, got)
			})
		}
	}
	run("session config", sessionHeader, sessionBodies, mw3SessionConfigContent)
	run("session extractor", sessionHeader, sessionBodies, mw3SessionExtractorContent)
	run("session store", sessionHeader, sessionBodies, mw3SessionStoreContent)
	run("session methods", sessionHeader, sessionBodies, mw3SessionMethodsContent)
	run("filesystem", fsHeader, fsBodies, mw3FilesystemContent)
}
