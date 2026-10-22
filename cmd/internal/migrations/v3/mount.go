package v3

import (
	"fmt"
	"go/ast"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateMount turns app.Mount(prefix, subApp) into app.Use(prefix, subApp),
// which is how v3 mounts an app. Only the Mount of a fiber app, group or router
// is renamed: other types have a Mount method of their own.
func MigrateMount(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateMountContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Mount usages: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating Mount usages")
	return nil
}

func migrateMountContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "Mount") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall || len(call.Args) != 2 || call.Ellipsis.IsValid() {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if isSel && sel.Sel.Name == "Mount" && f.kind(sel.X).isRouter() {
			edits.replaceNode(f.g, sel.Sel, "Use")
		}
		return true
	})
	return edits.apply(content)
}
