package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateUtilsImport(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reImport := regexp.MustCompile(`(?m)^(\s*(?:\w+\s+)?)"github\.com/gofiber/fiber/v\d+/utils"$`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		// update import path
		content = reImport.ReplaceAllString(content, `$1"github.com/gofiber/utils/v2"`)

		// protect byte helper replacements so we can safely map string helpers to the
		// standard library without affecting byte-based calls
		content = strings.ReplaceAll(content, "utils.TrimRightBytes", "utils.__fiberTrimRight__")
		content = strings.ReplaceAll(content, "utils.TrimLeftBytes", "utils.__fiberTrimLeft__")
		content = strings.ReplaceAll(content, "utils.TrimBytes", "utils.__fiberTrim__")
		content = strings.ReplaceAll(content, "utils.EqualFoldBytes", "utils.__fiberEqualFold__")

		// map removed string helpers to the strings package
		content = strings.ReplaceAll(content, "utils.TrimRight", "strings.TrimRight")
		content = strings.ReplaceAll(content, "utils.TrimLeft", "strings.TrimLeft")
		content = strings.ReplaceAll(content, "utils.Trim", "strings.Trim")
		content = strings.ReplaceAll(content, "utils.EqualFold", "strings.EqualFold")

		// restore byte helper placeholders to their new names
		content = strings.ReplaceAll(content, "utils.__fiberTrimRight__", "utils.TrimRight")
		content = strings.ReplaceAll(content, "utils.__fiberTrimLeft__", "utils.TrimLeft")
		content = strings.ReplaceAll(content, "utils.__fiberTrim__", "utils.Trim")
		content = strings.ReplaceAll(content, "utils.__fiberEqualFold__", "utils.EqualFold")

		// other removed helpers
		content = strings.ReplaceAll(content, "utils.GetString", "utils.ToString")
		content = strings.ReplaceAll(content, "utils.GetBytes", "utils.CopyBytes")
		content = strings.ReplaceAll(content, "utils.ImmutableString", "string")

		if strings.Contains(content, "utils.AssertEqual") {
			content = strings.ReplaceAll(content, "utils.AssertEqual", "assert.Equal")
			content = addImport(content, "github.com/stretchr/testify/assert")
		}

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate utils imports: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating utils imports")
	return nil
}
