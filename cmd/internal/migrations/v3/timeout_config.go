package v3

import (
	"fmt"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateTimeoutConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replaceCall(content, "timeout.New", func(call string, args []string) string {
			if len(args) != 2 {
				return call
			}
			return fmt.Sprintf("timeout.New(%s, timeout.Config{Timeout: %s})", args[0], args[1])
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate timeout middleware configs: %w", err)
	}

	cmd.Println("Migrating timeout middleware configs")
	return nil
}
