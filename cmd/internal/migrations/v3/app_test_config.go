package v3

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateAppTestConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return replaceCall(content, ".Test", func(call string, args []string) string {
			if len(args) != 2 {
				return call
			}
			arg := strings.TrimSpace(args[1])
			if strings.HasPrefix(arg, "fiber.TestConfig") {
				return call
			}
			if arg == "-1" {
				return fmt.Sprintf(".Test(%s, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})", args[0])
			}
			return fmt.Sprintf(".Test(%s, fiber.TestConfig{Timeout: %s})", args[0], arg)
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate app.Test calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating app.Test usages")
	return nil
}
