package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateSessionStore updates session.New assignments to session.NewStore.
func MigrateSessionStore(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reStore := regexp.MustCompile(`(?m)(:=|=)([\t ]*)session\.New\(`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return reStore.ReplaceAllString(content, `${1}${2}session.NewStore(`)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session store: %w", err)
	}

	cmd.Println("Migrating session store")
	return nil
}
