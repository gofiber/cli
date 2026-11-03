package v3

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateHandlerSignatures(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	sigReplacer := strings.NewReplacer("*fiber.Ctx", "fiber.Ctx")

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return sigReplacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate handler signatures: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating handler signatures")

	return nil
}
