package v3

import (
	"go/ast"
	"go/format"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_importsAdd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "block",
			src:  "package a\n\nimport (\n\t\"fmt\" // output\n)\n\nvar _ = fmt.Sprint\n",
			want: "package a\n\nimport (\n\t\"fmt\" // output\n\t\"github.com/stretchr/testify/require\"\n)\n\nvar _ = fmt.Sprint\n",
		},
		{
			name: "single import",
			src:  "package a\n\nimport \"fmt\" // output\n\nvar _ = fmt.Sprint\n",
			want: "package a\n\nimport (\n\t\"fmt\"\n\t\"github.com/stretchr/testify/require\"\n) // output\n\nvar _ = fmt.Sprint\n",
		},
		{
			name: "no import at all",
			src:  "package a\n\nvar _ = 1\n",
			want: "package a\n\nimport \"github.com/stretchr/testify/require\"\n\nvar _ = 1\n",
		},
		{
			name: "closing parenthesis shares a line",
			src:  "package a\n\nimport (\"fmt\")\n\nvar _ = fmt.Sprint\n",
			want: "package a\n\nimport (\"fmt\"\n\t\"github.com/stretchr/testify/require\"\n)\n\nvar _ = fmt.Sprint\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g, ok := parseSource(tc.src)
			require.True(t, ok)

			var edits editSet
			importsAdd(g, &edits, "", "github.com/stretchr/testify/require")
			got := edits.apply(tc.src)
			assert.Equal(t, tc.want, got)

			_, err := format.Source([]byte(got))
			require.NoError(t, err, "the result is valid Go")
		})
	}
}

func Test_importsAdd_WithAlias(t *testing.T) {
	t.Parallel()

	src := "package a\n\nimport (\n\t\"fmt\"\n)\n"
	g, ok := parseSource(src)
	require.True(t, ok)

	var edits editSet
	importsAdd(g, &edits, "testifyrequire", "github.com/stretchr/testify/require")
	assert.Equal(t, "package a\n\nimport (\n\t\"fmt\"\n\ttestifyrequire \"github.com/stretchr/testify/require\"\n)\n", edits.apply(src))
}

func Test_importsRewritePath(t *testing.T) {
	t.Parallel()

	src := "package a\n\nimport (\n\tu \"old/path\" // helpers\n\t. `other/path`\n)\n"
	g, ok := parseSource(src)
	require.True(t, ok)

	var edits editSet
	for _, s := range importsOf(g, func(string) bool { return true }) {
		importsRewritePath(g, &edits, s, "new/"+s.path)
	}
	assert.Equal(t, "package a\n\nimport (\n\tu \"new/old/path\" // helpers\n\t. `new/other/path`\n)\n", edits.apply(src))
}

func Test_importsNames(t *testing.T) {
	t.Parallel()

	src := `package a

import (
	"github.com/stretchr/testify/require"
	assertions "github.com/stretchr/testify/assert"
	_ "embed"
	. "strings"
)
`
	g, ok := parseSource(src)
	require.True(t, ok)

	assert.Equal(t, "require", importsNameOf(g, "github.com/stretchr/testify/require", "require"))
	assert.Equal(t, "assertions", importsNameOf(g, "github.com/stretchr/testify/assert", "assert"))
	assert.Empty(t, importsNameOf(g, "embed", "embed"), "blank imports cannot be referred to")
	assert.Empty(t, importsNameOf(g, "strings", "strings"), "dot imports cannot be referred to")
	assert.Empty(t, importsNameOf(g, "fmt", "fmt"))
	assert.True(t, importsBinds(g, "require"))
	assert.True(t, importsBinds(g, "assertions"))
	assert.False(t, importsBinds(g, "assert"))
	assert.False(t, importsBinds(g, "embed"))
}

func Test_importsShadowed(t *testing.T) {
	t.Parallel()

	src := `package a

import "example.com/utils"

type T struct{}

func param(utils T) { _ = utils.A }

func results() (utils T) { return utils.A }

func local() {
	_ = utils.Before
	utils := T{}
	_ = utils.After
}

func declVar() {
	var utils T
	_ = utils.Var
}

func rangeVar(list []T) {
	for _, utils := range list {
		_ = utils.Range
	}
	_ = utils.AfterLoop
}

func typeSwitch(x any) {
	switch utils := x.(type) {
	case T:
		_ = utils.Switch
	}
}

func ifInit() {
	if utils := (T{}); true {
		_ = utils.If
	}
	_ = utils.AfterIf
}

func closure() {
	f := func(utils T) { _ = utils.Closure }
	f(T{})
	_ = utils.AfterClosure
}

func selectStmt(ch chan T) {
	select {
	case utils := <-ch:
		_ = utils.Select
	}
}

func recv(utils T) { _ = func() { _ = utils.Nested } }
`
	g, ok := parseSource(src)
	require.True(t, ok)

	got := map[string]bool{}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		sel, isSel := n.(*ast.SelectorExpr)
		if !isSel || !isIdentNamed(sel.X, "utils") {
			return true
		}
		got[sel.Sel.Name] = importsShadowed("utils", sel.X.Pos(), stack)
		return true
	})

	want := map[string]bool{
		"A":            true,
		"Before":       false,
		"After":        true,
		"Var":          true,
		"Range":        true,
		"AfterLoop":    false,
		"Switch":       true,
		"If":           true,
		"AfterIf":      false,
		"Closure":      true,
		"AfterClosure": false,
		"Select":       true,
		"Nested":       true,
	}
	assert.Equal(t, want, got)
}

func Test_importsShadowed_SignatureIsNotShadowedByItsOwnParameter(t *testing.T) {
	t.Parallel()

	// The type of the second parameter is resolved in the file scope.
	src := "package a\n\nimport \"example.com/utils\"\n\nfunc f(utils int, x utils.T) {}\n"
	g, ok := parseSource(src)
	require.True(t, ok)

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if sel, isSel := n.(*ast.SelectorExpr); isSel {
			assert.False(t, importsShadowed("utils", sel.X.Pos(), stack))
		}
		return true
	})
}

func Test_importsDeclaredBefore_IgnoresNilStatements(t *testing.T) {
	t.Parallel()

	assert.False(t, importsDeclaredBefore([]ast.Stmt{nil}, "x", token.NoPos))
}
