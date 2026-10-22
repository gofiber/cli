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
func Test_MigrationsWriteTheirImports(t *testing.T) {
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
