package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateCORSConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reOrigins := regexp.MustCompile(`AllowOrigins:\s*"([^"]*)"`)
	reMethods := regexp.MustCompile(`AllowMethods:\s*"([^"]*)"`)
	reHeaders := regexp.MustCompile(`AllowHeaders:\s*"([^"]*)"`)
	reExpose := regexp.MustCompile(`ExposeHeaders:\s*"([^"]*)"`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		conv := func(src string, re *regexp.Regexp, field string) string {
			return re.ReplaceAllStringFunc(src, func(s string) string {
				matches := re.FindStringSubmatch(s)
				if len(matches) < 2 {
					return s
				}
				parts := strings.Split(matches[1], ",")
				for i, p := range parts {
					parts[i] = fmt.Sprintf("%q", strings.TrimSpace(p))
				}
				return fmt.Sprintf("%s: []string{%s}", field, strings.Join(parts, ", "))
			})
		}

		content = conv(content, reOrigins, "AllowOrigins")
		content = conv(content, reMethods, "AllowMethods")
		content = conv(content, reHeaders, "AllowHeaders")
		content = conv(content, reExpose, "ExposeHeaders")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate CORS configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating CORS middleware configs")
	return nil
}
