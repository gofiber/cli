package v3

import (
	"fmt"
	"go/ast"
	"slices"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxBinders maps the parser methods of the v2 Ctx to the binder of v3 that
// replaces them.
var ctxBinders = map[string]string{
	nameBodyParser:      clientFieldBody,
	nameCookieParser:    clientFieldCookie,
	nameParamsParser:    "URI",
	nameQueryParser:     ctxFnQuery,
	nameReqHeaderParser: "Header",
}

// ctxAllParams is the v2 method that returned all route parameters as a map.
const ctxAllParams = "AllParams"

// MigrateParserMethods turns the parsers of the request context into the
// binders of v3 (c.BodyParser(&v) becomes c.Bind().Body(&v)) and replaces
// AllParams, which v3 does not have, with the code that builds the same map.
// Only methods of a fiber context are rewritten.
func MigrateParserMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	methods := []string{"BodyParser", "CookieParser", "ParamsParser", "QueryParser", ctxAllParams}
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxMigrateParserCalls(content, fi, run, methods)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate parser methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating parser methods")
	return nil
}

// ctxMigrateParserCalls rewrites the calls of the given parser methods of the
// request context, and reports those whose receiver it cannot tell.
func ctxMigrateParserCalls(content string, fi internal.FileInfo, run *ctxRun, methods []string) string {
	if !ctxContainsAny(content, methods...) {
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
		if !ok || !slices.Contains(methods, sel.Sel.Name) {
			return true
		}

		kind, via := env.receiverKindVia(sel, stack, "Bind")
		switch kind {
		case ctxYes:
			if sel.Sel.Name == ctxAllParams {
				ctxQueueAllParams(&plan, noter, env, sel, stack)
				return true
			}
			plan.qualify(g, sel, via)
			plan.add(func(e *editSet) {
				e.replaceNode(g, sel.Sel, "Bind()."+ctxBinders[sel.Sel.Name])
			})
		case ctxUnknown:
			noter.notef(sel.Pos(),
				"%s looks like a Fiber v2 context method, but %s is not known to be a fiber context here, so it was left as is: in v3 use %s",
				g.text(sel), g.text(sel.X), ctxParserHint(sel))
		case ctxNo:
		}
		return true
	})

	return plan.apply(content)
}

// ctxParserHint spells the v3 replacement of the parser method sel.
func ctxParserHint(sel *ast.SelectorExpr) string {
	if binder, ok := ctxBinders[sel.Sel.Name]; ok {
		return "Bind()." + binder + "(...)"
	}
	return "a loop over Route().Params that calls Params"
}

// ctxQueueAllParams queues the replacement of the call c.AllParams(), which v3
// removed, with a function literal that builds the same map.
func ctxQueueAllParams(plan *ctxPlan, noter *ctxNoter, env *ctxEnv, sel *ast.SelectorExpr, stack []ast.Node) {
	call, isCall := ctxParentCall(sel, stack)
	if !isCall || len(call.Args) != 0 {
		noter.notef(sel.Pos(), "AllParams does not exist in Fiber v3: build the map with a loop over c.Route().Params that calls c.Params(name)")
		return
	}

	plan.add(func(e *editSet) {
		recv := ctxRenderNode(e, env.g, sel.X)
		e.replaceNode(env.g, call, fmt.Sprintf(`func(c %s) map[string]string {
	params := make(map[string]string, len(c.Route().Params))
	for _, name := range c.Route().Params {
		params[name] = c.Params(name)
	}
	return params
}(%s)`, env.qual(ctxTypeCtx), recv))
	})
}
