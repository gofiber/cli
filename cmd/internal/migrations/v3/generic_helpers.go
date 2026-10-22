package v3

import (
	"fmt"
	"go/ast"
	"slices"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxGenericHelpers maps the typed getters of the v2 Ctx to the generic
// helpers of v3: c.QueryInt(k, d) is fiber.Query[int](c, k, d).
var ctxGenericHelpers = map[string]struct{ fn, typ string }{
	"ParamsInt":  {fn: "Params", typ: ctxGoInt},
	"QueryInt":   {fn: ctxFnQuery, typ: ctxGoInt},
	"QueryFloat": {fn: ctxFnQuery, typ: "float64"},
	"QueryBool":  {fn: ctxFnQuery, typ: "bool"},
}

// ctxFnQuery is the generic helper for the query string, ctxGoInt the type of
// the integer helpers.
const (
	ctxFnQuery = "Query"
	ctxGoInt   = "int"
)

const ctxParamsInt = "ParamsInt"

// MigrateGenericHelpers turns the typed getters of the request context into the
// generic helpers of v3.
//
// QueryInt, QueryFloat and QueryBool become fiber.Query[T](c, key, default...).
// ParamsInt returned an error as well: without a default it becomes
// strconv.Atoi(c.Params(key)), which keeps the error, and with a default
// fiber.Params[int](c, key, default), error(nil), because v2 never returned an
// error then. Only methods of a fiber context are rewritten.
func MigrateGenericHelpers(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRunNotes()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateGenericHelpersContent(content, fi, run)
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

func migrateGenericHelpersContent(content string, fi internal.FileInfo, run *ctxRunNotes) string {
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

	env := newCtxEnv(g, names)
	noter := newCtxNoter(fi, g, run)
	strconvName := ctxImportName(content, "strconv", "strconv")
	if strconvName == "" {
		strconvName = "strconv"
	}
	needStrconv := false
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
		helper, ok := ctxGenericHelpers[sel.Sel.Name]
		if !ok {
			return true
		}

		generic := env.qual(helper.fn) + "[" + helper.typ + "]"
		switch env.ctxKindOf(sel.X, stack) {
		case ctxYes:
			if call.Ellipsis.IsValid() || len(call.Args) == 0 {
				noter.notef(sel.Pos(),
					"%s was removed in Fiber v3 and takes its default values from a slice here: use %s(c, key, ...) by hand",
					g.text(sel), generic)
				return true
			}
			twoValued := sel.Sel.Name == ctxParamsInt && ctxTwoValued(call, stack)
			// Without a default the error of ParamsInt is real: keep it.
			useAtoi := twoValued && len(call.Args) == 1
			needStrconv = needStrconv || useAtoi
			ctxQueueHelper(&plan, g, call, sel, generic, strconvName, useAtoi, twoValued && !useAtoi)
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
	return out
}

// ctxQueueHelper queues the rewrite of one call of a typed getter. The
// arguments keep their text, only what comes before and after them changes:
//
//	c.QueryInt(k, d)         generic(c, k, d)
//	c.ParamsInt(k)           strconv.Atoi(c.Params(k))             (atoi)
//	c.ParamsInt(k, d)        generic(c, k, d), error(nil)          (nilErr)
func ctxQueueHelper(plan *ctxPlan, g *goSource, call *ast.CallExpr, sel *ast.SelectorExpr, generic, strconvName string, atoi, nilErr bool) {
	plan.add(func(e *editSet) {
		recv := ctxRenderNode(e, g, sel.X)

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
