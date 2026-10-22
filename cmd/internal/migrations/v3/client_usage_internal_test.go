package v3

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bodyStmts parses the statements of a function body.
func bodyStmts(t *testing.T, body string) []ast.Stmt {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc f() {\n"+body+"\n}", parser.SkipObjectResolution)
	require.NoError(t, err)
	fn, ok := file.Decls[0].(*ast.FuncDecl)
	require.True(t, ok)
	return fn.Body.List
}

func Test_clientRefsIn_HonorsScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		refs int
		end  int
	}{
		{name: "plain uses", body: "use(errs); use(errs[0])", refs: 2, end: 2},
		{name: "selector field is not a use", body: "x.errs = 1; use(y.errs)", refs: 0, end: 2},
		{name: "redeclared in a nested if", body: "if errs := f(); errs != nil { use(errs) }; use(errs)", refs: 1, end: 2},
		{name: "initializer sees the outer variable", body: "if errs := g(errs); errs != nil { use(errs) }", refs: 1, end: 1},
		{name: "redeclared in a block", body: "{ errs := 1; use(errs) }; use(errs)", refs: 1, end: 2},
		{name: "parameter of a function literal", body: "f := func(errs int) int { return errs }; use(errs)", refs: 1, end: 2},
		{name: "captured by a function literal", body: "f := func() int { return len(errs) }", refs: 1, end: 1},
		{name: "range variable", body: "for _, errs := range xs { use(errs) }; use(errs)", refs: 1, end: 2},
		{name: "top level redeclaration ends the scope", body: "use(errs); a, errs := g(); use(errs)", refs: 1, end: 1},
		{name: "case clause scope", body: "switch x { case 1: errs := 1; use(errs); case 2: use(errs) }", refs: 1, end: 1},
		{name: "type switch binding", body: "switch errs := v.(type) { case int: use(errs) }; use(errs)", refs: 1, end: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			refs, end := clientRefsIn("errs", bodyStmts(t, tt.body))
			assert.Len(t, refs, tt.refs)
			assert.Equal(t, tt.end, end)
		})
	}
}

func Test_clientParseQuery(t *testing.T) {
	t.Parallel()

	pairs, ok := clientParseQuery("q=hello%20world&tags=a&tags=b&flag&x=a+b&")
	require.True(t, ok)
	assert.Equal(t, []clientParam{
		{"q", "hello world"}, {"tags", "a"}, {"tags", "b"}, {"flag", ""}, {"x", "a b"},
	}, pairs)

	_, ok = clientParseQuery("q=%zz")
	assert.False(t, ok)
}

func Test_clientConfigLit(t *testing.T) {
	t.Parallel()

	assert.Empty(t, clientConfigLit("client", nil))
	assert.Equal(t, `, client.Config{Timeout: 5 * time.Second}`,
		clientConfigLit("client", []clientCfg{{field: "Timeout", val: "5 * time.Second"}}))
	assert.Equal(t, `, client.Config{Header: map[string]string{"A": a, "B": b}, Referer: r}`,
		clientConfigLit("client", []clientCfg{
			{field: fieldHeader, key: `"A"`, val: "a"},
			{field: "Referer", val: "r"},
			{field: fieldHeader, key: `"B"`, val: "b"},
		}))
	lit := clientConfigLit("c", []clientCfg{
		{field: fieldHeader, key: `"A"`, val: "a"},
		{field: fieldCookie, key: `"k"`, val: "v"},
		{field: "Timeout", val: "d"},
	})
	assert.Equal(t, ", c.Config{\nHeader: map[string]string{\"A\": a},\nCookie: map[string]string{\"k\": v},\nTimeout: d,\n}", lit)
}

func Test_clientFunc_FreshNames(t *testing.T) {
	t.Parallel()

	fn := &clientFunc{mentions: map[string]bool{"resp": true, "resp2": true}}
	assert.Equal(t, "resp3", fn.fresh("resp"))
	assert.Equal(t, "resp4", fn.fresh("resp"), "a name that was handed out is taken")
	assert.Equal(t, "other", fn.fresh("other"))
}

func Test_clientLenComparison(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"len(e) > 0":  "err != nil",
		"len(e) != 0": "err != nil",
		"len(e) >= 1": "err != nil",
		"0 < len(e)":  "err != nil",
		"len(e) == 0": "err == nil",
		"len(e) < 1":  "err == nil",
		"0 == len(e)": "err == nil",
		"1 > len(e)":  "err == nil",
		"len(e) > 1":  "",
		"len(e) == 2": "",
	}
	for src, want := range tests {
		expr, err := parser.ParseExpr(src)
		require.NoError(t, err)
		cmp, ok := expr.(*ast.BinaryExpr)
		require.True(t, ok, src)
		call, isCall := cmp.X.(*ast.CallExpr)
		if !isCall {
			call, isCall = cmp.Y.(*ast.CallExpr)
			require.True(t, isCall, src)
		}
		got, ok := clientLenComparison(cmp, call, "err")
		assert.Equal(t, want != "", ok, src)
		assert.Equal(t, want, got, src)
	}
}

func gofmtSource(t *testing.T, src string) string {
	t.Helper()

	formatted, err := format.Source([]byte(src))
	require.NoError(t, err, "the source does not parse:\n%s", src)
	return strings.TrimSpace(string(formatted)) + "\n"
}

func Test_gofmtSource(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "package p\n", gofmtSource(t, "package   p"))
}
