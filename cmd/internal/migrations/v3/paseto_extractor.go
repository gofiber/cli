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

func MigratePasetoExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reImport := regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:([\w\.]+)\s+)?"github\.com/gofiber/contrib/paseto(?:/v\d+)?"`)
	reTokenPrefix := regexp.MustCompile(`(?m)\s*TokenPrefix:\s*([^,\n]+)`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		aliases := collectAliases(content, reImport, []string{"pasetoware", "paseto"})
		if len(aliases) == 0 {
			return content
		}

		updated := content
		for _, alias := range aliases {
			reConfig := regexp.MustCompile(regexp.QuoteMeta(alias) + `\.Config{(?:[^{}]|{[^{}]*})*}`)
			updated = reConfig.ReplaceAllStringFunc(updated, func(cfg string) string {
				schemeArg := ""
				hasPrefix := false
				if am := reTokenPrefix.FindStringSubmatch(cfg); len(am) > 1 {
					hasPrefix = true
					raw := strings.TrimSpace(am[1])
					if uq, err := strconv.Unquote(raw); err == nil {
						schemeArg = fmt.Sprintf("%q", uq)
					} else {
						schemeArg = raw
					}
				}

				cfg = replaceField(cfg, "TokenLookup", func(indent, val, comma, comment, newline string) string {
					lookup := strings.TrimSpace(val)
					lookup = strings.TrimPrefix(lookup, "[2]string")
					lookup = strings.TrimSpace(strings.TrimPrefix(lookup, "{"))
					lookup = strings.TrimSuffix(lookup, "}")
					parts := splitArgs(lookup)
					if len(parts) < 2 {
						return FormatFieldWithComment(indent, "// TODO: migrate TokenLookup", val, "", comment, newline)
					}

					source := strings.TrimSpace(parts[0])
					key := strings.TrimSpace(parts[1])

					sourceLower := strings.ToLower(source)
					if uq, err := strconv.Unquote(sourceLower); err == nil {
						sourceLower = strings.ToLower(uq)
					}
					switch {
					case strings.Contains(sourceLower, "lookupheader"):
						sourceLower = "header"
					case strings.Contains(sourceLower, "lookupquery"):
						sourceLower = "query"
					case strings.Contains(sourceLower, "lookupparam"):
						sourceLower = "param"
					case strings.Contains(sourceLower, "lookupcookie"):
						sourceLower = "cookie"
					case strings.Contains(sourceLower, "lookupform"):
						sourceLower = "form"
					default:
						// preserve original value for unsupported lookups
					}

					keyArg := key
					if uq, err := strconv.Unquote(keyArg); err == nil {
						keyArg = fmt.Sprintf("%q", uq)
					}

					var extractor string
					switch sourceLower {
					case "header":
						if strings.EqualFold(strings.Trim(keyArg, "\""), "Authorization") && hasPrefix {
							extractor = fmt.Sprintf("extractors.FromAuthHeader(%s)", schemeArg)
						} else {
							extractor = fmt.Sprintf("extractors.FromHeader(%s)", keyArg)
						}
					case "query":
						extractor = fmt.Sprintf("extractors.FromQuery(%s)", keyArg)
					case "param":
						extractor = fmt.Sprintf("extractors.FromParam(%s)", keyArg)
					case "cookie":
						extractor = fmt.Sprintf("extractors.FromCookie(%s)", keyArg)
					case "form":
						extractor = fmt.Sprintf("extractors.FromForm(%s)", keyArg)
					default:
						// leave extractor empty to emit TODO comment below
					}

					if extractor == "" {
						return FormatFieldWithComment(indent, "// TODO: migrate TokenLookup", val, "", comment, newline)
					}

					return FormatFieldWithComment(indent, "Extractor", extractor, comma, comment, newline)
				})

				cfg = removeConfigField(cfg, "TokenPrefix")
				return cfg
			})
		}

		if updated != content && strings.Contains(updated, "extractors.") {
			updated = addImport(updated, "github.com/gofiber/fiber/v3/extractors")
		}
		return updated
	})
	if err != nil {
		return fmt.Errorf("failed to migrate paseto extractor config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating paseto middleware configs")
	return nil
}
