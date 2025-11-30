package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

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
