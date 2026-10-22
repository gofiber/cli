package v3

import (
	"go/ast"
	"go/parser"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// modTypesModule is a module whose types are declared the ways the resolvers have
// to follow: aliases across packages, defined types, embedding, containers and
// generics.
func modTypesModule() map[string]string {
	return map[string]string{
		"internal/model/model.go": `package model

import "github.com/gofiber/fiber/v3"

type Base struct {
	ID int
	*fiber.App
}

type Wrapper struct {
	Base
	Name string
}

type Apps []*fiber.App

type ByName map[string]*fiber.App

type Queue chan *fiber.App

type Handle = *fiber.App

type Alias = fiber.App

type Defined fiber.App

type Box[T any] []T

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

type Chain1 = Chain2

type Chain2 = Chain1

func (b Base) Describe() string { return "" }

func (w *Wrapper) Run() error { return nil }
`,
		"internal/other/other.go": `package other

import (
	"example.com/app/internal/model"
	"github.com/gofiber/fiber/v3"
)

type App = model.Alias

type Fleet struct {
	Apps    model.Apps
	Current *App
	names   []string
	Handles []model.Handle
	Plain   *fiber.App
	Wrapped model.Wrapper
	model.Box[int]
}

func Make() *App { return nil }
`,
	}
}

// modParseType parses a type expression and returns it as written in the file.
func modParseType(t *testing.T, f *modFile, src string) modType {
	t.Helper()

	e, err := parser.ParseExpr(src)
	require.NoError(t, err, src)
	return modType{file: f, expr: e}
}

func Test_ModType_Named(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	ix, _ := modTestIndex(t, modTypesModule())
	other := ix.packageAt("example.com/app/internal/other")
	require.NotNil(t, other)
	otherFile := other.files[0]
	model := ix.packageAt("example.com/app/internal/model")
	require.NotNil(t, model)
	modelFile := model.files[0]

	const fiberPath = "github.com/gofiber/fiber/v3"
	cases := []struct {
		name     string
		file     *modFile
		src      string
		path     string
		typeName string
		declared bool
		pointer  bool
		ok       bool
	}{
		{name: "an external type", file: modelFile, src: "*fiber.App", path: fiberPath, typeName: "App", pointer: true, ok: true},
		{name: "without the pointer", file: modelFile, src: "fiber.App", path: fiberPath, typeName: "App", ok: true},
		{name: "in parentheses", file: modelFile, src: "(*fiber.App)", path: fiberPath, typeName: "App", pointer: true, ok: true},
		{name: "an alias of an external type", file: modelFile, src: "Alias", path: fiberPath, typeName: "App", ok: true},
		{name: "a pointer to an alias", file: modelFile, src: "*Alias", path: fiberPath, typeName: "App", pointer: true, ok: true},
		{name: "an alias of a pointer", file: modelFile, src: "Handle", path: fiberPath, typeName: "App", pointer: true, ok: true},
		{name: "an alias of an alias of another package", file: otherFile, src: "*App", path: fiberPath, typeName: "App", pointer: true, ok: true},
		{name: "an alias reached through its package", file: otherFile, src: "model.Handle", path: fiberPath, typeName: "App", pointer: true, ok: true},
		{name: "a defined type is its own type", file: modelFile, src: "Defined", path: "example.com/app/internal/model", typeName: "Defined", declared: true, ok: true},
		{name: "a defined type of another package", file: otherFile, src: "*model.Wrapper", path: "example.com/app/internal/model", typeName: "Wrapper", declared: true, pointer: true, ok: true},
		{name: "a generic type", file: otherFile, src: "model.Box[int]", path: "example.com/app/internal/model", typeName: "Box", declared: true, ok: true},
		{name: "a generic type with two arguments", file: modelFile, src: "Pair[string, int]", path: "example.com/app/internal/model", typeName: "Pair", declared: true, ok: true},
		{name: "a predeclared type", file: modelFile, src: "string", typeName: "string", ok: true},
		{name: "an unknown package", file: modelFile, src: "missing.Type"},
		{name: "a slice is not named", file: modelFile, src: "[]*fiber.App"},
		{name: "a function is not named", file: modelFile, src: "func() *fiber.App"},
		{name: "a cycle of aliases ends", file: modelFile, src: "Chain1"},
	}

	for _, c := range cases {
		got, ok := modParseType(t, c.file, c.src).named()
		require.Equal(t, c.ok, ok, c.name)
		if !c.ok {
			continue
		}
		assert.Equal(t, c.path, got.path, c.name)
		assert.Equal(t, c.typeName, got.name, c.name)
		assert.Equal(t, c.pointer, got.pointer, c.name)
		assert.Equal(t, c.declared, got.decl != nil, c.name)
	}

	t.Run("is", func(t *testing.T) {
		assert.True(t, modParseType(t, otherFile, "*App").is(testFiberApp))
		assert.True(t, modParseType(t, otherFile, "model.Alias").is(testFiberApp))
		assert.False(t, modParseType(t, otherFile, "model.Defined").is(testFiberApp), "a defined type has methods of its own")
		assert.False(t, modParseType(t, otherFile, "model.Apps").is(testFiberApp))
		assert.False(t, modType{}.is(testFiberApp))
	})
}

func Test_ModType_Shapes(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	ix, _ := modTestIndex(t, modTypesModule())
	model := ix.packageAt("example.com/app/internal/model")
	require.NotNil(t, model)
	file := model.files[0]
	otherFile := ix.packageAt("example.com/app/internal/other").files[0]

	t.Run("the type behind the names", func(t *testing.T) {
		under := modParseType(t, otherFile, "model.Handle").under()
		assert.IsType(t, &ast.StarExpr{}, under.expr, "an alias of a pointer")

		under = modParseType(t, otherFile, "model.Apps").under()
		assert.IsType(t, &ast.ArrayType{}, under.expr)

		under = modParseType(t, file, "Defined").under()
		assert.IsType(t, &ast.SelectorExpr{}, under.expr, "a type of another package is not looked into")

		under = modParseType(t, file, "Box[int]").under()
		assert.IsType(t, &ast.IndexExpr{}, under.expr, "a generic type is not looked into")

		under = modParseType(t, file, "Chain1").under()
		assert.True(t, under.valid(), "a cycle ends")
	})

	t.Run("elements and keys", func(t *testing.T) {
		for _, src := range []string{"Apps", "ByName", "Queue", "[]*fiber.App", "[3]*fiber.App", "map[int]*fiber.App"} {
			elem, ok := modParseType(t, file, src).elem()
			require.True(t, ok, src)
			assert.True(t, elem.is(testFiberApp), src)
		}
		for _, src := range []string{"fiber.App", "Base", "string", "func()"} {
			_, ok := modParseType(t, file, src).elem()
			assert.False(t, ok, src)
		}

		key, ok := modParseType(t, file, "ByName").key()
		require.True(t, ok)
		named, isNamed := key.named()
		require.True(t, isNamed)
		assert.Equal(t, "string", named.name)
		_, ok = modParseType(t, file, "Apps").key()
		assert.False(t, ok)
	})

	t.Run("fields", func(t *testing.T) {
		fields := modParseType(t, file, "Base").fields()
		require.Len(t, fields, 2)
		assert.Equal(t, "ID", fields[0].name)
		assert.False(t, fields[0].embedded)
		assert.Equal(t, "App", fields[1].name, "an embedded field is called like its type")
		assert.True(t, fields[1].embedded)
		assert.True(t, fields[1].typ.is(testFiberApp))

		assert.Len(t, modParseType(t, file, "*Wrapper").fields(), 2, "through a pointer")
		assert.Len(t, modParseType(t, otherFile, "model.Wrapper").fields(), 2, "of another package")
		assert.Empty(t, modParseType(t, file, "Defined").fields(), "a type of another package has fields the index cannot see")
		assert.Empty(t, modParseType(t, file, "Apps").fields())

		assert.Empty(t, modParseType(t, file, "Pair[string, int]").fields(), "what a generic type is made of depends on its arguments")

		fleet := modParseType(t, otherFile, "Fleet").fields()
		var names []string
		for _, fld := range fleet {
			names = append(names, fld.name)
		}
		assert.Equal(t, []string{"Apps", "Current", "names", "Handles", "Plain", "Wrapped", "Box"}, names)
	})

	t.Run("promoted fields", func(t *testing.T) {
		wrapper := modParseType(t, file, "Wrapper")

		fld, ok := wrapper.field("Name")
		require.True(t, ok)
		assert.False(t, fld.embedded)

		fld, ok = wrapper.field("ID")
		require.True(t, ok, "promoted from Base")
		assert.Equal(t, "ID", fld.name)

		fld, ok = wrapper.field("App")
		require.True(t, ok)
		assert.True(t, fld.typ.is(testFiberApp))

		_, ok = wrapper.field("Missing")
		assert.False(t, ok)
	})

	t.Run("methods", func(t *testing.T) {
		wrapper := modParseType(t, file, "*Wrapper")

		run := wrapper.method("Run")
		require.NotNil(t, run)
		assert.Equal(t, "Wrapper", run.recv)

		describe := wrapper.method("Describe")
		require.NotNil(t, describe, "promoted from Base")
		assert.Equal(t, "Base", describe.recv)

		assert.Nil(t, wrapper.method("Listen"), "a method of an external type is not declared in the module")
		assert.Nil(t, modParseType(t, file, "Apps").method("Run"))
	})

	t.Run("what a type holds", func(t *testing.T) {
		for _, src := range []string{"*fiber.App", "Base", "Wrapper", "Apps", "ByName", "Queue", "[]Apps", "map[string]Wrapper", "Handle", "Alias"} {
			assert.True(t, modParseType(t, file, src).holds(testFiberApp), src)
		}
		for _, src := range []string{"string", "Defined", "Box[int]", "[]string", "fiber.Config"} {
			assert.False(t, modParseType(t, file, src).holds(testFiberApp), src)
		}
		// a field that is not embedded is a field, not the type
		assert.False(t, modParseType(t, otherFile, "Fleet").holds(testFiberApp))
	})
}

// modYieldsModule has the declarations the expressions of the tests refer to.
func modYieldsModule() map[string]string {
	return map[string]string{
		"internal/factory/factory.go": `package factory

import "github.com/gofiber/fiber/v3"

type App = fiber.App

func New() *fiber.App { return fiber.New() }

func Two() (*fiber.App, error) { return nil, nil }

func Plain() int { return 0 }

func Aliased() *App { return nil }

var Default = New()
var Pair1, Pair2 = Two()
var Declared *App
var Literal = &fiber.App{}
var Converted = (*fiber.App)(nil)
var Newed = new(fiber.App)
var Other = Plain()
var Chain = Default
var Cycle1 = Cycle2
var Cycle2 = Cycle1
const Name = "factory"
`,
		"cmd/x/x.go": `package main

import (
	"example.com/app/internal/factory"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
	"example.com/app/internal/missing"
)

func f(x any, p *fiber.App) {
	_ = factory.New()
	_, _ = factory.Two()
	_ = factory.Plain()
	_ = factory.Aliased()
	_ = factory.Default
	_ = factory.Declared
	_ = factory.Pair1
	_ = factory.Pair2
	_ = factory.Literal
	_ = factory.Converted
	_ = factory.Newed
	_ = factory.Other
	_ = factory.Chain
	_ = factory.Cycle1
	_ = factory.Name
	_ = fiber.New()
	_ = fiber.NewOther()
	_ = &fiber.App{}
	_ = fiber.App{}
	_ = new(fiber.App)
	_ = (*fiber.App)(p)
	_ = x.(*fiber.App)
	_ = (fiber.New())
	_ = *p
	_ = missing.Thing()
	_ = session.New()
	_ = session.NewStore()
	_ = session.Config{}
	_ = len(x)
}
`,
	}
}

func Test_ModFile_Yields(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	ix, dir := modTestIndex(t, modYieldsModule())
	f := ix.fileAt(filepath.Join(dir, "cmd", "x", "x.go"))
	require.NotNil(t, f)

	type result struct {
		app   []bool // per value of the expression
		store []bool
	}
	want := map[string]result{
		"factory.New()":          {app: []bool{true}},
		"factory.Two()":          {app: []bool{true, false}},
		"factory.Plain()":        {app: []bool{false}},
		"factory.Aliased()":      {app: []bool{true}},
		"factory.Default":        {app: []bool{true}},
		"factory.Declared":       {app: []bool{true}},
		"factory.Pair1":          {app: []bool{true}},
		"factory.Pair2":          {app: []bool{false}},
		"factory.Literal":        {app: []bool{true}},
		"factory.Converted":      {app: []bool{true}},
		"factory.Newed":          {app: []bool{true}},
		"factory.Other":          {app: []bool{false}},
		"factory.Chain":          {app: []bool{true}},
		"factory.Cycle1":         {app: []bool{false}},
		"factory.Name":           {app: []bool{false}},
		"fiber.New()":            {app: []bool{true}},
		"fiber.NewOther()":       {app: []bool{false}},
		"&fiber.App{}":           {app: []bool{true}},
		"fiber.App{}":            {app: []bool{true}},
		"new(fiber.App)":         {app: []bool{true}},
		"(*fiber.App)(p)":        {app: []bool{true}},
		"x.(*fiber.App)":         {app: []bool{true}},
		"(fiber.New())":          {app: []bool{true}},
		"*p":                     {app: []bool{false}}, // what p holds is for the caller
		"missing.Thing()":        {app: []bool{false}},
		"session.New()":          {app: []bool{false}, store: []bool{true}},
		"session.NewStore()":     {app: []bool{false}, store: []bool{true}},
		"session.Config{}":       {app: []bool{false}, store: []bool{false}},
		"len(x)":                 {app: []bool{false}, store: []bool{false}},
		"factory.Pair1 // pair1": {},
	}
	delete(want, "factory.Pair1 // pair1")

	seen := map[string]bool{}
	f.g.walk(func(n ast.Node, _ []ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		text := f.g.text(as.Rhs[0])
		exp, known := want[text]
		require.True(t, known, "no expectation for %s", text)
		seen[text] = true

		for idx, wantApp := range exp.app {
			assert.Equal(t, wantApp, f.yields(as.Rhs[0], idx, testFiberApp), "%s as an app, value %d", text, idx)
		}
		for idx, wantStore := range exp.store {
			assert.Equal(t, wantStore, f.yields(as.Rhs[0], idx, testSessionStore), "%s as a store, value %d", text, idx)
		}
		return true
	})
	assert.Len(t, seen, len(want), "every expression was looked at")

	t.Run("the other values of a call and nil", func(t *testing.T) {
		call, err := parser.ParseExpr("factory.New()")
		require.NoError(t, err)
		assert.False(t, f.yields(call, 1, testFiberApp), "New has one value")
		assert.False(t, f.yields(nil, 0, testFiberApp))
	})
}

func Test_ModuleIndex_Finders(t *testing.T) {
	// the subtests share one index, which is not safe for concurrent use
	files := modYieldsModule()
	files["internal/server/server.go"] = `package server

import "github.com/gofiber/fiber/v3"

type Server struct {
	App    *fiber.App
	Apps   []*fiber.App
	Embedded
	name   string
}

type Embedded struct{ *fiber.App }

type Generic[T any] struct{ Items []*fiber.App }

type Alias = Server

func (s *Server) Current() *fiber.App { return s.App }

func (s *Server) Pair() (string, *fiber.App) { return "", nil }

func Many() []*fiber.App { return nil }

func Unrelated() string { return "" }
`
	files["internal/auth/auth.go"] = `package authx

import "github.com/gofiber/fiber/v3/middleware/session"

var Store = session.NewStore()
var Shared *session.Store
var Other = 1

func NewStore() *session.Store { return session.New() }

type Auth struct {
	Store *session.Store
	Name  string
}
`
	files["internal/health/health.go"] = `package health

import "github.com/gofiber/contrib/v3/healthcheck"

var Config = healthcheck.Config{}
var Probe = healthcheck.New(Config)
`
	ix, _ := modTestIndex(t, files)

	names := func(ds []*modDecl) []string {
		var out []string
		for _, d := range ds {
			if d.recv != "" {
				out = append(out, d.recv+"."+d.name)
				continue
			}
			out = append(out, d.file.pkg.name+"."+d.name)
		}
		return out
	}
	isApp := func(t modType) bool { return t.is(testFiberApp) }

	t.Run("functions that return apps", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"factory.New", "factory.Two", "factory.Aliased", "Server.Current", "Server.Pair"}, names(ix.funcsReturning(isApp)))
	})

	t.Run("functions that return something that holds apps", func(t *testing.T) {
		holds := func(t modType) bool { return t.holds(testFiberApp) }
		assert.ElementsMatch(t, []string{"factory.New", "factory.Two", "factory.Aliased", "Server.Current", "Server.Pair", "server.Many"}, names(ix.funcsReturning(holds)))
	})

	t.Run("variables that hold apps", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			"factory.Default", "factory.Pair1", "factory.Declared", "factory.Literal",
			"factory.Converted", "factory.Newed", "factory.Chain",
		}, names(ix.varsHolding(testFiberApp)))
	})

	t.Run("fields that hold apps", func(t *testing.T) {
		var got []string
		for _, fld := range ix.fieldsOfType(isApp) {
			got = append(got, fld.owner.name+"."+fld.name)
		}
		assert.ElementsMatch(t, []string{"Server.App", "Embedded.App"}, got)
	})

	t.Run("the same questions about session stores", func(t *testing.T) {
		store := func(t modType) bool { return t.is(testSessionStore) }
		assert.Equal(t, []string{"authx.NewStore"}, names(ix.funcsReturning(store)))
		assert.ElementsMatch(t, []string{"authx.Store", "authx.Shared"}, names(ix.varsHolding(testSessionStore)))

		fields := ix.fieldsOfType(store)
		require.Len(t, fields, 1)
		assert.Equal(t, "Auth", fields[0].owner.name)
		assert.Equal(t, "Store", fields[0].name)
	})

	t.Run("the same questions about healthcheck configs", func(t *testing.T) {
		health := modTarget{
			pkg:   regexp.MustCompile(`^github\.com/gofiber/contrib(/v\d+)?/healthcheck$`),
			types: []string{"Config"},
		}
		assert.Equal(t, []string{"health.Config"}, names(ix.varsHolding(health)))
	})
}
