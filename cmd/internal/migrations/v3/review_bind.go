package v3

import (
	"go/ast"
	"slices"
)

// reviewBinders are the methods of c.Bind() that replace the parsers of the v2
// context and whose errors read differently.
var reviewBinders = []string{"Body", "Query", "Header", "Cookie", "URI"}

// reviewBindNote tells how the text of the errors of the binders changed. v2's
// QueryParser, CookieParser, ParamsParser, ReqHeaderParser and the form side of
// BodyParser returned "failed to decode: " and the error of the decoder, the JSON
// and XML side of BodyParser the bare error; the binders of v3 return a
// *fiber.BindError.
const reviewBindNote = `Bind().Body, Query, Header, Cookie and URI return a *fiber.BindError: its text reads 'bind "page" from query: schema: error converting value for "page"' ` +
	`('bind from body: ...' when no field is known), where v2 returned 'failed to decode: schema: ...' (QueryParser, CookieParser, ParamsParser, ReqHeaderParser and form bodies) or the bare error of the decoder (JSON and XML bodies). ` +
	`An API that puts err.Error() into a response, or a test that compares it, shows the new text; errors.As(err, &bindErr) with a *fiber.BindError gives the Source and the Field.`

// checkBindErrors reports, once per project, the first use of a binder.
func (rf *reviewFile) checkBindErrors(call *ast.CallExpr, stack []ast.Node) {
	if rf.r.bindSeen {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !slices.Contains(reviewBinders, sel.Sel.Name) {
		return
	}
	bind, ok := sel.X.(*ast.CallExpr)
	if !ok || len(bind.Args) != 0 {
		return
	}
	inner, ok := bind.Fun.(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "Bind" || !rf.isCtxMethod(inner, stack) {
		return
	}

	rf.r.bindSeen = true
	rf.notef(sel.Sel.Pos(), "%s", reviewBindNote)
}
