package v3

import (
	"fmt"
	"go/ast"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var timeoutPath = middlewarePath("timeout")

// MigrateTimeoutConfig moves the constructors of the timeout middleware to its config:
//
//   - timeout.New(h, d) is timeout.New(h, timeout.Config{Timeout: d}).
//   - timeout.NewWithContext(h, d, errs...), the context aware constructor that v2 had
//     next to the one that raced, is the same New: v3 always works with the context, and
//     the errors that count as a timeout are the Errors of the config.
//
// The package is found through its import, so an alias is understood and a function of
// another package that is called New is left alone.
func MigrateTimeoutConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, migrateTimeoutContent)
	if err != nil {
		return fmt.Errorf("failed to migrate timeout middleware configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating timeout middleware configs")
	return nil
}

func migrateTimeoutContent(content string) string {
	if !strings.Contains(content, "timeout") {
		return content
	}
	src, ok := parseForEdits(content)
	if !ok {
		return content
	}
	pkg := src.importName(timeoutPath)
	if pkg == "" {
		return content
	}

	ast.Inspect(src.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != pkg {
			return true
		}

		switch {
		case sel.Sel.Name == funcNew && len(call.Args) == 2 && !call.Ellipsis.IsValid():
			if !isTimeoutConfig(call.Args[1], pkg) {
				src.replace(call.Args[1], pkg+".Config{Timeout: "+src.text(call.Args[1])+"}")
			}
		case sel.Sel.Name == "NewWithContext" && len(call.Args) >= 2:
			src.replace(call, pkg+".New("+src.text(call.Args[0])+", "+timeoutConfigOf(src, call, pkg)+")")
		}
		return true
	})

	return src.apply()
}

// timeoutConfigOf returns the timeout.Config literal that stands for the arguments of a
// NewWithContext call: the duration, and the errors.
func timeoutConfigOf(src *sourceEdits, call *ast.CallExpr, pkg string) string {
	cfg := pkg + ".Config{Timeout: " + src.text(call.Args[1])
	if errs := call.Args[2:]; len(errs) > 0 {
		if call.Ellipsis.IsValid() {
			cfg += ", Errors: " + src.text(errs[0])
		} else {
			texts := make([]string, 0, len(errs))
			for _, e := range errs {
				texts = append(texts, src.text(e))
			}
			cfg += ", Errors: []error{" + strings.Join(texts, ", ") + "}"
		}
	}
	return cfg + "}"
}

// isTimeoutConfig reports whether e is a timeout.Config literal already.
func isTimeoutConfig(e ast.Expr, pkg string) bool {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	typ, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || typ.Sel.Name != "Config" {
		return false
	}
	id, ok := typ.X.(*ast.Ident)
	return ok && id.Name == pkg
}
