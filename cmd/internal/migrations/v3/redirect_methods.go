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

func MigrateRedirectMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		orig := content

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "", content, parser.ParseComments)
		if err != nil {
			return migrateRedirectStrings(content)
		}

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
			if !ok {
				return true
			}

			ident := baseIdent(sel.X)
			if ident == nil || !isFiberCtx(orig, ident.Name) {
				return true
			}

			switch sel.Sel.Name {
			case "Redirect":
				if len(call.Args) == 0 {
					return true
				}

				redirectCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: sel.X, Sel: ast.NewIdent("Redirect")}}
				target := call.Args[0]
				if len(call.Args) > 1 {
					redirectCall = &ast.CallExpr{
						Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("Status")},
						Args: []ast.Expr{call.Args[1]},
					}
				}

				*call = ast.CallExpr{
					Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("To")},
					Args: []ast.Expr{target},
				}
				modified = true
			case "RedirectBack":
				redirectCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: sel.X, Sel: ast.NewIdent("Redirect")}}
				args := call.Args
				if len(call.Args) > 1 {
					redirectCall = &ast.CallExpr{
						Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("Status")},
						Args: []ast.Expr{call.Args[len(call.Args)-1]},
					}
					args = call.Args[:len(call.Args)-1]
				}

				*call = ast.CallExpr{
					Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("Back")},
					Args: args,
				}
				modified = true
			case "RedirectToRoute":
				redirectCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: sel.X, Sel: ast.NewIdent("Redirect")}}
				args := call.Args
				if len(call.Args) > 2 {
					redirectCall = &ast.CallExpr{
						Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("Status")},
						Args: []ast.Expr{call.Args[len(call.Args)-1]},
					}
					args = call.Args[:len(call.Args)-1]
				}

				*call = ast.CallExpr{
					Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("Route")},
					Args: args,
				}
				modified = true
			default:
				return true
			}

			return true
		})

		if !modified {
			return migrateRedirectStrings(content)
		}

		var buf bytes.Buffer
		if err := format.Node(&buf, fset, file); err == nil {
			content = buf.String()
		}

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate redirect methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating redirect methods")
	return nil
}

func migrateRedirectStrings(content string) string {
	replacer := strings.NewReplacer(
		".RedirectBack(", ".Redirect().Back(",
		".RedirectToRoute(", ".Redirect().Route(",
	)

	re := regexp.MustCompile(`\.Redirect\([^)]`)
	content = re.ReplaceAllStringFunc(content, func(s string) string {
		return strings.Replace(s, ".Redirect(", ".Redirect().To(", 1)
	})

	return replacer.Replace(content)
}
