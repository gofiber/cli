package v3

import (
	"fmt"
	"go/ast"
	"slices"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateSessionStore renames session.New to session.NewStore wherever the
// fiber session middleware is used: in v2 it made the store, in v3 it makes the
// middleware handler and the store has its own constructor.
//
// Only the package the file imports from fiber under that import's name is
// touched: another package named session (the AWS SDK, the project's own) keeps
// its New. A call that is passed straight to Use is the v3 middleware and also
// stays.
func MigrateSessionStore(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs := newMw3Pkgs()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return mw3SessionStoreContent(content, fi, pkgs)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session store: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating session store")
	return nil
}

func mw3SessionStoreContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	f, ok := mw3OpenSessionFile(fi, src, pkgs)
	if !ok {
		return src
	}

	var edits editSet
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !f.isMember(call.Fun, mw3SessionNew, stack) || mw3IsUseArgument(call, stack) {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok {
			edits.replaceNode(f.g, sel.Sel, mw3SessionNewStore)
		}
		return true
	})
	return edits.apply(src)
}

// mw3IsUseArgument reports whether the call is an argument of a Use call: the
// middleware handler of v3, not a store.
func mw3IsUseArgument(call *ast.CallExpr, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	outer, ok := stack[len(stack)-1].(*ast.CallExpr)
	if !ok || outer == call {
		return false
	}
	sel, ok := outer.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != nameUse {
		return false
	}
	return slices.Contains(outer.Args, ast.Expr(call))
}
