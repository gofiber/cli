package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateRedirectMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(
		".RedirectBack(", ".Redirect().Back(",
		".RedirectToRoute(", ".Redirect().Route(",
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Redirect\([^)]`)
		content = re.ReplaceAllStringFunc(content, func(s string) string {
			return strings.Replace(s, ".Redirect(", ".Redirect().To(", 1)
		})
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate redirect methods: %w", err)
	}

	cmd.Println("Migrating redirect methods")
	return nil
}
