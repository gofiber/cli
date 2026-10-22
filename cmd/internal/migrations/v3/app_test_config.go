package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateAppTestConfig turns the millisecond timeout of app.Test into a
// fiber.TestConfig. v2 returned an error when the timeout elapsed, so the
// config sets FailOnTimeout; v3 treats a timeout of zero or less as no timeout,
// as -1 was in v2.
func MigrateAppTestConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateAppTestContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate app.Test calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating app.Test usages")
	return nil
}

func migrateAppTestContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "Test") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	var imps appImports
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall || len(call.Args) < 2 {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "Test" || f.kind(sel.X) != appKindApp {
			return true
		}

		if call.Ellipsis.IsValid() || len(call.Args) > 2 {
			internal.Notef(fi, f.g.line(call.Pos()),
				"app.Test takes a single fiber.TestConfig in v3: build it from the millisecond timeouts by hand (Timeout is a time.Duration, FailOnTimeout: true keeps the error v2 returned on timeout)")
			return true
		}

		arg := call.Args[1]
		if t, tf := f.valueType(arg, 0); t != nil && tf.fiberTypeName(t) == "TestConfig" {
			return true // migrated already
		}

		edits.replaceNode(f.g, arg, testConfigFor(f, arg, &imps))
		return true
	})
	return imps.apply(edits.apply(content))
}

// testConfigFor returns the fiber.TestConfig that stands for the timeout arg of
// app.Test in milliseconds.
func testConfigFor(f *appFile, arg ast.Expr, imps *appImports) string {
	cfg := imps.fiberQualifier(f) + "TestConfig"
	if isNegativeNumber(arg) {
		return cfg + "{Timeout: 0, FailOnTimeout: false}"
	}
	timeQ := imps.qualifier(f, "time", "time")
	return cfg + "{Timeout: " + timeQ + "Duration(" + f.g.text(arg) + ") * " + timeQ + "Millisecond, FailOnTimeout: true}"
}

// isNegativeNumber reports whether e is a negative numeric literal such as -1,
// which v2 read as "no timeout".
func isNegativeNumber(e ast.Expr) bool {
	u, ok := e.(*ast.UnaryExpr)
	if !ok || u.Op != token.SUB {
		return false
	}
	lit, ok := u.X.(*ast.BasicLit)
	return ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT)
}
