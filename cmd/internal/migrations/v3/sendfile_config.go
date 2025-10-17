package v3

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateSendFileConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return replaceCall(content, ".SendFile", func(call string, args []string) string {
			if len(args) != 2 {
				return call
			}

			arg := strings.TrimSpace(args[1])
			if arg == "" {
				return call
			}
			if strings.HasPrefix(arg, "fiber.SendFile") || strings.HasPrefix(arg, "&fiber.SendFile") {
				return call
			}
			if strings.HasPrefix(arg, "SendFile{") || strings.HasPrefix(arg, "&SendFile{") {
				return call
			}

			return fmt.Sprintf(".SendFile(%s, fiber.SendFile{Compress: %s})", args[0], args[1])
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate SendFile calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating SendFile compress flag")
	return nil
}
