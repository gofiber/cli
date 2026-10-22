package v3

import (
	"go/ast"
	"go/parser"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// modTestFiles writes the files (path relative to the module to source) into a new
// directory, which gets a go.mod unless the files have one, and returns it.
func modTestFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	files = maps.Clone(files)
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/app\n\ngo 1.25\n"
	}
	for name, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(src), 0o600))
	}
	return dir
}

// modTestIndex writes the files and returns the index of their module, asked for
// the way a migration of the directory would.
func modTestIndex(t *testing.T, files map[string]string) (*moduleIndex, string) {
	t.Helper()

	dir := modTestFiles(t, files)
	ix := newModuleIndex(internal.FileInfo{Root: dir, Path: filepath.Join(dir, "main.go"), Rel: "main.go"})
	require.NotNil(t, ix)
	return ix, dir
}

// modTestModule is a module of the shape real projects have: a command that does
// not import fiber and calls into internal packages, one of which exports apps.
func modTestModule() map[string]string {
	return map[string]string{
		"cmd/api/main.go": `package main

import (
	"log"

	"example.com/app/internal/server"
)

func main() {
	log.Fatal(server.New().Listen(":3000"))
}
`,
		"internal/server/server.go": `package server

import "github.com/gofiber/fiber/v3"

type App = fiber.App

type Server struct {
	App    *fiber.App
	Apps   []*fiber.App
	byName map[string]*fiber.App
	extra  *App
	name   string
}

var (
	Default       = New()
	Plain         = &fiber.App{}
	Typed  *fiber.App
	first, second = pair()
)

const Name = "server"

func New() *fiber.App { return fiber.New() }

func Aliased() *App { return nil }

func pair() (*fiber.App, error) { return nil, nil }

func (s *Server) Run() error { return nil }

func (s *Server) First() *fiber.App { return s.App }

func init() {}
`,
		"internal/server/server_test.go": `package server

import "github.com/gofiber/fiber/v3"

func newTestApp() *fiber.App { return fiber.New() }
`,
		"internal/server/server_x_test.go": `package server_test

import "example.com/app/internal/server"

var _ = server.New
`,
		"internal/auth/auth.go": `package authx

import "github.com/gofiber/fiber/v3/middleware/session"

var Store = session.NewStore()

func NewStore() *session.Store { return session.New() }
`,
		"main.go": "package main\n\nfunc main() {}\n",
	}
}

var (
	testFiberApp = modTarget{
		pkg:   regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+$`),
		types: []string{"App"},
		funcs: []string{"New"},
	}
	testSessionStore = modTarget{
		pkg:   regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/session$`),
		types: []string{"Store"},
		funcs: []string{"New", "NewStore"},
	}
)

func Test_NewModuleIndex(t *testing.T) {
	t.Parallel()

	t.Run("the module of the file", func(t *testing.T) {
		t.Parallel()

		ix, dir := modTestIndex(t, modTestModule())

		assert.Equal(t, "example.com/app", ix.mod.path)
		assert.Equal(t, dir, ix.mod.root)
	})

	t.Run("nil outside of a module", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600))

		assert.Nil(t, newModuleIndex(internal.FileInfo{Root: dir, Path: filepath.Join(dir, "main.go")}))
	})

	t.Run("one index per module and run", func(t *testing.T) {
		t.Parallel()

		files := modTestModule()
		files["tools/go.mod"] = "module example.com/tools\n\ngo 1.25\n"
		files["tools/gen/gen.go"] = "package gen\n"
		dir := modTestFiles(t, files)

		var cache moduleIndexes
		file := func(rel string) internal.FileInfo {
			return internal.FileInfo{Root: dir, Path: filepath.Join(dir, filepath.FromSlash(rel)), Rel: rel}
		}

		outer := cache.of(file("cmd/api/main.go"))
		require.NotNil(t, outer)
		assert.Same(t, outer, cache.of(file("internal/server/server.go")))

		inner := cache.of(file("tools/gen/gen.go"))
		require.NotNil(t, inner)
		assert.NotSame(t, outer, inner)
		assert.Equal(t, "example.com/tools", inner.mod.path)

		other := t.TempDir()
		assert.Nil(t, cache.of(internal.FileInfo{Root: other, Path: filepath.Join(other, "main.go")}))
	})
}

func Test_ModuleIndex_Packages(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	files := modTestModule()
	files["vendor/example.com/dep/dep.go"] = "package dep\n"
	files["testdata/fixture.go"] = "package fixture\n"
	files["_skipped/skipped.go"] = "package skipped\n"
	files[".hidden/hidden.go"] = "package hidden\n"
	files["web/node_modules/pkg/pkg.go"] = "package pkg\n"
	files["tools/go.mod"] = "module example.com/tools\n\ngo 1.25\n"
	files["tools/gen/gen.go"] = "package gen\n"
	files["gen/gen.go"] = "//go:build ignore\n\npackage main\n"
	files["broken/broken.go"] = "package broken\n\nfunc ( {\n"
	files["broken/ok.go"] = "package broken\n\nfunc Fine() {}\n"
	files["mixed/a.go"] = "package a\n"
	files["mixed/b.go"] = "package a\n"
	files["mixed/c.go"] = "package c\n"
	files["onlytests/x_test.go"] = "package onlytests\n"
	files["docs/README.md"] = "# no go files\n"
	files["internal/server/_ignored.go"] = "package server\n\nfunc Ignored() {}\n"
	ix, dir := modTestIndex(t, files)

	t.Run("by import path", func(t *testing.T) {
		server := ix.packageAt("example.com/app/internal/server")
		require.NotNil(t, server)
		assert.Equal(t, "server", server.name)
		assert.Equal(t, "example.com/app/internal/server", server.importPath)
		assert.Equal(t, filepath.Join(dir, "internal", "server"), server.dir)
		assert.True(t, server.complete)
		assert.False(t, server.external)

		auth := ix.packageAt("example.com/app/internal/auth")
		require.NotNil(t, auth)
		assert.Equal(t, "authx", auth.name, "the name is the package clause, not the directory")

		root := ix.packageAt("example.com/app")
		require.NotNil(t, root)
		assert.Equal(t, "main", root.name)
		assert.Equal(t, "example.com/app", root.importPath)
	})

	t.Run("files of a package: the package first, its tests after, the external tests apart", func(t *testing.T) {
		server := ix.packageAt("example.com/app/internal/server")
		require.NotNil(t, server)

		var names []string
		for _, f := range server.files {
			names = append(names, filepath.Base(f.path))
			assert.Same(t, server, f.pkg)
		}
		assert.Equal(t, []string{"server.go", "server_test.go"}, names, "the file whose name starts with _ is not a file of the package")

		md := ix.dir(server.dir)
		require.NotNil(t, md.xtest)
		assert.Equal(t, "server_test", md.xtest.name)
		assert.True(t, md.xtest.external)
		assert.Empty(t, md.xtest.importPath, "the external tests cannot be imported")
		require.Len(t, md.xtest.files, 1)
		assert.Same(t, md.xtest, md.xtest.files[0].pkg)
		assert.True(t, md.xtest.files[0].test)
	})

	t.Run("what is not part of the module", func(t *testing.T) {
		for _, p := range []string{
			"example.com/app/vendor/example.com/dep", "example.com/app/testdata", "example.com/app/_skipped",
			"example.com/app/.hidden", "example.com/app/web/node_modules/pkg", "example.com/app/tools/gen",
			"example.com/tools/gen", "example.com/app/gen", "example.com/app/docs", "example.com/app/missing",
			"example.com/other/internal/server", "example.com/application",
		} {
			assert.Nil(t, ix.packageAt(p), p)
		}
	})

	t.Run("a directory that is not whole", func(t *testing.T) {
		broken := ix.packageAt("example.com/app/broken")
		require.NotNil(t, broken)
		assert.False(t, broken.complete, "a file that does not parse may declare anything")
		assert.NotNil(t, broken.lookup("Fine"))

		mixed := ix.packageAt("example.com/app/mixed")
		require.NotNil(t, mixed)
		assert.Equal(t, "a", mixed.name, "the clause most files have")
		assert.False(t, mixed.complete)

		onlyTests := ix.packageAt("example.com/app/onlytests")
		require.NotNil(t, onlyTests)
		assert.Equal(t, "onlytests", onlyTests.name)
	})

	t.Run("every package in the order of the directories", func(t *testing.T) {
		var got []string
		for _, p := range ix.packages() {
			got = append(got, p.importPath)
		}
		assert.Equal(t, []string{
			"example.com/app",
			"example.com/app/broken",
			"example.com/app/cmd/api",
			"example.com/app/internal/auth",
			"example.com/app/internal/server",
			"example.com/app/mixed",
			"example.com/app/onlytests",
		}, got)

		var withTests []string
		for _, p := range ix.allPackages() {
			withTests = append(withTests, p.name)
		}
		assert.Contains(t, withTests, "server_test")
	})

	t.Run("import paths and directories", func(t *testing.T) {
		got, ok := ix.importPathOf(filepath.Join(dir, "internal", "auth"))
		assert.True(t, ok)
		assert.Equal(t, "example.com/app/internal/auth", got)

		got, ok = ix.importPathOf(dir)
		assert.True(t, ok)
		assert.Equal(t, "example.com/app", got)

		_, ok = ix.importPathOf(filepath.Join(dir, "tools", "gen"))
		assert.False(t, ok, "a nested module has its own paths")

		d, ok := ix.dirOf("example.com/app/cmd/api")
		assert.True(t, ok)
		assert.Equal(t, filepath.Join(dir, "cmd", "api"), d)

		_, ok = ix.dirOf("example.com/applications")
		assert.False(t, ok)
	})
}

func Test_ModuleIndex_Declarations(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	files := modTestModule()
	files["internal/server/platform_linux.go"] = "package server\n\nfunc platform() string { return \"linux\" }\n"
	files["internal/server/platform_other.go"] = "package server\n\nfunc platform() string { return \"other\" }\n"
	files["internal/server/generic.go"] = `package server

type Set[T comparable] struct{ items map[T]bool }

func (s *Set[T]) Add(v T) {}

type Pair[K comparable, V any] struct{}

func (p Pair[K, V]) Key() K { var k K; return k }
`
	ix, _ := modTestIndex(t, files)
	server := ix.packageAt("example.com/app/internal/server")
	require.NotNil(t, server)

	t.Run("by kind", func(t *testing.T) {
		cases := []struct {
			name string
			kind modKind
		}{
			{"New", modFunc},
			{"Server", modTypeDecl},
			{"App", modTypeDecl},
			{"Default", modVar},
			{"Typed", modVar},
			{"first", modVar},
			{"second", modVar},
			{"Name", modConst},
			{"Set", modTypeDecl},
		}
		for _, c := range cases {
			d := server.lookup(c.name)
			require.NotNil(t, d, c.name)
			assert.Equal(t, c.kind, d.kind, c.name)
			assert.NotNil(t, d.node(), c.name)
			assert.False(t, d.dup, c.name)
		}
		assert.True(t, server.lookup("New").exported())
		assert.False(t, server.lookup("pair").exported())
	})

	t.Run("init and the blank identifier declare nothing", func(t *testing.T) {
		assert.Nil(t, server.lookup("init"))
		assert.Nil(t, server.lookup("_"))
		assert.Nil(t, server.lookup("Missing"))
	})

	t.Run("a name declared for several platforms", func(t *testing.T) {
		d := server.lookup("platform")
		require.NotNil(t, d)
		assert.True(t, d.dup)
		assert.Equal(t, "platform_linux.go", filepath.Base(d.file.path), "the first declaration")
	})

	t.Run("the names of a group and of a shared call", func(t *testing.T) {
		first, second := server.lookup("first"), server.lookup("second")
		require.NotNil(t, first)
		require.NotNil(t, second)
		assert.Same(t, first.value, second.value)

		e, idx, ok := first.varInit()
		require.True(t, ok)
		assert.IsType(t, &ast.CallExpr{}, e)
		assert.Equal(t, 0, idx)
		_, idx, _ = second.varInit()
		assert.Equal(t, 1, idx, "the second value of the call")
	})

	t.Run("variables", func(t *testing.T) {
		typed := server.lookup("Typed")
		require.NotNil(t, typed)
		typ, ok := typed.varType()
		assert.True(t, ok)
		assert.True(t, typ.is(testFiberApp))
		_, _, ok = typed.varInit()
		assert.False(t, ok)

		def := server.lookup("Default")
		require.NotNil(t, def)
		_, ok = def.varType()
		assert.False(t, ok)
		e, _, ok := def.varInit()
		assert.True(t, ok)
		assert.IsType(t, &ast.CallExpr{}, e)
	})

	t.Run("methods by receiver", func(t *testing.T) {
		run := server.method("Server", "Run")
		require.NotNil(t, run)
		assert.Equal(t, modMethod, run.kind)
		assert.Equal(t, "Server", run.recv)
		assert.Nil(t, server.lookup("Run"), "a method is no package level name")
		assert.Nil(t, server.method("Server", "Missing"))
		assert.Nil(t, server.method("Other", "Run"))

		assert.NotNil(t, server.method("Set", "Add"), "a generic receiver")
		assert.NotNil(t, server.method("Pair", "Key"), "a receiver with several type parameters")
	})

	t.Run("signatures", func(t *testing.T) {
		pair := server.lookup("pair")
		require.NotNil(t, pair)
		results := pair.results()
		require.Len(t, results, 2)
		assert.True(t, results[0].is(testFiberApp))
		assert.False(t, results[1].is(testFiberApp))
		assert.Empty(t, server.lookup("Name").results())

		add := server.method("Set", "Add")
		require.NotNil(t, add)
		params := add.params()
		require.Len(t, params, 1)
		assert.Equal(t, "v", params[0].name)
	})

	t.Run("test files are seen by the tests of the package only", func(t *testing.T) {
		assert.Nil(t, server.lookup("newTestApp"))

		testFile := ix.fileAt(filepath.Join(server.dir, "server_test.go"))
		require.NotNil(t, testFile)
		assert.True(t, testFile.test)
		assert.NotNil(t, server.lookupIn(testFile, "newTestApp"))
		assert.NotNil(t, server.lookupIn(testFile, "New"), "and everything the package declares")

		libFile := ix.fileAt(filepath.Join(server.dir, "server.go"))
		require.NotNil(t, libFile)
		assert.Nil(t, server.lookupIn(libFile, "newTestApp"))

		xtest := ix.dir(server.dir).xtest
		require.NotNil(t, xtest)
		assert.Nil(t, xtest.lookup("newTestApp"), "the external tests are another package")
	})

	t.Run("the declarations of a package", func(t *testing.T) {
		var names []string
		for _, d := range server.decls(false) {
			names = append(names, d.name)
		}
		assert.Contains(t, names, "New")
		assert.Contains(t, names, "Run", "methods are declarations too")
		assert.NotContains(t, names, "newTestApp")

		var withTests []string
		for _, d := range server.decls(true) {
			withTests = append(withTests, d.name)
		}
		assert.Contains(t, withTests, "newTestApp")
	})
}

func Test_ModFile_Imports(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	files := map[string]string{
		"lib/lib.go": "package lib\n",
		"v2/v2.go":   "package thing\n",
		"util/u.go":  "package util\n",
		"main.go": `package main

import (
	"fmt"
	alias "example.com/app/lib"
	"example.com/app/v2"
	. "example.com/app/util"
	_ "example.com/app/blank"
	yaml "gopkg.in/yaml.v3"
	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v3"
	f2 "github.com/gofiber/fiber/v2"
)

func main() { fmt.Println() }
`,
	}
	ix, dir := modTestIndex(t, files)
	f := ix.fileAt(filepath.Join(dir, "main.go"))
	require.NotNil(t, f)

	t.Run("the name an import binds", func(t *testing.T) {
		cases := map[string]string{
			"fmt":   "fmt",
			"alias": "example.com/app/lib",
			"thing": "example.com/app/v2", // the package clause, not the last element of the path
			"redis": "github.com/go-redis/redis/v8",
			"fiber": "github.com/gofiber/fiber/v3",
			"f2":    "github.com/gofiber/fiber/v2",
			"yaml":  "gopkg.in/yaml.v3",
		}
		for name, importPath := range cases {
			imp, ok := f.importFor(name)
			require.True(t, ok, name)
			assert.Equal(t, importPath, imp.path, name)
		}

		for _, name := range []string{"lib", "v2", "util", "blank", "_", ".", "", "missing"} {
			_, ok := f.importFor(name)
			assert.False(t, ok, "%q is not bound", name)
		}
	})

	t.Run("a package of the module", func(t *testing.T) {
		imp, ok := f.importFor("alias")
		require.True(t, ok)
		require.NotNil(t, imp.pkg())
		assert.Equal(t, "lib", imp.pkg().name)

		fiber, ok := f.importFor("fiber")
		require.True(t, ok)
		assert.Nil(t, fiber.pkg(), "fiber is not in the module")
	})

	t.Run("dot imports", func(t *testing.T) {
		dots := f.dotImports()
		require.Len(t, dots, 1)
		assert.Equal(t, "example.com/app/util", dots[0].path)
		assert.Empty(t, dots[0].binds())
	})

	t.Run("the names for a path pattern", func(t *testing.T) {
		fiberRe := regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+$`)
		assert.Equal(t, []string{"f2", "fiber"}, f.importNames(fiberRe))
		assert.Equal(t, []string{"jwtware"}, f.importNames(regexp.MustCompile(`redis`), "jwtware"), "the names the caller knows the package by")
		assert.Empty(t, f.importNames(regexp.MustCompile(`blank`)), "blank imports bind nothing")
		assert.Equal(t, []string{"."}, f.importNames(regexp.MustCompile(`util`)))
	})

	t.Run("which files import a package", func(t *testing.T) {
		assert.True(t, f.imports("fmt"))
		assert.False(t, f.imports("os"))
	})
}

func Test_ModFile_DeclOf(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	files := modTestModule()
	files["internal/util/util.go"] = "package util\n\nfunc Helper() {}\n\ntype Box[T any] struct{}\n"
	files["internal/server/use.go"] = `package server

import (
	"example.com/app/internal/auth"
	. "example.com/app/internal/util"
)

func use() {
	_ = authx.Store
	Helper()
	_ = Box[int]{}
}
`
	ix, _ := modTestIndex(t, files)
	server := ix.packageAt("example.com/app/internal/server")
	require.NotNil(t, server)
	use := ix.fileAt(filepath.Join(server.dir, "use.go"))
	require.NotNil(t, use)

	exprs := map[string]ast.Expr{}
	use.g.walk(func(n ast.Node, _ []ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			exprs["authx.Store"] = x
		case *ast.CallExpr:
			exprs["Helper()"] = x.Fun
		case *ast.CompositeLit:
			exprs["Box[int]"] = x.Type
		}
		return true
	})

	t.Run("qualified by an import", func(t *testing.T) {
		d := use.declOf(exprs["authx.Store"])
		require.NotNil(t, d)
		assert.Equal(t, modVar, d.kind)
		assert.Equal(t, "example.com/app/internal/auth", d.file.pkg.importPath)
	})

	t.Run("the import a selector goes through", func(t *testing.T) {
		sel, ok := exprs["authx.Store"].(*ast.SelectorExpr)
		require.True(t, ok)
		imp, found := use.qualifier(sel)
		require.True(t, found)
		assert.Equal(t, "example.com/app/internal/auth", imp.path)

		chain, err := parser.ParseExpr("a.b.c")
		require.NoError(t, err)
		chainSel, isSel := chain.(*ast.SelectorExpr)
		require.True(t, isSel)
		_, found = use.qualifier(chainSel)
		assert.False(t, found, "a.b is not a package")
	})

	t.Run("dot imported", func(t *testing.T) {
		d := use.declOf(exprs["Helper()"])
		require.NotNil(t, d)
		assert.Equal(t, modFunc, d.kind)
		assert.Equal(t, "example.com/app/internal/util", d.file.pkg.importPath)
	})

	t.Run("generic instantiation", func(t *testing.T) {
		d := use.declOf(exprs["Box[int]"])
		require.NotNil(t, d)
		assert.Equal(t, "Box", d.name)
	})

	t.Run("of the package itself", func(t *testing.T) {
		d := use.declOf(&ast.Ident{Name: "New"})
		require.NotNil(t, d)
		assert.Equal(t, modFunc, d.kind)
		assert.Nil(t, use.declOf(&ast.Ident{Name: "Missing"}))
		assert.Nil(t, use.declOf(&ast.BasicLit{}))
	})
}

func Test_ModDeclaredLocally(t *testing.T) {
	t.Parallel()

	src := `package p

import "example.com/app/server"

func param(server int) { _ = server.A }

func local() {
	server := 1
	_ = server.A
}

func before() {
	_ = server.A
	server := 1
	_ = server
}

func ranged(list []int) {
	for _, server := range list {
		_ = server.A
	}
}

func switched(v any) {
	switch server := v.(type) {
	case int:
		_ = server.A
	}
}

func literal() {
	f := func(server int) { _ = server.A }
	f(1)
}

func initialized() {
	if server := 1; server > 0 {
		_ = server.A
	}
}

func clean() {
	_ = server.A
}

func (server T) method() { _ = server.A }
`
	g, ok := parseSource(src)
	require.True(t, ok)

	uses := map[string][]bool{}
	var current string
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if fn, isFn := n.(*ast.FuncDecl); isFn {
			current = fn.Name.Name
		}
		if sel, isSel := n.(*ast.SelectorExpr); isSel {
			if id, isIdent := sel.X.(*ast.Ident); isIdent && id.Name == "server" {
				uses[current] = append(uses[current], modDeclaredLocally("server", id.Pos(), stack))
			}
		}
		return true
	})

	assert.Equal(t, map[string][]bool{
		"param":       {true},
		"local":       {true},
		"before":      {false},
		"ranged":      {true},
		"switched":    {true},
		"literal":     {true},
		"initialized": {true},
		"clean":       {false},
		"method":      {true},
	}, uses)
}

func Test_ModuleIndex_Importers(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	files := modTestModule()
	files["cmd/worker/main.go"] = `package main

import srv "example.com/app/internal/server"

func main() { srv.New() }
`
	files["cmd/worker/main_test.go"] = `package main

import "example.com/app/internal/server"

var _ = server.Name
`
	files["cmd/comment/main.go"] = "package main\n\n// the package example.com/app/internal/server is not imported\nvar s = \"example.com/app/internal/server\"\n"
	files["cmd/ignored/main.go"] = "//go:build ignore\n\npackage main\n\nimport \"example.com/app/internal/server\"\n"
	files["cmd/broken/main.go"] = "package main\n\nimport \"example.com/app/internal/server\"\n\nfunc ( {\n"
	files["vendor/x/x.go"] = "package x\n\nimport \"example.com/app/internal/server\"\n"
	ix, dir := modTestIndex(t, files)

	rel := func(fs []*modFile) []string {
		var out []string
		for _, f := range fs {
			r, err := filepath.Rel(dir, f.path)
			require.NoError(t, err)
			out = append(out, filepath.ToSlash(r))
		}
		return out
	}

	t.Run("the files that import a package, tests included", func(t *testing.T) {
		assert.Equal(t, []string{
			"cmd/api/main.go",
			"cmd/worker/main.go",
			"cmd/worker/main_test.go",
			"internal/server/server_x_test.go",
		}, rel(ix.importers("example.com/app/internal/server")))
	})

	t.Run("a package nobody imports", func(t *testing.T) {
		assert.Empty(t, ix.importers("example.com/app/internal/auth"))
		assert.Empty(t, ix.importers("example.com/nothing"))
	})

	t.Run("a package outside of the module", func(t *testing.T) {
		got := rel(ix.importers("github.com/gofiber/fiber/v3"))
		assert.Equal(t, []string{"internal/server/server.go", "internal/server/server_test.go"}, got)
	})

	t.Run("the answer is kept", func(t *testing.T) {
		first := ix.importers("example.com/app/internal/server")
		second := ix.importers("example.com/app/internal/server")
		require.NotEmpty(t, first)
		assert.Same(t, first[0], second[0])
	})
}

func Test_ModuleIndex_ReadsOnceAndForgets(t *testing.T) {
	t.Parallel()

	ix, dir := modTestIndex(t, modTestModule())
	path := filepath.Join(dir, "internal", "server", "server.go")

	f := ix.fileAt(path)
	require.NotNil(t, f)
	assert.Same(t, f, ix.fileAt(path), "a file is parsed once")
	assert.Same(t, f, ix.packageAt("example.com/app/internal/server").lookup("New").file)

	// the text on disk changes: the index keeps what it read until it is told
	require.NoError(t, os.WriteFile(path, []byte("package server\n\nfunc Replaced() {}\n"), 0o600))
	assert.NotNil(t, ix.packageAt("example.com/app/internal/server").lookup("New"))
	assert.Nil(t, ix.packageAt("example.com/app/internal/server").lookup("Replaced"))

	ix.invalidate(path)
	server := ix.packageAt("example.com/app/internal/server")
	require.NotNil(t, server)
	assert.Nil(t, server.lookup("New"))
	assert.NotNil(t, server.lookup("Replaced"))
	assert.NotSame(t, f, ix.fileAt(path))

	// the other files of the directory are not read again
	testFile := ix.fileAt(filepath.Join(dir, "internal", "server", "server_test.go"))
	require.NotNil(t, testFile)
	ix.invalidate(path)
	assert.Same(t, testFile, ix.fileAt(filepath.Join(dir, "internal", "server", "server_test.go")))

	t.Run("a directory that appears", func(t *testing.T) {
		t.Parallel()

		ix, dir := modTestIndex(t, modTestModule())
		assert.Nil(t, ix.packageAt("example.com/app/internal/extra"))
		before := len(ix.packages())

		newFile := filepath.Join(dir, "internal", "extra", "extra.go")
		require.NoError(t, os.MkdirAll(filepath.Dir(newFile), 0o750))
		require.NoError(t, os.WriteFile(newFile, []byte("package extra\n"), 0o600))
		ix.invalidate(newFile)

		require.NotNil(t, ix.packageAt("example.com/app/internal/extra"))
		assert.Len(t, ix.packages(), before+1)
	})

	t.Run("a file that goes", func(t *testing.T) {
		t.Parallel()

		ix, dir := modTestIndex(t, modTestModule())
		testPath := filepath.Join(dir, "internal", "server", "server_test.go")
		require.NotNil(t, ix.fileAt(testPath))
		require.NoError(t, os.Remove(testPath))

		ix.invalidate(testPath)

		assert.Nil(t, ix.fileAt(testPath))
		server := ix.packageAt("example.com/app/internal/server")
		require.NotNil(t, server)
		assert.Len(t, server.files, 1)
	})
}

func Test_NilModuleIndex(t *testing.T) {
	t.Parallel()

	var ix *moduleIndex

	assert.Nil(t, ix.packageAt("example.com/app"))
	assert.Nil(t, ix.packageIn("/"))
	assert.Nil(t, ix.packages())
	assert.Nil(t, ix.allPackages())
	assert.Nil(t, ix.fileAt("/main.go"))
	assert.Nil(t, ix.importers("fmt"))
	assert.Nil(t, ix.funcsReturning(func(modType) bool { return true }))
	assert.Nil(t, ix.varsHolding(testFiberApp))
	assert.Nil(t, ix.fieldsOfType(func(modType) bool { return true }))
	ix.invalidate("/main.go")

	_, ok := ix.importPathOf("/")
	assert.False(t, ok)
	_, ok = ix.dirOf("example.com/app")
	assert.False(t, ok)

	var pkg *modPackage
	assert.Nil(t, pkg.lookup("New"))
	assert.Nil(t, pkg.lookupIn(nil, "New"))
	assert.Nil(t, pkg.method("T", "M"))
	assert.Nil(t, pkg.decls(true))

	var decl *modDecl
	assert.Nil(t, decl.results())
	assert.Nil(t, decl.params())
	_, ok = decl.varType()
	assert.False(t, ok)
	_, _, ok = decl.varInit()
	assert.False(t, ok)

	var cache moduleIndexes
	assert.Nil(t, cache.of(internal.FileInfo{Root: t.TempDir(), Path: "main.go"}))
}
