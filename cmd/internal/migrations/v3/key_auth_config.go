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
	reConfig := regexp.MustCompile(`keyauth\.Config{[^}]*}`)
	reKeyLookup := regexp.MustCompile(`(?m)(\s*)KeyLookup:\s*("[^"]+")(,?)(\n?)`)
	reAuthScheme := regexp.MustCompile(`(?m)\s*AuthScheme:\s*([^,\n]+)`)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return reConfig.ReplaceAllStringFunc(content, func(cfg string) string {
			keyMatch := reKeyLookup.FindStringSubmatch(cfg)
			if len(keyMatch) < 5 {
				// remove AuthScheme if present
				return removeConfigField(cfg, "AuthScheme")
			}

			indent := keyMatch[1]
			val := strings.TrimSpace(keyMatch[2])
			comma := keyMatch[3]
			newline := keyMatch[4]

			if uq, err := strconv.Unquote(val); err == nil {
				val = uq
			}

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
						extractors = append(extractors, fmt.Sprintf("keyauth.FromAuthHeader(%q, %q)", header, scheme))
					} else {
						extractors = append(extractors, fmt.Sprintf("keyauth.FromHeader(%q)", header))
					}
				case strings.HasPrefix(p, "query:"):
					extractors = append(extractors, fmt.Sprintf("keyauth.FromQuery(%q)", strings.TrimPrefix(p, "query:")))
				case strings.HasPrefix(p, "param:"):
					extractors = append(extractors, fmt.Sprintf("keyauth.FromParam(%q)", strings.TrimPrefix(p, "param:")))
				case strings.HasPrefix(p, "form:"):
					extractors = append(extractors, fmt.Sprintf("keyauth.FromForm(%q)", strings.TrimPrefix(p, "form:")))
				case strings.HasPrefix(p, "cookie:"):
					extractors = append(extractors, fmt.Sprintf("keyauth.FromCookie(%q)", strings.TrimPrefix(p, "cookie:")))
				default:
					// unrecognized source; remove field and return
					cfg = removeConfigField(cfg, "AuthScheme")
					return removeConfigField(cfg, "KeyLookup")
				}
			}

			extractor := ""
			if len(extractors) == 1 {
				extractor = extractors[0]
			} else if len(extractors) > 1 {
				extractor = fmt.Sprintf("keyauth.Chain(%s)", strings.Join(extractors, ", "))
			}

			cfg = removeConfigField(cfg, "AuthScheme")
			if extractor == "" {
				return removeConfigField(cfg, "KeyLookup")
			}

			newField := fmt.Sprintf("%sExtractor: %s%s%s", indent, extractor, comma, newline)
			cfg = reKeyLookup.ReplaceAllString(cfg, newField)
			return cfg
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate keyauth configs: %w", err)
	}

	cmd.Println("Migrating keyauth middleware configs")
	return nil
}
