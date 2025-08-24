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

		// UserContext() removed - Ctx implements context.Context
		reUserCtx := regexp.MustCompile(`(\w+)\.UserContext\(\)`)
		content = reUserCtx.ReplaceAllStringFunc(content, func(match string) string {
			parts := reUserCtx.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			ident := parts[1]
			if isFiberCtx(orig, ident) {
				return ident
			}
			return match
		})

		// old Context() returned fasthttp.RequestCtx
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "", content, parser.ParseComments)
		if err == nil {
			modified := false
			baseIdent := func(expr ast.Expr) *ast.Ident {
				for {
					switch e := expr.(type) {
					case *ast.Ident:
						return e
					case *ast.SelectorExpr:
						expr = e.X
					case *ast.CallExpr:
						expr = e.Fun
					default:
						return nil
					}
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Context" || len(call.Args) != 0 {
					return true
				}
				if ident := baseIdent(sel.X); ident != nil && isFiberCtx(orig, ident.Name) {
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

		// SetUserContext removed - comment out the call
		reSetUserCtx := regexp.MustCompile(`(?m)^(\s*)(.*?\b(\w+)\.SetUserContext\([^\n]*\).*)$`)
		content = reSetUserCtx.ReplaceAllStringFunc(content, func(line string) string {
			if strings.Contains(line, "TODO: SetUserContext was removed") {
				return line
			}
			parts := reSetUserCtx.FindStringSubmatch(line)
			if len(parts) != 4 {
				return line
			}
			ident := parts[3]
			if !isFiberCtx(orig, ident) {
				return line
			}
			return fmt.Sprintf("%s// TODO: SetUserContext was removed, please migrate manually: %s", parts[1], parts[2])
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
