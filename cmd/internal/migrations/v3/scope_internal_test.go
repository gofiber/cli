package v3

import (
	"go/ast"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_importNames(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"fmt"
	f "github.com/gofiber/fiber/v2"
	_ "github.com/gofiber/fiber/v3"
	jwtware "github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v2/middleware/cors"
	. "github.com/gofiber/fiber/v2/middleware/helmet"
)

func broken( {
`
	jwt := regexp.MustCompile(`^github\.com/gofiber/contrib/jwt$`)
	mw := regexp.MustCompile(`^github\.com/gofiber/fiber/v\d/middleware/`)

	assert.Equal(t, []string{"f"}, fiberNames(src))
	assert.Equal(t, []string{"jwtware"}, importNames(src, jwt, "jwtware", "jwt"))
	assert.Equal(t, []string{".", "cors"}, importNames(src, mw))
	assert.True(t, isImported(src, jwt))
	assert.False(t, isImported(src, regexp.MustCompile(`^example\.com/`)))
	assert.Empty(t, importNames("not go at all", jwt))
}

func Test_importNames_Defaults(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v3"
	"gopkg.in/yaml.v3"
)
`
	assert.Equal(t, []string{"jwt", "jwtware"}, importNames(src, regexp.MustCompile(`contrib/jwt$`), "jwtware", "jwt"))
	assert.Equal(t, []string{"fiber"}, fiberNames(src))
	assert.Equal(t, []string{"yaml"}, importNames(src, regexp.MustCompile(`yaml`)))
}

func Test_editSet(t *testing.T) {
	t.Parallel()

	src := "0123456789"

	var edits editSet
	assert.Equal(t, src, edits.apply(src))

	edits.replace(7, 9, "C")
	edits.replace(1, 3, "A")
	edits.replace(2, 3, "inner")
	edits.replace(5, 5, "|")
	assert.Equal(t, "0A34|56C9", edits.apply(src))

	var bad editSet
	bad.replace(4, 2, "x")
	bad.replace(8, 20, "y")
	assert.Equal(t, src, bad.apply(src))
}

func Test_resolveIdent_FiberCtx(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

var global *fiber.Ctx

type server struct{}

func (s *server) method(c *fiber.Ctx, other string) error {
	return c.SendFile("a")
}

func handler(c *fiber.Ctx) error {
	d := c
	_ = d
	cc, err := lookup()
	_ = cc
	_ = err
	var typed *fiber.Ctx
	untyped := fiber.New()

	inner := func(c *other.Conn) error {
		return c.SendFile("b")
	}
	_ = inner

	for _, c := range conns {
		c.SendFile("c")
	}

	switch c := v.(type) {
	case int:
		c.SendFile("d")
	}

	typed.SendFile("e")
	untyped.SendFile("f")
	global.SendFile("g")
	c.Status(404).SendFile("h")
	c.Locals("k").SendFile("i")
	missing.SendFile("j")
	return c.SendFile("k")
}
`
	g, ok := parseSource(src)
	require.True(t, ok)
	names := fiberNames(src)

	got := map[string]bool{}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "SendFile" || len(call.Args) != 1 {
			return true
		}
		lit, isLit := call.Args[0].(*ast.BasicLit)
		if !isLit {
			return true
		}
		got[lit.Value] = isFiberCtxExpr(sel.X, names, stack)
		return true
	})

	assert.Equal(t, map[string]bool{
		`"a"`: true,  // method parameter
		`"b"`: false, // shadowed by a closure parameter of another type
		`"c"`: false, // range variable
		`"d"`: false, // type switch binding
		`"e"`: true,  // var declaration
		`"f"`: false, // fiber.New() is an app
		`"g"`: true,  // package level variable
		`"h"`: true,  // chained through Status
		`"i"`: false, // Locals does not return the context
		`"j"`: false, // undeclared
		`"k"`: true,  // outer c once the inner scopes are left
	}, got)
}

func Test_isFiberType(t *testing.T) {
	t.Parallel()

	parse := func(expr string) ast.Expr {
		g, ok := parseSource("package p\nvar _ " + expr + "\n")
		require.True(t, ok)
		spec, isValue := g.file.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
		require.True(t, isValue)
		return spec.Type
	}

	assert.True(t, isFiberType(parse("*fiber.Ctx"), []string{"fiber"}, "Ctx"))
	assert.True(t, isFiberType(parse("fiber.Ctx"), []string{"fiber"}, "Ctx"))
	assert.True(t, isFiberType(parse("**f.Ctx"), []string{"f"}, "Ctx"))
	assert.False(t, isFiberType(parse("*other.Ctx"), []string{"fiber"}, "Ctx"))
	assert.False(t, isFiberType(parse("*fiber.App"), []string{"fiber"}, "Ctx"))
	assert.False(t, isFiberType(parse("Ctx"), []string{"fiber"}, "Ctx"))
}

func Test_replaceKeyLookup_OneLineTODO(t *testing.T) {
	t.Parallel()

	repl := func(indent, val, comma, comment, newline string) string {
		return FormatFieldWithComment(indent, "Extractor", val, comma, comment, newline)
	}

	// The unmigratable value must not turn the rest of the line into a comment.
	got := replaceKeyLookup(`keyauth.Config{KeyLookup: cfg.Lookup, Next: skip}`, repl)
	assert.Equal(t, `keyauth.Config{/* TODO: migrate KeyLookup: cfg.Lookup */ Next: skip}`, got)

	got = replaceKeyLookup(`keyauth.Config{Next: skip, KeyLookup: cfg.Lookup}`, repl)
	assert.Equal(t, `keyauth.Config{Next: skip, /* TODO: migrate KeyLookup: cfg.Lookup */ }`, got)

	got = replaceKeyLookup("keyauth.Config{\n\tKeyLookup: cfg.Lookup,\n\tNext: skip,\n}", repl)
	assert.Equal(t, "keyauth.Config{\n\t// TODO: migrate KeyLookup: cfg.Lookup\n\tNext: skip,\n}", got)
}
