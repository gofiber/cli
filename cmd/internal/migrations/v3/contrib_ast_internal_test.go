package v3

import (
	"go/ast"
	"go/format"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// firstLiteral parses src and returns the first composite literal in it.
func firstLiteral(t *testing.T, src string) (*goSource, *ast.CompositeLit) {
	t.Helper()

	g, ok := parseSource(src)
	require.True(t, ok, src)

	var lit *ast.CompositeLit
	g.walk(func(n ast.Node, _ []ast.Node) bool {
		if l, ok := n.(*ast.CompositeLit); ok && lit == nil {
			lit = l
		}
		return lit == nil
	})
	require.NotNil(t, lit, src)
	return g, lit
}

func Test_contribRemoveElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lit  string
		want string
		idx  []int
	}{
		{
			name: "line of its own, first",
			lit:  "T{\n\tA: 1,\n\tB: 2,\n\tC: 3,\n}",
			idx:  []int{0},
			want: "T{\n\tB: 2,\n\tC: 3,\n}",
		},
		{
			name: "line of its own, middle with a comment",
			lit:  "T{\n\tA: 1,\n\tB: 2, // two\n\tC: 3,\n}",
			idx:  []int{1},
			want: "T{\n\tA: 1,\n\tC: 3,\n}",
		},
		{
			name: "line of its own, last",
			lit:  "T{\n\tA: 1,\n\tB: 2,\n}",
			idx:  []int{1},
			want: "T{\n\tA: 1,\n}",
		},
		{
			name: "element that spans lines",
			lit:  "T{\n\tA: func() {\n\t\treturn\n\t},\n\tB: 2,\n}",
			idx:  []int{0},
			want: "T{\n\tB: 2,\n}",
		},
		{
			name: "all of them",
			lit:  "T{\n\tA: 1,\n\tB: 2,\n}",
			idx:  []int{0, 1},
			want: "T{\n}",
		},
		{
			name: "one line, first",
			lit:  "T{A: 1, B: 2, C: 3}",
			idx:  []int{0},
			want: "T{B: 2, C: 3}",
		},
		{
			name: "one line, middle",
			lit:  "T{A: 1, B: 2, C: 3}",
			idx:  []int{1},
			want: "T{A: 1, C: 3}",
		},
		{
			name: "one line, last",
			lit:  "T{A: 1, B: 2, C: 3}",
			idx:  []int{2},
			want: "T{A: 1, B: 2}",
		},
		{
			name: "one line, only",
			lit:  "T{A: 1}",
			idx:  []int{0},
			want: "T{}",
		},
		{
			name: "one line, neighbors go together",
			lit:  "T{A: 1, B: 2, C: 3, D: 4}",
			idx:  []int{1, 2},
			want: "T{A: 1, D: 4}",
		},
		{
			name: "one line, the last two",
			lit:  "T{A: 1, B: 2, C: 3}",
			idx:  []int{1, 2},
			want: "T{A: 1}",
		},
		{
			name: "one line, apart",
			lit:  "T{A: 1, B: 2, C: 3, D: 4}",
			idx:  []int{0, 2},
			want: "T{B: 2, D: 4}",
		},
		{
			name: "first element shares the line of the brace",
			lit:  "T{A: 1,\n\tB: 2,\n}",
			idx:  []int{0},
			want: "T{\n\tB: 2,\n}",
		},
		{
			name: "last element of a line that goes on",
			lit:  "T{\n\tA: 1, B: 2,\n\tC: 3,\n}",
			idx:  []int{1},
			want: "T{\n\tA: 1, \n\tC: 3,\n}",
		},
		{
			name: "windows line breaks",
			lit:  "T{\r\n\tA: 1,\r\n\tB: 2,\r\n}",
			idx:  []int{0},
			want: "T{\r\n\tB: 2,\r\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := "package p\n\nvar _ = " + tt.lit + "\n"
			g, lit := firstLiteral(t, src)

			var edits editSet
			contribRemoveElements(g, &edits, lit, tt.idx...)

			got := edits.apply(src)
			assert.Equal(t, "package p\n\nvar _ = "+tt.want+"\n", got)
			_, err := format.Source([]byte(got))
			require.NoError(t, err, got)
		})
	}
}

func Test_contribAppendElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lit  string
		want string
	}{
		{name: "empty", lit: "T{}", want: "T{X: 1}"},
		{name: "empty over lines", lit: "T{\n}", want: "T{\n\tX: 1,\n}"},
		{name: "one line", lit: "T{A: 1}", want: "T{A: 1, X: 1}"},
		{name: "one line with a trailing comma", lit: "T{A: 1,}", want: "T{A: 1, X: 1,}"},
		{name: "one per line", lit: "T{\n\tA: 1,\n\tB: 2,\n}", want: "T{\n\tA: 1,\n\tB: 2,\n\tX: 1,\n}"},
		{name: "comment after the last one", lit: "T{\n\tA: 1,\n\tB: 2, // two\n}", want: "T{\n\tA: 1,\n\tB: 2, // two\n\tX: 1,\n}"},
		{name: "last element spans lines", lit: "T{\n\tA: func() {\n\t},\n}", want: "T{\n\tA: func() {\n\t},\n\tX: 1,\n}"},
		{name: "indented", lit: "T{\n\t\tA: 1,\n\t}", want: "T{\n\t\tA: 1,\n\t\tX: 1,\n\t}"},
		{name: "windows line breaks", lit: "T{\r\n\tA: 1,\r\n}", want: "T{\r\n\tA: 1,\r\n\tX: 1,\r\n}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := "package p\n\nvar _ = " + tt.lit + "\n"
			g, lit := firstLiteral(t, src)

			var edits editSet
			contribAppendElement(g, &edits, lit, "X: 1")

			got := edits.apply(src)
			assert.Equal(t, "package p\n\nvar _ = "+tt.want+"\n", got)
			_, err := format.Source([]byte(got))
			require.NoError(t, err, got)
		})
	}
}

func Test_contribAddImport(t *testing.T) {
	t.Parallel()

	const fiberPath = "github.com/gofiber/fiber/v3"

	tests := []struct {
		name     string
		src      string
		path     string
		alias    string
		want     string
		wantName string
	}{
		{
			name:     "no imports",
			src:      "package p\n\nvar x = 1\n",
			path:     fiberPath,
			want:     "package p\n\nimport \"github.com/gofiber/fiber/v3\"\n\nvar x = 1\n",
			wantName: "fiber",
		},
		{
			name:     "single import",
			src:      "package p\n\nimport \"fmt\"\n\nvar x = 1\n",
			path:     fiberPath,
			want:     "package p\n\nimport \"fmt\"\nimport \"github.com/gofiber/fiber/v3\"\n\nvar x = 1\n",
			wantName: "fiber",
		},
		{
			name:     "third party import goes to the last group",
			src:      "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/a/b\"\n)\n",
			path:     fiberPath,
			want:     "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/a/b\"\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
			wantName: "fiber",
		},
		{
			name:     "standard library import goes to the first group",
			src:      "package p\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/a/b\"\n)\n",
			path:     "slices",
			want:     "package p\n\nimport (\n\t\"slices\"\n\t\"fmt\"\n\n\t\"github.com/a/b\"\n)\n",
			wantName: "slices",
		},
		{
			name:     "standard library import opens a group when there is none",
			src:      "package p\n\nimport (\n\t\"github.com/a/b\"\n)\n",
			path:     "errors",
			want:     "package p\n\nimport (\n\t\"errors\"\n\n\t\"github.com/a/b\"\n)\n",
			wantName: "errors",
		},
		{
			name:     "explicit name",
			src:      "package p\n\nimport (\n\t\"github.com/a/b\"\n)\n",
			path:     fiberPath + "/extractors",
			alias:    "fiberextractors",
			want:     "package p\n\nimport (\n\t\"github.com/a/b\"\n\tfiberextractors \"github.com/gofiber/fiber/v3/extractors\"\n)\n",
			wantName: "fiberextractors",
		},
		{
			name:     "already imported",
			src:      "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
			path:     fiberPath,
			want:     "package p\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n",
			wantName: "fiber",
		},
		{
			name:     "already imported under another name",
			src:      "package p\n\nimport f \"github.com/gofiber/fiber/v3\"\n",
			path:     fiberPath,
			want:     "package p\n\nimport f \"github.com/gofiber/fiber/v3\"\n",
			wantName: "f",
		},
		{
			name:     "imported for its side effects only",
			src:      "package p\n\nimport _ \"github.com/gofiber/fiber/v3\"\n",
			path:     fiberPath,
			want:     "package p\n\nimport _ \"github.com/gofiber/fiber/v3\"\nimport \"github.com/gofiber/fiber/v3\"\n",
			wantName: "fiber",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, ok := parseSource(tt.src)
			require.True(t, ok)

			var edits editSet
			name := contribAddImport(g, &edits, tt.path, tt.alias)

			got := edits.apply(tt.src)
			assert.Equal(t, tt.wantName, name)
			_, err := format.Source([]byte(got))
			require.NoError(t, err, got)
			assert.Equal(t, tt.want, got)
		})
	}
}
