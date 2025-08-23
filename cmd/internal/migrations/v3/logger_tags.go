package v3

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateLoggerTags(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return strings.ReplaceAll(content, "logger.TagHeader", "logger.TagReqHeader")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate logger tags: %w", err)
	}

	cmd.Println("Migrating logger tag constants")
	return nil
}
