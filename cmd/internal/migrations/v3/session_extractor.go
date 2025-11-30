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
			cfg = replaceKeyLookup(cfg, func(indent, val, comma, comment, newline string) string {
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
						if comment != "" {
							comment = " " + comment
						}
						return fmt.Sprintf("%s// TODO: migrate KeyLookup: %s%s%s", indent, val, comment, newline)
					}
				}

				if len(extractors) == 0 {
					if comment != "" {
						comment = " " + comment
					}
					return fmt.Sprintf("%s// TODO: migrate KeyLookup: %s%s%s", indent, val, comment, newline)
				}

				extractor := extractors[0]
				if len(extractors) > 1 {
					extractor = fmt.Sprintf("extractors.Chain(%s)", strings.Join(extractors, ", "))
				}

				if comment != "" {
					comment = " " + comment
				}
				return fmt.Sprintf("%sExtractor: %s%s%s%s", indent, extractor, comma, comment, newline)
			})
			if _, err := b.WriteString(cfg); err != nil {
				return content
			}
			last = end
		}
		if _, err := b.WriteString(content[last:]); err != nil {
			return content
		}

		updated := b.String()
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
