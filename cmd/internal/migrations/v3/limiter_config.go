package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateLimiterConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		reConfig := regexp.MustCompile(`limiter\.Config{[^}]*}`)
		return reConfig.ReplaceAllStringFunc(content, func(s string) string {
			s = strings.ReplaceAll(s, "Duration:", "Expiration:")
			s = strings.ReplaceAll(s, "Store:", "Storage:")
			s = strings.ReplaceAll(s, "Key:", "KeyGenerator:")
			return s
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate limiter configs: %w", err)
	}

	cmd.Println("Migrating limiter middleware configs")
	return nil
}
