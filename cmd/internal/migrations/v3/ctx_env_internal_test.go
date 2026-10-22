package v3

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ctxAddImport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		path string
		def  string
		want string
	}{
		{
			name: "already imported",
			src:  "package p\n\nimport \"strconv\"\n",
			path: "strconv",
			def:  "strconv",
			want: "package p\n\nimport \"strconv\"\n",
		},
		{
			name: "already imported with an alias",
			src:  "package p\n\nimport sc \"strconv\"\n",
			path: "strconv",
			def:  "strconv",
			want: "package p\n\nimport sc \"strconv\"\n",
		},
		{
			name: "a single import becomes a block",
			src:  "package p\n\nimport \"github.com/gofiber/fiber/v3\"\n\nvar _ = fiber.New\n",
			path: "strconv",
			def:  "strconv",
			want: "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n\t\"strconv\"\n)\n\nvar _ = fiber.New\n",
		},
		{
			name: "a standard package goes before the others",
			src:  "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
			path: "strconv",
			def:  "strconv",
			want: "package p\n\nimport (\n\t\"strconv\"\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
		},
		{
			name: "next to the package that shares its path",
			src:  "package p\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
			path: "strconv",
			def:  "strconv",
			want: "package p\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n\t\"strconv\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
		},
		{
			name: "a subpackage goes after its module",
			src:  "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3\" // the framework\n)\n",
			path: "github.com/gofiber/fiber/v3/binder",
			def:  "binder",
			want: "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3\" // the framework\n\t\"github.com/gofiber/fiber/v3/binder\"\n)\n",
		},
		{
			name: "only a third party import is there",
			src:  "package p\n\nimport (\n\t\"example.com/x\"\n)\n",
			path: "github.com/gofiber/fiber/v3/binder",
			def:  "binder",
			want: "package p\n\nimport (\n\t\"example.com/x\"\n\t\"github.com/gofiber/fiber/v3/binder\"\n)\n",
		},
		{
			name: "no import at all",
			src:  "package p\n\nvar x = 1\n",
			path: "strconv",
			def:  "strconv",
			want: "package p\n\nimport \"strconv\"\n\nvar x = 1\n",
		},
		{
			name: "a blank import does not count",
			src:  "package p\n\nimport _ \"strconv\"\n",
			path: "strconv",
			def:  "strconv",
			want: "package p\n\nimport (\n\t_ \"strconv\"\n\t\"strconv\"\n)\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ctxAddImport(tc.src, tc.path, tc.def))
		})
	}
}

func Test_ctxRenameTagKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tag       string
		want      string
		conflicts []string
	}{
		{name: "params", tag: `params:"id"`, want: `uri:"id"`},
		{name: "reqHeader with others", tag: `json:"id" reqHeader:"X-Id"  xml:"id"`, want: `json:"id" header:"X-Id"  xml:"id"`},
		{name: "both", tag: `params:"a" reqHeader:"b"`, want: `uri:"a" header:"b"`},
		{name: "whole keys only", tag: `myparams:"a" reqHeaderX:"b" xparams:"c"`, want: `myparams:"a" reqHeaderX:"b" xparams:"c"`},
		{name: "a value that looks like a key", tag: `json:"params:\"x\""`, want: `json:"params:\"x\""`},
		{name: "conflict", tag: `params:"a" uri:"b"`, want: `params:"a" uri:"b"`, conflicts: []string{"params"}},
		{name: "malformed", tag: `params:id`, want: `params:id`},
		{name: "empty", tag: ``, want: ``},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, conflicts := ctxRenameTagKeys(tc.tag)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.conflicts, conflicts)
		})
	}
}

func Test_ctxRender(t *testing.T) {
	t.Parallel()

	src := "abc(def(ghi))"

	var edits editSet
	edits.replace(8, 11, "GHI") // ghi
	edits.replace(0, 0, ">")    // in front of everything

	assert.Equal(t, "def(GHI)", ctxRender(&edits, src, 4, 12))
	assert.Equal(t, ">abc(def(GHI))", ctxRender(&edits, src, 0, 13))
	assert.Equal(t, "abc(def(ghi))", ctxRender(&editSet{}, src, 0, 13))

	// An edit of the whole range replaces the edits inside of it.
	edits.replace(4, 12, "X")
	assert.Equal(t, "X", ctxRender(&edits, src, 4, 12))
	assert.Equal(t, "GHI", ctxRender(&edits, src, 8, 11))
}

// ctxEnvOf parses src and returns the environment of its fiber import.
func ctxEnvOf(t *testing.T, src string) (*ctxEnv, *goSource) {
	t.Helper()

	g, ok := parseSource(src)
	require.True(t, ok)
	return newCtxEnv(g, fiberNames(src)), g
}

// ctxKindsOf returns, for every call of the method probe in src, what the
// environment says about its receiver, keyed by the text of the first argument.
func ctxKindsOf(t *testing.T, src string) map[string]ctxKind {
	t.Helper()

	env, g := ctxEnvOf(t, src)
	got := map[string]ctxKind{}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "probe" {
			return true
		}
		got[g.text(call.Args[0])] = env.receiverKind(sel, stack)
		return true
	})
	return got
}

func Test_ctxEnv_ctxKindOf(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"example.com/web"

	f "github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type Ctx = f.Ctx

type holder struct {
	c   *f.Ctx
	Ctx *f.Ctx
	n   int
}

type wrapper struct {
	*f.Ctx
	user string
}

type plain struct{ name string }

type shadow struct {
	*f.Ctx
	probe func(string)
}

type shadowMethod struct{ *f.Ctx }

func (shadowMethod) probe(string) {}

type nested struct{ h *holder }

func (h *holder) self() *holder   { return h }
func (h *holder) get() *f.Ctx     { return h.c }
func (w *wrapper) inner() *f.Ctx  { return w.Ctx }
func newHolder() *holder          { return &holder{} }
func current() *f.Ctx             { return nil }
func ctxs() []*f.Ctx              { return nil }

var global *f.Ctx

func handlers(c *f.Ctx, sh *shadow, sm *shadowMethod, w *wrapper, p plain, h *holder, n nested, alias Ctx, ext *web.Ctx, cs []*f.Ctx, byName map[string]*f.Ctx) {
	c.probe("param")
	w.probe("embedding struct")
	p.probe("other struct")
	sh.probe("own field")
	sm.probe("own method")
	h.c.probe("field")
	h.Ctx.probe("field called like the type")
	h.n.probe("int field")
	h.self().c.probe("method result then field")
	h.get().probe("method result")
	w.inner().probe("embedded method result")
	n.h.c.probe("nested field")
	alias.probe("alias")
	ext.probe("another package")
	global.probe("package variable")
	current().probe("function result")
	newHolder().c.probe("function result field")
	cs[0].probe("slice element")
	byName["x"].probe("map element")
	c.Status(200).probe("chain")
	c.Status(200).Type("json").probe("longer chain")
	(c).probe("parenthesized")
	unknown.probe("undeclared")
	web.Current(c).probe("call of another package")

	app := f.New()
	acquired := app.AcquireCtx(&fasthttp.RequestCtx{})
	acquired.probe("acquired")
	copied := acquired
	copied.probe("copy")
	var declared *f.Ctx
	declared.probe("var")
	var inferred = current()
	inferred.probe("var inferred")
	made := &wrapper{}
	made.probe("composite literal")
	asserted := any(c).(*f.Ctx)
	asserted.probe("assertion")
	_ = func(c *plain) { c.probe("shadowing closure parameter") }
	{
		c := plain{}
		c.probe("shadowing variable")
	}
	c.probe("param again")
}
`
	got := ctxKindsOf(t, src)
	want := map[string]ctxKind{
		`"param"`:                       ctxYes,
		`"embedding struct"`:            ctxYes,
		`"other struct"`:                ctxNo,
		`"own field"`:                   ctxNo,
		`"own method"`:                  ctxNo,
		`"field"`:                       ctxYes,
		`"field called like the type"`:  ctxYes,
		`"int field"`:                   ctxNo,
		`"method result then field"`:    ctxYes,
		`"method result"`:               ctxYes,
		`"embedded method result"`:      ctxYes,
		`"nested field"`:                ctxYes,
		`"alias"`:                       ctxYes,
		`"another package"`:             ctxNo,
		`"package variable"`:            ctxYes,
		`"function result"`:             ctxYes,
		`"function result field"`:       ctxYes,
		`"slice element"`:               ctxYes,
		`"map element"`:                 ctxYes,
		`"chain"`:                       ctxYes,
		`"longer chain"`:                ctxYes,
		`"parenthesized"`:               ctxYes,
		`"undeclared"`:                  ctxUnknown,
		`"call of another package"`:     ctxUnknown,
		`"acquired"`:                    ctxYes,
		`"copy"`:                        ctxYes,
		`"var"`:                         ctxYes,
		`"var inferred"`:                ctxYes,
		`"composite literal"`:           ctxYes,
		`"assertion"`:                   ctxYes,
		`"shadowing closure parameter"`: ctxNo,
		`"shadowing variable"`:          ctxNo,
		`"param again"`:                 ctxYes,
	}
	assert.Equal(t, want, got)
}

func Test_ctxEnv_dotImport(t *testing.T) {
	t.Parallel()

	src := `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx, other *Config) {
	c.probe("ctx")
	other.probe("config")
}
`
	got := ctxKindsOf(t, src)
	assert.Equal(t, ctxYes, got[`"ctx"`])
	assert.NotEqual(t, ctxYes, got[`"config"`])
}

func Test_ctxEnv_qual(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "fiber.Ctx", (&ctxEnv{names: []string{"fiber"}}).qual("Ctx"))
	assert.Equal(t, "f.Ctx", (&ctxEnv{names: []string{"f"}}).qual("Ctx"))
	assert.Equal(t, "f.Ctx", (&ctxEnv{names: []string{".", "f"}}).qual("Ctx"))
	assert.Equal(t, "Ctx", (&ctxEnv{names: []string{"."}}).qual("Ctx"))
	assert.Equal(t, "fiber.Ctx", (&ctxEnv{}).qual("Ctx"))
}
