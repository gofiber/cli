package v3

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateMount(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(".Mount(", ".Use(")

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Mount usages: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating Mount usages")
	return nil
}
