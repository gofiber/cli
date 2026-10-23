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

// ctxQueryFloatNote says which numbers QueryFloat stopped reading: the generic helper
// parses decimal numbers only.
const ctxQueryFloatNote = "QueryFloat is fiber.Query[float64] now, which reads decimal numbers only: the other forms " +
	"that strconv.ParseFloat accepted (inf, infinity and nan in any case, hexadecimal numbers such as 0x1p-2, " +
	"and underscores between the digits as in 1_000.5) give the default value, or 0 without one; call " +
	"strconv.ParseFloat(c.Query(key), 64) where a client may send them"

// ctxQueryFloat is the typed getter whose numbers the generic helper reads differently.
const ctxQueryFloat = "QueryFloat"

// MigrateGenericHelpers turns the typed getters of the request context into the
// generic helpers of v3.
//
// QueryInt, QueryFloat and QueryBool become fiber.Query[T](c, key, default...).
// ParamsInt returned an error as well: with a default it becomes
// fiber.Params[int](c, key, default), error(nil), because v2 never returned an
// error then. Without one the error is real, and v2 returned 0 with it: the call
// becomes fiber.Params[int](c, key) when the code drops the error, which reads 0 for
// what it cannot parse, strconv.Atoi(c.Params(key)) when the statement is followed by
// a check of the error that leaves (strconv.Atoi returns the largest int for a number
// that does not fit, but nothing reads the value), and else a function literal that
// returns 0 with the error. With a spread of defaults it depends on how many there
// are when the call runs, so it becomes a function literal that is called at once and
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
	g, env, ok := run.open(fi, content)
	if !ok {
		return content
	}
	noter := newCtxNoter(fi, g, run)
	var plan ctxPlan

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if sel, isSel := n.(*ast.SelectorExpr); isSel {
			ctxQueueHelperValue(&plan, noter, env, sel, stack)
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || env.isCtxType(sel.X, stack) {
			return true
		}
		helper, ok := ctxGenericHelpers[sel.Sel.Name]
		if !ok {
			return true
		}

		// the notes name the helper the way the file does, the code that replaces the
		// call the way it can be written where the call is
		generic, named := env.qual(call.Pos(), helper.fn)+"["+helper.typ+"]", env.spell(helper.fn)+"["+helper.typ+"]"
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
			if sel.Sel.Name == ctxQueryFloat {
				noter.oncef("queryfloat", sel.Pos(), "%s", ctxQueryFloatNote)
			}

			// The getters that return one value take the same default values in v3, a
			// slice spread included. ParamsInt does not: whether it returns an error
			// depends on the number of defaults, which is only known when it runs.
			if call.Ellipsis.IsValid() && sel.Sel.Name == ctxParamsInt && len(call.Args) == 2 {
				noter.oncef("paramsint-error", sel.Pos(), "%s", ctxParamsIntNote)
				ctxQueueParamsIntLiteral(&plan, env, call, sel, env.pkgAt(call.Pos(), "strconv"), handed)
				return true
			}
			if len(call.Args) == 0 || (call.Ellipsis.IsValid() && sel.Sel.Name == ctxParamsInt) {
				noter.notef(sel.Pos(),
					"%s was removed in Fiber v3 and takes its default values from a slice here: use %s(c, key, ...) by hand",
					g.text(sel), named)
				return true
			}
			twoValued := sel.Sel.Name == ctxParamsInt && ctxTwoValued(call, stack)
			// Without a default the error of ParamsInt is real, and the value that goes with
			// it is 0: where the code drops the error the generic helper has that value
			// (it reads 0 for what it cannot parse), where it leaves on an error without
			// looking at the value Atoi is as good, and for any other use the call becomes a
			// literal that returns 0 with the error.
			useAtoi, after := false, ""
			if twoValued && len(call.Args) == 1 {
				switch errUse := ctxParamsIntErr(call, stack); errUse.use {
				case ctxErrIgnored:
					plan.add(func(e *editSet) { e.replace(g.offset(errUse.from), g.offset(errUse.to), "") })
					twoValued = false
				case ctxErrGuarded:
					useAtoi = true
					noter.oncef("paramsint-error", sel.Pos(), "%s", ctxParamsIntNote)
				case ctxErrReset:
					useAtoi = true
					after = "\nif " + errUse.errName + " != nil {\n" + g.text(errUse.value) + " = 0\n}"
					noter.oncef("paramsint-error", sel.Pos(), "%s", ctxParamsIntNote)
				case ctxErrReturned:
					noter.oncef("paramsint-error", sel.Pos(), "%s", ctxParamsIntNote)
					ctxQueueParamsIntReturn(&plan, env, errUse.ret, call, sel, env.pkgAt(call.Pos(), "strconv"), via, stack)
					return true
				case ctxErrOther:
					noter.oncef("paramsint-error", sel.Pos(), "%s", ctxParamsIntNote)
					ctxQueueParamsIntLiteral(&plan, env, call, sel, env.pkgAt(call.Pos(), "strconv"), handed)
					return true
				}
			}
			if !useAtoi {
				via = handed
			}
			ctxQueueHelper(&plan, g, call, sel, generic, env.pkgAt(call.Pos(), "strconv"), via, after, useAtoi, twoValued && !useAtoi)
		case ctxUnknown:
			noter.notef(sel.Pos(),
				"%s was removed in Fiber v3, but %s is not known to be a fiber context here, so it was left as is: use %s(ctx, key) instead",
				g.text(sel), g.text(sel.X), named)
		case ctxNo:
		}
		return true
	})

	return env.finish(content, plan.apply(content))
}

// ctxQueueHelperValue queues the replacement of a typed getter that is used as a
// value, without being called, with a function literal that has the signature of
// the v2 method (see ctxValueBuilder). The literal of ParamsInt needs strconv and
// fmt, which finish imports.
func ctxQueueHelperValue(plan *ctxPlan, noter *ctxNoter, env *ctxEnv, sel *ast.SelectorExpr, stack []ast.Node) {
	helper, ok := ctxGenericHelpers[sel.Sel.Name]
	if !ok {
		return
	}

	// A method expression, (*fiber.Ctx).QueryInt, is a value even when it is
	// called: the context is its first argument.
	kind := ctxYes
	if !env.isCtxType(sel.X, stack) {
		if _, isCall := ctxParentCall(sel, stack); isCall {
			return
		}
		kind = env.receiverKind(sel, stack)
	}
	switch kind {
	case ctxYes:
	case ctxUnknown:
		noter.notef(sel.Pos(),
			"%s was removed in Fiber v3 and is used as a value, but %s is not known to be a fiber context here, so it was left as is: call %s[%s](ctx, key, ...) from a function literal",
			env.g.text(sel), env.g.text(sel.X), env.spell(helper.fn), helper.typ)
		return
	case ctxNo:
		return
	}

	if sel.Sel.Name == ctxQueryFloat {
		noter.oncef("queryfloat", sel.Pos(), "%s", ctxQueryFloatNote)
	}
	if sel.Sel.Name != ctxParamsInt {
		b := env.newValueBuilder(sel, stack, ctxMembersAll)
		plan.add(func(e *editSet) { e.replaceNode(env.g, sel, b.text(e, b.query(helper))) })
		return
	}

	strconvName, fmtName := env.pkgAt(sel.Pos(), "strconv"), env.pkgAt(sel.Pos(), "fmt")
	b := env.newValueBuilder(sel, stack, ctxMembersParams, strconvName, fmtName)
	plan.add(func(e *editSet) { e.replaceNode(env.g, sel, b.text(e, b.paramsInt(strconvName, fmtName))) })
}

// ctxQueueHelper queues the rewrite of one call of a typed getter. The
// arguments keep their text, only what comes before and after them changes:
//
//	c.QueryInt(k, d)         generic(c, k, d)
//	c.ParamsInt(k)           strconv.Atoi(c.Params(k))             (atoi)
//	c.ParamsInt(k, d)        generic(c, k, d), error(nil)          (nilErr)
//
// after is code that goes behind the statement the call ends.
func ctxQueueHelper(plan *ctxPlan, g *goSource, call *ast.CallExpr, sel *ast.SelectorExpr, generic, strconvName, via, after string, atoi, nilErr bool) {
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
		if after != "" {
			end := g.offset(call.End())
			e.replace(end, end, after)
		}
	})
}

// ctxQueueParamsIntReturn queues the rewrite of return c.ParamsInt(key): the statement
// that returns the two results becomes the ones of strconv.Atoi, with the 0 that v2
// returned with an error.
//
//	value, err := strconv.Atoi(c.Params(key))
//	if err != nil {
//	    return 0, err
//	}
//	return value, nil
func ctxQueueParamsIntReturn(plan *ctxPlan, env *ctxEnv, ret *ast.ReturnStmt, call *ast.CallExpr, sel *ast.SelectorExpr, strconvName, via string, stack []ast.Node) {
	g := env.g
	// the names stay clear of every name of the function: a result called value or err would
	// be declared already (the stack is only good while the walk is at the call)
	namer := newCtxNamer([]ast.Expr{sel.X, call.Args[0]}, append(ctxFiberNames(env), strconvName)...)
	namer.collectFunc(stack)
	value, errName := namer.fresh("value"), namer.fresh("err")
	plan.add(func(e *editSet) {
		recv := ctxRenderNode(e, g, sel.X)
		if via != "" {
			recv += "." + via
		}
		text := fmt.Sprintf("%[1]s, %[2]s := %[3]s.Atoi(%[4]s.Params(%[5]s))\nif %[2]s != nil {\nreturn 0, %[2]s\n}\nreturn %[1]s, nil",
			value, errName, strconvName, recv, ctxRenderNode(e, g, call.Args[0]))
		e.replaceNode(g, ret, text)
	})
}

// ctxQueueParamsIntLiteral queues the rewrite of c.ParamsInt(key) and of
// c.ParamsInt(key, defaults...), whose defaults are a slice: v2 returned the first
// default, and no error, when the parameter is no integer and there is a default,
// and 0 with the error when there is none (strconv.Atoi returns the largest int for a
// number that does not fit). A function literal that is called at once has the same
// two results wherever the call was used:
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
		key := call.Args[0]
		operands := []ast.Expr{sel.X, key}
		if len(call.Args) > 1 {
			operands = append(operands, call.Args[1])
		}
		lit := &ctxLiteral{namer: newCtxNamer(operands, append(ctxFiberNames(env), strconvName)...)}
		recvText := ctxRenderNode(e, g, sel.X)
		if via != "" {
			recvText += "." + via
		}
		recv := lit.bind(ctxOperand{expr: sel.X, text: recvText, typ: env.qual(call.Pos(), ctxTypeCtx), name: "c"})
		keyRef := lit.bind(ctxOperand{expr: key, text: ctxRenderNode(e, g, key), typ: ctxLitString, name: ctxLitKey})

		value, errName := lit.namer.fresh("value"), lit.namer.fresh("err")
		failure := "return 0, " + errName
		if len(call.Args) > 1 {
			defaults := call.Args[1]
			defaultsRef := lit.bind(ctxOperand{expr: defaults, text: ctxRenderNode(e, g, defaults), typ: ctxLitInts, name: "defaults", spread: true})
			failure = fmt.Sprintf("if len(%[1]s) > 0 {\nreturn %[1]s[0], nil\n}\n%[2]s", defaultsRef, failure)
		}
		body := fmt.Sprintf(`%[1]s, %[2]s := %[3]s.Atoi(%[4]s.Params(%[5]s))
if %[2]s != nil {
%[6]s
}
return %[1]s, nil`, value, errName, strconvName, recv, keyRef, failure)

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
