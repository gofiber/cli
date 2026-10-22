package v3

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ctxStableOf returns, for every use of an identifier as the receiver of the
// method probe in src, whether ctxVariableStable vouches for the variable, keyed
// by the text of the first argument.
func ctxStableOf(t *testing.T, src string) map[string]bool {
	t.Helper()

	g, ok := parseSource(src)
	require.True(t, ok)

	got := map[string]bool{}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall || len(call.Args) != 1 {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "probe" {
			return true
		}
		id, isIdent := sel.X.(*ast.Ident)
		if !isIdent {
			return true
		}
		got[g.text(call.Args[0])] = ctxVariableStable(id.Name, id.Pos(), stack)
		return true
	})
	return got
}

func Test_ctxVariableStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want map[string]bool
		name string
		src  string
	}{
		{
			name: "a parameter that nothing assigns",
			src: `package main

func handler(c *ctx, other *ctx) {
	c.probe("plain")
	func() { c.probe("inside a closure") }()
	other.probe("the other parameter")
}
`,
			want: map[string]bool{`"plain"`: true, `"inside a closure"`: true, `"the other parameter"`: true},
		},
		{
			name: "assigned, incremented or addressed",
			src: `package main

func assigned(c *ctx, other *ctx) {
	c.probe("assigned")
	c = other
}

func addressed(c *ctx) {
	c.probe("addressed")
	reset(&c)
}

func multi(c *ctx) {
	c.probe("assigned in a tuple")
	c, err := next()
	_ = err
}

func loop(c *ctx) {
	c.probe("assigned in a loop")
	for c = first(); c != nil; c = c.next {
	}
}

func counter(n *int) {
	n.probe("incremented")
	*n++
	n++
}
`,
			want: map[string]bool{`"assigned"`: false, `"addressed"`: false, `"assigned in a tuple"`: false, `"assigned in a loop"`: false, `"incremented"`: false},
		},
		{
			name: "variables of the function",
			src: `package main

func handler() {
	c := make()
	c.probe("declared once")

	var d = make()
	d.probe("declared by var")

	e := make()
	e.probe("assigned later")
	e = make()

	for _, f := range list() {
		f.probe("a range variable")
	}
	for g := range list() {
		g.probe("a range key")
	}
}
`,
			want: map[string]bool{`"declared once"`: true, `"declared by var"`: true, `"assigned later"`: false, `"a range variable"`: true, `"a range key"`: true},
		},
		{
			name: "a name that is declared again below is not vouched for above",
			src: `package main

func handler(c *ctx) {
	c.probe("outer")
	{
		c := other()
		c.probe("inner")
	}
}

func lits() {
	a := func(c *ctx) { c.probe("first literal") }
	b := func(c *ctx) { c.probe("second literal") }
	a(nil)
	b(nil)
}
`,
			want: map[string]bool{`"outer"`: false, `"inner"`: true, `"first literal"`: true, `"second literal"`: true},
		},
		{
			name: "package-level variables and unknown names are not vouched for",
			src: `package main

var c *ctx

func handler() {
	c.probe("package level")
	d.probe("unknown")
}
`,
			want: map[string]bool{`"package level"`: false, `"unknown"`: false},
		},
		{
			name: "fields and parameters of types do not count as declarations",
			src: `package main

type holder struct{ c *ctx }

func handler(c *ctx, h struct{ c *ctx }, f func(c int)) {
	c.probe("a field of the same name")
	var local struct{ c int }
	_ = local
}
`,
			want: map[string]bool{`"a field of the same name"`: true},
		},
		{
			name: "a type switch binding",
			src: `package main

func handler(v any) {
	switch c := v.(type) {
	case *ctx:
		c.probe("single type")
	default:
		c.probe("default")
	}
}
`,
			want: map[string]bool{`"single type"`: true, `"default"`: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ctxStableOf(t, tc.src))
		})
	}
}
