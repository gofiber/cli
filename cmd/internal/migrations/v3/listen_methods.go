package v3

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateListenMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(
		".ListenTLSWithCertificate(", ".Listen(",
		".ListenTLS(", ".Listen(",
		".ListenMutualTLSWithCertificate(", ".Listen(",
		".ListenMutualTLS(", ".Listen(",
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listen methods: %w", err)
	}

	cmd.Println("Migrating listen methods")
	return nil
}
