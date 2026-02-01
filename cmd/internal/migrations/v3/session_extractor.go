package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateSessionExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reConfig := regexp.MustCompile(`session\.Config{`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		updated := IterateConfigBlocks(content, reConfig, func(cfg string) string {
			return replaceKeyLookup(cfg, func(indent, val, comma, comment, newline string) string {
				parts := strings.Split(val, ",")
				var extractors []string
				for _, p := range parts {
					p = strings.TrimSpace(p)
					switch {
					case strings.HasPrefix(p, "cookie:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromCookie(%q)", strings.TrimPrefix(p, "cookie:")))
					case strings.HasPrefix(p, "header:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromHeader(%q)", strings.TrimPrefix(p, "header:")))
					case strings.HasPrefix(p, "query:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromQuery(%q)", strings.TrimPrefix(p, "query:")))
					default:
						return FormatFieldWithComment(indent, "// TODO: migrate KeyLookup", val, "", comment, newline)
					}
				}

				extractor := BuildExtractorChain(extractors)
				if extractor == "" {
					return FormatFieldWithComment(indent, "// TODO: migrate KeyLookup", val, "", comment, newline)
				}

				return FormatFieldWithComment(indent, "Extractor", extractor, comma, comment, newline)
			})
		})

		if updated != content {
			updated = addImport(updated, "github.com/gofiber/fiber/v3/extractors")
		}
		return updated
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session extractor config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating session KeyLookup config")
	return nil
}

// instead of KeyLookup/AuthScheme and removes the deprecated fields.
