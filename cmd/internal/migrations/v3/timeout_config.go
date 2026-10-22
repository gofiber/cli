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
func MigrateTimeoutConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateTimeoutContent)
	if err != nil {
		return fmt.Errorf("failed to migrate timeout middleware configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating timeout middleware configs")
	return nil
}

func migrateTimeoutContent(content string, fi internal.FileInfo) string {
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

	var edits editSet
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
			migrateTimeoutNew(g, &edits, call, id.Name, names, stack)
		case "NewWithContext":
			migrateTimeoutNewWithContext(g, &edits, call, sel, id.Name)
		}
		return true
	})
	return edits.apply(content)
}

// migrateTimeoutNew rewrites timeout.New(handler, d); a call that already passes
// a config is left alone.
func migrateTimeoutNew(g *goSource, edits *editSet, call *ast.CallExpr, pkg string, names []string, stack []ast.Node) {
	if len(call.Args) != 2 || call.Ellipsis.IsValid() || mw2IsTyped(call.Args[1], names, mw2ConfigType, stack, 0) {
		return
	}
	edits.replaceNode(g, call.Args[1], pkg+".Config{Timeout: "+g.text(call.Args[1])+"}")
}

// migrateTimeoutNewWithContext rewrites timeout.NewWithContext(handler, d, errs...)
// to timeout.New(handler, timeout.Config{Timeout: d, Errors: []error{errs...}}).
func migrateTimeoutNewWithContext(g *goSource, edits *editSet, call *ast.CallExpr, sel *ast.SelectorExpr, pkg string) {
	if len(call.Args) < 2 {
		return
	}

	cfg := pkg + ".Config{Timeout: " + g.text(call.Args[1])
	if extra := call.Args[2:]; len(extra) > 0 {
		if call.Ellipsis.IsValid() {
			cfg += ", Errors: " + g.text(extra[len(extra)-1])
		} else {
			texts := make([]string, len(extra))
			for i, e := range extra {
				texts[i] = g.text(e)
			}
			cfg += ", Errors: []error{" + strings.Join(texts, ", ") + "}"
		}
	}
	cfg += "}"

	end := g.offset(call.Args[len(call.Args)-1].End())
	if call.Ellipsis.IsValid() {
		end = g.offset(call.Ellipsis) + len("...")
	}
	edits.replaceNode(g, sel.Sel, "New")
	edits.replace(g.offset(call.Args[1].Pos()), end, cfg)
}
