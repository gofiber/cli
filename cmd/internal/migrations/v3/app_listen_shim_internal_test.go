package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shim is a package of this module, which is built and tested like the rest
// of it, and the migration adds its text to the projects. These tests tie the two.
func Test_appShimGenerated(t *testing.T) {
	t.Parallel()

	files := []struct {
		generated func(string) generatedFile
		embedded  string
		name      string
		names     []string
	}{
		{
			generated: appShimGenerated, embedded: appShimEmbedded, name: appShimFile,
			names: []string{appShimNew, appShimConfig},
		},
		{
			generated: appShimCallsGenerated, embedded: appShimCallsEmbedded, name: appShimCallsFile,
			names: []string{appShimListen, appShimListener},
		},
	}

	for _, tt := range files {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gf := tt.generated(appFiberV3Path)
			assert.Equal(t, tt.name, gf.name)
			assert.Contains(t, gf.source, gf.marker, "a later run knows the file by it")

			// the text is the source of the package, with the name of the package left open
			assert.Equal(t, 1, strings.Count(gf.source, generatedPkgPlaceholder))
			assert.Equal(t, tt.embedded, strings.ReplaceAll(gf.source, generatedPkgPlaceholder, "listenshim"))

			// it becomes a file of any package, and imports fiber from where the project does
			for _, pkg := range []string{"main", "server"} {
				src := strings.ReplaceAll(tt.generated("example.com/fork/fiber/v3").source, generatedPkgPlaceholder, pkg)
				file, err := parser.ParseFile(token.NewFileSet(), tt.name, src, parser.ImportsOnly)
				require.NoError(t, err)
				assert.Equal(t, pkg, file.Name.Name)

				var imports []string
				for _, spec := range file.Imports {
					imports = append(imports, spec.Path.Value)
				}
				assert.Contains(t, imports, `"example.com/fork/fiber/v3"`)
				assert.NotContains(t, imports, appShimFiberImp)
			}

			// every name it declares is the shim's: nothing of the project's can be taken for one
			// of them, and the planner knows the names it writes into the code
			file, err := parser.ParseFile(token.NewFileSet(), tt.name, tt.embedded, parser.SkipObjectResolution)
			require.NoError(t, err)
			var declared []string
			for _, d := range file.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					declared = append(declared, d.Name.Name)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							declared = append(declared, spec.Name.Name)
						case *ast.ValueSpec:
							for _, id := range spec.Names {
								declared = append(declared, id.Name)
							}
						}
					}
				}
			}
			for _, name := range declared {
				assert.True(t, strings.HasPrefix(name, "fiberMigrate"), name)
				assert.False(t, ast.IsExported(name), name)
			}
			assert.ElementsMatch(t, declared, gf.idents, "the names that block the file")
			for _, name := range tt.names {
				assert.Contains(t, declared, name)
			}
		})
	}
}

func Test_isShimFile(t *testing.T) {
	t.Parallel()

	gf := appShimGenerated(appFiberV3Path)

	assert.True(t, isShimFile("/p/"+appShimFile, gf.source))
	assert.True(t, isShimFile("/p/"+appShimCallsFile, appShimCallsGenerated(appFiberV3Path).source))
	assert.False(t, isShimFile("/p/"+appShimFile, "package main\n"), "a file of the project with that name")
	assert.False(t, isShimFile("/p/main.go", gf.source), "a file with another name")
}

func Test_appAfterStatement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string // what follows the position it returns
		end  int
	}{
		{name: "nothing follows", src: "a := 1\nb := 2\n", end: 6, want: "\nb := 2\n"},
		{name: "a line comment stays with the statement", src: "a := 1 // one\nb := 2\n", end: 6, want: "\nb := 2\n"},
		{name: "a block comment stays with the statement", src: "a := 1 /* one */\nb := 2\n", end: 6, want: "\nb := 2\n"},
		{name: "windows line endings", src: "a := 1 // one\r\nb := 2\r\n", end: 6, want: "\r\nb := 2\r\n"},
		{name: "the end of the file", src: "a := 1", end: 6, want: ""},
		{name: "code follows on the line", src: "a := 1; b := 2\n", end: 6, want: "; b := 2\n"},
		{name: "a block comment and code", src: "a := 1 /* one */ b()\n", end: 6, want: " /* one */ b()\n"},
		{name: "a block that ends the line", src: "{ a := 1 }\n", end: 8, want: " }\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			at := appAfterStatement(tt.src, tt.end)
			assert.Equal(t, tt.want, tt.src[at:])
		})
	}
}

func Test_appPureReceiver(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"app":            true,
		"s.app":          true,
		"s.srv.app":      true,
		"(s.app)":        true,
		"*p":             true,
		"apps[0]":        true,
		"apps[i]":        true,
		"s.apps[name]":   true,
		"build()":        false,
		"s.build().app":  false,
		"apps[next()]":   false,
		"fiber.New()":    false,
		"<-ch":           false,
		"s.apps[i+1]":    false,
		"(&Server{}).ap": false,
	}

	for src, want := range tests {
		t.Run(src, func(t *testing.T) {
			t.Parallel()

			e, err := parser.ParseExpr(src)
			require.NoError(t, err)
			assert.Equal(t, want, appPureReceiver(e))
		})
	}
}

func Test_appEditedText(t *testing.T) {
	t.Parallel()

	src := "package p\n\nvar a = fiber.New(cfg)\n"
	g, ok := parseSource(src)
	require.True(t, ok)
	decl, isGen := g.file.Decls[0].(*ast.GenDecl)
	require.True(t, isGen)
	spec, isValue := decl.Specs[0].(*ast.ValueSpec)
	require.True(t, isValue)
	call := spec.Values[0]
	start := g.offset(call.Pos())

	edits := []textEdit{
		{start: start, end: start + len("fiber.New"), text: "fiberMigrateNew"},
		{start: start + len("fiber.New(cfg"), end: start + len("fiber.New(cfg"), text: ", cfgListen"},
		{start: 0, end: 0, text: "// before\n"},
	}

	text, rest := appEditedText(edits, g, call)

	assert.Equal(t, "fiberMigrateNew(cfg, cfgListen)", text)
	assert.Equal(t, []textEdit{{start: 0, end: 0, text: "// before\n"}}, rest)
}
