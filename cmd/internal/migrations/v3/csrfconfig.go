package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateCSRFConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reConfig := regexp.MustCompile(`csrf\.Config{`)
	reSession := regexp.MustCompile(`(?m)\s*SessionKey:\s*[^,\n]+,?\s*(//[^\n]*)?\n`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		updated := IterateConfigBlocks(content, reConfig, func(cfg string) string {
			cfg = strings.ReplaceAll(cfg, "Expiration:", "IdleTimeout:")
			cfg = reSession.ReplaceAllString(cfg, "")

			return replaceKeyLookup(cfg, func(indent, val, comma, comment, newline string) string {
				var extractor string
				switch {
				case strings.HasPrefix(val, "header:"):
					extractor = fmt.Sprintf("Extractor: extractors.FromHeader(%q)", strings.TrimPrefix(val, "header:"))
				case strings.HasPrefix(val, "form:"):
					extractor = fmt.Sprintf("Extractor: extractors.FromForm(%q)", strings.TrimPrefix(val, "form:"))
				case strings.HasPrefix(val, "query:"):
					extractor = fmt.Sprintf("Extractor: extractors.FromQuery(%q)", strings.TrimPrefix(val, "query:"))
				default:
					return FormatFieldWithComment(indent, "// TODO: migrate KeyLookup", val, "", comment, newline)
				}

				return FormatFieldWithComment(indent, extractor, "", comma, comment, newline)
			})
		})

		if updated != content {
			updated = addImport(updated, "github.com/gofiber/fiber/v3/extractors")
		}
		return updated
	})
	if err != nil {
		return fmt.Errorf("failed to migrate CSRF configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating CSRF middleware configs")
	return nil
}
