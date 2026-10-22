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

// MigrateAddMethod wraps the method of router.Add in a slice, which is how v3
// registers a handler for several methods:
//
//	app.Add(fiber.MethodGet, "/foo", h) -> app.Add([]string{fiber.MethodGet}, "/foo", h)
//
// Only the Add of a fiber app, group or router is touched. It also points at the
// routes registered with a spread slice of handlers (app.Get(path, handlers...)),
// which v3 does not accept because its handlers are ...any.
func MigrateAddMethod(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateAddMethodContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Add method calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating Add method calls")
	return nil
}

// appSpreadMethods are the routing methods whose handlers are ...Handler in v2
// and ...any in v3.
var appSpreadMethods = []string{
	appMethodGet, "Head", "Post", "Put", "Delete", "Connect", "Options", "Trace", "Patch", "Query", "All", appMethodAdd, appMethodGroup,
}

func migrateAddMethodContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, ".Add(") && !strings.Contains(content, "...)") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || !slices.Contains(appSpreadMethods, sel.Sel.Name) || !f.kind(sel.X).isRouter() {
			return true
		}

		if sel.Sel.Name == appMethodAdd && len(call.Args) >= 2 && !f.isMethodList(call.Args[0]) {
			first := call.Args[0]
			edits.replace(f.g.offset(first.Pos()), f.g.offset(first.Pos()), "[]string{")
			edits.replace(f.g.offset(first.End()), f.g.offset(first.End()), "}")
		}
		if call.Ellipsis.IsValid() && f.spreadsHandlers(call) {
			internal.Notef(fi, f.g.line(call.Pos()),
				"%s takes its handlers as ...any in v3, so a []fiber.Handler cannot be spread into it: convert it to []any first and pass the first handler on its own",
				sel.Sel.Name)
		}
		return true
	})
	return edits.apply(content)
}

// isMethodList reports whether e is already a list of methods, as the first
// argument of Add is in v3.
func (f *appFile) isMethodList(e ast.Expr) bool {
	t, tf := f.valueType(e, 0)
	if t == nil {
		return false
	}
	_, isList := appStripType(t).(*ast.ArrayType)
	_ = tf
	return isList
}

// spreadsHandlers reports whether the slice spread into the call is a list of
// v2 handlers. A slice of any is what v3 takes, an unknown type is assumed to
// be handlers when the call has the shape of v2 (the handlers are all there is
// after the path).
func (f *appFile) spreadsHandlers(call *ast.CallExpr) bool {
	spread := call.Args[len(call.Args)-1]
	t, tf := f.valueType(spread, 0)
	if t != nil {
		elem := appSliceElem(t)
		switch {
		case elem == nil:
			return false
		case tf.fiberTypeName(elem) == "Handler":
			return true
		default:
			return false
		}
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case appMethodAdd:
		return len(call.Args) == 3
	case appMethodGroup:
		return false
	default:
		return len(call.Args) == 2
	}
}

// appSliceElem returns the element type of a slice type or of the type of a
// variadic parameter.
func appSliceElem(t ast.Expr) ast.Expr {
	switch x := appStripType(t).(type) {
	case *ast.ArrayType:
		return x.Elt
	case *ast.Ellipsis:
		return x.Elt
	}
	return nil
}
