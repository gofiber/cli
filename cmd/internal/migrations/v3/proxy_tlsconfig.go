package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateProxyTLSConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`proxy\.WithTlsConfig\(([^)]+)\)`)
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content,
			"proxy.WithClient(&fasthttp.Client{TLSConfig: $1})")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate proxy TLS config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating proxy TLS config")
	return nil
}
