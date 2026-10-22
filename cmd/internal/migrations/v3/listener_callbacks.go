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

// appRemovedCallbacks are the callbacks fiber.ListenConfig had in the v3
// pre-releases (alpha, beta, rc) and lost before 3.0.0.
var appRemovedCallbacks = []string{"OnShutdownError", "OnShutdownSuccess"}

// MigrateListenerCallbacks takes the OnShutdownError and OnShutdownSuccess
// callbacks out of a fiber.ListenConfig, where only the v3 pre-releases had
// them. The code of a callback is kept in a TODO comment: v3 runs such code from
// app.Hooks().OnPostShutdown, whose handler receives the shutdown error, and
// where to register it depends on the app.
func MigrateListenerCallbacks(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return appMigrateListenerCallbacksContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener callbacks: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating listener callbacks")
	return nil
}

func appMigrateListenerCallbacksContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "OnShutdownError") && !strings.Contains(content, "OnShutdownSuccess") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if x.Type != nil && slices.Contains([]string{appNameListenConfig, "ListenerConfig"}, f.fiberTypeName(x.Type)) {
				appCommentOutCallbacks(f, &edits, x)
			}
		case *ast.SelectorExpr:
			if slices.Contains(appRemovedCallbacks, x.Sel.Name) {
				if t, tf := f.valueType(x.X, 0); t != nil && slices.Contains([]string{appNameListenConfig, "ListenerConfig"}, tf.fiberTypeName(t)) {
					internal.Notef(fi, f.g.line(x.Pos()), appCallbackNote, x.Sel.Name)
				}
			}
		}
		return true
	})
	return edits.apply(content)
}

const appCallbackNote = "%s was removed from fiber.ListenConfig before v3.0.0: run that code from app.Hooks().OnPostShutdown(func(err error) error { ... }) instead"

// appCommentOutCallbacks replaces the removed callbacks of a fiber.ListenConfig
// literal by TODO comments that hold their code.
func appCommentOutCallbacks(f *appFile, edits *editSet, lit *ast.CompositeLit) {
	g := f.g
	oneLine := g.line(lit.Lbrace) == g.line(lit.Rbrace)

	var drop []int
	for i, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		for _, name := range appRemovedCallbacks {
			if isIdentNamed(kv.Key, name) {
				drop = append(drop, i)
				internal.Notef(f.fi, g.line(kv.Pos()), appCallbackNote, name)
			}
		}
	}
	if len(drop) == 0 {
		return
	}

	if oneLine {
		var kept, comments []string
		for i, el := range lit.Elts {
			if slices.Contains(drop, i) {
				comments = append(comments, "/* TODO(fiber migrate): "+strings.ReplaceAll(g.text(el), "*/", "* /")+" */")
			} else {
				kept = append(kept, g.text(el))
			}
		}
		inner := strings.Join(comments, " ")
		if len(kept) > 0 {
			inner += " " + strings.Join(kept, ", ")
		}
		edits.replace(g.offset(lit.Lbrace)+1, g.offset(lit.Rbrace), inner)
		return
	}

	for _, i := range drop {
		el := lit.Elts[i]
		from, to, ok := appWholeLines(g.src, g.offset(el.Pos()), g.offset(el.End()))
		if !ok {
			continue // shares its line with other code: left for the compiler
		}
		var b strings.Builder
		for _, line := range strings.SplitAfter(strings.TrimSuffix(g.src[from:to], "\n"), "\n") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			b.WriteString(indent + "// TODO(fiber migrate): " + strings.TrimSpace(line) + "\n") //nolint:errcheck // WriteString never returns an error
		}
		edits.replace(from, to, b.String())
	}
}
