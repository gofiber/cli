package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxHeaderMapsSince is the release of Fiber v2 whose GetReqHeaders and
// GetRespHeaders return a map[string][]string, as v3 does. The releases before
// returned a map[string]string, with the last value of a header that is repeated.
var ctxHeaderMapsSince = semver.MustParse("2.50.0")

// The methods of the request context whose result changed inside v2.
const (
	ctxGetReqHeaders  = "GetReqHeaders"
	ctxGetRespHeaders = "GetRespHeaders"
	ctxGetRespHeader  = "GetRespHeader"
)

// ctxHeaderMapsAdapter turns the map of v3 into the map of the older releases of
// v2: one value for a header, the last one of those the request or the response
// carries, as the old methods stored them.
const ctxHeaderMapsAdapter = `func(h map[string][]string) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		m[k] = v[len(v)-1]
	}
	return m
}(`

// MigrateHeaderMaps follows GetReqHeaders and GetRespHeaders of the request
// context over the release of Fiber v2 that changed them, for a project that is
// on an older one. The release before v2.50 returned map[string]string, and v3
// returns map[string][]string, so
//
//	c.GetReqHeaders()["X-Real-IP"]   ->  c.Get("X-Real-IP")
//	c.GetRespHeaders()["ETag"]       ->  c.GetRespHeader("ETag")
//
// where a header is looked up by name, and any other use keeps the map it had: the
// call is wrapped in a function that makes the map of strings from the map of v3,
// with the last value of a header that is repeated. A project on v2.50 or later is
// not touched: its methods are the ones of v3.
func MigrateHeaderMaps(cmd *cobra.Command, cwd string, curr, _ *semver.Version) error {
	if curr == nil || !curr.LessThan(ctxHeaderMapsSince) {
		return nil
	}

	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxHeaderMapsContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate the header maps of the request context: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating the header maps of the request context")
	return nil
}

func ctxHeaderMapsContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !ctxContainsAny(content, ctxGetReqHeaders, ctxGetRespHeaders) {
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
		if !ok || (sel.Sel.Name != ctxGetReqHeaders && sel.Sel.Name != ctxGetRespHeaders) {
			return true
		}

		lookup := appMethodGet
		if sel.Sel.Name == ctxGetRespHeaders {
			lookup = ctxGetRespHeader
		}
		call, isCall := ctxParentCall(sel, stack)
		var idx *ast.IndexExpr
		var introduced []string
		if isCall && len(call.Args) == 0 {
			if idx = ctxHeaderIndex(call, stack); idx != nil {
				introduced = []string{lookup}
			}
		}

		kind, via := env.receiverKindVia(sel, stack, introduced...)
		switch kind {
		case ctxNo:
			return true
		case ctxUnknown:
			noter.oncef("header-maps-unknown", sel.Pos(),
				"%s returns a map[string][]string in v3, and the release of Fiber v2 this project is on returned a map[string]string; "+
					"if the receiver is a Fiber context, use c.Get(name) or c.GetRespHeader(name) for one header", sel.Sel.Name)
			return true
		case ctxYes:
		}

		if !isCall || len(call.Args) != 0 {
			noter.oncef("header-maps-value", sel.Pos(),
				"%s is used as a value here: it returns a map[string][]string in v3, where the release of Fiber v2 this project is on "+
					"returned a map[string]string; wrap it as the migration does for its calls", sel.Sel.Name)
			return true
		}

		if idx == nil && ctxHeadersAdapted(call, stack) {
			return true
		}
		plan.qualify(g, sel, via)
		if idx != nil {
			noter.oncef("header-maps-first", sel.Pos(),
				"c.Get(name) gives the first value of a header the client sent more than once, where the map of the release of Fiber v2 this "+
					"project is on held the last one")
			plan.add(func(e *editSet) {
				e.replaceNode(g, sel.Sel, lookup)
				e.replace(g.offset(call.Lparen), g.offset(idx.Lbrack)+1, "(")
				e.replace(g.offset(idx.Rbrack), g.offset(idx.Rbrack)+1, ")")
			})
			return true
		}

		plan.add(func(e *editSet) {
			e.replace(g.offset(call.Pos()), g.offset(call.Pos()), ctxHeaderMapsAdapter)
			e.replace(g.offset(call.End()), g.offset(call.End()), ")")
		})
		return true
	})

	return env.finish(content, plan.apply(content))
}

// ctxHeadersAdapted reports whether the call is the argument of the function that
// this migration wrote around it: a function literal of one parameter, a map of
// string slices.
func ctxHeadersAdapted(call *ast.CallExpr, stack []ast.Node) bool {
	if len(stack) < 2 {
		return false
	}
	outer, ok := stack[len(stack)-2].(*ast.CallExpr)
	if !ok || len(outer.Args) != 1 || outer.Args[0] != ast.Expr(call) {
		return false
	}
	lit, ok := outer.Fun.(*ast.FuncLit)
	if !ok || len(lit.Type.Params.List) != 1 {
		return false
	}
	m, ok := lit.Type.Params.List[0].Type.(*ast.MapType)
	if !ok {
		return false
	}
	arr, ok := m.Value.(*ast.ArrayType)
	return ok && arr.Len == nil && isIdentNamed(arr.Elt, "string")
}

// ctxHeaderIndex returns the index expression that reads one header from the map
// the call returns, when that is all the program does with it: not a store into
// the map and not the second result of a lookup, whose presence the single value of
// the method cannot tell.
func ctxHeaderIndex(call *ast.CallExpr, stack []ast.Node) *ast.IndexExpr {
	if len(stack) < 2 {
		return nil
	}
	idx, ok := stack[len(stack)-2].(*ast.IndexExpr)
	if !ok || idx.X != ast.Expr(call) {
		return nil
	}

	if len(stack) < 3 {
		return idx
	}
	switch parent := stack[len(stack)-3].(type) {
	case *ast.AssignStmt:
		// c.GetReqHeaders()[k] = v, and v, ok := c.GetReqHeaders()[k]
		if len(parent.Lhs) != len(parent.Rhs) || slices.Contains(parent.Lhs, ast.Expr(idx)) {
			return nil
		}
	case *ast.ValueSpec:
		if len(parent.Names) != len(parent.Values) {
			return nil
		}
	case *ast.IncDecStmt, *ast.RangeStmt:
		return nil
	case *ast.UnaryExpr:
		if parent.Op == token.AND {
			return nil
		}
	}
	return idx
}
