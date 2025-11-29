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
		reCacheControl := regexp.MustCompile(`\bCacheControl:\s*([^,}]+)`)
		invertBoolExpr := func(expr string) string {
			expr = strings.TrimSpace(expr)
			switch expr {
			case "true":
				return "false"
			case "false":
				return "true"
			}

			if strings.HasPrefix(expr, "!") {
				return strings.TrimSpace(strings.TrimPrefix(expr, "!"))
			}

			return "!(" + expr + ")"
		}

		return reConfig.ReplaceAllStringFunc(content, func(s string) string {
			s = strings.ReplaceAll(s, "Store:", "Storage:")
			s = strings.ReplaceAll(s, "Key:", "KeyGenerator:")
			s = reCacheControl.ReplaceAllStringFunc(s, func(match string) string {
				value := strings.TrimSpace(strings.TrimPrefix(match, "CacheControl:"))
				return "DisableCacheControl: " + invertBoolExpr(value)
			})
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
