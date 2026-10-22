package v3

import (
	"fmt"
	"go/ast"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateViewBind renames c.Bind(vars), which added variables to the views, to
// c.ViewBind(vars): v3 uses Bind for the request binders. The method used as a
// value or as a method expression is renamed too. Only the methods of a fiber
// context are rewritten.
func MigrateViewBind(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxViewBindContent(content, fi, run)
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

func ctxViewBindContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !ctxContainsAny(content, ".Bind") {
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
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Bind" {
			return true
		}

		// c.Bind(vars) adds view variables; c.Bind() is the binder of v3. The
		// method used as a value, f := c.Bind, and as a method expression,
		// (*fiber.Ctx).Bind, is the one of v2.
		call, isCall := ctxParentCall(sel, stack)
		expr := env.isCtxType(sel.X)
		if isCall && !expr && len(call.Args) != 1 {
			return true
		}

		kind, via := ctxYes, ""
		if !expr {
			kind, via = env.receiverKindVia(sel, stack, "ViewBind")
		}
		switch kind {
		case ctxYes:
			plan.qualify(g, sel, via)
			plan.add(func(e *editSet) { e.replaceNode(g, sel.Sel, "ViewBind") })
		case ctxUnknown:
			if isCall && ctxIsFiberMap(call.Args[0], env) {
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
		return x.Type != nil && ctxIsFiberType(x.Type, env.names, "Map")
	case *ast.CallExpr:
		return ctxIsFiberType(x.Fun, env.names, "Map")
	}
	return false
}
