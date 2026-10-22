package v3

import (
	"fmt"
	"go/ast"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateViewBind renames c.Bind(vars), which added variables to the views, to
// c.ViewBind(vars): v3 uses Bind for the request binders. Only the calls of a
// fiber context are rewritten.
func MigrateViewBind(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateViewBindContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate ViewBind calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating view binding helpers")
	return nil
}

func migrateViewBindContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !ctxContainsAny(content, ".Bind(") {
		return content
	}
	names := fiberNames(content)
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	env := newCtxEnv(g, names, run.siblings(fi, g)...)
	noter := newCtxNoter(fi, g, run)
	var plan ctxPlan

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Bind" {
			return true
		}

		switch env.receiverKind(sel, stack, "ViewBind") {
		case ctxYes:
			plan.add(func(e *editSet) { e.replaceNode(g, sel.Sel, "ViewBind") })
		case ctxUnknown:
			if ctxIsFiberMap(call.Args[0], env) {
				noter.notef(sel.Pos(),
					"%s adds view variables in Fiber v2 and is called ViewBind in v3, but %s is not known to be a fiber context here, so it was left as is",
					g.text(sel), g.text(sel.X))
			}
		case ctxNo:
		}
		return true
	})

	return plan.apply(content)
}

// ctxIsFiberMap reports whether e is a fiber.Map literal or conversion.
func ctxIsFiberMap(e ast.Expr, env *ctxEnv) bool {
	switch x := e.(type) {
	case *ast.CompositeLit:
		return x.Type != nil && isFiberType(x.Type, env.names, "Map")
	case *ast.CallExpr:
		return isFiberType(x.Fun, env.names, "Map")
	}
	return false
}
