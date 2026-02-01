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

func MigrateKeyAuthConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reConfig := regexp.MustCompile(`keyauth\.Config{(?:[^{}]|{[^{}]*})*}`)
	reAuthScheme := regexp.MustCompile(`(?m)\s*AuthScheme:\s*([^,\n]+)`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		updated := reConfig.ReplaceAllStringFunc(content, func(cfg string) string {
			cfg = replaceKeyLookup(cfg, func(indent, val, comma, comment, newline string) string {
				scheme := "Bearer"
				if am := reAuthScheme.FindStringSubmatch(cfg); len(am) > 1 {
					scheme = strings.TrimSpace(am[1])
					if uq, err := strconv.Unquote(scheme); err == nil {
						scheme = uq
					}
				}

				parts := strings.Split(val, ",")
				var extractors []string
				for _, p := range parts {
					p = strings.TrimSpace(p)
					switch {
					case strings.HasPrefix(p, "header:"):
						header := strings.TrimPrefix(p, "header:")
						if strings.EqualFold(header, "Authorization") {
							extractors = append(extractors, fmt.Sprintf("extractors.FromAuthHeader(%q)", scheme))
						} else {
							extractors = append(extractors, fmt.Sprintf("extractors.FromHeader(%q)", header))
						}
					case strings.HasPrefix(p, "query:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromQuery(%q)", strings.TrimPrefix(p, "query:")))
					case strings.HasPrefix(p, "param:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromParam(%q)", strings.TrimPrefix(p, "param:")))
					case strings.HasPrefix(p, "form:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromForm(%q)", strings.TrimPrefix(p, "form:")))
					case strings.HasPrefix(p, "cookie:"):
						extractors = append(extractors, fmt.Sprintf("extractors.FromCookie(%q)", strings.TrimPrefix(p, "cookie:")))
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

			cfg = removeConfigField(cfg, "AuthScheme")
			return cfg
		})

		if updated != content {
			updated = addImport(updated, "github.com/gofiber/fiber/v3/extractors")
		}
		return updated
	})
	if err != nil {
		return fmt.Errorf("failed to migrate keyauth configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating keyauth middleware configs")
	return nil
}
