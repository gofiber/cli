package v3

import (
	"fmt"
	"go/ast"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxContextMarker is appended to a file that has been through
// MigrateContextMethods. The migration renames Context to RequestCtx and
// UserContext to Context, so a second run would turn the migrated Context
// calls into RequestCtx ones: a marked file is left alone.
const ctxContextMarker = "// fiber:context-methods migrated"

// The methods of the context in v2 and v3.
const (
	ctxContext         = "Context"
	ctxUserContext     = "UserContext"
	ctxSetUserContext  = "SetUserContext"
	ctxSetContext      = "SetContext"
	ctxRequestCtxName  = "RequestCtx"
	ctxContextCallName = ".Context("
)

// MigrateContextMethods follows the renames of the context accessors:
//
//	c.Context()         (the *fasthttp.RequestCtx)  c.RequestCtx()
//	c.UserContext()     (the context.Context)       c.Context()
//	c.SetUserContext(x)                             c.SetContext(x)
//
// Only methods of a fiber context are rewritten.
func MigrateContextMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxContextMethodsContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate context methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating context methods")
	return nil
}

func ctxContextMethodsContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if strings.Contains(content, ctxContextMarker) ||
		!ctxContainsAny(content, ctxContextCallName, ctxUserContext, ctxSetUserContext) {
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

	// A context that already has SetContext, the v3 name, was migrated by hand
	// at least in part, and its Context() is the v3 one.
	var contextCalls []*ast.SelectorExpr
	v3Style := false
	var plan ctxPlan

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		switch sel.Sel.Name {
		case ctxContext:
			call, isCall := ctxParentCall(sel, stack)
			if isCall && len(call.Args) == 0 && env.receiverKind(sel, stack, ctxRequestCtxName) == ctxYes {
				contextCalls = append(contextCalls, sel)
			}
		case ctxSetContext:
			v3Style = v3Style || env.receiverKind(sel, stack) == ctxYes
		case ctxUserContext, ctxSetUserContext:
			renamed := ctxContext
			if sel.Sel.Name == ctxSetUserContext {
				renamed = ctxSetContext
			}
			switch env.receiverKind(sel, stack, renamed) {
			case ctxYes:
				plan.add(func(e *editSet) { e.replaceNode(g, sel.Sel, renamed) })
			case ctxUnknown:
				noter.notef(sel.Pos(),
					"%s was renamed %s in Fiber v3, but %s is not known to be a fiber context here, so it was left as is",
					g.text(sel), renamed, g.text(sel.X))
			case ctxNo:
			}
		}
		return true
	})

	if v3Style && len(contextCalls) > 0 {
		noter.notef(contextCalls[0].Pos(),
			"this file calls SetContext on a fiber context, so its Context() calls were taken for the v3 ones and not renamed to RequestCtx(); check those that need the *fasthttp.RequestCtx")
	} else {
		for _, sel := range contextCalls {
			plan.add(func(e *editSet) { e.replaceNode(g, sel.Sel, ctxRequestCtxName) })
		}
	}

	out := plan.apply(content)
	if out != content && !strings.Contains(out, ctxContextMarker) {
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += ctxContextMarker + "\n"
	}
	return out
}
