package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateLoggerConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		reConfig := regexp.MustCompile(`logger\.Config{`)
		return IterateConfigBlocks(content, reConfig, func(s string) string {
			return strings.ReplaceAll(s, "Output:", "Stream:")
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate logger configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating logger middleware configs")
	return nil
}
