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

// clientBodyStmts parses the statements of a function body.
func clientBodyStmts(t *testing.T, body string) []ast.Stmt {
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

			refs, end := clientRefsIn("errs", clientBodyStmts(t, tt.body))
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
			{field: clientFieldHeader, key: `"A"`, val: "a"},
			{field: "Referer", val: "r"},
			{field: clientFieldHeader, key: `"B"`, val: "b"},
		}))
	lit := clientConfigLit("c", []clientCfg{
		{field: clientFieldHeader, key: `"A"`, val: "a"},
		{field: clientFieldCookie, key: `"k"`, val: "v"},
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

func Test_clientJoinChain(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "client.C().R().SetMethod(\"GET\")", clientJoinChain([]string{"client.C()", "R()", "SetMethod(\"GET\")"}))
	assert.Equal(t, "a."+strings.Repeat("x", 120), clientJoinChain([]string{"a", strings.Repeat("x", 120)}),
		"a chain of two parts stays on a line however long it is")

	long := clientJoinChain([]string{"client.C()", "R()", "SetMethod(\"GET\")", "SetURL(url)", "SetHeader(a, b)", "AddHeader(c, d)", "Send()"})
	assert.Equal(t, "client.C().R().\nSetMethod(\"GET\").\nSetURL(url).\nSetHeader(a, b).\nAddHeader(c, d).\nSend()", long)

	wide := clientJoinChain([]string{"client.New()", "R()", "SetURL(" + strings.Repeat("u", 90) + ")", "Send()"})
	assert.Contains(t, wide, ".\nSend()")
}

func Test_clientSkipBlankLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		del  string
		want string
	}{
		{name: "after the opening brace", src: "f() {\n\tx()\n\n\ty()\n}", del: "\tx()\n", want: "f() {\n\ty()\n}"},
		{name: "between two blank lines", src: "a()\n\n\tx()\n\n\ty()", del: "\tx()\n", want: "a()\n\n\ty()"},
		{name: "after a statement", src: "a()\n\tx()\n\n\ty()", del: "\tx()\n", want: "a()\n\n\ty()"},
		{name: "before a closing brace", src: "f() {\n\tx()\n}", del: "\tx()\n", want: "f() {\n}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ls := strings.Index(tt.src, tt.del)
			le := ls + len(tt.del)
			end := clientSkipBlankLine(tt.src, ls, le)
			assert.Equal(t, tt.want, tt.src[:ls]+tt.src[end:])
		})
	}
}

func Test_clientBlockEnds(t *testing.T) {
	t.Parallel()

	f := &clientFile{log: []string{"log"}}
	tests := map[string]bool{
		"return err":                      true,
		"x(); return":                     true,
		"continue":                        true,
		"break":                           true,
		"panic(err)":                      true,
		"log.Fatal(err)":                  true,
		"log.Panicf(\"x\")":               true,
		"os.Exit(1)":                      true,
		"log.Println(err)":                false,
		"x()":                             false,
		"":                                false,
		"if a { return } else { return }": true,
		"if a { return }":                 false,
		"if a { return } else { x() }":    false,
		"{ return }":                      true,
		"if a { return } else if b { return } else { panic(1) }": true,
	}
	for body, want := range tests {
		stmts := clientBodyStmts(t, body)
		assert.Equal(t, want, f.blockEnds(&ast.BlockStmt{List: stmts}), body)
	}
}

func Test_clientOverwritten(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"use(1)":                       true,
		"err = f(); use(err)":          true,
		"x, err = f(); use(err)":       true,
		"use(err)":                     false,
		"if err != nil { return }":     false,
		"err = g(err)":                 false,
		"err := f(); use(err)":         true,
		"err := f(err)":                false,
		"use(1); err = f()":            true,
		"if c { err = f() }; use(err)": false,
	}
	for body, want := range tests {
		assert.Equal(t, want, clientOverwritten("err", clientBodyStmts(t, body)), body)
	}
}

func Test_clientDirect(t *testing.T) {
	t.Parallel()

	assigns := []clientTermAssign{
		{side: clientTermSide{text: "code", isNew: true}, expr: "resp.StatusCode()"},
		{side: clientTermSide{text: "body", isNew: true}, expr: "resp.Body()"},
	}
	assert.Equal(t, []string{"code, body := resp.StatusCode(), resp.Body()"}, clientDirect(assigns))

	assigns[0].side.isNew = false
	assert.Equal(t, []string{"code = resp.StatusCode()", "body := resp.Body()"}, clientDirect(assigns))
	assert.Empty(t, clientDirect(nil))
}

func Test_clientErrsSlice(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"x", "var errs []error"}, clientErrsSlice([]string{"x"}, clientErrsOwn, "errs", "err", false))
	assert.Equal(t, []string{"errs = nil"}, clientErrsSlice(nil, clientErrsExisting, "errs", "err", false))
	assert.Equal(t, []string{"var errs []error", "if err != nil {", "errs = []error{err}", "}"},
		clientErrsSlice(nil, clientErrsOwn, "errs", "err", true))
	assert.Empty(t, clientErrsSlice(nil, clientErrsRewrite, "errs", "err", true))
}

func Test_clientBodyMedia(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "application/json", clientBodyMedia(clientBodyJSON))
	assert.Equal(t, "application/xml", clientBodyMedia(clientBodyXML))
	assert.Equal(t, "application/x-www-form-urlencoded", clientBodyMedia(clientBodyForm))
	assert.Equal(t, "multipart/form-data", clientBodyMedia(clientBodyMultipart))
	assert.Empty(t, clientBodyMedia(clientBodyRaw))
	assert.Empty(t, clientBodyMedia(clientBodyNone))
}

func clientGofmtSource(t *testing.T, src string) string {
	t.Helper()

	formatted, err := format.Source([]byte(src))
	require.NoError(t, err, "the source does not parse:\n%s", src)
	return strings.TrimSpace(string(formatted)) + "\n"
}

func Test_clientGofmtSource(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "package p\n", clientGofmtSource(t, "package   p"))
}
