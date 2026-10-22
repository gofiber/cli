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
//	c.Redirect(to[, status])                  c.Redirect().Status(status).To(to)
//	c.RedirectBack(fallback[, status])        c.Redirect().Status(status).Back(fallback)
//	c.RedirectToRoute(name, params[, status]) c.Redirect().Status(status).Route(name, fiber.RedirectConfig{Params: params})
//
// v2 answers 302 Found when there is no status where the builder of v3 answers
// 303 See Other, so the status is spelled out: fiber.StatusFound. Only the first
// status counts in v2; a spread of statuses becomes a function literal that
// picks the first one, or the default. Only methods of a fiber context are
// rewritten.
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
		if sel, isSel := n.(*ast.SelectorExpr); isSel {
			ctxQueueRedirectValue(&plan, noter, env, sel, stack)
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || env.isCtxType(sel.X) {
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

// ctxQueueRedirectValue queues the replacement of a redirect method that is used
// as a value, without being called, with a function literal that has the
// signature of the v2 method and calls the builder of v3 (see ctxValueBuilder).
func ctxQueueRedirectValue(plan *ctxPlan, noter *ctxNoter, env *ctxEnv, sel *ast.SelectorExpr, stack []ast.Node) {
	method := sel.Sel.Name
	if _, ok := ctxRedirectTarget[method]; !ok {
		return
	}

	// A method expression, (*fiber.Ctx).Redirect, is a value even when it is
	// called: the context is its first argument.
	kind := ctxYes
	if !env.isCtxType(sel.X) {
		if _, isCall := ctxParentCall(sel, stack); isCall {
			return
		}
		kind = env.receiverKind(sel, stack)
	}
	switch kind {
	case ctxYes:
		b := env.newValueBuilder(sel, stack)
		plan.add(func(e *editSet) { e.replaceNode(env.g, sel, b.text(e, b.redirect(method))) })
	case ctxUnknown:
		// Redirect alone is too common a name to tell anything.
		if method != ctxRedirect {
			noter.notef(sel.Pos(),
				"%s looks like a Fiber v2 context method used as a value, but %s is not known to be a fiber context here, so it was left as is: in v3 call Redirect().%s(...) from a function literal",
				env.g.text(sel), env.g.text(sel.X), ctxRedirectTarget[method].method)
		}
	case ctxNo:
	}
}

// ctxRedirectTarget is the method of the v3 builder that ends the chain, the
// number of arguments before the status the v2 method takes and the name its
// first argument gets as the parameter of a function literal.
var ctxRedirectTarget = map[string]struct {
	method string
	param  string
	args   int
}{
	ctxRedirect:        {method: "To", param: "to", args: 1},
	ctxRedirectBack:    {method: "Back", param: "fallback", args: 1},
	ctxRedirectToRoute: {method: "Route", param: "name", args: 2},
}

// ctxQueueRedirect queues the rewrite of one call of a v2 redirect method. The
// arguments before the status are the same for the v2 method and its v3
// counterpart, except for the params of RedirectToRoute, which move into the
// RedirectConfig, so it is the position that tells the status, not its type. via
// is the field to go through when the receiver is a wrapper that hides Redirect.
//
// v2 answers 302 Found unless it is told otherwise where the builder of v3
// answers 303 See Other, so a call without a status gets Status(StatusFound).
// v2 uses the first status only and ignores the others: the ones after it are
// left out, and a call that has a spread of statuses becomes a function literal
// that picks the first one.
func ctxQueueRedirect(plan *ctxPlan, noter *ctxNoter, env *ctxEnv, call *ast.CallExpr, sel *ast.SelectorExpr, via string) {
	g := env.g
	name := sel.Sel.Name
	target := ctxRedirectTarget[name]

	// The status comes after the target and, for a route, the params.
	statusAt := target.args
	var statuses []ast.Expr
	if len(call.Args) > statusAt {
		statuses = call.Args[statusAt:]
	}
	spread := call.Ellipsis.IsValid()
	if spread && len(statuses) != 1 {
		noter.notef(sel.Pos(),
			"%s was called with a spread of arguments that Redirect().%s does not take: rewrite this call by hand as Redirect().Status(code).%s(...)",
			g.text(sel), target.method, target.method)
		return
	}

	if spread || ctxRedirectNeedsLiteral(call.Args[:min(len(call.Args), statusAt)], statuses) {
		ctxQueueRedirectLiteral(plan, noter, env, call, sel, via)
		return
	}

	// Only the name changes while the arguments stay where they are.
	hasParams := name == ctxRedirectToRoute && len(call.Args) > 1
	plan.qualify(g, sel, via)
	if len(statuses) == 0 && !hasParams {
		plan.add(func(e *editSet) {
			e.replaceNode(g, sel.Sel, "Redirect().Status("+env.qual(ctxStatusFound)+")."+target.method)
		})
		return
	}

	plan.add(func(e *editSet) {
		status := env.qual(ctxStatusFound)
		kept := call.Args[:min(len(call.Args), statusAt)]
		if len(statuses) > 0 {
			status = ctxRenderNode(e, g, statuses[0])
			kept = append(slices.Clone(kept), statuses[0])
		}
		text := "Redirect().Status(" + status + ")"

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

		if comments := ctxStrayComments(g, call, kept); len(comments) > 0 {
			text += " " + strings.Join(comments, " ")
		}
		e.replace(g.offset(sel.Sel.Pos()), g.offset(call.End()), text)
	})
}

// ctxStatusFound is the status v2 answers a redirect with when it is not told
// another one.
const ctxStatusFound = "StatusFound"

// ctxRedirectNeedsLiteral reports whether a call without a spread of statuses
// can not simply be rewritten as the chain of the builder: a status after the
// first is only left out when evaluating it does nothing, and the first one
// moves in front of the arguments that precede it, which is only the same when
// not both of them have effects.
func ctxRedirectNeedsLiteral(before, statuses []ast.Expr) bool {
	if len(statuses) == 0 {
		return false
	}
	if slices.ContainsFunc(statuses[1:], func(e ast.Expr) bool { return !ctxIsPure(e) }) {
		return true
	}
	return !ctxIsPure(statuses[0]) && slices.ContainsFunc(before, func(e ast.Expr) bool { return !ctxIsPure(e) })
}

// ctxQueueRedirectLiteral queues the rewrite of a call whose statuses have to
// be evaluated or picked at run time as a function literal that is called at
// once:
//
//	c.Redirect(to, codes...)    func() error {
//	                                status := fiber.StatusFound
//	                                if len(codes) > 0 { status = codes[0] }
//	                                return c.Redirect().Status(status).To(to)
//	                            }()
//
// The operands that do something when they are evaluated are handed to the
// literal as arguments, in the order the call evaluated them.
func ctxQueueRedirectLiteral(plan *ctxPlan, noter *ctxNoter, env *ctxEnv, call *ast.CallExpr, sel *ast.SelectorExpr, via string) {
	g := env.g
	name := sel.Sel.Name
	target := ctxRedirectTarget[name]
	statusAt := target.args

	plan.add(func(e *editSet) {
		before := call.Args[:min(len(call.Args), statusAt)]
		var statuses []ast.Expr
		if len(call.Args) > statusAt {
			statuses = call.Args[statusAt:]
		}

		lit := &ctxLiteral{namer: newCtxNamer(append([]ast.Expr{sel.X}, call.Args...), ctxFiberNames(env)...)}
		recvText := ctxRenderNode(e, g, sel.X)
		if via != "" {
			recvText += "." + via
		}
		recv := lit.bind(ctxOperand{expr: sel.X, text: recvText, typ: env.qual(ctxTypeCtx), name: "c"})

		var args []string
		for i, arg := range before {
			if name == ctxRedirectToRoute && i == 1 {
				if cfg := ctxRouteConfig(e, noter, env, arg); cfg != "" {
					args = append(args, lit.bind(ctxOperand{expr: arg, text: cfg, typ: env.qual("RedirectConfig"), name: "cfg"}))
				}
				continue
			}
			args = append(args, lit.bind(ctxOperand{expr: arg, text: ctxRenderNode(e, g, arg), typ: ctxLitString, name: target.param}))
		}

		// The first status is the one v2 uses; the other ones are not used, but
		// evaluating them may do something.
		first := ctxOperand{expr: statuses[0], text: ctxRenderNode(e, g, statuses[0]), typ: ctxLitInt, name: "status"}
		var body, status string
		if call.Ellipsis.IsValid() {
			first.typ, first.name, first.spread = ctxLitInts, "codes", true
			codes := lit.bind(first)
			status = lit.namer.fresh("status")
			body = fmt.Sprintf("%[1]s := %[2]s\nif len(%[3]s) > 0 {\n%[1]s = %[3]s[0]\n}\n", status, env.qual(ctxStatusFound), codes)
		} else {
			status = lit.bind(first)
			if extras := statuses[1:]; slices.ContainsFunc(extras, func(x ast.Expr) bool { return !ctxIsPure(x) }) {
				texts := make([]string, len(extras))
				for i, extra := range extras {
					texts[i] = ctxRenderNode(e, g, extra)
				}
				lit.bind(ctxOperand{text: strings.Join(texts, ", "), typ: ctxLitInts, unused: true})
			}
		}
		body += "return " + recv + ".Redirect().Status(" + status + ")." + target.method + "(" + strings.Join(args, ", ") + ")"

		text := lit.text("error", body)
		if comments := ctxStrayComments(g, call, append(slices.Clone(before), statuses[0])); len(comments) > 0 {
			text += " " + strings.Join(comments, " ")
		}
		e.replaceNode(g, call, text)
	})
}

// ctxFiberNames returns the names the file binds to the fiber package, which
// the body of a function literal refers to.
func ctxFiberNames(env *ctxEnv) []string {
	return slices.DeleteFunc(slices.Clone(env.names), func(name string) bool { return name == "." })
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
