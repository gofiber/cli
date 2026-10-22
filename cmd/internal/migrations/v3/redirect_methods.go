package v3

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// The redirect methods of the v2 Ctx.
const (
	ctxRedirect        = "Redirect"
	ctxRedirectBack    = "RedirectBack"
	ctxRedirectToRoute = "RedirectToRoute"
)

// MigrateRedirectMethods turns the redirect methods of the request context into
// the Redirect() builder of v3:
//
//	c.Redirect(to[, status])                 c.Redirect().[Status(status).]To(to)
//	c.RedirectBack(fallback[, status])       c.Redirect().[Status(status).]Back(fallback)
//	c.RedirectToRoute(name, params[, status]) c.Redirect().[Status(status).]Route(name, fiber.RedirectConfig{Params: params})
//
// Only methods of a fiber context are rewritten.
func MigrateRedirectMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxRedirectContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate redirect methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating redirect methods")
	return nil
}

func ctxRedirectContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !strings.Contains(content, ctxRedirect) {
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
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		switch sel.Sel.Name {
		case ctxRedirect:
			// c.Redirect() without arguments is the builder of v3.
			if len(call.Args) == 0 {
				return true
			}
		case ctxRedirectBack, ctxRedirectToRoute:
		default:
			return true
		}

		kind, via := env.receiverKindVia(sel, stack, ctxRedirect)
		switch kind {
		case ctxYes:
			ctxQueueRedirect(&plan, noter, env, call, sel, via)
		case ctxUnknown:
			// Redirect alone is too common a name to tell anything.
			if sel.Sel.Name != ctxRedirect {
				noter.notef(sel.Pos(),
					"%s looks like a Fiber v2 context method, but %s is not known to be a fiber context here, so it was left as is: in v3 use Redirect().%s(...)",
					g.text(sel), g.text(sel.X), ctxRedirectTarget[sel.Sel.Name].method)
			}
		case ctxNo:
		}
		return true
	})

	return plan.apply(content)
}

// ctxRedirectTarget is the method of the v3 builder that ends the chain, and the
// number of arguments before the status the v2 method takes.
var ctxRedirectTarget = map[string]struct {
	method string
	args   int
}{
	ctxRedirect:        {method: "To", args: 1},
	ctxRedirectBack:    {method: "Back", args: 1},
	ctxRedirectToRoute: {method: "Route", args: 2},
}

// ctxQueueRedirect queues the rewrite of one call of a v2 redirect method. The
// arguments before the status are the same for the v2 method and its v3
// counterpart, except for the params of RedirectToRoute, which move into the
// RedirectConfig, so it is the position that tells the status, not its type. via
// is the field to go through when the receiver is a wrapper that hides Redirect.
func ctxQueueRedirect(plan *ctxPlan, noter *ctxNoter, env *ctxEnv, call *ast.CallExpr, sel *ast.SelectorExpr, via string) {
	g := env.g
	name := sel.Sel.Name
	target := ctxRedirectTarget[name]

	// The status comes after the target and, for a route, the params.
	statusAt := target.args
	if call.Ellipsis.IsValid() || len(call.Args) > statusAt+1 {
		noter.notef(sel.Pos(),
			"%s was called with more arguments than Redirect().%s takes: rewrite this call by hand as Redirect().Status(code).%s(...)",
			g.text(sel), target.method, target.method)
		return
	}

	hasStatus := len(call.Args) > statusAt
	if !hasStatus {
		noter.oncef("redirect-status", sel.Pos(),
			"A redirect without a status answers 303 See Other in Fiber v3, where v2 answered 302 Found: add .Status(%s) to the ones where 302 matters",
			env.qual("StatusFound"))
	}

	// Only the name changes while the arguments stay where they are.
	hasParams := name == ctxRedirectToRoute && len(call.Args) > 1
	plan.qualify(g, sel, via)
	if !hasStatus && !hasParams {
		plan.add(func(e *editSet) { e.replaceNode(g, sel.Sel, "Redirect()."+target.method) })
		return
	}

	plan.add(func(e *editSet) {
		text := "Redirect()"
		if hasStatus {
			text += ".Status(" + ctxRenderNode(e, g, call.Args[statusAt]) + ")"
		}

		var args []string
		for i, arg := range call.Args[:min(len(call.Args), statusAt)] {
			if name == ctxRedirectToRoute && i == 1 {
				if cfg := ctxRouteConfig(e, noter, env, arg); cfg != "" {
					args = append(args, cfg)
				}
				continue
			}
			args = append(args, ctxRenderNode(e, g, arg))
		}
		text += "." + target.method + "(" + strings.Join(args, ", ") + ")"

		if comments := ctxStrayComments(g, call); len(comments) > 0 {
			text += " " + strings.Join(comments, " ")
		}
		e.replace(g.offset(sel.Sel.Pos()), g.offset(call.End()), text)
	})
}

// ctxRouteConfig returns the RedirectConfig that carries the params of a v2
// RedirectToRoute: its Params and, for the "queries" entry v2 took out of the
// params, its Queries. It is empty when there is nothing to pass.
func ctxRouteConfig(e *editSet, noter *ctxNoter, env *ctxEnv, params ast.Expr) string {
	g := env.g
	cfgType := env.qual("RedirectConfig")

	if isIdentNamed(params, "nil") {
		return ""
	}

	lit, ok := params.(*ast.CompositeLit)
	if !ok {
		noter.notef(params.Pos(),
			"RedirectToRoute took a \"queries\" entry of the params as the query string; in v3 pass it as RedirectConfig.Queries if %s can hold one",
			g.text(params))
		return cfgType + "{Params: " + ctxRenderNode(e, g, params) + "}"
	}

	var entry *ast.KeyValueExpr
	for _, elt := range lit.Elts {
		if kv, isKV := elt.(*ast.KeyValueExpr); isKV && ctxIsStringLiteral(kv.Key, "queries") {
			entry = kv
			break
		}
	}
	if entry == nil {
		return cfgType + "{Params: " + ctxRenderNode(e, g, lit) + "}"
	}

	value := ctxRenderNode(e, g, entry.Value)
	if len(lit.Elts) == 1 {
		return cfgType + "{Queries: " + value + "}"
	}

	// The literal without the entry, which keeps the rest of it as it is.
	without := editSet{edits: slices.Clone(e.edits)}
	start, end := ctxElementSpan(g, entry)
	without.replace(start, end, "")
	rest := ctxRender(&without, g.src, g.offset(lit.Pos()), g.offset(lit.End()))
	return cfgType + "{Params: " + rest + ", Queries: " + value + "}"
}

// ctxElementSpan returns the offsets of an element of a composite literal, the
// comma that follows it and, when the element has its lines to itself, those
// lines.
func ctxElementSpan(g *goSource, elt ast.Expr) (start, end int) {
	start, end = g.offset(elt.Pos()), g.offset(elt.End())
	if rest := strings.TrimLeft(g.src[end:], " \t"); strings.HasPrefix(rest, ",") {
		end = len(g.src) - len(rest) + 1
	}

	lineStart := strings.LastIndexByte(g.src[:start], '\n') + 1
	if strings.TrimSpace(g.src[lineStart:start]) != "" {
		return start, end
	}
	if nl := strings.IndexByte(g.src[end:], '\n'); nl >= 0 && strings.TrimSpace(g.src[end:end+nl]) == "" {
		return lineStart, end + nl + 1
	}
	return start, end
}

// ctxIsStringLiteral reports whether e is the string literal "value".
func ctxIsStringLiteral(e ast.Expr, value string) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && (lit.Value == `"`+value+`"` || lit.Value == "`"+value+"`")
}
