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
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		orig := content

		// UserContext() removed - Ctx implements context.Context
		reUserCtx := regexp.MustCompile(`(\w+)\.UserContext\(\)`)
		content = reUserCtx.ReplaceAllStringFunc(content, func(match string) string {
			parts := reUserCtx.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			ident := parts[1]
			if isFiberCtx(orig, ident) {
				return ident
			}
			return match
		})

		// SetUserContext removed - comment out the call
		reSetUserCtx := regexp.MustCompile(`(?m)^(\s*)(.*?\b(\w+)\.SetUserContext\([^\n]*\).*)$`)
		content = reSetUserCtx.ReplaceAllStringFunc(content, func(line string) string {
			if strings.Contains(line, "TODO: SetUserContext was removed") {
				return line
			}
			parts := reSetUserCtx.FindStringSubmatch(line)
			if len(parts) != 4 {
				return line
			}
			ident := parts[3]
			if !isFiberCtx(orig, ident) {
				return line
			}
			return fmt.Sprintf("%s// TODO: SetUserContext was removed, please migrate manually: %s", parts[1], parts[2])
		})

		// old Context() returned fasthttp.RequestCtx
		reReqCtx := regexp.MustCompile(`(\w+)\.Context\(\)`)
		content = reReqCtx.ReplaceAllStringFunc(content, func(match string) string {
			parts := reReqCtx.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			ident := parts[1]
			if isFiberCtx(orig, ident) {
				return ident + ".RequestCtx()"
			}
			return match
		})

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate context methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating context methods")
	return nil
}
