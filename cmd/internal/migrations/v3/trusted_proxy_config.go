package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateTrustedProxyConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reEnable := regexp.MustCompile(`EnableTrustedProxyCheck`)
	reProxies := regexp.MustCompile(`TrustedProxies:\s*([^,\n]+),`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		content = reEnable.ReplaceAllString(content, "TrustProxy")
		content = reProxies.ReplaceAllString(content, "TrustProxyConfig: fiber.TrustProxyConfig{Proxies: $1},")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate trusted proxy config: %w", err)
	}

	cmd.Println("Migrating trusted proxy config")
	return nil
}

// in Fiber v3. It renames Prefork and Network fields and adapts them to the new
// listener configuration fields.
