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
func MigrateTimeoutConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	var swallowed timeoutSwallowed
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateTimeoutContent(content, fi, &swallowed)
	})
	swallowed.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate timeout middleware configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating timeout middleware configs")
	return nil
}

func migrateTimeoutContent(content string, fi internal.FileInfo, swallowed *timeoutSwallowed) string {
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

// timeoutRewrite is the state of the rewrite of one file.
type timeoutRewrite struct {
	// onTimeout is the field every config gets.
	onTimeout string
	g         *goSource
	fi        internal.FileInfo
	swallowed *timeoutSwallowed
	edits     editSet
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
	rw.edits.replaceNode(rw.g, call.Args[1], rw.config(pkg, rw.g.text(call.Args[1]), ""))
	rw.rewrote = true
	rw.swallowed.add(rw.fi, rw.g.line(call.Pos()))
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
func (t *timeoutSwallowed) report(root string) {
	if t.places == 0 {
		return
	}
	msg := "timeout.New(handler, duration) of v2 logged an error the handler returned and sent what the handler had " +
		"written, usually 200 with an empty body, and answered 408 only after the whole time when the handler panicked; " +
		"the v3 middleware passes the error on to the error handler and answers 500 at once after a panic"
	if t.places > 1 {
		msg += fmt.Sprintf(" (%d more in this project)", t.places-1)
	}
	internal.AddNote(root, internal.Note{File: t.file, Line: t.line, Message: msg})
}

// config writes the timeout.Config that takes the place of the duration (and of
// the errors) of a call, a field to a line.
func (rw *timeoutRewrite) config(pkg, timeout, errs string) string {
	fields := []string{"Timeout: " + timeout}
	if errs != "" {
		fields = append(fields, "Errors: "+errs)
	}
	fields = append(fields, rw.onTimeout)
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
	cfg := rw.config(pkg, g.text(call.Args[1]), errs)

	end := g.offset(call.Args[len(call.Args)-1].End())
	if call.Ellipsis.IsValid() {
		end = g.offset(call.Ellipsis) + len("...")
	}
	rw.edits.replaceNode(g, sel.Sel, "New")
	rw.edits.replace(g.offset(call.Args[1].Pos()), end, cfg)
	rw.rewrote = true
}
