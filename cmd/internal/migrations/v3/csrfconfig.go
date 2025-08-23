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

func MigrateCSRFConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reConfig := regexp.MustCompile(`csrf\.Config{[^}]*}`)
	reSession := regexp.MustCompile(`\s*SessionKey:\s*[^,]+,?\n`)
	reKeyLookup := regexp.MustCompile(`(\s*)KeyLookup:\s*([^,\n]+)(,?)(\n?)`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		content = reConfig.ReplaceAllStringFunc(content, func(s string) string {
			return strings.ReplaceAll(s, "Expiration:", "IdleTimeout:")
		})
		content = reSession.ReplaceAllString(content, "")

		content = reKeyLookup.ReplaceAllStringFunc(content, func(s string) string {
			sub := reKeyLookup.FindStringSubmatch(s)
			indent := sub[1]
			val := strings.TrimSpace(sub[2])
			comma := sub[3]
			newline := sub[4]

			if uq, err := strconv.Unquote(val); err == nil {
				val = uq
			}

			var extractor string
			switch {
			case strings.HasPrefix(val, "header:"):
				extractor = fmt.Sprintf("Extractor: csrf.FromHeader(%q)", strings.TrimPrefix(val, "header:"))
			case strings.HasPrefix(val, "form:"):
				extractor = fmt.Sprintf("Extractor: csrf.FromForm(%q)", strings.TrimPrefix(val, "form:"))
			case strings.HasPrefix(val, "query:"):
				extractor = fmt.Sprintf("Extractor: csrf.FromQuery(%q)", strings.TrimPrefix(val, "query:"))
			default:
				// Unsupported or insecure value (e.g. cookie) - remove
				return ""
			}

			return fmt.Sprintf("%s%s%s%s", indent, extractor, comma, newline)
		})

		return content
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
