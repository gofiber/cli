package v3

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/imports"
)

// appTestLiteral parses a file whose first composite literal of the type T is
// edited by the test.
func appTestLiteral(t *testing.T, src string) (*goSource, *ast.CompositeLit) {
	t.Helper()

	g, ok := parseSource(src)
	require.True(t, ok)
	var lit *ast.CompositeLit
	g.walk(func(n ast.Node, _ []ast.Node) bool {
		if c, isLit := n.(*ast.CompositeLit); isLit && lit == nil {
			lit = c
		}
		return lit == nil
	})
	require.NotNil(t, lit)
	return g, lit
}

func Test_appRemoveElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		repl  map[int]string
		name  string
		src   string
		want  string
		drop  []int
		works bool
	}{
		{
			name: "one line, first", drop: []int{0}, works: true,
			src:  "package p\nvar _ = T{A: 1, B: 2, C: 3}\n",
			want: "package p\nvar _ = T{B: 2, C: 3}\n",
		},
		{
			name: "one line, last", drop: []int{2}, works: true,
			src:  "package p\nvar _ = T{A: 1, B: 2, C: 3}\n",
			want: "package p\nvar _ = T{A: 1, B: 2}\n",
		},
		{
			name: "one line, several", drop: []int{1, 2}, works: true,
			src:  "package p\nvar _ = T{A: 1, B: 2, C: 3}\n",
			want: "package p\nvar _ = T{A: 1}\n",
		},
		{
			name: "one line, all", drop: []int{0, 1}, works: true,
			src:  "package p\nvar _ = T{A: 1, B: f(1, 2)}\n",
			want: "package p\nvar _ = T{}\n",
		},
		{
			name: "one line with a replacement", drop: []int{0}, works: true, repl: map[int]string{1: "X: 9"},
			src:  "package p\nvar _ = T{A: 1, B: 2}\n",
			want: "package p\nvar _ = T{X: 9}\n",
		},
		{
			name: "one line with a comment between", drop: []int{0}, works: false,
			src: "package p\nvar _ = T{A: 1, /* why */ B: 2}\n",
		},
		{
			name: "several lines", drop: []int{1}, works: true,
			src:  "package p\nvar _ = T{\n\tA: 1,\n\tB: f(1,\n\t\t2), // note\n\tC: 3,\n}\n",
			want: "package p\nvar _ = T{\n\tA: 1,\n\tC: 3,\n}\n",
		},
		{
			name: "several lines, all", drop: []int{0, 1}, works: true,
			src:  "package p\nvar _ = T{\n\tA: 1,\n\tB: 2,\n}\n",
			want: "package p\nvar _ = T{}\n",
		},
		{
			name: "several lines, all with a comment", drop: []int{0}, works: true,
			src:  "package p\nvar _ = T{\n\t// why\n\tA: 1,\n}\n",
			want: "package p\nvar _ = T{\n\t// why\n}\n",
		},
		{
			name: "several lines, shared line", drop: []int{0}, works: false,
			src: "package p\nvar _ = T{\n\tA: 1, B: 2,\n}\n",
		},
		{
			name: "several lines, closing brace on the line", drop: []int{1}, works: false,
			src: "package p\nvar _ = T{\n\tA: 1,\n\tB: 2}\n",
		},
		{
			name: "several lines with a replacement", drop: []int{0}, works: true, repl: map[int]string{1: "X: 9"},
			src:  "package p\nvar _ = T{\n\tA: 1,\n\tB: 2,\n}\n",
			want: "package p\nvar _ = T{\n\tX: 9,\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, lit := appTestLiteral(t, tt.src)
			var edits editSet
			ok := appRemoveElements(g, &edits, lit, tt.drop, tt.repl)
			assert.Equal(t, tt.works, ok)
			if !tt.works {
				assert.Equal(t, tt.src, edits.apply(tt.src), "nothing is scheduled")
				return
			}
			assert.Equal(t, tt.want, edits.apply(tt.src))
		})
	}
}

func Test_appWholeLines(t *testing.T) {
	t.Parallel()

	src := "a\n\tx := 1 // note\n\ty := 2, /* c */\n\tz := 3; w := 4\n"
	x := indexOf(src, "x :=")
	from, to, ok := appWholeLines(src, x, x+len("x := 1"))
	require.True(t, ok)
	assert.Equal(t, "\tx := 1 // note\n", src[from:to])

	y := indexOf(src, "y :=")
	from, to, ok = appWholeLines(src, y, y+len("y := 2"))
	require.True(t, ok)
	assert.Equal(t, "\ty := 2, /* c */\n", src[from:to])

	z := indexOf(src, "z :=")
	_, _, ok = appWholeLines(src, z, z+len("z := 3"))
	assert.False(t, ok, "another statement follows on the line")
}

// appFormat formats Go source the way the migrations do after an edit, with
// sorted and grouped imports; the imports that nothing uses stay.
func appFormat(t *testing.T, src string) string {
	t.Helper()

	opts := &imports.Options{Comments: true, TabIndent: true, TabWidth: 8, FormatOnly: true}
	out, err := imports.Process("main.go", []byte(src), opts)
	require.NoError(t, err, src)
	return string(out)
}

func indexOf(s, sub string) int {
	for i := range s {
		if len(s)-i >= len(sub) && s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func Test_appImports(t *testing.T) {
	t.Parallel()

	files := appTestFiles(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"a.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func static() {}

func main() { fmt.Println(fiber.New) }
`,
		"b.go": `package main

import "os"

var _ = os.Args
`,
		"c.go": `package main

func c() {}
`,
	})

	t.Run("an import the file has is used", func(t *testing.T) {
		t.Parallel()
		var imps appImports
		assert.Equal(t, "fmt.", imps.qualifier(files["a.go"], "fmt", "fmt"))
		assert.Equal(t, "fiber.", imps.fiberQualifier(files["a.go"]))
		assert.Empty(t, imps.reqs)
	})

	t.Run("a taken name gets another one", func(t *testing.T) {
		t.Parallel()
		var imps appImports
		// main.go declares a function called static
		assert.Equal(t, "staticmw.", imps.qualifier(files["a.go"], "github.com/gofiber/fiber/v3/middleware/static", "static"))
		assert.Equal(t, "staticmw.", imps.qualifier(files["a.go"], "github.com/gofiber/fiber/v3/middleware/static", "static"))
		assert.Equal(t, []appImportReq{{path: "github.com/gofiber/fiber/v3/middleware/static", alias: "staticmw"}}, imps.reqs)
	})

	t.Run("a file without fiber imports it the way the package does", func(t *testing.T) {
		t.Parallel()
		var imps appImports
		assert.Equal(t, "fiber.", imps.fiberQualifier(files["c.go"]))
		assert.Equal(t, []appImportReq{{path: "github.com/gofiber/fiber/v2"}}, imps.reqs)

		out := imps.apply("package main\n\nimport \"os\"\n\nvar _ = os.Args\n")
		assert.Equal(t, "package main\n\nimport (\n\t\"os\"\n\n\t\"github.com/gofiber/fiber/v2\"\n)\n\nvar _ = os.Args\n", appFormat(t, out))
	})

	t.Run("imports are added to every shape of import declaration", func(t *testing.T) {
		t.Parallel()
		imps := appImports{reqs: []appImportReq{{path: "time"}, {path: "example.com/x", alias: "xx"}}}

		want := "package p\n\nimport (\n\t\"os\"\n\t\"time\"\n\n\txx \"example.com/x\"\n)\n"
		assert.Equal(t, want, appFormat(t, imps.apply("package p\n\nimport (\n\t\"os\"\n)\n")), "a grouped declaration")
		assert.Equal(t, want, appFormat(t, imps.apply("package p\n\nimport \"os\"\n")), "a single import")
		assert.Equal(t, "package p\n\nimport (\n\t\"time\"\n\n\txx \"example.com/x\"\n)\n", appFormat(t, imps.apply("package p\n")), "no import at all")
		assert.Equal(t, "not go", imps.apply("not go"))
		assert.Equal(t, "package p\n", (&appImports{}).apply("package p\n"), "nothing to add")
	})
}

func Test_appConstStrings(t *testing.T) {
	t.Parallel()

	files := appTestFiles(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"main.go": `package main

import (
	"os"
	"path/filepath"
)

const (
	root   = "./public"
	prefix = "/" + "static"
	empty  = ""
)

var dynamic = os.Getenv("X")

func use(base string) {
	use(root)
	use(prefix)
	use(empty)
	use(dynamic)
	use(root + "/x")
	use(filepath.Join(base, "assets"))
	use(filepath.Join(base, empty))
	use(base)
	use(base + "/x")
	use(os.Getenv("Y"))
	use(` + "`raw`" + `)
}
`,
	})
	f := files["main.go"]

	var args []ast.Expr
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 1 && isIdentNamed(call.Fun, "use") {
			args = append(args, call.Args[0])
		}
		return true
	})
	require.Len(t, args, 11)

	type result struct {
		value    string
		constant bool
		notEmpty bool
	}
	want := []result{
		{"./public", true, true},
		{"/static", true, true},
		{"", true, false},
		{"", false, false},
		{"./public/x", true, true},
		{"", false, true},
		{"", false, false},
		{"", false, false},
		{"", false, true},
		{"", false, false},
		{"raw", true, true},
	}
	for i, arg := range args {
		value, constant := f.constString(arg, 0)
		if !constant {
			value = "" // the text of an expression that is not constant means nothing
		}
		got := result{value, constant, f.provablyNotEmpty(arg, 0)}
		assert.Equal(t, want[i], got, f.g.text(arg))
	}
}

func Test_appImports_apply(t *testing.T) {
	t.Parallel()

	const (
		static = "github.com/gofiber/fiber/v3/middleware/static"
		etag   = "github.com/gofiber/fiber/v3/middleware/etag"
	)

	tests := []struct {
		name string
		src  string
		want string
		reqs []appImportReq
	}{
		{
			name: "a third party import joins the third party group",
			reqs: []appImportReq{{path: static}},
			src:  "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n\nvar _ = fmt.Sprint\n",
			want: "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3\"\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n)\n\nvar _ = fmt.Sprint\n",
		},
		{
			name: "a standard library import joins the standard library group",
			reqs: []appImportReq{{path: "time"}},
			src:  "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n\nvar _ = fmt.Sprint\n",
			want: "package p\n\nimport (\n\t\"fmt\"\n\t\"time\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n\nvar _ = fmt.Sprint\n",
		},
		{
			name: "several imports of both kinds",
			reqs: []appImportReq{{path: static}, {path: "time"}, {path: etag}, {path: "crypto/tls"}},
			src:  "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n\nvar _ = fmt.Sprint\n",
			want: "package p\n\nimport (\n\t\"crypto/tls\"\n\t\"fmt\"\n\t\"time\"\n\n\t\"github.com/gofiber/fiber/v3\"\n\t\"github.com/gofiber/fiber/v3/middleware/etag\"\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n)\n\nvar _ = fmt.Sprint\n",
		},
		{
			name: "a standard library import in a block of third party ones is a group of its own",
			reqs: []appImportReq{{path: "time"}},
			src:  "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
			want: "package p\n\nimport (\n\t\"time\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
		},
		{
			name: "a third party import in a block of standard library ones is a group of its own",
			reqs: []appImportReq{{path: static}, {path: etag}},
			src:  "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nvar _, _ = fmt.Sprint, os.Args\n",
			want: "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n\t\"github.com/gofiber/fiber/v3/middleware/etag\"\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n)\n\nvar _, _ = fmt.Sprint, os.Args\n",
		},
		{
			name: "an alias is kept",
			reqs: []appImportReq{{path: static, alias: "staticmw"}},
			src:  "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
			want: "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n\tstaticmw \"github.com/gofiber/fiber/v3/middleware/static\"\n)\n",
		},
		{
			name: "the comment of an import stays with it",
			reqs: []appImportReq{{path: etag}},
			src:  "package p\n\nimport (\n\t// the web framework\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
			want: "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3/middleware/etag\"\n\t// the web framework\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
		},
		{
			name: "an import that shares the line of the parenthesis",
			reqs: []appImportReq{{path: static}, {path: "time"}},
			src:  "package p\n\nimport (\"fmt\"\n\t\"os\")\n\nvar _, _ = fmt.Sprint, os.Args\n",
			want: "package p\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"time\"\n\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n)\n\nvar _, _ = fmt.Sprint, os.Args\n",
		},
		{
			name: "everything on one line",
			reqs: []appImportReq{{path: static}},
			src:  "package p\n\nimport (\"fmt\")\n\nvar _ = fmt.Sprint\n",
			want: "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n)\n\nvar _ = fmt.Sprint\n",
		},
		{
			name: "an empty declaration",
			reqs: []appImportReq{{path: "time"}},
			src:  "package p\n\nimport ()\n",
			want: "package p\n\nimport (\n\t\"time\"\n)\n",
		},
		{
			name: "cgo keeps its import and its comment",
			reqs: []appImportReq{{path: static}},
			src:  "package p\n\n// #include <stdio.h>\nimport \"C\"\n\nvar _ = C.puts\n",
			want: "package p\n\n// #include <stdio.h>\nimport \"C\"\n\nimport (\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n)\n\nvar _ = C.puts\n",
		},
		{
			name: "cgo next to another import",
			reqs: []appImportReq{{path: static}},
			src:  "package p\n\nimport \"fmt\"\n\n// #include <stdio.h>\nimport \"C\"\n\nvar _, _ = C.puts, fmt.Sprint\n",
			want: "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n)\n\n// #include <stdio.h>\nimport \"C\"\n\nvar _, _ = C.puts, fmt.Sprint\n",
		},
		{
			name: "a single import",
			reqs: []appImportReq{{path: static}},
			src:  "package p\n\n// Package comment.\nimport \"fmt\" // the printer\n\nvar _ = fmt.Sprint\n",
			want: "package p\n\n// Package comment.\nimport (\n\t\"fmt\"\n\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n) // the printer\n\nvar _ = fmt.Sprint\n",
		},
		{
			name: "no import at all",
			reqs: []appImportReq{{path: static}, {path: "time"}},
			src:  "package p\n\nvar x = 1\n",
			want: "package p\n\nimport (\n\t\"time\"\n\n\t\"github.com/gofiber/fiber/v3/middleware/static\"\n)\n\nvar x = 1\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			imps := appImports{reqs: tt.reqs}
			out := imps.apply(tt.src)
			assert.Equal(t, tt.want, appFormat(t, out))

			_, ok := parseSource(out)
			assert.True(t, ok, "the result is valid Go before formatting: %s", out)
		})
	}
}
