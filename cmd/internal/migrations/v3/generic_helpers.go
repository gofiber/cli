package v3

import (
	"fmt"
	"go/ast"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var strconvPath = regexp.MustCompile(`^strconv$`)

// migrateParamsIntErrors rewrites the calls of ParamsInt whose error the code gets and that
// have no default: v2 returned 0 and an error for a parameter that is no integer, and the
// code answers it (a 400 for /users/abc). fiber.Params[int] has no error to give, so the
// call becomes strconv.Atoi(c.Params("id")), which fails for the same parameters. A call
// with a default never returned an error, and is migrated with the others below.
func migrateParamsIntErrors(content string) string {
	if !strings.Contains(content, ".ParamsInt(") {
		return content
	}
	src, ok := parseForEdits(content)
	if !ok {
		return content
	}

	name := src.importName(strconvPath)
	needsImport := false
	rewrite := func(e ast.Expr) {
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "ParamsInt" {
			return
		}

		if name == "" {
			name = "strconv"
			needsImport = true
		}
		src.replace(call, name+".Atoi("+src.text(sel.X)+".Params("+src.text(call.Args[0])+"))")
	}

	ast.Inspect(src.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt: // two values assigned
			if len(n.Lhs) == 2 && len(n.Rhs) == 1 {
				rewrite(n.Rhs[0])
			}
		case *ast.ValueSpec: // two values declared
			if len(n.Names) == 2 && len(n.Values) == 1 {
				rewrite(n.Values[0])
			}
		case *ast.ReturnStmt: // both values returned
			if len(n.Results) == 1 {
				rewrite(n.Results[0])
			}
		}
		return true
	})

	out := src.apply()
	if needsImport && out != content {
		out = addImport(out, "strconv")
	}
	return out
}

func MigrateGenericHelpers(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reParamsIntAssign := regexp.MustCompile(`(\w+)\s*,\s*(\w+)\s*:=\s*(\w+)\.ParamsInt\(([^)]*)\)`)
	reQueryIntAssign := regexp.MustCompile(`(\w+)\s*,\s*(\w+)\s*:=\s*(\w+)\.QueryInt\(([^)]*)\)`)
	reQueryFloatAssign := regexp.MustCompile(`(\w+)\s*,\s*(\w+)\s*:=\s*(\w+)\.QueryFloat\(([^)]*)\)`)
	reQueryBoolAssign := regexp.MustCompile(`(\w+)\s*,\s*(\w+)\s*:=\s*(\w+)\.QueryBool\(([^)]*)\)`)

	reParamsInt := regexp.MustCompile(`(\w+)\.ParamsInt\(`)
	reQueryInt := regexp.MustCompile(`(\w+)\.QueryInt\(`)
	reQueryFloat := regexp.MustCompile(`(\w+)\.QueryFloat\(`)
	reQueryBool := regexp.MustCompile(`(\w+)\.QueryBool\(`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		content = migrateParamsIntErrors(content)

		assignReplacements := []struct {
			re   *regexp.Regexp
			repl string
		}{
			{reParamsIntAssign, "$1, $2 := fiber.Params[int]($3, $4), error(nil)"},
			{reQueryIntAssign, "$1, $2 := fiber.Query[int]($3, $4), error(nil)"},
			{reQueryFloatAssign, "$1, $2 := fiber.Query[float64]($3, $4), error(nil)"},
			{reQueryBoolAssign, "$1, $2 := fiber.Query[bool]($3, $4), error(nil)"},
		}
		for _, r := range assignReplacements {
			content = r.re.ReplaceAllString(content, r.repl)
		}

		content = reParamsInt.ReplaceAllString(content, "fiber.Params[int]($1, ")
		content = reQueryInt.ReplaceAllString(content, "fiber.Query[int]($1, ")
		content = reQueryFloat.ReplaceAllString(content, "fiber.Query[float64]($1, ")
		content = reQueryBool.ReplaceAllString(content, "fiber.Query[bool]($1, ")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate generic helpers: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating generic helpers")
	return nil
}
