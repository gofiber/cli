package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	reLoggerImport      = regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:([\w.]+)\s+)?"github\.com/gofiber/fiber/(?:v2|v3)/middleware/logger"`)
	reLoggerOutputField = regexp.MustCompile(`((?:^|[{\n])\s*)Output(\s*:)`)
)

func MigrateLoggerConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		aliases := collectAliases(content, reLoggerImport, []string{"logger"})
		if len(aliases) == 0 {
			return content
		}

		for _, alias := range aliases {
			reConfig := regexp.MustCompile(regexp.QuoteMeta(alias) + `\.Config\s*\{`)
			content = IterateConfigBlocks(content, reConfig, func(s string) string {
				return reLoggerOutputField.ReplaceAllString(s, "${1}Stream${2}")
			})
		}

		return content
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
