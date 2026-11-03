package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateEnvVarConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`\s*ExcludeVars:\s*[^,]+,?\n`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, "")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate EnvVar configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating EnvVar middleware configs")
	return nil
}

// to the new Fiber v3 handler based API. It replaces the old middleware usage
//
//	app.Use(healthcheck.New(...))
//
// with explicit registrations using `app.Get` on the liveness and readiness
// endpoints and adapts the configuration structure.
