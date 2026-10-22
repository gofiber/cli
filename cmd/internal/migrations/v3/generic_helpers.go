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

// ctxGenericHelpers maps the typed getters of the v2 Ctx to the generic
// helpers of v3: c.QueryInt(k, d) is fiber.Query[int](c, k, d).
var ctxGenericHelpers = map[string]ctxHelper{
	"ParamsInt":  {fn: ctxFnParams, typ: ctxGoInt},
	"QueryInt":   {fn: ctxFnQuery, typ: ctxGoInt},
	"QueryFloat": {fn: ctxFnQuery, typ: "float64"},
	"QueryBool":  {fn: ctxFnQuery, typ: "bool"},
}

// ctxHelper is the generic helper of v3 that stands for a typed getter: its name
// and the type it is instantiated with.
type ctxHelper struct{ fn, typ string }

// ctxFnQuery is the generic helper for the query string, ctxFnParams the one for
// the route parameters, ctxGoInt the type of the integer helpers.
const (
	ctxFnQuery  = "Query"
	ctxFnParams = "Params"
	ctxGoInt    = "int"
)

const ctxParamsInt = "ParamsInt"

// ctxParamsIntNote says that the error of ParamsInt lost the text v2 put in
// front of it: the default error handler sends the text of an error in the body.
const ctxParamsIntNote = "ParamsInt without a default value is strconv.Atoi(c.Params(key)) now: its error reads like " +
	"the one of strconv, where v2 put \"failed to convert: \" in front of it, and the default error handler sends " +
	"the text of an error in the body of the response; wrap it with fmt.Errorf(\"failed to convert: %w\", err) " +
	"if something depends on it"

// MigrateGenericHelpers turns the typed getters of the request context into the
// generic helpers of v3.
//
// QueryInt, QueryFloat and QueryBool become fiber.Query[T](c, key, default...).
// ParamsInt returned an error as well: without a default it becomes
// strconv.Atoi(c.Params(key)), which keeps the error, and with a default
// fiber.Params[int](c, key, default), error(nil), because v2 never returned an
// error then. With a spread of defaults it depends on how many there are when
// the call runs, so it becomes a function literal that is called at once and
// returns the first default, or the error when there is none. Only methods of a
// fiber context are rewritten.
func MigrateGenericHelpers(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxGenericHelpersContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate generic helpers: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating generic helpers")
	return nil
}

func ctxGenericHelpersContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !ctxContainsAny(content, ctxParamsInt, "QueryInt", "QueryFloat", "QueryBool") {
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
	strconvName := ctxImportName(content, "strconv", "strconv")
	if strconvName == "" {
		strconvName = "strconv"
	}
	fmtName := ctxImportName(content, "fmt", "fmt")
	if fmtName == "" {
		fmtName = "fmt"
	}
	needStrconv, needFmt := false, false
	var plan ctxPlan

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if sel, isSel := n.(*ast.SelectorExpr); isSel {
			if ctxQueueHelperValue(&plan, noter, env, sel, stack, strconvName, fmtName) {
				needStrconv = needStrconv || sel.Sel.Name == ctxParamsInt
				needFmt = needFmt || sel.Sel.Name == ctxParamsInt
			}
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
		helper, ok := ctxGenericHelpers[sel.Sel.Name]
		if !ok {
			return true
		}

		generic := env.qual(helper.fn) + "[" + helper.typ + "]"
		kind, via := env.receiverKindVia(sel, stack, ctxMembersParams...)
		switch kind {
		case ctxYes:
			// The helper and the literal that replace the call take a fiber.Ctx, which a
			// type that embeds the context may not be (a member of its own with the name
			// of a method of the context, another embedded type with the same method), and
			// the method of v2 used the members of the context and not those of the type:
			// the embedded context goes there. A call of Params on the receiver, in the
			// code that replaces ParamsInt, only needs it when the type declares Params.
			handed := env.embeddedCtxOf(sel.X, stack)

			// The getters that return one value take the same default values in v3, a
			// slice spread included. ParamsInt does not: whether it returns an error
			// depends on the number of defaults, which is only known when it runs.
			if call.Ellipsis.IsValid() && sel.Sel.Name == ctxParamsInt && len(call.Args) == 2 &&
				!importsShadowed(strconvName, call.Pos(), stack) {
				needStrconv = true
				noter.oncef("paramsint-error", sel.Pos(), "%s", ctxParamsIntNote)
				ctxQueueParamsIntLiteral(&plan, env, call, sel, strconvName, handed)
				return true
			}
			if len(call.Args) == 0 || (call.Ellipsis.IsValid() && sel.Sel.Name == ctxParamsInt) {
				noter.notef(sel.Pos(),
					"%s was removed in Fiber v3 and takes its default values from a slice here: use %s(c, key, ...) by hand",
					g.text(sel), generic)
				return true
			}
			twoValued := sel.Sel.Name == ctxParamsInt && ctxTwoValued(call, stack)
			// Without a default the error of ParamsInt is real: keep it.
			useAtoi := twoValued && len(call.Args) == 1
			needStrconv = needStrconv || useAtoi
			if useAtoi {
				noter.oncef("paramsint-error", sel.Pos(), "%s", ctxParamsIntNote)
			}
			if !useAtoi {
				via = handed
			}
			ctxQueueHelper(&plan, g, call, sel, generic, strconvName, via, useAtoi, twoValued && !useAtoi)
		case ctxUnknown:
			noter.notef(sel.Pos(),
				"%s was removed in Fiber v3, but %s is not known to be a fiber context here, so it was left as is: use %s(ctx, key) instead",
				g.text(sel), g.text(sel.X), generic)
		case ctxNo:
		}
		return true
	})

	out := plan.apply(content)
	if needStrconv {
		out = ctxAddImport(out, "strconv", "strconv")
	}
	if needFmt {
		out = ctxAddImport(out, "fmt", "fmt")
	}
	return out
}

// ctxQueueHelperValue queues the replacement of a typed getter that is used as a
// value, without being called, with a function literal that has the signature of
// the v2 method (see ctxValueBuilder). It reports whether it queued one; the
// literal of ParamsInt needs strconv and fmt, which the caller imports.
func ctxQueueHelperValue(plan *ctxPlan, noter *ctxNoter, env *ctxEnv, sel *ast.SelectorExpr, stack []ast.Node, strconvName, fmtName string) bool {
	helper, ok := ctxGenericHelpers[sel.Sel.Name]
	if !ok {
		return false
	}

	// A method expression, (*fiber.Ctx).QueryInt, is a value even when it is
	// called: the context is its first argument.
	kind := ctxYes
	if !env.isCtxType(sel.X) {
		if _, isCall := ctxParentCall(sel, stack); isCall {
			return false
		}
		kind = env.receiverKind(sel, stack)
	}
	switch kind {
	case ctxYes:
	case ctxUnknown:
		noter.notef(sel.Pos(),
			"%s was removed in Fiber v3 and is used as a value, but %s is not known to be a fiber context here, so it was left as is: call %s[%s](ctx, key, ...) from a function literal",
			env.g.text(sel), env.g.text(sel.X), env.qual(helper.fn), helper.typ)
		return false
	case ctxNo:
		return false
	}

	if sel.Sel.Name != ctxParamsInt {
		b := env.newValueBuilder(sel, stack, ctxMembersAll)
		plan.add(func(e *editSet) { e.replaceNode(env.g, sel, b.text(e, b.query(helper))) })
		return false
	}

	if importsShadowed(strconvName, sel.Pos(), stack) || importsShadowed(fmtName, sel.Pos(), stack) {
		noter.notef(sel.Pos(),
			"%s was removed in Fiber v3 and is used as a value, but %s or %s is declared here, so the function that replaces it can not be written: use strconv.Atoi(c.Params(key)) from a function literal",
			env.g.text(sel), strconvName, fmtName)
		return false
	}
	b := env.newValueBuilder(sel, stack, ctxMembersParams, strconvName, fmtName)
	plan.add(func(e *editSet) { e.replaceNode(env.g, sel, b.text(e, b.paramsInt(strconvName, fmtName))) })
	return true
}

// ctxQueueHelper queues the rewrite of one call of a typed getter. The
// arguments keep their text, only what comes before and after them changes:
//
//	c.QueryInt(k, d)         generic(c, k, d)
//	c.ParamsInt(k)           strconv.Atoi(c.Params(k))             (atoi)
//	c.ParamsInt(k, d)        generic(c, k, d), error(nil)          (nilErr)
func ctxQueueHelper(plan *ctxPlan, g *goSource, call *ast.CallExpr, sel *ast.SelectorExpr, generic, strconvName, via string, atoi, nilErr bool) {
	plan.add(func(e *editSet) {
		recv := ctxRenderNode(e, g, sel.X)
		if via != "" {
			recv += "." + via
		}

		head, tail := generic+"("+recv+", ", ""
		switch {
		case atoi:
			// The closing parenthesis of the call becomes the one of Params.
			head, tail = strconvName+".Atoi("+recv+".Params(", ")"
		case nilErr:
			tail = ", error(nil)"
		}

		e.replace(g.offset(call.Pos()), g.offset(call.Lparen)+1, head)
		if tail != "" {
			end := g.offset(call.End())
			e.replace(end, end, tail)
		}
	})
}

// ctxQueueParamsIntLiteral queues the rewrite of c.ParamsInt(key, defaults...),
// whose defaults are a slice: v2 returned the first default, and no error, when
// the parameter is no integer and there is a default, and the error when there is
// none. A function literal that is called at once has the same two results
// wherever the call was used:
//
//	func() (int, error) {
//	    value, err := strconv.Atoi(c.Params(key))
//	    if err != nil {
//	        if len(defaults) > 0 {
//	            return defaults[0], nil
//	        }
//	        return 0, err
//	    }
//	    return value, nil
//	}()
func ctxQueueParamsIntLiteral(plan *ctxPlan, env *ctxEnv, call *ast.CallExpr, sel *ast.SelectorExpr, strconvName, via string) {
	g := env.g
	plan.add(func(e *editSet) {
		key, defaults := call.Args[0], call.Args[1]
		lit := &ctxLiteral{namer: newCtxNamer([]ast.Expr{sel.X, key, defaults}, append(ctxFiberNames(env), strconvName)...)}
		recvText := ctxRenderNode(e, g, sel.X)
		if via != "" {
			recvText += "." + via
		}
		recv := lit.bind(ctxOperand{expr: sel.X, text: recvText, typ: env.qual(ctxTypeCtx), name: "c"})
		keyRef := lit.bind(ctxOperand{expr: key, text: ctxRenderNode(e, g, key), typ: ctxLitString, name: ctxLitKey})
		defaultsRef := lit.bind(ctxOperand{expr: defaults, text: ctxRenderNode(e, g, defaults), typ: ctxLitInts, name: "defaults", spread: true})

		value, errName := lit.namer.fresh("value"), lit.namer.fresh("err")
		body := fmt.Sprintf(`%[1]s, %[2]s := %[3]s.Atoi(%[4]s.Params(%[5]s))
if %[2]s != nil {
if len(%[6]s) > 0 {
return %[6]s[0], nil
}
return 0, %[2]s
}
return %[1]s, nil`, value, errName, strconvName, recv, keyRef, defaultsRef)

		text := lit.text("(int, error)", body)
		if comments := ctxStrayComments(g, call, call.Args); len(comments) > 0 {
			text += " " + strings.Join(comments, " ")
		}
		e.replaceNode(g, call, text)
	})
}

// ctxTwoValued reports whether the call is used where both of its results are:
// as the whole right side of a two-variable assignment or declaration, as the
// only operand of a return in a function with two results, or as the only
// argument of another call.
func ctxTwoValued(call *ast.CallExpr, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}

	switch parent := stack[len(stack)-1].(type) {
	case *ast.AssignStmt:
		return len(parent.Lhs) == 2 && len(parent.Rhs) == 1
	case *ast.ValueSpec:
		return len(parent.Names) == 2 && len(parent.Values) == 1
	case *ast.ReturnStmt:
		return len(parent.Results) == 1 && ctxResultCount(stack) == 2
	case *ast.CallExpr:
		return len(parent.Args) == 1 && parent.Args[0] == call
	}
	return false
}

// ctxResultCount returns the number of results of the function that encloses
// the innermost node of stack, -1 when there is none.
func ctxResultCount(stack []ast.Node) int {
	for _, node := range slices.Backward(stack) {
		var results *ast.FieldList
		switch fn := node.(type) {
		case *ast.FuncLit:
			results = fn.Type.Results
		case *ast.FuncDecl:
			results = fn.Type.Results
		default:
			continue
		}

		count := 0
		if results != nil {
			for _, f := range results.List {
				count += max(len(f.Names), 1)
			}
		}
		return count
	}
	return -1
}
