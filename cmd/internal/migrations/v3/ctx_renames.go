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
//	c.Range(size)      c.Range(int64(size)) the size is int64 in v3, and so are the Start and End of the ranges:
//	                                        where they are read, int(rs.Start) gives the int of v2
//
// The same goes for the methods used as values (f := c.Protocol becomes
// f := c.Scheme; f := c.Range a function literal with the signature of v2) and
// as method expressions. Only methods of a fiber context are rewritten.
func MigrateCtxRenames(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxRenamesContent(content, fi, run)
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

func ctxRenamesContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !ctxContainsAny(content, ".Format", ".Protocol", ".Hostname", ctxTypeRange) {
		return content
	}
	g, env, ok := run.open(fi, content)
	if !ok {
		return content
	}
	noter := newCtxNoter(fi, g, run)
	var plan ctxPlan

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		call, isCall := ctxParentCall(sel, stack)
		// A method expression, (*fiber.Ctx).Range, is a value even when it is called:
		// the context is its first argument.
		expr := env.isCtxType(sel.X, stack)
		isValue := !isCall || expr

		rename, wrap := "", false
		switch sel.Sel.Name {
		case ctxFormat:
			if !isValue && (len(call.Args) != 1 || call.Ellipsis.IsValid() || ctxIsResFmt(env, call.Args[0], stack)) {
				return true
			}
			rename = "AutoFormat"
		case ctxProtocol:
			if !isValue && len(call.Args) != 0 {
				return true
			}
			rename = "Scheme"
		case ctxHostname:
			if !isValue && len(call.Args) != 0 {
				return true
			}
			rename = "Host"
		case ctxRange:
			if !isValue && (len(call.Args) != 1 || call.Ellipsis.IsValid()) {
				return true
			}
			wrap = !isValue
		default:
			return true
		}

		kind, via := ctxYes, ""
		if !expr {
			kind, via = env.receiverKindVia(sel, stack, rename)
		}
		if kind != ctxYes {
			return true
		}

		switch {
		case rename != "":
			plan.qualify(g, sel, via)
			plan.add(func(e *editSet) { e.replaceNode(g, sel.Sel, rename) })
			if rename == "AutoFormat" {
				noter.oncef("format", sel.Pos(),
					"AutoFormat negotiates the same formats as Format did, but answers a request without an Accept header in plain text where Format answered with an HTML paragraph")
			}
		case isValue:
			// Range as a value is a function that takes an int and returns the ranges
			b := env.newValueBuilder(sel, stack, nil)
			plan.add(func(e *editSet) { e.replaceNode(g, sel, b.text(e, b.rangeOf())) })
		case wrap && ctxNeedsInt64(env, call.Args[0], stack):
			arg := call.Args[0]
			plan.add(func(e *editSet) {
				e.replaceNode(g, arg, "int64("+ctxRenderNode(e, g, arg)+")")
			})
		}
		return true
	})

	// The Start and End of the ranges are int64 in v3, where v2 had int.
	ctxQueueRangeReads(&plan, noter, env)

	return env.finish(content, plan.apply(content))
}

// ctxIsResFmt reports whether arg is a ResFmt, which only Format of v3 takes: such
// a call is v3 code already.
func ctxIsResFmt(env *ctxEnv, arg ast.Expr, stack []ast.Node) bool {
	t := env.typeOf(arg, stack)
	return t != nil && ctxIsFiberType(t, env.names, "ResFmt")
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
