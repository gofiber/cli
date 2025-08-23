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
	reSession := regexp.MustCompile(`(?m)\s*SessionKey:\s*[^,]+,?\n`)
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
			cfg = reSession.ReplaceAllString(cfg, "")

			cfg = replaceKeyLookup(cfg, func(indent, val, comma, comment, newline string) string {
				var extractor string
				switch {
				case strings.HasPrefix(val, "header:"):
					extractor = fmt.Sprintf("Extractor: csrf.FromHeader(%q)", strings.TrimPrefix(val, "header:"))
				case strings.HasPrefix(val, "form:"):
					extractor = fmt.Sprintf("Extractor: csrf.FromForm(%q)", strings.TrimPrefix(val, "form:"))
				case strings.HasPrefix(val, "query:"):
					extractor = fmt.Sprintf("Extractor: csrf.FromQuery(%q)", strings.TrimPrefix(val, "query:"))
				default:
					if comment != "" {
						comment = " " + comment
					}
					return fmt.Sprintf("%s// TODO: migrate KeyLookup: %s%s%s", indent, val, comment, newline)
				}

				if comment != "" {
					comment = " " + comment
				}
				return fmt.Sprintf("%s%s%s%s%s", indent, extractor, comma, comment, newline)
			})

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
		return fmt.Errorf("failed to migrate CSRF configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating CSRF middleware configs")
	return nil
}
