package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateSessionConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reConfig := regexp.MustCompile(`session\.Config{`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return IterateConfigBlocks(content, reConfig, func(cfg string) string {
			return strings.ReplaceAll(cfg, "Expiration:", "IdleTimeout:")
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating session middleware configs")
	return nil
}
