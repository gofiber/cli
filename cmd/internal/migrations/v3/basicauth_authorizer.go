package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateBasicauthAuthorizer(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`Authorizer:\s*func\(([^)]*)\)`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, `Authorizer: func($1, _ fiber.Ctx)`)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate basicauth authorizer: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating basicauth authorizer")
	return nil
}

// * removes ContextUsername and ContextPassword fields
// * hashes plaintext Users entries using SHA-256
