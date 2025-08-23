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
		matches := reConfig.FindAllStringIndex(content, -1)
		if len(matches) == 0 {
			return content
		}

		var b strings.Builder
		last := 0
		for _, m := range matches {
			if _, err := b.WriteString(content[last:m[0]]); err != nil {
				return content
			}
			start := m[0]
			end := extractBlock(content, m[1], '{', '}')
			cfg := content[start:end]
			cfg = strings.ReplaceAll(cfg, "Expiration:", "IdleTimeout:")
			if _, err := b.WriteString(cfg); err != nil {
				return content
			}
			last = end
		}
		if _, err := b.WriteString(content[last:]); err != nil {
			return content
		}
		return b.String()
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
