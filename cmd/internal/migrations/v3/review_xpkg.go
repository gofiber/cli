package v3

import (
	"go/ast"
	"slices"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// scanModuleContexts looks at a file that imports no Fiber package for the removed
// methods of the context: it can hold a value of a type another package of the
// module declares, a wrapper that embeds the context or an alias of it, and call
// what v3 removed on it. Nothing else of the file concerns Fiber.
func (r *reviewer) scanModuleContexts(content string, fi internal.FileInfo) {
	removed := reviewRemovedMethods[reviewCorePath+".Ctx"]
	if !slices.ContainsFunc(removed, func(name string) bool { return strings.Contains(content, "."+name+"(") }) {
		return
	}
	if !r.run.reachesContexts(fi, ctxImportsOf(content)) {
		return
	}
	// The migrations said when the file does not parse.
	g, ok := parseSource(content)
	if !ok {
		return
	}

	rf := &reviewFile{r: r, g: g, fi: fi, packages: map[string]string{}}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && slices.Contains(removed, sel.Sel.Name) && rf.isCtxMethod(sel, stack) {
			rf.notef(sel.Sel.Pos(), "%s was removed in Fiber v3%s%s", sel.Sel.Name, reviewHint("Ctx."+sel.Sel.Name), rf.wrapperHint(sel, stack))
		}
		return true
	})
}
