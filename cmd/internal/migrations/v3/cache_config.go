package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateCacheConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		reConfig := regexp.MustCompile(`cache\.Config{[^}]*}`)
		return reConfig.ReplaceAllStringFunc(content, func(s string) string {
			s = strings.ReplaceAll(s, "Store:", "Storage:")
			s = strings.ReplaceAll(s, "Key:", "KeyGenerator:")
			return s
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate cache configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating cache middleware configs")
	return nil
}
