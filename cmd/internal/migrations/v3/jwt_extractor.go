package v3

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateJWTExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reConfig := regexp.MustCompile(`jwtware\.Config{(?:[^{}]|{[^{}]*})*}`)
	reAuthScheme := regexp.MustCompile(`(?m)^\s*AuthScheme:\s*([^,\n]+)`)
	reAuthLine := regexp.MustCompile(`(?m)^\s*AuthScheme:\s*[^\n]+\n?`)
	reFilter := regexp.MustCompile(`(?m)^(\s*)Filter:\s*`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		updated := reConfig.ReplaceAllStringFunc(content, func(cfg string) string {
			schemeArg := "\"Bearer\""
			if am := reAuthScheme.FindStringSubmatch(cfg); len(am) > 1 {
				raw := strings.TrimSpace(am[1])
				if uq, err := strconv.Unquote(raw); err == nil {
					schemeArg = fmt.Sprintf("%q", uq)
				} else {
					schemeArg = raw
				}
			}

			cfg = replaceStringField(cfg, "TokenLookup", func(indent, val, comma, comment, newline string) string {
				parts := strings.Split(val, ",")
				var extractors []string
				for _, p := range parts {
					p = strings.TrimSpace(p)
					switch {
					case strings.HasPrefix(p, "header:"):
						header := strings.TrimPrefix(p, "header:")
						if strings.EqualFold(header, "Authorization") {
							extractors = append(extractors, fmt.Sprintf("extractors.FromAuthHeader(%s)", schemeArg))
						} else {
							extractors = append(extractors, fmt.Sprintf("extractors.FromHeader(%q)", header))
						}
					case strings.HasPrefix(p, "query:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromQuery(%q)", strings.TrimPrefix(p, "query:")))
					case strings.HasPrefix(p, "param:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromParam(%q)", strings.TrimPrefix(p, "param:")))
					case strings.HasPrefix(p, "cookie:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromCookie(%q)", strings.TrimPrefix(p, "cookie:")))
					case strings.HasPrefix(p, "form:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromForm(%q)", strings.TrimPrefix(p, "form:")))
					default:
						if comment != "" {
							comment = " " + comment
						}
						return fmt.Sprintf("%s// TODO: migrate TokenLookup: %s%s%s", indent, val, comment, newline)
					}
				}

				extractor := ""
				switch len(extractors) {
				case 1:
					extractor = extractors[0]
				case 0:
				default:
					extractor = fmt.Sprintf("extractors.Chain(%s)", strings.Join(extractors, ", "))
				}

				if extractor == "" {
					if comment != "" {
						comment = " " + comment
					}
					return fmt.Sprintf("%s// TODO: migrate TokenLookup: %s%s%s", indent, val, comment, newline)
				}

				if comment != "" {
					comment = " " + comment
				}
				return fmt.Sprintf("%sExtractor: %s%s%s%s", indent, extractor, comma, comment, newline)
			})

			cfg = reAuthLine.ReplaceAllString(cfg, "")
			cfg = reFilter.ReplaceAllString(cfg, "${1}Next: ")
			return cfg
		})

		if updated != content && strings.Contains(updated, "extractors.") {
			updated = addImport(updated, "github.com/gofiber/fiber/v3/extractors")
		}
		return updated
	})
	if err != nil {
		return fmt.Errorf("failed to migrate jwt extractor config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating jwt middleware configs")
	return nil
}
