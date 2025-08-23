package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateViewBind(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reViewBind := regexp.MustCompile(`(\w+)\.Bind\(([^)]+)\)`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		orig := content
		return reViewBind.ReplaceAllStringFunc(content, func(match string) string {
			parts := reViewBind.FindStringSubmatch(match)
			if len(parts) != 3 {
				return match
			}
			ident := parts[1]
			args := parts[2]
			if isFiberCtx(orig, ident) {
				return ident + ".ViewBind(" + args + ")"
			}
			return match
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate ViewBind calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating view binding helpers")
	return nil
}
