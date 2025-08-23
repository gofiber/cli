package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateViewBind(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	// Replace .Bind() with arguments, not the Bind() from the binding package
	reViewBind := regexp.MustCompile(`\.Bind\(([^)]+)\)`)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return reViewBind.ReplaceAllString(content, ".ViewBind($1)")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate ViewBind calls: %w", err)
	}

	cmd.Println("Migrating view binding helpers")
	return nil
}
