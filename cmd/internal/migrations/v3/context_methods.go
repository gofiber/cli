package v3

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateContextMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		orig := content

		// old Context() returned fasthttp.RequestCtx
		if !strings.Contains(orig, ".SetContext(") {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "", content, parser.ParseComments)
			if err == nil {
				modified := false
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Context" || len(call.Args) != 0 {
						return true
					}
					if ident := GetBaseIdent(sel.X); ident != nil && isFiberCtx(orig, ident.Name) {
						sel.Sel.Name = "RequestCtx"
						modified = true
					}
					return true
				})
				if modified {
					var buf bytes.Buffer
					if err := format.Node(&buf, fset, file); err == nil {
						content = buf.String()
					}
				}
			}
		}

		// UserContext() replaced by Context()
		reUserCtx := regexp.MustCompile(`(\w+)\.UserContext\(\)`)
		content = reUserCtx.ReplaceAllStringFunc(content, func(match string) string {
			parts := reUserCtx.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			ident := parts[1]
			if isFiberCtx(orig, ident) {
				return ident + ".Context()"
			}
			return match
		})

		// SetUserContext renamed to SetContext
		reSetUserCtx := regexp.MustCompile(`(\w+)\.SetUserContext\(`)
		content = reSetUserCtx.ReplaceAllStringFunc(content, func(match string) string {
			parts := reSetUserCtx.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			ident := parts[1]
			if isFiberCtx(orig, ident) {
				return ident + ".SetContext("
			}
			return match
		})

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate context methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating context methods")
	return nil
}
