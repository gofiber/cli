package v3

import (
	"go/ast"
	"go/parser"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ctxIsPure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expr string
		want bool
	}{
		{expr: "c", want: true},
		{expr: "301", want: true},
		{expr: `"/x"`, want: true},
		{expr: "fiber.StatusFound", want: true},
		{expr: "opts.codes", want: true},
		{expr: "h.c.codes[0]", want: true},
		{expr: "codes[1:]", want: true},
		{expr: "(a + b) * 2", want: true},
		{expr: "-n", want: true},
		{expr: "*p", want: true},
		{expr: "int(x)", want: true},
		{expr: "[]byte(s)", want: true},
		{expr: `len("x")`, want: true},
		{expr: "min(a, b)", want: true},
		{expr: "[]int{1, 2}", want: true},
		{expr: `map[string]int{"a": 1}`, want: true},
		{expr: "func() int { return 1 }", want: true},
		{expr: "next()", want: false},
		{expr: "c.Path()", want: false},
		{expr: "c.Status(200)", want: false},
		{expr: "codes()", want: false},
		{expr: "int(next())", want: false},
		{expr: "<-ch", want: false},
		{expr: "[]int{next()}", want: false},
		{expr: "m[next()]", want: false},
		{expr: "append(a, b)", want: false},
		{expr: "f(xs...)", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()

			e, err := parser.ParseExpr(tc.expr)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ctxIsPure(e))
		})
	}
}

func Test_ctxNamer(t *testing.T) {
	t.Parallel()

	e, err := parser.ParseExpr("h.c.Redirect(status.URL(), codes[i])")
	require.NoError(t, err)

	// The fields behind a selector are no names in scope, the receivers and the
	// arguments are.
	namer := newCtxNamer([]ast.Expr{e}, "fiber")
	assert.True(t, namer["h"])
	assert.True(t, namer["status"])
	assert.True(t, namer["codes"])
	assert.True(t, namer["i"])
	assert.True(t, namer["fiber"])
	assert.False(t, namer["c"])
	assert.False(t, namer["URL"])

	assert.Equal(t, "c", namer.fresh("c"))
	assert.Equal(t, "c2", namer.fresh("c"))
	assert.Equal(t, "status2", namer.fresh("status"))
	assert.Equal(t, "status3", namer.fresh("status"))
}

func Test_ctxLiteral(t *testing.T) {
	t.Parallel()

	pure, err := parser.ParseExpr("codes")
	require.NoError(t, err)
	call, err := parser.ParseExpr("pick()")
	require.NoError(t, err)

	t.Run("operands that do something become parameters", func(t *testing.T) {
		t.Parallel()

		lit := &ctxLiteral{namer: ctxNamer{}}
		assert.Equal(t, "to", lit.bind(ctxOperand{expr: call, text: "next()", typ: "string", name: "to"}))
		assert.Equal(t, "status", lit.bind(ctxOperand{expr: call, text: "pick()", typ: "int", name: "status"}))
		assert.Equal(t, "_", lit.bind(ctxOperand{text: "a(), b()", typ: "...int", unused: true}))
		assert.Equal(t, "func(to string, status int, _ ...int) error {\nreturn 1\n}(next(), pick(), a(), b())",
			lit.text("error", "return 1"))
	})

	t.Run("the ones that do not stay where they are", func(t *testing.T) {
		t.Parallel()

		lit := &ctxLiteral{namer: ctxNamer{}}
		assert.Equal(t, "codes", lit.bind(ctxOperand{expr: pure, text: "codes", typ: "...int", name: "codes", spread: true}))
		assert.Empty(t, lit.bind(ctxOperand{expr: pure, text: "codes", unused: true}))
		assert.Equal(t, "func() error {\nreturn 1\n}()", lit.text("error", "return 1"))
	})

	t.Run("a spread is passed on", func(t *testing.T) {
		t.Parallel()

		lit := &ctxLiteral{namer: ctxNamer{"codes": true}}
		assert.Equal(t, "codes2", lit.bind(ctxOperand{expr: call, text: "list()", typ: "...int", name: "codes", spread: true}))
		assert.Equal(t, "func(codes2 ...int) error {\nreturn 1\n}(list()...)", lit.text("error", "return 1"))
	})
}
