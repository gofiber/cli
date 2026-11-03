package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateListenerCallbacks(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reErr := regexp.MustCompile(`\s*OnShutdownError:\s*[^,]+,?\n`)
	reSuccess := regexp.MustCompile(`\s*OnShutdownSuccess:\s*[^,]+,?\n`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		content = reErr.ReplaceAllString(content, "")
		content = reSuccess.ReplaceAllString(content, "")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener callbacks: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating listener callbacks")
	return nil
}

// and adapts inline hook functions to accept the error parameter.
