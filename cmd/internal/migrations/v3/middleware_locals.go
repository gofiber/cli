package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateMiddlewareLocals(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		replacements := []struct {
			re   *regexp.Regexp
			repl string
		}{
			{regexp.MustCompile(`(\w+)\.Locals\("requestid"\)`), `requestid.FromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("csrf"\)`), `csrf.TokenFromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("csrf_handler"\)`), `csrf.HandlerFromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("session"\)`), `session.FromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("username"\)`), `basicauth.UsernameFromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("password"\)`), `basicauth.PasswordFromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("token"\)`), `keyauth.TokenFromContext($1)`},
		}
		for _, r := range replacements {
			content = r.re.ReplaceAllString(content, r.repl)
		}

		reTypeAssert := regexp.MustCompile(`([\w\.]+FromContext\([^\)]+\))\.\([^\)]+\)`)
		content = reTypeAssert.ReplaceAllString(content, "$1")

		reComma := regexp.MustCompile(`(\w+)\s*,\s*(\w+)\s*:=\s*([\w\.]+FromContext\([^\)]+\))`)
		content = reComma.ReplaceAllString(content, "$1, $2 := $3, true")

		reCtxKey := regexp.MustCompile(`\s*ContextKey:\s*[^,}\n]+,?`)
		content = reCtxKey.ReplaceAllString(content, "")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate middleware locals: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating middleware locals")
	return nil
}
