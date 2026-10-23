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

// MigrateTimeoutConfig moves the timeout middleware to its v3 config: the
// duration of timeout.New(handler, d) and of timeout.NewWithContext(handler, d,
// errs...), the call v2's own documentation recommends, becomes the Timeout of a
// timeout.Config and the extra errors become its Errors. The package is found
// through its import path, whatever name the file gives it.
//
// v2 answered 408 Request Timeout when the time was up, v3 answers 503 Service
// Unavailable unless Config.OnTimeout returns fiber.ErrRequestTimeout, which the
// config gets, so that the status stays the one the clients know.
//
// v2 sent that error through the ErrorHandler of the application, and v3 writes
// the answer of OnTimeout itself when the time is up while the handler still runs,
// so the ErrorHandler never sees it. When a config of the project sets an
// ErrorHandler, the OnTimeout of a timeout.New(handler, duration) and of a
// timeout.NewWithContext calls it (see timeoutApps and timeoutOnTimeoutHandled).
func MigrateTimeoutConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	var swallowed timeoutSwallowed
	apps := scanTimeoutApps(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateTimeoutContent(content, fi, &swallowed, apps)
	})
	swallowed.report(cwd, apps)
	if err != nil {
		return fmt.Errorf("failed to migrate timeout middleware configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating timeout middleware configs")
	return nil
}

func migrateTimeoutContent(content string, fi internal.FileInfo, swallowed *timeoutSwallowed, apps *timeoutApps) string {
	if !strings.Contains(content, "New") {
		return content
	}
	names := importNames(content, mw2Timeout, "timeout")
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	fiber, needsFiber := mw1FiberName(content)
	rw := &timeoutRewrite{g: g, fi: fi, swallowed: swallowed, onTimeout: timeoutOnTimeout(fiber)}
	if apps.custom {
		rw.onTimeoutHandled = timeoutOnTimeoutHandled(fiber)
	}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || !slices.Contains(names, id.Name) {
			return true
		}

		switch sel.Sel.Name {
		case mw2New:
			rw.migrateNew(call, id.Name, names, stack)
		case "NewWithContext":
			rw.migrateNewWithContext(call, sel, id.Name)
		}
		return true
	})

	out := rw.edits.apply(content)
	if needsFiber && rw.rewrote {
		out = mw1AddImport(out, mw1FiberCoreImport)
	}
	return out
}

// timeoutOnTimeout writes the OnTimeout field that keeps the status v2 answered
// with: 408 Request Timeout, where v3 answers 503 Service Unavailable. fiber is
// the name the file gives to the core package; the parameter is blank when that
// name is the one a context is called by.
func timeoutOnTimeout(fiber string) string {
	param := "c"
	if fiber == param {
		param = "_"
	}
	return "OnTimeout: func(" + param + " " + fiber + ".Ctx) error { return " + fiber + ".ErrRequestTimeout }"
}

// timeoutOnTimeoutHandled writes the OnTimeout field of a timeout.New or
// timeout.NewWithContext in a project whose applications may have an ErrorHandler
// of their own. v2 returned fiber.ErrRequestTimeout from the middleware and the
// router handed it to the ErrorHandler. v3 does that only for a handler that has
// returned: when the time is up while the handler still runs, which is also the
// case of a handler that waits for its context, it sends the answer that OnTimeout
// leaves on the context at once and the router never hands the error to the
// ErrorHandler. The two cases tell apart by the context of the request, which is
// the one with the deadline, done, in the first, and the one of the request again
// in the second, so OnTimeout calls the ErrorHandler for the first and leaves the
// second to the router: the ErrorHandler runs once, as in v2, and the error is
// returned in both cases for the middleware around the timeout.
func timeoutOnTimeoutHandled(fiber string) string {
	ctx, errName := "c", nameErr
	if fiber == ctx {
		ctx = "ctx"
	}
	if fiber == errName {
		errName = "reqErr"
	}
	return "OnTimeout: func(" + ctx + " " + fiber + ".Ctx) error {\n" +
		errName + " := " + fiber + ".ErrRequestTimeout\n" +
		"if " + ctx + ".Context().Err() != nil {\n" +
		"// v3 sends this answer itself: the error handler of the application gives it the shape it had in v2\n" +
		"_ = " + ctx + ".App().Config().ErrorHandler(" + ctx + ", " + errName + ")\n" +
		"}\n" +
		"return " + errName + "\n}"
}

// timeoutRewrite is the state of the rewrite of one file.
type timeoutRewrite struct {
	// onTimeout is the field every config gets.
	onTimeout string
	// onTimeoutHandled is the field every config gets instead when the project has an
	// ErrorHandler; empty otherwise.
	onTimeoutHandled string
	g                *goSource
	fi               internal.FileInfo
	swallowed        *timeoutSwallowed
	edits            editSet
	// rewrote is set once a call has been given a config that refers to the core
	// package.
	rewrote bool
}

// migrateNew rewrites timeout.New(handler, d); a call that already passes a
// config is left alone.
func (rw *timeoutRewrite) migrateNew(call *ast.CallExpr, pkg string, names []string, stack []ast.Node) {
	if len(call.Args) != 2 || call.Ellipsis.IsValid() || mw2IsTyped(call.Args[1], names, mw2ConfigType, stack, 0) {
		return
	}
	rw.edits.replaceNode(rw.g, call.Args[1], rw.config(pkg, rw.g.text(call.Args[1]), "", rw.onTimeoutField()))
	rw.rewrote = true
	rw.swallowed.add(rw.fi, rw.g.line(call.Pos()))
}

// onTimeoutField returns the OnTimeout field a config gets.
func (rw *timeoutRewrite) onTimeoutField() string {
	if rw.onTimeoutHandled != "" {
		return rw.onTimeoutHandled
	}
	return rw.onTimeout
}

// timeoutSwallowed collects the calls of the timeout.New(handler, d) of v2, which
// only logged an error its handler returned, so that the project is told once.
type timeoutSwallowed struct {
	file   string
	line   int
	places int
}

func (t *timeoutSwallowed) add(fi internal.FileInfo, line int) {
	if t.places == 0 {
		t.file, t.line = fi.Rel, line
	}
	t.places++
}

// report records the note for the project in root, if a call was rewritten.
func (t *timeoutSwallowed) report(root string, apps *timeoutApps) {
	if t.places == 0 {
		return
	}
	msg := "timeout.New(handler, duration) of v2 logged an error the handler returned and sent what the handler had " +
		"written, usually 200 with an empty body, and answered 408 only after the whole time when the handler panicked; " +
		"the v3 middleware passes the error on to the error handler and answers 500 at once after a panic"
	if t.places > 1 {
		msg += fmt.Sprintf(" (%d more in this project)", t.places-1)
	}
	if !apps.custom && !apps.known() {
		msg += ". v2 also gave the 408 of a timeout to the ErrorHandler of the application, and v3 sends the answer of OnTimeout itself: " +
			"no application with a default ErrorHandler could be found in this project, so if yours has an ErrorHandler of its own, " +
			"call it from OnTimeout when the deadline has passed (err := fiber.ErrRequestTimeout; if c.Context().Err() != nil { _ = c.App().Config().ErrorHandler(c, err) }; return err)"
	}
	internal.AddNote(root, internal.Note{File: t.file, Line: t.line, Message: msg})
}

// timeoutApps is what the project says about the ErrorHandler of its applications.
// It is read from the non-test files of the whole tree before any file is
// rewritten, because the application is made in another file, or another package,
// than the middleware.
type timeoutApps struct {
	// custom is set when a fiber.Config of the project sets an ErrorHandler.
	custom bool
	// apps counts the applications made with a config that can be read (none, or a
	// literal), open the ones made from a config that is held elsewhere.
	apps, open int
}

// known reports whether every application of the project is made with a config that
// can be read, so that the project does not have an ErrorHandler when custom is not
// set.
func (t *timeoutApps) known() bool { return t.apps > 0 && t.open == 0 }

// scanTimeoutApps reads the project.
func scanTimeoutApps(cwd string) *timeoutApps {
	t := &timeoutApps{}
	//nolint:errcheck // a tree that cannot be read is migrated, or reported, by the pass that writes it
	_, _ = internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		t.scan(content, fi)
		return content
	})
	return t
}

// scan reads one file.
func (t *timeoutApps) scan(content string, fi internal.FileInfo) {
	if strings.HasSuffix(fi.Path, "_test.go") {
		return
	}
	names := fiberNamesOf(content)
	if !strings.Contains(content, "ErrorHandler") && len(names) == 0 {
		return
	}
	g, ok := parseSource(content)
	if !ok {
		return
	}

	g.walk(func(n ast.Node, _ []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if mw1KeyValue(x, "ErrorHandler") != nil && mayBeAppConfig(x, names) {
				t.custom = true
			}
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if sel, isSel := lhs.(*ast.SelectorExpr); isSel && sel.Sel.Name == "ErrorHandler" {
					t.custom = true
				}
			}
		case *ast.CallExpr:
			t.scanApp(x, names)
		}
		return true
	})
}

// mayBeAppConfig reports whether the literal may be the config of an application:
// the literal of fiber.Config, of the Config of the module package, or of a type
// whose name does not say that it belongs to another package. The configs of the
// middleware have an ErrorHandler too, and the literal says which it is.
func mayBeAppConfig(lit *ast.CompositeLit, fiberNames []string) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return true
	}
	id, isIdent := sel.X.(*ast.Ident)
	if !isIdent || sel.Sel.Name != "Config" {
		return true
	}
	return slices.Contains(fiberNames, id.Name) || id.Name == moduleShimPkg
}

// scanApp counts a call that makes an application.
func (t *timeoutApps) scanApp(call *ast.CallExpr, fiberNames []string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	isNew := sel.Sel.Name == mw2New && mw1IsMember(sel, fiberNames, mw2New)
	if !isNew && sel.Sel.Name != "NewApp" {
		return
	}
	switch {
	case len(call.Args) == 0:
		t.apps++
	case len(call.Args) == 1 && isCompositeLiteral(call.Args[0]):
		t.apps++
	default:
		t.open++
	}
}

// isCompositeLiteral reports whether e is a composite literal.
func isCompositeLiteral(e ast.Expr) bool {
	_, ok := ast.Unparen(e).(*ast.CompositeLit)
	return ok
}

// config writes the timeout.Config that takes the place of the duration (and of
// the errors) of a call, a field to a line.
func (*timeoutRewrite) config(pkg, timeout, errs, onTimeout string) string {
	fields := []string{"Timeout: " + timeout}
	if errs != "" {
		fields = append(fields, "Errors: "+errs)
	}
	fields = append(fields, onTimeout)
	return pkg + ".Config{\n" + strings.Join(fields, ",\n") + ",\n}"
}

// migrateNewWithContext rewrites timeout.NewWithContext(handler, d, errs...)
// to timeout.New(handler, timeout.Config{Timeout: d, Errors: []error{errs...}, OnTimeout: ...}).
func (rw *timeoutRewrite) migrateNewWithContext(call *ast.CallExpr, sel *ast.SelectorExpr, pkg string) {
	if len(call.Args) < 2 {
		return
	}
	g := rw.g

	errs := ""
	if extra := call.Args[2:]; len(extra) > 0 {
		if call.Ellipsis.IsValid() {
			errs = g.text(extra[len(extra)-1])
		} else {
			texts := make([]string, len(extra))
			for i, e := range extra {
				texts[i] = g.text(e)
			}
			errs = "[]error{" + strings.Join(texts, ", ") + "}"
		}
	}
	cfg := rw.config(pkg, g.text(call.Args[1]), errs, rw.onTimeoutField())

	end := g.offset(call.Args[len(call.Args)-1].End())
	if call.Ellipsis.IsValid() {
		end = g.offset(call.Ellipsis) + len("...")
	}
	rw.edits.replaceNode(g, sel.Sel, "New")
	rw.edits.replace(g.offset(call.Args[1].Pos()), end, cfg)
	rw.rewrote = true
}
