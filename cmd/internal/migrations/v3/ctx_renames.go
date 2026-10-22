package v3

import (
	"fmt"
	"go/ast"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// The methods of the request context whose meaning moved in v3.
const (
	ctxFormat   = "Format"
	ctxProtocol = "Protocol"
	ctxHostname = "Hostname"
	ctxRange    = "Range"
)

// MigrateCtxRenames follows the request context methods that kept their
// signature or gained a type, but lost their old meaning to another method:
//
//	c.Format(body)     c.AutoFormat(body)   Format takes ResFmt handlers in v3
//	c.Protocol()       c.Scheme()           Protocol is "HTTP/1.1" now, not "http"/"https"
//	c.Hostname()       c.Host()             Hostname leaves out the port now
//	c.Range(size)      c.Range(int64(size)) the size, and the ranges, are int64 in v3
//
// Only methods of a fiber context are rewritten.
func MigrateCtxRenames(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRunNotes()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateCtxRenamesContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate context method renames: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating context method renames")
	return nil
}

func migrateCtxRenamesContent(content string, fi internal.FileInfo, run *ctxRunNotes) string {
	if !ctxContainsAny(content, ".Format(", ".Protocol(", ".Hostname(", ".Range(") {
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

	env := newCtxEnv(g, names)
	noter := newCtxNoter(fi, g, run)
	var plan ctxPlan

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		rename, wrap := "", false
		switch {
		case sel.Sel.Name == ctxFormat && len(call.Args) == 1 && !call.Ellipsis.IsValid() && !ctxIsResFmt(env, call.Args[0], stack):
			rename = "AutoFormat"
		case sel.Sel.Name == ctxProtocol && len(call.Args) == 0:
			rename = "Scheme"
		case sel.Sel.Name == ctxHostname && len(call.Args) == 0:
			rename = "Host"
		case sel.Sel.Name == ctxRange && len(call.Args) == 1 && !call.Ellipsis.IsValid():
			wrap = true
		default:
			return true
		}
		if env.ctxKindOf(sel.X, stack) != ctxYes {
			return true
		}

		if rename != "" {
			plan.add(func(e *editSet) { e.replaceNode(g, sel.Sel, rename) })
			if rename == "AutoFormat" {
				noter.oncef("format", sel.Pos(),
					"AutoFormat negotiates the same formats as Format did, but answers a request without an Accept header in plain text where Format answered with an HTML paragraph")
			}
			return true
		}

		if arg := call.Args[0]; wrap && ctxNeedsInt64(env, arg, stack) {
			plan.add(func(e *editSet) {
				e.replaceNode(g, arg, "int64("+ctxRenderNode(e, g, arg)+")")
			})
		}
		noter.oncef("range", sel.Pos(),
			"c.Range takes an int64 size in Fiber v3, and the Start and End of the returned ranges are int64: convert them where they are used as int")
		return true
	})

	return plan.apply(content)
}

// ctxIsResFmt reports whether arg is a ResFmt, which only Format of v3 takes: such
// a call is v3 code already.
func ctxIsResFmt(env *ctxEnv, arg ast.Expr, stack []ast.Node) bool {
	t := env.typeOf(arg, stack)
	return t != nil && isFiberType(t, env.names, "ResFmt")
}

// ctxNeedsInt64 reports whether arg must be converted to be passed where an
// int64 is expected: everything but an integer constant, a conversion to int64
// and a variable declared as one.
func ctxNeedsInt64(env *ctxEnv, arg ast.Expr, stack []ast.Node) bool {
	switch x := arg.(type) {
	case *ast.BasicLit:
		return !ctxIsIntLiteral(x)
	case *ast.CallExpr:
		if isIdentNamed(x.Fun, "int64") {
			return false
		}
	case *ast.ParenExpr:
		return ctxNeedsInt64(env, x.X, stack)
	}

	return !isIdentNamed(env.typeOf(arg, stack), "int64")
}
