package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateBasicauthStorePassword(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`(\s*)StorePassword:\s*([^,\n]+)(,?)`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, `$1// TODO: StorePassword removed ($2)$3`)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate basicauth StorePassword: %w", err)
	}

	cmd.Println("Migrating basicauth StorePassword option")
	return nil
}
