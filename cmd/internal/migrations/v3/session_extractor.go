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

func MigrateSessionExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reKeyLookup := regexp.MustCompile(`(\s*)KeyLookup:\s*([^,\n]+)(,?)(\n?)`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return reKeyLookup.ReplaceAllStringFunc(content, func(s string) string {
			sub := reKeyLookup.FindStringSubmatch(s)
			indent := sub[1]
			val := strings.TrimSpace(sub[2])
			comma := sub[3]
			newline := sub[4]

			if uq, err := strconv.Unquote(val); err == nil {
				val = uq
			}

			parts := strings.Split(val, ",")
			var extractors []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				switch {
				case strings.HasPrefix(p, "cookie:"):
					extractors = append(extractors, fmt.Sprintf("session.FromCookie(%q)", strings.TrimPrefix(p, "cookie:")))
				case strings.HasPrefix(p, "header:"):
					extractors = append(extractors, fmt.Sprintf("session.FromHeader(%q)", strings.TrimPrefix(p, "header:")))
				case strings.HasPrefix(p, "query:"):
					extractors = append(extractors, fmt.Sprintf("session.FromQuery(%q)", strings.TrimPrefix(p, "query:")))
				default:
					return ""
				}
			}

			if len(extractors) == 0 {
				return ""
			}

			extractor := extractors[0]
			if len(extractors) > 1 {
				extractor = fmt.Sprintf("session.Chain(%s)", strings.Join(extractors, ", "))
			}

			return fmt.Sprintf("%sExtractor: %s%s%s", indent, extractor, comma, newline)
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session extractor config: %w", err)
	}

	cmd.Println("Migrating session KeyLookup config")
	return nil
}

// instead of KeyLookup/AuthScheme and removes the deprecated fields.
