package v3

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
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
			return content
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

				if len(call.Args) > 1 {
					*call = wrapWithRedirectStatus(sel.X, call.Args[0], call.Args[1], "To")
				} else {
					redirectCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: sel.X, Sel: ast.NewIdent("Redirect")}}
					*call = ast.CallExpr{
						Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("To")},
						Args: []ast.Expr{call.Args[0]},
					}
				}
				modified = true
			case "RedirectBack":
				transformRedirectCall(call, sel.X, "Back", func(ctx ast.Expr, args []ast.Expr, status ast.Expr) ast.CallExpr {
					return wrapWithRedirectStatus(ctx, args[0], status, "Back")
				})
				modified = true
			case "RedirectToRoute":
				transformRedirectCall(call, sel.X, "Route", wrapRouteWithStatus)
				modified = true
			default:
				return true
			}

			return true
		})

		if !modified {
			return content
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

func transformRedirectCall(call *ast.CallExpr, ctx ast.Expr, method string, statusWrapper func(ast.Expr, []ast.Expr, ast.Expr) ast.CallExpr) {
	redirectCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ctx, Sel: ast.NewIdent("Redirect")}}
	args := call.Args

	if len(call.Args) > 1 && isStatusArg(call.Args[len(call.Args)-1]) {
		statusArg := call.Args[len(call.Args)-1]
		args = call.Args[:len(call.Args)-1]
		*call = statusWrapper(ctx, args, statusArg)
		return
	}

	*call = ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent(method)},
		Args: args,
	}
}

func wrapWithRedirectStatus(ctx, target, status ast.Expr, method string) ast.CallExpr {
	redirectCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ctx, Sel: ast.NewIdent("Redirect")}}

	targetIdent := ast.NewIdent("__fiberRedirectTarget")
	statusIdent := ast.NewIdent("__fiberRedirectStatus")

	redirectStatus := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("Status")},
		Args: []ast.Expr{statusIdent},
	}

	redirectWithTarget := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: redirectStatus, Sel: ast.NewIdent(method)},
		Args: []ast.Expr{targetIdent},
	}

	return ast.CallExpr{
		Fun: &ast.FuncLit{
			Type: &ast.FuncType{Params: &ast.FieldList{}},
			Body: &ast.BlockStmt{List: []ast.Stmt{
				&ast.AssignStmt{Lhs: []ast.Expr{targetIdent}, Tok: token.DEFINE, Rhs: []ast.Expr{target}},
				&ast.AssignStmt{Lhs: []ast.Expr{statusIdent}, Tok: token.DEFINE, Rhs: []ast.Expr{status}},
				&ast.ReturnStmt{Results: []ast.Expr{redirectWithTarget}},
			}},
		},
	}
}

func wrapRouteWithStatus(ctx ast.Expr, args []ast.Expr, status ast.Expr) ast.CallExpr {
	redirectCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ctx, Sel: ast.NewIdent("Redirect")}}
	statusIdent := ast.NewIdent("__fiberRedirectStatus")

	var routeArgIdents []ast.Expr
	var stmts []ast.Stmt
	for i, arg := range args {
		ident := ast.NewIdent(fmt.Sprintf("__fiberRedirectRouteArg%d", i))
		routeArgIdents = append(routeArgIdents, ident)
		stmts = append(stmts, &ast.AssignStmt{Lhs: []ast.Expr{ident}, Tok: token.DEFINE, Rhs: []ast.Expr{arg}})
	}

	stmts = append(stmts, &ast.AssignStmt{Lhs: []ast.Expr{statusIdent}, Tok: token.DEFINE, Rhs: []ast.Expr{status}})

	redirectStatus := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: redirectCall, Sel: ast.NewIdent("Status")},
		Args: []ast.Expr{statusIdent},
	}

	redirectRoute := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: redirectStatus, Sel: ast.NewIdent("Route")},
		Args: routeArgIdents,
	}

	stmts = append(stmts, &ast.ReturnStmt{Results: []ast.Expr{redirectRoute}})

	return ast.CallExpr{
		Fun: &ast.FuncLit{
			Type: &ast.FuncType{Params: &ast.FieldList{}},
			Body: &ast.BlockStmt{List: stmts},
		},
	}
}

func isStatusArg(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.BasicLit:
		return v.Kind == token.INT
	case *ast.UnaryExpr:
		return (v.Op == token.ADD || v.Op == token.SUB) && isStatusArg(v.X)
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok {
			return (ident.Name == "fiber" || ident.Name == "http") && strings.HasPrefix(v.Sel.Name, "Status")
		}
	}

	return false
}
