package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateContextMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		// UserContext() removed - Ctx implements context.Context
		reUserCtx := regexp.MustCompile(`(\w+)\.UserContext\(\)`)
		content = reUserCtx.ReplaceAllString(content, `$1`)

		// SetUserContext removed - comment out the call
		reSetUserCtx := regexp.MustCompile(`(?m)^(\s*)(.*\.SetUserContext\([^\n]*\).*)$`)
		content = reSetUserCtx.ReplaceAllString(content, `$1// TODO: SetUserContext was removed, please migrate manually: $2`)

		// old Context() returned fasthttp.RequestCtx
		reReqCtx := regexp.MustCompile(`(\w+)\.Context\(\)`)
		content = reReqCtx.ReplaceAllString(content, `$1.RequestCtx()`)

		// remaining Context() usages return context.Context -> remove call
		content = strings.ReplaceAll(content, ".Context()", "")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate context methods: %w", err)
	}

	cmd.Println("Migrating context methods")
	return nil
}
