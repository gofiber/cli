package v3

import (
	"fmt"
	"go/ast"
	"regexp"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// The releases of Fiber v2 that changed the signature of a callback.
var (
	// v2OnListenSince is the release whose OnListen handlers receive a ListenData.
	// The releases before took none: func() error.
	v2OnListenSince = semver.MustParse("2.48.0")
	// v2StackTraceSince is the release whose StackTraceHandler of the recover
	// middleware receives the request context. The releases before took the
	// recovered value only: func(e any).
	v2StackTraceSince = semver.MustParse("2.23.0")
)

const (
	v2OnListen        = "OnListen"
	v2StackTrace      = "StackTraceHandler"
	v2ListenData      = "ListenData"
	v2StackTraceValue = "e"
)

// v2RecoverPathRe finds the import path of the recover middleware.
var v2RecoverPathRe = regexp.MustCompile(`^github\.com/gofiber/fiber/v[23]/middleware/recover$`)

// MigrateOlderV2Callbacks gives the callbacks whose signature changed inside v2 the
// signature they have in v3, for a project that is on a release older than the
// change, whose code is written for the old one:
//
//	app.Hooks().OnListen(func() error { ... })          ->  app.Hooks().OnListen(func(fiber.ListenData) error { ... })   (before v2.48)
//	recover.Config{StackTraceHandler: func(e any) { }}  ->  recover.Config{StackTraceHandler: func(_ fiber.Ctx, e any) { }}   (before v2.23)
//
// A handler that is a function or method value is wrapped
// (OnListen(start) -> OnListen(func(fiber.ListenData) error { return start() })), and one that is
// anything else is reported. A project on a later release is not touched: its
// callbacks are the ones of v3.
func MigrateOlderV2Callbacks(cmd *cobra.Command, cwd string, curr, _ *semver.Version) error {
	if curr == nil || !curr.LessThan(v2OnListenSince) {
		return nil
	}

	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return v2CallbacksContent(ix, content, fi, curr)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate the callbacks of older releases of Fiber v2: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating the callbacks of older releases of Fiber v2")
	return nil
}

func v2CallbacksContent(ix *appIndex, content string, fi internal.FileInfo, curr *semver.Version) string {
	listen := strings.Contains(content, v2OnListen)
	stack := curr.LessThan(v2StackTraceSince) && strings.Contains(content, v2StackTrace)
	if !listen && !stack {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	c := &v2Callbacks{f: f}
	if stack {
		c.recover = v2RecoverNames(f)
	}
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if listen {
				c.onListen(x)
			}
		case *ast.CompositeLit:
			c.stackTrace(x)
		case *ast.AssignStmt:
			c.stackTraceAssigned(x)
		}
		return true
	})
	return c.imports.apply(c.edits.apply(content))
}

// v2Callbacks is the rewrite of one file.
type v2Callbacks struct {
	f       *appFile
	edits   editSet
	imports appImports
	// recover are the names the file has for the recover middleware, when the
	// release asks for the rewrite of its StackTraceHandler.
	recover []string
}

// v2RecoverNames returns the identifiers the file binds the recover middleware to.
func v2RecoverNames(f *appFile) []string {
	var names []string
	for _, imp := range f.imports {
		switch {
		case !v2RecoverPathRe.MatchString(imp.path) || imp.name == "_" || imp.name == ".":
		case imp.name != "":
			names = append(names, imp.name)
		default:
			names = append(names, defaultImportName(imp.path))
		}
	}
	return names
}

// onListen adapts the handlers of Hooks().OnListen.
func (c *v2Callbacks) onListen(call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != v2OnListen || c.f.receiverKind(call, sel) != appKindHooks {
		return
	}

	if call.Ellipsis.IsValid() {
		internal.Notef(c.f.fi, c.f.g.line(call.Pos()),
			"OnListen handlers are func(fiber.ListenData) error in v3, and func() error up to v2.47: convert the handlers you spread into it by hand")
		return
	}
	for _, arg := range call.Args {
		c.listenHandler(arg)
	}
}

// listenHandler adapts one OnListen handler, a func() error, to the
// func(fiber.ListenData) error that v3 takes.
func (c *v2Callbacks) listenHandler(arg ast.Expr) {
	g := c.f.g
	typ := func() string { return c.imports.fiberQualifier(c.f) + v2ListenData }

	switch h := arg.(type) {
	case *ast.FuncLit:
		if h.Type.Params == nil || len(h.Type.Params.List) > 0 {
			return // takes the listen data already
		}
		at := g.offset(h.Type.Params.Opening) + 1
		c.edits.replace(at, at, typ())
	default:
		if !appIsPureReference(arg) {
			internal.Notef(c.f.fi, g.line(arg.Pos()),
				"OnListen handlers are func(fiber.ListenData) error in v3: wrap %s in func(fiber.ListenData) error { return ...() }", g.text(arg))
			return
		}
		c.edits.replaceNode(g, arg, "func("+typ()+") error { return "+g.text(arg)+"() }")
	}
}

// stackTrace adapts the StackTraceHandler of a recover.Config literal.
func (c *v2Callbacks) stackTrace(lit *ast.CompositeLit) {
	if len(c.recover) == 0 || !c.isRecoverConfig(lit) {
		return
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok || !isIdentNamed(kv.Key, v2StackTrace) {
			continue
		}
		c.stackTraceHandler(kv.Value)
	}
}

// isRecoverConfig reports whether the literal is a recover.Config.
func (c *v2Callbacks) isRecoverConfig(lit *ast.CompositeLit) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != appNameConfig {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains(c.recover, id.Name)
}

// stackTraceHandler adapts one StackTraceHandler, a func(e any), to the
// func(c fiber.Ctx, e any) that v3 takes.
func (c *v2Callbacks) stackTraceHandler(value ast.Expr) {
	g := c.f.g
	ctx := func() string { return c.imports.fiberQualifier(c.f) + ctxTypeCtx }

	switch h := value.(type) {
	case *ast.FuncLit:
		params := h.Type.Params
		if params == nil || len(params.List) != 1 || len(params.List[0].Names) > 1 {
			return // takes the context already, or is not the handler of a release this old
		}
		at := g.offset(params.Opening) + 1
		if len(params.List[0].Names) == 1 {
			c.edits.replace(at, at, "_ "+ctx()+", ")
			return
		}
		c.edits.replace(at, at, ctx()+", ")
	default:
		if !appIsPureReference(value) {
			internal.Notef(c.f.fi, g.line(value.Pos()),
				"the StackTraceHandler of recover is func(c fiber.Ctx, e any) in v3, and func(e any) up to v2.22: wrap %s in func(_ fiber.Ctx, e any) { ...(e) }", g.text(value))
			return
		}
		c.edits.replaceNode(g, value, "func(_ "+ctx()+", "+v2StackTraceValue+" any) { "+g.text(value)+"("+v2StackTraceValue+") }")
	}
}

// stackTraceAssigned reports the assignments to a StackTraceHandler field, which
// this migration cannot follow to the value.
func (c *v2Callbacks) stackTraceAssigned(assign *ast.AssignStmt) {
	if len(c.recover) == 0 {
		return
	}
	for _, lhs := range assign.Lhs {
		if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == v2StackTrace {
			internal.Notef(c.f.fi, c.f.g.line(sel.Pos()),
				"the StackTraceHandler of recover is func(c fiber.Ctx, e any) in v3, and func(e any) up to v2.22: adapt the handler you assign here")
		}
	}
}
