package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateShutdownHook(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		reName := regexp.MustCompile(`\.Hooks\(\)\.OnShutdown\(`)
		content = reName.ReplaceAllString(content, ".Hooks().OnPostShutdown(")

		reInline := regexp.MustCompile(`\.OnPostShutdown\(func\(\s*\)`)
		content = reInline.ReplaceAllString(content, ".OnPostShutdown(func(err error)")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate shutdown hooks: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating shutdown hooks")
	return nil
}
