package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateMonitorImport(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`github\.com/gofiber/fiber/([^/]+)/middleware/monitor`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, "github.com/gofiber/contrib/monitor")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate monitor import: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating monitor middleware import")
	return nil
}
