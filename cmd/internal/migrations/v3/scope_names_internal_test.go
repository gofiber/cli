package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeImporter hands out an empty package called fiber for every import path, which
// is all the type checker needs to tell the package from a variable of that name.
type fakeImporter struct{}

func (fakeImporter) Import(path string) (*types.Package, error) {
	pkg := types.NewPackage(path, "fiber")
	pkg.MarkComplete()
	return pkg, nil
}

// Test_localScopes_agreeWithTheTypeChecker compares the scopes with the type
// checker: every use of fiber as the operand of a selector is the package exactly
// where hiddenAt says that nothing hides the name.
func Test_localScopes_agreeWithTheTypeChecker(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"parameter": `
func a(fiber int) { _ = fiber.x }
func b() { _ = fiber.x }`,
		"result": `
func a() (fiber int) { _ = fiber.x; return }
func b() { _ = fiber.x }`,
		"receiver": `
type T struct{}
func (fiber T) m() { _ = fiber.x }
func (T) n() { _ = fiber.x }`,
		"short variable": `
func a() {
	_ = fiber.before
	fiber := 1
	_ = fiber.after
}
func b() { _ = fiber.other }`,
		"short variable that reads the package": `
func a() {
	fiber := fiber.New()
	_ = fiber.x
}`,
		"redeclaration": `
func a() {
	fiber, err := 1, 2
	fiber, other := 3, 4
	_, _, _ = fiber.x, err, other
}`,
		"variable in a group": `
func a() {
	var (
		first  = fiber.x
		fiber  = 2
		second = fiber.y
	)
	_, _ = first, second
}`,
		"variable with an initializer that reads the package": `
func a() {
	var fiber = fiber.New()
	_ = fiber.x
}`,
		"constant": `
func a() {
	_ = fiber.before
	const fiber = 1
	_ = fiber.after
}`,
		"type": `
func a() {
	_ = fiber.before
	type fiber struct{}
	var _ fiber
	_ = fiber.after
}`,
		"block": `
func a() {
	{
		fiber := 1
		_ = fiber.inside
	}
	_ = fiber.after
}`,
		"if": `
func a() {
	if fiber := 1; fiber.cond {
		_ = fiber.body
	} else if other := 2; other.x {
		_ = fiber.second
	} else {
		_ = fiber.last
	}
	_ = fiber.after
}`,
		"for": `
func a() {
	for fiber := 0; fiber.cond; fiber.post {
		_ = fiber.body
	}
	_ = fiber.after
}`,
		"range": `
func a() {
	for fiber := range fiber.items {
		_ = fiber.body
	}
	_ = fiber.after
}`,
		"range with key and value": `
func a(m map[string]int) {
	for key, fiber := range m {
		_, _ = key, fiber.body
	}
	for fiber, value := range m {
		_, _ = fiber.body, value
	}
	_ = fiber.after
}`,
		"range that assigns": `
func a() {
	var fiber int
	for fiber = range 3 {
		_ = fiber.body
	}
}`,
		"switch": `
func a() {
	switch fiber := 1; fiber.tag {
	case fiber.label:
		_ = fiber.body
	default:
		_ = fiber.last
	}
	_ = fiber.after
}`,
		"type switch": `
func a(v any) {
	switch fiber := v.(type) {
	case fiber.T:
		_ = fiber.first
	case int:
		_ = fiber.second
	default:
		_ = fiber.third
	}
	_ = fiber.after
}`,
		"type switch with an init statement": `
func a(v any) {
	switch fiber := 1; v.(type) {
	case int:
		_ = fiber.body
	}
	_ = fiber.after
}`,
		"select": `
func a(ch chan int) {
	select {
	case fiber := <-ch:
		_ = fiber.first
	case fiber, ok := <-ch:
		_, _ = fiber.second, ok
	case <-ch:
		_ = fiber.third
	default:
		_ = fiber.last
	}
	_ = fiber.after
}`,
		"clause bodies": `
func a(x int) {
	switch x {
	case 1:
		fiber := 1
		_ = fiber.first
	case 2:
		_ = fiber.second
	}
	_ = fiber.after
}`,
		"function literal": `
func a() {
	f := func(fiber int) { _ = fiber.x }
	g := func() { _ = fiber.y }
	h := func() (fiber int) { _ = fiber.z; return }
	_, _, _ = f, g, h
}`,
		"function literal before and after a variable": `
func a() {
	before := func() { _ = fiber.x }
	fiber := 1
	after := func() { _ = fiber.y }
	_, _ = before, after
}`,
		"variable that holds a function that reads the package": `
func a() {
	fiber := func() { _ = fiber.x }
	_ = fiber
}`,
		"type parameter": `
func a[fiber any](v fiber) { _ = fiber.x }
func b() { _ = fiber.y }`,
		"receiver type parameter": `
type Box[T any] struct{}
func (b Box[fiber]) m(v fiber) { _ = fiber.x }
func (b Box[T]) n() { _ = fiber.y }`,
		"function type parameters have no scope": `
func a(cb func(fiber int)) { _ = fiber.x }
type I interface{ m(fiber int) }
func b() { _ = fiber.y }`,
		"closure inside a hiding function": `
func a(fiber int) {
	func() {
		_ = fiber.x
		func(fiber string) { _ = fiber.y }("")
		_ = fiber.z
	}()
}`,
		"labeled statement": `
func a() {
L:
	fiber := 1
	_ = fiber.x
	goto L
}`,
		"assignment is no declaration": `
func a() {
	var x int
	x = 1
	_ = fiber.after
	_ = x
}`,
		"shadow in a sibling function only": `
func a() { fiber := 1; _ = fiber.x }
func b() { _ = fiber.y }
func c() { _ = fiber.z }`,
	}

	// the cases in which the name is the package everywhere
	hidesNothing := []string{
		"shadow in a sibling function only", "assignment is no declaration",
		"variable that holds a function that reads the package", "function type parameters have no scope",
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := "package p\n\nimport \"example.com/fiber\"\n" + body + "\n"
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
			require.NoError(t, err)

			info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
			conf := types.Config{Importer: fakeImporter{}, Error: func(error) {}}
			if _, checkErr := conf.Check("p", fset, []*ast.File{file}, info); checkErr != nil {
				t.Logf("the type checker does not know the members of the fake package: %v", checkErr)
			}

			scopes := newLocalScopes(file)
			checked, hidden := 0, 0
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || id.Name != "fiber" {
					return true
				}
				obj := info.Uses[id]
				if obj == nil {
					return true
				}

				_, isPackage := obj.(*types.PkgName)
				assert.Equal(t, !isPackage, scopes.hiddenAt("fiber", id.Pos()),
					"line %d: the type checker says %T", fset.Position(id.Pos()).Line, obj)
				checked++
				if !isPackage {
					hidden++
				}
				return true
			})
			assert.Positive(t, checked, "no use of the name was compared")
			if !slices.Contains(hidesNothing, name) {
				assert.Positive(t, hidden, "the case hides the name nowhere")
			}
		})
	}
}

func Test_localScopes_declared(t *testing.T) {
	t.Parallel()

	g, ok := parseSource(`package p

import "example.com/fiber"

func a(fiber int) {}

func b() { _ = fiber.x }
`)
	require.True(t, ok)
	assert.True(t, g.declaresLocally("fiber"))
	assert.False(t, g.declaresLocally("other"))

	var inA, inB token.Pos
	ast.Inspect(g.file, func(n ast.Node) bool {
		if fn, isFn := n.(*ast.FuncDecl); isFn {
			if fn.Name.Name == "a" {
				inA = fn.Body.Lbrace + 1
			} else {
				inB = fn.Body.Lbrace + 1
			}
		}
		return true
	})
	assert.True(t, g.hiddenAt("fiber", inA))
	assert.False(t, g.hiddenAt("fiber", inB))
}
