package v3

import (
	"go/ast"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxModuleTarget is the file of the modules these tests write that they ask the
// environment about.
const ctxModuleTarget = "cmd/api/main.go"

// ctxModuleKinds writes the files as a module and returns, for every call of
// probe in the target file, what the environment of that file says about its
// receiver, keyed by the text of the first argument.
func ctxModuleKinds(t *testing.T, files map[string]string) map[string]ctxKind {
	t.Helper()

	dir := modTestFiles(t, files)
	path := filepath.Join(dir, filepath.FromSlash(ctxModuleTarget))
	src, err := os.ReadFile(path) // #nosec G304 -- a file the test wrote
	require.NoError(t, err)

	g, ok := parseSource(string(src))
	require.True(t, ok)
	env := newCtxRun().newEnv(internal.FileInfo{Root: dir, Path: path, Rel: ctxModuleTarget}, g, fiberNames(string(src)))
	return ctxKindsWith(g, env)
}

// ctxModuleWeb is a package that declares the types a project builds on the
// context: an alias, wrappers, a generic handler type and holders.
const ctxModuleWeb = `package web

import (
	"iter"

	"github.com/gofiber/fiber/v2"
)

type Ctx = fiber.Ctx
type Ctx2 = Ctx

type Context struct {
	*fiber.Ctx
	User string
}

type Deep struct{ Context }

type Hider struct{ *fiber.Ctx }

func (h *Hider) probe(string) {}

type Aliased struct{ *Ctx }

type Plain struct{ Name string }

type HandlerFunc[T any] func(c *Context, in T) error

type Box[T any] struct{ V T }

func (b Box[T]) Get() T { return b.V }

type Holder struct {
	Ctx     *fiber.Ctx
	Aliased Ctx2
	Wrapped *Context
	N       int
}

func (h *Holder) Current() *fiber.Ctx { return h.Ctx }

func Current() *fiber.Ctx { return nil }

func Pair() (int, *Context) { return 0, nil }

func Wrap(c *fiber.Ctx) *Context { return &Context{Ctx: c} }

func Identity[T any](v T) T { return v }

func Plains() []Plain { return nil }

func Count() int { return 0 }

var Global *fiber.Ctx

var Default = &Context{}

var Handlers = map[string]*Context{}

var Counter = Count()

// Provider hands out the context.
type Provider interface{ Ctx() *fiber.Ctx }

// Provider2 embeds the interface.
type Provider2 interface {
	Provider
	Name() string
}

func NewProvider() Provider { return nil }

type impl struct{ c *fiber.Ctx }

func (i impl) Ctx() *fiber.Ctx { return i.c }

func Impl() impl { return impl{} }

// App builds the app.
func App() *fiber.App { return fiber.New() }

var Default2 = fiber.New()

// Queue holds contexts.
var Queue = make(chan *fiber.Ctx, 1)

// Seq yields the context.
func Seq(c *fiber.Ctx) iter.Seq[*fiber.Ctx] {
	return func(yield func(*fiber.Ctx) bool) { _ = yield(c) }
}

// Pairs yields a key and the context.
func Pairs(c *fiber.Ctx) iter.Seq2[string, *fiber.Ctx] {
	return func(yield func(string, *fiber.Ctx) bool) { _ = yield("k", c) }
}

// Fresh makes a context holder.
var Fresh = new(Holder)
`

func Test_ctxEnv_typesOfAnotherPackage(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"internal/web/web.go": ctxModuleWeb,
		"internal/util/util.go": `package util

import "example.com/app/internal/web"

type Ctx3 = web.Ctx2

type Request struct{ Ctx *web.Context }

func From(c *web.Context) *web.Context { return c }

func Fresh() web.Ctx2 { return nil }
`,
		"internal/plain/plain.go": `package plain

type Thing struct{ N int }

func (t Thing) probe(string) {}

func Make() Thing { return Thing{} }
`,
		"cmd/api/main.go": `package main

import (
	"example.com/app/internal/plain"
	"example.com/app/internal/util"
	"example.com/app/internal/web"
)

func handlers(c *web.Ctx, c2 web.Ctx2, c3 util.Ctx3, w *web.Context, d *web.Deep, h *web.Hider, a *web.Aliased, p web.Plain, bx web.Box[*web.Context], holder *web.Holder, req *util.Request, th plain.Thing, pr web.Provider, pr2 web.Provider2) {
	c.probe("alias")
	c2.probe("alias of an alias")
	c3.probe("alias of an alias of another package")
	w.probe("wrapper")
	d.probe("deep wrapper")
	h.probe("wrapper that declares the method")
	a.probe("wrapper that embeds the alias")
	p.probe("struct without a context")
	holder.Ctx.probe("field")
	holder.Aliased.probe("field of an alias type")
	holder.Wrapped.probe("field of a wrapper type")
	holder.N.probe("field of another type")
	holder.Current().probe("method result")
	req.Ctx.probe("field of a type of the third package")
	bx.Get().probe("method result of a generic type")
	bx.V.probe("field of a generic type")
	web.Current().probe("function result")
	web.Wrap(nil).probe("wrapper result")
	util.From(w).probe("result of a function that takes the wrapper")
	util.Fresh().probe("result of an alias type")
	web.Global.probe("package variable")
	web.Default.probe("package variable made by a literal")
	web.Handlers["x"].probe("element of a package variable")
	web.Identity(w).probe("generic function")
	web.Plains()[0].probe("slice of structs without a context")
	web.Counter.probe("package variable made by a call that returns an int")
	plain.Make().probe("function result without a context")
	th.probe("method of a struct without a context")
	app := web.App()
	acquired := app.AcquireCtx(nil)
	acquired.probe("context acquired from an app of another package")
	web.Default2.AcquireCtx(nil).probe("context acquired from an app variable of another package")
	received := <-web.Queue
	received.probe("received from a channel of another package")
	for each := range web.Seq(nil) {
		each.probe("range over an iterator of another package")
	}
	for _, each := range web.Pairs(nil) {
		each.probe("range over a pair iterator of another package")
	}
	web.Fresh.Ctx.probe("field of a variable made by new")
	pr.Ctx().probe("method of an interface")
	pr2.Ctx().probe("method of an embedded interface")
	web.NewProvider().Ctx().probe("method of an interface returned by a function")
	web.Impl().Ctx().probe("method of a type that is not exported")
	unknown.probe("undeclared")
	web.Missing().probe("name the package does not declare")

	_, second := web.Pair()
	second.probe("second result")

	var literal web.HandlerFunc[string] = func(c *web.Context, in string) error {
		c.probe("parameter of a generic func type")
		return nil
	}
	_ = literal

	local := web.Wrap(nil)
	local.probe("local variable")
	for _, each := range []*web.Context{} {
		each.probe("range variable")
	}

	{
		web := plain.Thing{}
		web.probe("a local called like the package")
	}
}
`,
	}

	got := ctxModuleKinds(t, files)
	want := map[string]ctxKind{
		`"alias"`:                                                    ctxYes,
		`"alias of an alias"`:                                        ctxYes,
		`"alias of an alias of another package"`:                     ctxYes,
		`"wrapper"`:                                                  ctxYes,
		`"deep wrapper"`:                                             ctxYes,
		`"wrapper that declares the method"`:                         ctxNo,
		`"wrapper that embeds the alias"`:                            ctxYes,
		`"struct without a context"`:                                 ctxNo,
		`"field"`:                                                    ctxYes,
		`"field of an alias type"`:                                   ctxYes,
		`"field of a wrapper type"`:                                  ctxYes,
		`"field of another type"`:                                    ctxNo,
		`"method result"`:                                            ctxYes,
		`"field of a type of the third package"`:                     ctxYes,
		`"method result of a generic type"`:                          ctxYes,
		`"field of a generic type"`:                                  ctxYes,
		`"function result"`:                                          ctxYes,
		`"wrapper result"`:                                           ctxYes,
		`"result of a function that takes the wrapper"`:              ctxYes,
		`"result of an alias type"`:                                  ctxYes,
		`"package variable"`:                                         ctxYes,
		`"package variable made by a literal"`:                       ctxYes,
		`"element of a package variable"`:                            ctxYes,
		`"generic function"`:                                         ctxYes,
		`"slice of structs without a context"`:                       ctxNo,
		`"package variable made by a call that returns an int"`:      ctxNo,
		`"function result without a context"`:                        ctxNo,
		`"method of a struct without a context"`:                     ctxNo,
		`"undeclared"`:                                               ctxUnknown,
		`"name the package does not declare"`:                        ctxUnknown,
		`"second result"`:                                            ctxYes,
		`"parameter of a generic func type"`:                         ctxYes,
		`"local variable"`:                                           ctxYes,
		`"range variable"`:                                           ctxYes,
		`"a local called like the package"`:                          ctxNo,
		`"range over an iterator of another package"`:                ctxYes,
		`"range over a pair iterator of another package"`:            ctxYes,
		`"received from a channel of another package"`:               ctxYes,
		`"field of a variable made by new"`:                          ctxYes,
		`"context acquired from an app of another package"`:          ctxYes,
		`"context acquired from an app variable of another package"`: ctxYes,
		`"method of an interface"`:                                   ctxYes,
		`"method of an embedded interface"`:                          ctxYes,
		`"method of an interface returned by a function"`:            ctxYes,
		`"method of a type that is not exported"`:                    ctxYes,
	}
	assert.Equal(t, want, got)
}

func Test_ctxEnv_typesOfAnotherPackageNames(t *testing.T) {
	t.Parallel()

	// The package is imported under another name, by a dot, from a directory that is
	// not called like it, and from a path with a major version.
	base := map[string]string{
		"internal/web/web.go": ctxModuleWeb,
		"internal/web-kit/kit.go": `package webkit

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }
`,
		"internal/api/v2/api.go": `package api

import "github.com/gofiber/fiber/v2"

type Context struct{ *fiber.Ctx }
`,
	}

	tests := []struct {
		name   string
		source string
		want   ctxKind
	}{
		{
			name:   "renamed import",
			source: "import w \"example.com/app/internal/web\"\n\nfunc h(c *w.Context) { c.probe(\"x\") }\n",
			want:   ctxYes,
		},
		{
			name:   "dot import",
			source: "import . \"example.com/app/internal/web\"\n\nfunc h(c *Context, a Ctx2) { c.probe(\"x\") }\n",
			want:   ctxYes,
		},
		{
			name:   "dot import of an alias",
			source: "import . \"example.com/app/internal/web\"\n\nfunc h(c Ctx2) { c.probe(\"x\") }\n",
			want:   ctxYes,
		},
		{
			name:   "directory that is not the name of the package",
			source: "import \"example.com/app/internal/web-kit\"\n\nfunc h(c *webkit.Context) { c.probe(\"x\") }\n",
			want:   ctxYes,
		},
		{
			name:   "major version in the path",
			source: "import \"example.com/app/internal/api/v2\"\n\nfunc h(c *api.Context) { c.probe(\"x\") }\n",
			want:   ctxYes,
		},
		{
			name:   "package that is not imported",
			source: "import \"example.com/app/internal/web\"\n\nfunc h(c *other.Context) { c.probe(\"x\") }\n",
			want:   ctxNo,
		},
		{
			name:   "blank import",
			source: "import _ \"example.com/app/internal/web\"\n\nfunc h(c *web.Context) { c.probe(\"x\") }\n",
			want:   ctxNo,
		},
		{
			name:   "package outside of the module",
			source: "import \"example.org/lib/web\"\n\nfunc h(c *web.Context) { c.probe(\"x\") }\n",
			want:   ctxNo,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files := maps.Clone(base)
			files["cmd/api/main.go"] = "package main\n\n" + tc.source
			assert.Equal(t, map[string]ctxKind{`"x"`: tc.want}, ctxModuleKinds(t, files))
		})
	}
}

func Test_ctxEnv_declarationsOfOtherFiles(t *testing.T) {
	t.Parallel()

	// The declarations of the other files of the package are written with the imports
	// of those files: the fiber package under another name, a package of the module
	// by an alias, a bare name for a type of the package of that file.
	files := map[string]string{
		"internal/web/web.go": ctxModuleWeb,
		"cmd/api/types.go": `package main

import (
	f "github.com/gofiber/fiber/v2"

	w "example.com/app/internal/web"
)

type Ctx = f.Ctx

type Context struct {
	*f.Ctx
}

type Wrapped struct{ *w.Context }

type Holder struct {
	c  *f.Ctx
	wc w.Ctx2
	h  *Hold
}

type Hold struct{ ctx *f.Ctx }

func current() *f.Ctx { return nil }

func pair() (int, *Hold) { return 0, nil }

var global *f.Ctx

var wrapped = &Wrapped{}

var made = current()

var queue = make(chan *f.Ctx, 1)

var holders = new(Hold)
`,
		"cmd/api/main.go": `package main

func handlers(c *Ctx, x *Context, wr *Wrapped, h *Holder) {
	c.probe("alias")
	x.probe("wrapper")
	wr.probe("wrapper of a wrapper of another package")
	h.c.probe("field")
	h.wc.probe("field of an alias type of another package")
	h.h.ctx.probe("field of a field")
	current().probe("function result")
	global.probe("package variable")
	wrapped.probe("package variable made by a literal")
	made.probe("package variable made by a call")
	_, second := pair()
	second.ctx.probe("second result")
	received := <-queue
	received.probe("received from a channel made by make")
	holders.ctx.probe("field of a variable made by new")
}
`,
	}

	got := ctxModuleKinds(t, files)
	want := map[string]ctxKind{
		`"alias"`:   ctxYes,
		`"wrapper"`: ctxYes,
		`"wrapper of a wrapper of another package"`: ctxYes,
		`"field"`: ctxYes,
		`"field of an alias type of another package"`: ctxYes,
		`"field of a field"`:                          ctxYes,
		`"function result"`:                           ctxYes,
		`"package variable"`:                          ctxYes,
		`"package variable made by a literal"`:        ctxYes,
		`"package variable made by a call"`:           ctxYes,
		`"second result"`:                             ctxYes,
		`"received from a channel made by make"`:      ctxYes,
		`"field of a variable made by new"`:           ctxYes,
	}
	assert.Equal(t, want, got)
}

func Test_ctxEnv_embeddedContextOfAnotherPackage(t *testing.T) {
	t.Parallel()

	dir := modTestFiles(t, map[string]string{
		"internal/web/web.go": ctxModuleWeb + `
type hidden struct{ *fiber.Ctx }

type Exposed struct{ hidden }
`,
		"cmd/api/main.go": `package main

import "example.com/app/internal/web"

func handlers(w *web.Context, d *web.Deep, h *web.Hider, a *web.Aliased, p web.Plain, e *web.Exposed) {
	_, _, _, _, _, _ = w, d, h, a, p, e
}
`,
	})
	path := filepath.Join(dir, "cmd", "api", "main.go")
	src, err := os.ReadFile(path) // #nosec G304 -- a file the test wrote
	require.NoError(t, err)
	g, ok := parseSource(string(src))
	require.True(t, ok)
	env := newCtxRun().newEnv(internal.FileInfo{Root: dir, Path: path, Rel: "cmd/api/main.go"}, g, nil)

	paths := map[string]string{}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if id, isIdent := n.(*ast.Ident); isIdent && len(stack) > 0 {
			if _, isCall := stack[len(stack)-1].(*ast.CallExpr); !isCall {
				paths[id.Name] = env.embeddedCtxOf(id, stack)
			}
		}
		return true
	})

	assert.Equal(t, "Ctx", paths["w"])
	assert.Equal(t, "Context.Ctx", paths["d"])
	assert.Equal(t, "Ctx", paths["h"])
	assert.Equal(t, "Ctx", paths["a"])
	assert.Empty(t, paths["p"])
	// the path runs through a field this package can not name
	assert.Empty(t, paths["e"])
}

func Test_ctxRun_open(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"internal/web/web.go": ctxModuleWeb,
		"internal/handlers/fiber.go": `package handlers

import "github.com/gofiber/fiber/v2"

var _ *fiber.Ctx
`,
		"internal/handlers/uses.go": `package handlers

import "example.com/app/internal/web"

var _ web.Ctx
`,
		"internal/handlers/bare.go": `package handlers

func bare() {}
`,
		"internal/handlers/broken.go": `package handlers

import "example.com/app/internal/web"

func broken( {
`,
		"internal/plain/plain.go": `package plain

import "strings"

var _ = strings.ToUpper

type Thing int
`,
		"internal/plain/other.go": `package plain

func other() {}
`,
		"internal/external/external.go": `package external

import "example.org/lib"

var _ = lib.Make
`,
		"internal/far/far.go": `package far

import "example.com/app/internal/mid"

var _ = mid.Make
`,
		"internal/mid/mid.go": `package mid

import "example.com/app/internal/web"

func Make() *web.Context { return nil }
`,
		"internal/unrelated/unrelated.go": `package unrelated

import "example.com/app/internal/plain"

var _ = plain.Thing
`,
	}
	dir := modTestFiles(t, files)

	tests := []struct {
		path string
		// ok is whether the file is looked at, and names whether it imports fiber
		ok    bool
		names bool
	}{
		{path: "internal/handlers/fiber.go", ok: true, names: true},
		{path: "internal/handlers/uses.go", ok: true},
		{path: "internal/handlers/bare.go", ok: true},
		{path: "internal/web/web.go", ok: true, names: true},
		{path: "internal/plain/plain.go"},
		{path: "internal/plain/other.go"},
		{path: "internal/external/external.go"},
		// it reaches fiber through a package that reaches it through another
		{path: "internal/far/far.go", ok: true},
		{path: "internal/mid/mid.go", ok: true},
		// the packages it imports have nothing to do with fiber
		{path: "internal/unrelated/unrelated.go"},
		// it imports a package of the module, but does not parse
		{path: "internal/handlers/broken.go"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			run := newCtxRun()
			path := filepath.Join(dir, filepath.FromSlash(tc.path))
			src, err := os.ReadFile(path) // #nosec G304 -- a file the test wrote
			require.NoError(t, err)

			g, env, ok := run.open(internal.FileInfo{Root: dir, Path: path, Rel: tc.path}, string(src))
			assert.Equal(t, tc.ok, ok)
			if ok {
				assert.NotNil(t, g)
				assert.Equal(t, tc.names, len(env.names) > 0)
			}
			internal.DrainNotes(dir)
		})
	}
}

func Test_ctxRun_openWithoutAModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, src string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
		return path
	}
	write("a.go", "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ *fiber.Ctx\n")
	sibling := write("b.go", "package main\n\nfunc b() {}\n")
	foreign := write("c.go", "package other\n\nfunc c() {}\n")

	run := newCtxRun()
	_, _, ok := run.open(internal.FileInfo{Root: dir, Path: sibling, Rel: "b.go"}, "package main\n\nfunc b() {}\n")
	assert.True(t, ok, "the package has a file that imports fiber")

	_, _, ok = run.open(internal.FileInfo{Root: dir, Path: foreign, Rel: "c.go"}, "package other\n\nfunc c() {}\n")
	assert.False(t, ok, "it is another package")
}

func Test_ctxEnv_finish(t *testing.T) {
	t.Parallel()

	const (
		content = "package p\n\nfunc f() {}\n"
		out     = "package p\n\nfunc f() { _ = fiber.Query[int] }\n"
	)
	parse := func(src string) *goSource {
		g, ok := parseSource(src)
		require.True(t, ok)
		return g
	}

	t.Run("nothing needed the package", func(t *testing.T) {
		t.Parallel()
		env := &ctxEnv{g: parse(content)}
		assert.Equal(t, out, env.finish(content, out))
	})

	t.Run("the output is the input", func(t *testing.T) {
		t.Parallel()
		env := &ctxEnv{g: parse(content), usedFiber: true}
		assert.Equal(t, content, env.finish(content, content))
	})

	t.Run("the file imports it already", func(t *testing.T) {
		t.Parallel()
		env := &ctxEnv{g: parse(content), usedFiber: true, names: []string{"fiber"}}
		assert.Equal(t, out, env.finish(content, out))
	})

	t.Run("the path of the sibling that imports it", func(t *testing.T) {
		t.Parallel()
		sibling := parse("package p\n\nimport f \"github.com/gofiber/fiber/v3\"\n\nvar _ f.Ctx\n")
		env := &ctxEnv{g: parse(content), usedFiber: true, alias: "fiber", files: []*ast.File{sibling.file}}
		assert.Equal(t, "package p\n\nimport \"github.com/gofiber/fiber/v3\"\n\nfunc f() { _ = fiber.Query[int] }\n", env.finish(content, out))
	})

	t.Run("an alias", func(t *testing.T) {
		t.Parallel()
		sibling := parse("package p\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ fiber.Ctx\n")
		env := &ctxEnv{g: parse(content), usedFiber: true, alias: "fiberv3", files: []*ast.File{sibling.file}}
		assert.Equal(t, "package p\n\nimport fiberv3 \"github.com/gofiber/fiber/v2\"\n\nfunc f() { _ = fiber.Query[int] }\n", env.finish(content, out))
	})

	t.Run("no path to import", func(t *testing.T) {
		t.Parallel()
		env := &ctxEnv{g: parse(content), usedFiber: true, alias: "fiber"}
		assert.Equal(t, out, env.finish(content, out))
	})
}

func Test_ctxEnv_freeFiberName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "free", src: "package p\n\nfunc f() {}\n", want: "fiber"},
		{name: "a variable of the package", src: "package p\n\nvar fiber = 1\n", want: "fiberv3"},
		{name: "a constant", src: "package p\n\nconst fiber = 1\n", want: "fiberv3"},
		{name: "a function", src: "package p\n\nfunc fiber() {}\n", want: "fiberv3"},
		{name: "a type", src: "package p\n\ntype fiber struct{}\n", want: "fiberv3"},
		{name: "an import", src: "package p\n\nimport \"example.org/x/fiber\"\n\nvar _ = fiber.X\n", want: "fiberv3"},
		{name: "an import under that name", src: "package p\n\nimport fiber \"example.org/x\"\n", want: "fiberv3"},
		{name: "a parameter", src: "package p\n\nfunc f(fiber int) {}\n", want: "fiberv3"},
		{name: "a field", src: "package p\n\ntype s struct{ fiber int }\n", want: "fiberv3"},
		{name: "both are taken", src: "package p\n\nvar fiber, fiberv3 = 1, 2\n", want: "fiberv3_2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g, ok := parseSource(tc.src)
			require.True(t, ok)
			assert.Equal(t, tc.want, newCtxEnv(g, nil).freeFiberName())
		})
	}
}

func Test_ctxEnv_declarationsOfOtherFilesThatDotImport(t *testing.T) {
	t.Parallel()

	// The sibling dot-imports fiber, and uses names that its own package declares
	// bare: they are the package's, not fiber's.
	files := map[string]string{
		"internal/web/web.go": ctxModuleWeb,
		"cmd/api/types.go": `package main

import (
	. "github.com/gofiber/fiber/v2"

	. "example.com/app/internal/web"
)

type Wrapper struct{ *Ctx }

type Holder struct {
	W    *Wrapper
	Self *Holder
	Web  *Context
	Al   Ctx2
}

func Current() *Wrapper { return nil }

var Shared = &Holder{}
`,
		"cmd/api/main.go": `package main

func f() {
	Current().probe("function result of a sibling that dot-imports fiber")
	Shared.W.probe("field of a name of the package")
	Shared.Self.W.probe("field of a field of the package")
	Shared.Web.probe("field of a name of a package of the module that the sibling dot-imports")
	Shared.Al.probe("alias of a package of the module that the sibling dot-imports")
}
`,
	}

	want := map[string]ctxKind{
		`"function result of a sibling that dot-imports fiber"`:                     ctxYes,
		`"field of a name of the package"`:                                          ctxYes,
		`"field of a field of the package"`:                                         ctxYes,
		`"field of a name of a package of the module that the sibling dot-imports"`: ctxYes,
		`"alias of a package of the module that the sibling dot-imports"`:           ctxYes,
	}
	assert.Equal(t, want, ctxModuleKinds(t, files))
}

func Test_ctxEnv_moreWaysToReachAContextOfAnotherPackage(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"internal/web/web.go": ctxModuleWeb + `
// WithProvider embeds the interface.
type WithProvider struct{ Provider }

// Getter2 holds a function.
var Getter2 = func() *fiber.Ctx { return nil }

// Getter returns a function.
func (h *Holder) Getter() func() *fiber.Ctx { return nil }

// Job holds a function that takes and returns a context.
type Job struct {
	Run func(c *fiber.Ctx) *fiber.Ctx
}

// Named is a function type.
type Named func() *Context

// Chain returns a chain of contexts.
func Chain(c *fiber.Ctx) (*fiber.Ctx, error) { return c, nil }
`,
		"cmd/api/main.go": `package main

import "example.com/app/internal/web"

func handlers(wp *web.WithProvider, h *web.Holder, j *web.Job, v any, ch <-chan *web.Context, n web.Named) {
	wp.Ctx().probe("method promoted from an embedded interface")
	web.Getter2().probe("package variable that holds a function")
	h.Getter()().probe("function returned by a method")
	j.Run(nil).probe("function field")
	f := h.Current
	f().probe("method value")
	(&web.Holder{}).Ctx.probe("field of a literal")
	v.(web.Provider).Ctx().probe("method of an asserted interface")
	web.Named(nil)().probe("conversion to a function type")
	(<-ch).probe("receive from a parameter")
	n().probe("call of a value of a function type")
	got, err := web.Chain(nil)
	got.probe("first result of a call that returns an error")
	_ = err
	var arr [2]*web.Context
	arr[0].probe("element of an array")
	for i := range arr {
		arr[i].probe("element by index")
	}
	func(c *web.Context) { c.probe("parameter of a literal") }(nil)
	defer func() { h.Ctx.probe("field read in a deferred closure") }()
	go func() { h.Aliased.probe("alias field read in a goroutine") }()
}
`,
	}

	got := ctxModuleKinds(t, files)
	for key, kind := range got {
		assert.Equal(t, ctxYes, kind, key)
	}
	assert.Len(t, got, 16)
}

func Test_ctxEnv_callsOfValuesOfAFunctionType(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

type Named func() *fiber.Ctx

type Holder struct {
	handlers []func() *fiber.Ctx
	byName   map[string]func(*fiber.Ctx) *fiber.Ctx
}

func (h *Holder) Getter() func() *fiber.Ctx { return nil }

func f(h *Holder, c *fiber.Ctx) {
	Named(nil)().probe("conversion")
	h.Getter()().probe("call of a call")
	h.handlers[0]().probe("element of a slice")
	h.byName["a"](c).probe("element of a map")
	(h.Getter())().probe("parenthesized")
}
`
	got := ctxKindsOf(t, src)
	for key, kind := range got {
		assert.Equal(t, ctxYes, kind, key)
	}
	assert.Len(t, got, 5)
}
