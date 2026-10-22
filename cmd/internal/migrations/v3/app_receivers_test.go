package v3

import (
	"go/ast"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appTestFiles writes files (relative path to source) below a temporary
// directory and returns them opened in an index of it.
func appTestFiles(t *testing.T, files map[string]string) map[string]*appFile {
	t.Helper()

	root := t.TempDir()
	for name, src := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	}

	ix := newAppIndex(root)
	opened := map[string]*appFile{}
	for name, src := range files {
		if filepath.Ext(name) != ".go" {
			continue
		}
		f, ok := ix.openSource(filepath.Join(root, filepath.FromSlash(name)), src)
		require.True(t, ok, name)
		opened[name] = f
	}
	return opened
}

// appTestCalls returns the receivers of every call of the method name in f, in
// source order.
func appTestCalls(f *appFile, method string) []ast.Expr {
	var recvs []ast.Expr
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == method {
			recvs = append(recvs, sel.X)
		}
		return true
	})
	return recvs
}

func Test_appKinds(t *testing.T) {
	t.Parallel()

	files := appTestFiles(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"main.go": `package main

import (
	f "github.com/gofiber/fiber/v3"
)

type registry struct{}

func (registry) Mount(string, any) {}

type server struct {
	app  *f.App
	grp  f.Router
	hook *f.Hooks
}

type embedded struct{ *f.App }

func param(app *f.App, r f.Router, reg *registry) {
	app.Mount("param", nil)
	r.Mount("router", nil)
	reg.Mount("registry", nil)
}

func locals() {
	app := f.New()
	grp := app.Group("/g")
	chained := app.Mount("/a", nil).Group("/b")
	var typed *f.App
	var iface f.Router = app
	other := make(map[string]int)
	hooks := app.Hooks()
	cfg := f.Config{}
	app.Mount("local", nil)
	grp.Mount("group", nil)
	chained.Mount("chained", nil)
	typed.Mount("typed", nil)
	iface.Mount("iface", nil)
	other.Mount("map", nil)
	hooks.OnShutdown(nil)
	cfg.Mount("config", nil)
	app.Config().Mount("configcall", nil)
}

func fields(s *server, e embedded) {
	s.app.Mount("field", nil)
	s.grp.Mount("fieldrouter", nil)
	s.hook.OnShutdown(nil)
	s.app.Hooks().OnShutdown(nil)
	e.Mount("embedded", nil)
	s.missing.Mount("missing", nil)
}

func results() {
	a := build()
	a.Mount("single", nil)
	b, err := buildErr()
	_ = err
	b.Mount("multi", nil)
	newApp().Mount("direct", nil)
	build().Mount("call", nil)
	closure := func() *f.App { return nil }
	closure().Mount("closure", nil)
	n := notApp()
	n.Mount("notapp", nil)
}

func build() *f.App { return f.New() }

func buildErr() (*f.App, error) { return f.New(), nil }

func newApp() *f.App { return nil }

func notApp() *registry { return nil }
`,
	})

	f := files["main.go"]
	got := map[string]appKind{}
	// map every Mount/OnShutdown call to the string literal it passes
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || (sel.Sel.Name != "Mount" && sel.Sel.Name != "OnShutdown") {
			return true
		}
		label := sel.Sel.Name
		if lit, isLit := call.Args[0].(*ast.BasicLit); isLit {
			label = lit.Value
		}
		got[label] = f.kind(sel.X)
		return true
	})

	want := map[string]appKind{
		`"param"`:       appKindApp,
		`"router"`:      appKindRouter,
		`"registry"`:    appKindNone,
		`"local"`:       appKindApp,
		`"group"`:       appKindRouter,
		`"chained"`:     appKindRouter,
		`"typed"`:       appKindApp,
		`"iface"`:       appKindRouter,
		`"map"`:         appKindNone,
		`"config"`:      appKindConfig,
		`"configcall"`:  appKindConfig,
		`"field"`:       appKindApp,
		`"fieldrouter"`: appKindRouter,
		`"embedded"`:    appKindApp,
		`"missing"`:     appKindNone,
		`"single"`:      appKindApp,
		`"multi"`:       appKindApp,
		`"direct"`:      appKindApp,
		`"call"`:        appKindApp,
		`"closure"`:     appKindApp,
		`"notapp"`:      appKindNone,
	}
	for label, kind := range want {
		require.Contains(t, got, label)
		assert.Equal(t, kind, got[label], label)
	}
	assert.Equal(t, appKindHooks, got["OnShutdown"])
}

func Test_appKinds_AcrossFiles(t *testing.T) {
	t.Parallel()

	files := appTestFiles(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"app.go": `package main

import "github.com/gofiber/fiber/v2"

type Server struct{ app *fiber.App }

func NewApp() *fiber.App { return fiber.New() }

func NewServer() *Server { return &Server{app: NewApp()} }

func (s *Server) App() *fiber.App { return s.app }

var shared = fiber.New()
`,
		"main.go": `package main

func main() {
	app := NewApp()
	app.Listen(":1")
	s := NewServer()
	s.app.Listen(":2")
	s.App().Listen(":3")
	shared.Listen(":4")
	unknown.Listen(":5")
}
`,
	})

	f := files["main.go"]
	recvs := appTestCalls(f, "Listen")
	require.Len(t, recvs, 5)
	for i, want := range []appKind{appKindApp, appKindApp, appKindApp, appKindApp, appKindNone} {
		assert.Equal(t, want, f.kind(recvs[i]), "call %d", i+1)
	}
}

func Test_appKinds_OtherPackage(t *testing.T) {
	t.Parallel()

	files := appTestFiles(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"internal/srv/srv.go": `package server

import "github.com/gofiber/fiber/v2"

func New() *fiber.App { return fiber.New() }

type Thing struct{}

func (Thing) Mount(string, any) {}

func NewThing() Thing { return Thing{} }
`,
		"cmd/main.go": `package main

import "example.com/demo/internal/srv"

func main() {
	app := server.New()
	app.Listen(":1")
	t := server.NewThing()
	t.Mount("x", nil)
}
`,
	})

	f := files["cmd/main.go"]
	assert.Equal(t, appKindApp, f.kind(appTestCalls(f, "Listen")[0]))
	assert.Equal(t, appKindNone, f.kind(appTestCalls(f, "Mount")[0]))
}

func Test_appOrigins(t *testing.T) {
	t.Parallel()

	files := appTestFiles(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"app.go": `package main

import "github.com/gofiber/fiber/v2"

type server struct{ app *fiber.App }

func newServer() *server {
	s := &server{}
	s.app = fiber.New(fiber.Config{AppName: "field"})
	return s
}

func build(quiet bool) *fiber.App {
	if quiet {
		return fiber.New(fiber.Config{AppName: "quiet"})
	}
	return fiber.New(fiber.Config{AppName: "loud"})
}

var global *fiber.App

func setup() { global = fiber.New(fiber.Config{AppName: "global"}) }
`,
		"main.go": `package main

import "github.com/gofiber/fiber/v2"

func run(app *fiber.App) {
	app.Listen(":param")
}

func main() {
	inline := fiber.New(fiber.Config{AppName: "inline"})
	inline.Listen(":inline")

	built := build(true)
	built.Listen(":built")

	s := newServer()
	s.app.Listen(":field")

	global.Listen(":global")

	var late *fiber.App
	late = fiber.New(fiber.Config{AppName: "late"})
	late.Listen(":late")

	var never *fiber.App
	never.Listen(":never")

	fiber.New(fiber.Config{AppName: "direct"}).Listen(":direct")

	multi, err := fiber.New(), error(nil)
	_ = err
	multi.Listen(":multi")

	phi := fiber.New(fiber.Config{AppName: "phi1"})
	if err != nil {
		phi = fiber.New(fiber.Config{AppName: "phi2"})
	}
	phi.Listen(":phi")
}
`,
	})

	f := files["main.go"]
	apps := map[string]appOrigins{}
	for _, recv := range appTestCalls(f, "Listen") {
		call, ok := f.parent[f.parent[recv]].(*ast.CallExpr)
		require.True(t, ok)
		label, ok := call.Args[0].(*ast.BasicLit)
		require.True(t, ok)
		apps[label.Value] = f.originsOf(recv, 0, 0)
	}

	appNames := func(o appOrigins) []string {
		var names []string
		for _, n := range o.news {
			lit, ok := n.call.Args[0].(*ast.CompositeLit)
			require.True(t, ok)
			kv, ok := lit.Elts[0].(*ast.KeyValueExpr)
			require.True(t, ok)
			names = append(names, n.file.g.text(kv.Value))
		}
		return names
	}

	tests := []struct {
		label    string
		names    []string
		complete bool
	}{
		{`":param"`, nil, false},
		{`":inline"`, []string{`"inline"`}, true},
		{`":built"`, []string{`"quiet"`, `"loud"`}, true},
		{`":field"`, []string{`"field"`}, true},
		{`":global"`, []string{`"global"`}, true},
		{`":late"`, []string{`"late"`}, true},
		{`":never"`, nil, false},
		{`":direct"`, []string{`"direct"`}, true},
		{`":phi"`, []string{`"phi1"`, `"phi2"`}, true},
	}
	for _, tt := range tests {
		got, ok := apps[tt.label]
		require.True(t, ok, tt.label)
		assert.ElementsMatch(t, tt.names, appNames(got), tt.label)
		assert.Equal(t, tt.complete, got.complete, tt.label)
	}
}

// The text based helpers of common.go and the AST based kinds agree on the
// simple files the helpers were written for.
func Test_appKinds_AgreeWithTheTextHelpers(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	grp := app.Group("/g")
	var r fiber.Router
	r = grp
	app.Mount("app", nil)
	grp.Mount("grp", nil)
	r.Mount("r", nil)
}
`
	files := appTestFiles(t, map[string]string{"go.mod": "module example.com/demo\n", "main.go": src})
	f := files["main.go"]

	recvs := appTestCalls(f, "Mount")
	require.Len(t, recvs, 3)
	for i, name := range []string{"app", "grp", "r"} {
		assert.Equal(t, isFiberRouter(src, name), f.kind(recvs[i]).isRouter(), name)
	}
	assert.False(t, isFiberRouter(src, "main"))
}

func Test_appOrigins_ExportedAcrossPackages(t *testing.T) {
	t.Parallel()

	files := appTestFiles(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"internal/server/server.go": `package server

import "github.com/gofiber/fiber/v2"

type Runner struct{ App *fiber.App }

func NewRunner() *Runner {
	return &Runner{App: fiber.New(fiber.Config{AppName: "runner"})}
}

var Shared *fiber.App

var Other = fiber.New(fiber.Config{AppName: "other"})

func Init() { Shared = fiber.New(fiber.Config{AppName: "shared"}) }
`,
		"cmd/main.go": `package main

import (
	"github.com/gofiber/fiber/v2"

	"example.com/demo/internal/server"
)

func main() {
	r := server.NewRunner()
	r.App.Listen(":runner")
	server.Shared.Listen(":shared")
	server.Other.Listen(":other")

	other := &server.Runner{}
	other.App = fiber.New(fiber.Config{AppName: "assigned"})
	r.App.Listen(":again")
	server.Shared = fiber.New(fiber.Config{AppName: "main"})
}
`,
	})

	f := files["cmd/main.go"]
	got := map[string]appOrigins{}
	for _, recv := range appTestCalls(f, "Listen") {
		call, ok := f.parent[f.parent[recv]].(*ast.CallExpr)
		require.True(t, ok)
		label, ok := call.Args[0].(*ast.BasicLit)
		require.True(t, ok)
		got[label.Value] = f.originsOf(recv, 0, 0)
	}

	names := func(o appOrigins) []string {
		var out []string
		for _, n := range o.news {
			lit, ok := n.call.Args[0].(*ast.CompositeLit)
			require.True(t, ok)
			kv, ok := lit.Elts[0].(*ast.KeyValueExpr)
			require.True(t, ok)
			out = append(out, n.file.g.text(kv.Value))
		}
		return out
	}

	assert.ElementsMatch(t, []string{`"runner"`, `"assigned"`}, names(got[`":runner"`]))
	assert.True(t, got[`":runner"`].complete)
	assert.ElementsMatch(t, []string{`"shared"`, `"main"`}, names(got[`":shared"`]))
	assert.True(t, got[`":shared"`].complete)
	assert.ElementsMatch(t, []string{`"other"`}, names(got[`":other"`]))
	assert.True(t, got[`":other"`].complete)
}
