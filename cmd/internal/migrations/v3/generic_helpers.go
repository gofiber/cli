package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateGenericHelpers(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reParamsInt := regexp.MustCompile(`(\w+)\.ParamsInt\(`)
	reQueryInt := regexp.MustCompile(`(\w+)\.QueryInt\(`)
	reQueryFloat := regexp.MustCompile(`(\w+)\.QueryFloat\(`)
	reQueryBool := regexp.MustCompile(`(\w+)\.QueryBool\(`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
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
