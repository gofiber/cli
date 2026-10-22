package v3

import (
	"go/ast"
	"slices"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// reviewModuleCalls are the methods of the context that the review looks for in a
// file that imports no Fiber package: the ones v3 removed, and the ones that
// changed what they do.
var reviewModuleCalls = append(slices.Clone(reviewRemovedMethods[reviewCorePath+".Ctx"]), mw3SendFile, "Download", "Redirect")

// scanModuleContexts looks at a file that imports no Fiber package for what v3
// changed in the methods of the context: it can hold a value of a type another
// package of the module declares, a wrapper that embeds the context or an alias
// of it, and call them on it. Nothing else of the file concerns Fiber.
func (r *reviewer) scanModuleContexts(content string, fi internal.FileInfo) {
	if !slices.ContainsFunc(reviewModuleCalls, func(name string) bool { return strings.Contains(content, "."+name+"(") }) {
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
		if call, ok := n.(*ast.CallExpr); ok {
			rf.checkCall(call, stack)
		}
		return true
	})
}
