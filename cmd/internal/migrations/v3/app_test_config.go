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
		return appMigrateTestContent(ix, content, fi)
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

func appMigrateTestContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "Test") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	var imps appImports
	taken := map[*ast.FuncDecl]map[string]bool{}
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall || len(call.Args) < 2 {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "Test" || f.receiverKind(call, sel) != appKindApp {
			return true
		}

		if call.Ellipsis.IsValid() && len(call.Args) == 2 && f.spreadTestConfig(&edits, &imps, call, taken) {
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

		edits.replaceNode(f.g, arg, appTestConfigFor(f, arg, &imps))
		return true
	})
	return imps.apply(edits.apply(content))
}

// appTestConfigFor returns the fiber.TestConfig that stands for the timeout arg of
// app.Test in milliseconds.
func appTestConfigFor(f *appFile, arg ast.Expr, imps *appImports) string {
	cfg := imps.fiberQualifier(f) + "TestConfig"
	if appIsNegativeNumber(arg) {
		return cfg + "{Timeout: 0, FailOnTimeout: false}"
	}
	timeQ := imps.qualifier(f, "time", "time")
	return cfg + "{Timeout: " + timeQ + "Duration(" + f.g.text(arg) + ") * " + timeQ + "Millisecond, FailOnTimeout: true}"
}

// appIsNegativeNumber reports whether e is a negative numeric literal such as -1,
// which v2 read as "no timeout".
func appIsNegativeNumber(e ast.Expr) bool {
	u, ok := e.(*ast.UnaryExpr)
	if !ok || u.Op != token.SUB {
		return false
	}
	lit, ok := u.X.(*ast.BasicLit)
	return ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT)
}

// spreadTestConfig migrates app.Test(req, timeouts...), where the optional
// timeout is a variadic parameter passed on: before the statement of the call
// it builds the config v2 stood for (a second, an error when it elapses, the
// first timeout when there is one), and the call gets that config.
func (f *appFile) spreadTestConfig(edits *editSet, imps *appImports, call *ast.CallExpr, taken map[*ast.FuncDecl]map[string]bool) bool {
	g := f.g
	spread := call.Args[1]
	if !appIsPureReference(spread) {
		return false
	}
	stmt := f.statementOf(call)
	if stmt == nil {
		return false
	}

	decl := f.enclosingDecl(call)
	if taken[decl] == nil {
		taken[decl] = map[string]bool{}
	}
	name := f.freshName(call, "testConfig", taken[decl])
	taken[decl][name] = true

	cfg := imps.fiberQualifier(f) + "TestConfig"
	timeQ := imps.qualifier(f, "time", "time")
	ms := g.text(spread)
	edits.replace(g.offset(stmt.Pos()), g.offset(stmt.Pos()),
		name+" := "+cfg+"{Timeout: "+timeQ+"Second, FailOnTimeout: true}\n"+
			"if len("+ms+") > 0 {\n"+
			name+".Timeout = "+timeQ+"Duration("+ms+"[0]) * "+timeQ+"Millisecond\n"+
			"}\n")

	// the spread goes away: the call takes the config
	edits.replace(g.offset(spread.Pos()), g.offset(call.Ellipsis)+len("..."), name)
	return true
}
