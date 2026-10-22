package v3

import (
	"fmt"
	"go/ast"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateShutdownHook turns the OnShutdown hook, which v2 ran after the server
// was shut down, into OnPostShutdown, whose handler receives the shutdown
// error:
//
//	app.Hooks().OnShutdown(func() error { ... }) -> app.Hooks().OnPostShutdown(func(error) error { ... })
//	app.Hooks().OnShutdown(cleanup)              -> app.Hooks().OnPostShutdown(func(error) error { return cleanup() })
//
// The hooks may be held in a variable (hooks := app.Hooks()).
func MigrateShutdownHook(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return appMigrateShutdownHookContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate shutdown hooks: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating shutdown hooks")
	return nil
}

func appMigrateShutdownHookContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "OnShutdown") {
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
		if !isSel || sel.Sel.Name != "OnShutdown" || f.receiverKind(call, sel) != appKindHooks {
			return true
		}

		edits.replaceNode(f.g, sel.Sel, "OnPostShutdown")
		if call.Ellipsis.IsValid() {
			internal.Notef(fi, f.g.line(call.Pos()),
				"OnPostShutdown handlers are func(error) error in v3: convert the handlers you spread into it by hand")
			return true
		}
		for _, arg := range call.Args {
			appMigrateShutdownHandler(f, &edits, arg)
		}
		return true
	})
	return edits.apply(content)
}

// appMigrateShutdownHandler adapts one OnShutdown handler, a func() error, to the
// func(error) error that OnPostShutdown takes.
func appMigrateShutdownHandler(f *appFile, edits *editSet, arg ast.Expr) {
	switch h := arg.(type) {
	case *ast.FuncLit:
		if h.Type.Params == nil || len(h.Type.Params.List) > 0 {
			return // already takes the shutdown error
		}
		at := f.g.offset(h.Type.Params.Opening) + 1
		edits.replace(at, at, "error")
	default:
		if !appIsPureReference(arg) {
			internal.Notef(f.fi, f.g.line(arg.Pos()),
				"OnPostShutdown handlers are func(error) error in v3: wrap %s in func(error) error { return ...() }", f.g.text(arg))
			return
		}
		edits.replaceNode(f.g, arg, "func(error) error { return "+f.g.text(arg)+"() }")
	}
}

// appIsPureReference reports whether e names a function or method value
// without calling anything: cleanup, res.Close, pkg.Cleanup.
func appIsPureReference(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return appIsPureReference(x.X)
	case *ast.ParenExpr:
		return appIsPureReference(x.X)
	}
	return false
}
