package v3

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseGoFile_InvalidContent(t *testing.T) {
	t.Parallel()

	_, err := parseGoFile("package main\n func")
	assert.Error(t, err)
}

func Test_collectImportAliases(t *testing.T) {
	t.Parallel()

	tests := map[string]struct { //nolint:govet // fieldalignment warning is not relevant for test data shapes
		content    string
		importPath string
		expected   map[string]struct{}
	}{
		"default alias": {
			importPath: "github.com/gofiber/fiber/v3/middleware/session",
			content:    "package main\nimport \"github.com/gofiber/fiber/v3/middleware/session\"\n",
			expected:   map[string]struct{}{"session": {}},
		},
		"explicit alias": {
			importPath: "github.com/gofiber/fiber/v3/middleware/session",
			content:    "package main\nimport sess \"github.com/gofiber/fiber/v3/middleware/session\"\n",
			expected:   map[string]struct{}{"sess": {}},
		},
		"blank import ignored": {
			importPath: "github.com/gofiber/fiber/v3/middleware/session",
			content:    "package main\nimport _ \"github.com/gofiber/fiber/v3/middleware/session\"\n",
			expected:   map[string]struct{}{},
		},
		"dot import ignored": {
			importPath: "github.com/gofiber/fiber/v3/middleware/session",
			content:    "package main\nimport . \"github.com/gofiber/fiber/v3/middleware/session\"\n",
			expected:   map[string]struct{}{},
		},
		"unrelated import": {
			importPath: "github.com/gofiber/fiber/v3/middleware/session",
			content:    "package main\nimport \"github.com/example/other\"\n",
			expected:   map[string]struct{}{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			file, err := parseGoFile(tt.content)
			require.NoError(t, err)

			aliases := collectImportAliases(file, tt.importPath)
			assert.Equal(t, tt.expected, aliases)
		})
	}
}

func Test_collectAssignedCallIdents(t *testing.T) {
	t.Parallel()

	content := `package main

func target() (int, error) { return 0, nil }
func other() int { return 1 }

func main() {
    primary, secondary := target()
    single := target()
    _, captured := target()
    value, err := other()
    first, second := other(), target()
    field.Name = target()
}
`

	file, err := parseGoFile(content)
	require.NoError(t, err)

	matches := collectAssignedCallIdents(file, func(call *ast.CallExpr) bool {
		if ident, ok := call.Fun.(*ast.Ident); ok {
			return ident.Name == "target"
		}
		return false
	})

	assert.Contains(t, matches, "primary")
	assert.Contains(t, matches, "secondary")
	assert.Contains(t, matches, "single")
	assert.Contains(t, matches, "captured")
	assert.Contains(t, matches, "second")
	assert.NotContains(t, matches, "value")
	assert.NotContains(t, matches, "first")
}
