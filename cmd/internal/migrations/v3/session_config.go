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
	err := internal.ChangeFileContent(cwd, func(content string) string {
		reConfig := regexp.MustCompile(`session\.Config{[^}]*}`)
		return reConfig.ReplaceAllStringFunc(content, func(s string) string {
			return strings.ReplaceAll(s, "Expiration:", "IdleTimeout:")
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session configs: %w", err)
	}

	cmd.Println("Migrating session middleware configs")
	return nil
}
