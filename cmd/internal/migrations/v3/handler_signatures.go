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

// ctxReflectPath matches the import path of the reflect package.
var ctxReflectPath = regexp.MustCompile(`^reflect$`)

// MigrateHandlerSignatures turns the *fiber.Ctx of v2, a pointer to a struct,
// into the fiber.Ctx of v3, an interface. Only types are rewritten, in files
// that import the fiber package, under whatever name they import it.
func MigrateHandlerSignatures(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxHandlerSignaturesContent(content, fi, run)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate handler signatures: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating handler signatures")

	return nil
}

func ctxHandlerSignaturesContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !strings.Contains(content, "*") {
		return content
	}
	g, env, ok := run.open(fi, content)
	if !ok {
		return content
	}
	names := env.names
	reflectNames := importNames(content, ctxReflectPath, "reflect")

	var edits editSet
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		star, ok := n.(*ast.StarExpr)
		if !ok {
			return true
		}

		// The pointer to an alias of the context, to Ctx in a package that says
		// type Ctx = fiber.Ctx, is a pointer to the interface just the same.
		ctxType := ctxQualifier(names) + ctxTypeCtx
		if !ctxIsCtxTypeName(star.X, names, stack) {
			if !env.isCtxAlias(star.X, stack) {
				return true
			}
			ctxType = g.text(star.X)
		}

		if conv, ok := ctxConversionOf(star, stack); ok {
			ctxMigrateCtxConversion(g, &edits, fi, star, conv, stack[:len(stack)-2], reflectNames, ctxType)
			return true
		}

		ctxDropStar(g, &edits, star)
		return true
	})

	return edits.apply(content)
}

// ctxIsCtxTypeName reports whether e names the Ctx type of the fiber package:
// fiber.Ctx or, in a file that dot-imports fiber, a Ctx that no declaration of
// the file hides.
func ctxIsCtxTypeName(e ast.Expr, names []string, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return isFiberType(x, names, ctxTypeCtx)
	case *ast.Ident:
		if x.Name != ctxTypeCtx || !slices.Contains(names, ".") {
			return false
		}
		_, declared := resolveIdent(x.Name, x.Pos(), stack)
		return !declared
	}
	return false
}

// ctxConversionOf returns the conversion (*fiber.Ctx)(x) that star is the type
// of, if it is one. stack are the ancestors of star.
func ctxConversionOf(star *ast.StarExpr, stack []ast.Node) (*ast.CallExpr, bool) {
	if len(stack) < 2 {
		return nil, false
	}
	paren, ok := stack[len(stack)-1].(*ast.ParenExpr)
	if !ok || paren.X != star {
		return nil, false
	}
	call, ok := stack[len(stack)-2].(*ast.CallExpr)
	if !ok || call.Fun != paren || len(call.Args) != 1 {
		return nil, false
	}
	return call, true
}

// ctxDropStar schedules the removal of the * of a pointer type.
func ctxDropStar(g *goSource, edits *editSet, star *ast.StarExpr) {
	from, to := g.offset(star.Star), g.offset(star.X.Pos())
	if strings.TrimSpace(g.src[from+1:to]) != "" {
		to = from + 1
	}
	edits.replace(from, to, "")
}

// ctxMigrateCtxConversion handles the conversion (*fiber.Ctx)(nil). It used to
// make a nil pointer of the type of the handlers' argument; fiber.Ctx(nil), the
// closest v3 has, is a nil interface, which reflect.TypeOf does not see as a type
// at all. So the conversion stays what it is, a pointer to the interface, and
// reflect.TypeOf((*fiber.Ctx)(nil)) gets the .Elem() that makes it the type of
// the handlers' argument again. An interface assertion keeps working with the
// plain interface, anything else is left to the user.
func ctxMigrateCtxConversion(g *goSource, edits *editSet, fi internal.FileInfo, star *ast.StarExpr, conv *ast.CallExpr, ancestors []ast.Node, reflectNames []string, ctxType string) {
	if len(ancestors) == 0 {
		return
	}

	switch parent := ancestors[len(ancestors)-1].(type) {
	case *ast.CallExpr:
		if !ctxIsReflectTypeOf(parent, reflectNames) || len(parent.Args) != 1 || parent.Args[0] != conv {
			break
		}
		if ctxIsElemCalled(parent, ancestors[:len(ancestors)-1]) {
			return
		}
		end := g.offset(parent.End())
		edits.replace(end, end, ".Elem()")
		return
	case *ast.ValueSpec:
		if len(parent.Names) == 1 && parent.Names[0].Name == "_" {
			ctxDropStar(g, edits, star)
			return
		}
	}

	internal.Notef(fi, g.line(conv.Pos()),
		"%s is a nil pointer to the Ctx interface now, which is not the type of the handlers' argument: where that was meant, use reflect.TypeFor[%s]() or the interface itself",
		g.text(conv), ctxType)
}

// ctxIsReflectTypeOf reports whether call is reflect.TypeOf(...).
func ctxIsReflectTypeOf(call *ast.CallExpr, reflectNames []string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "TypeOf" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains(reflectNames, id.Name)
}

// ctxIsElemCalled reports whether call is the operand of a call of .Elem().
func ctxIsElemCalled(call *ast.CallExpr, ancestors []ast.Node) bool {
	if len(ancestors) < 2 {
		return false
	}
	sel, ok := ancestors[len(ancestors)-1].(*ast.SelectorExpr)
	if !ok || sel.X != call || sel.Sel.Name != "Elem" {
		return false
	}
	outer, ok := ancestors[len(ancestors)-2].(*ast.CallExpr)
	return ok && outer.Fun == sel
}
