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
		ctxMap := map[string]string{
			"requestid":    "requestid.FromContext(%s)",
			"csrf":         "csrf.TokenFromContext(%s)",
			"csrf_handler": "csrf.HandlerFromContext(%s)",
			"session":      "session.FromContext(%s)",
			"username":     "basicauth.UsernameFromContext(%s)",
			"password":     "basicauth.PasswordFromContext(%s)",
			"token":        "keyauth.TokenFromContext(%s)",
		}

		extractors := []struct {
			pkg     string
			field   string
			replFmt string
		}{
			{"csrf", "ContextKey", "csrf.TokenFromContext(%s)"},
			{"keyauth", "ContextKey", "keyauth.TokenFromContext(%s)"},
			{"session", "ContextKey", "session.FromContext(%s)"},
			{"basicauth", "ContextUsername", "basicauth.UsernameFromContext(%s)"},
			{"basicauth", "ContextPassword", "basicauth.PasswordFromContext(%s)"},
		}

		for _, e := range extractors {
			re := regexp.MustCompile(e.pkg + `\.Config{[^}]*` + e.field + `:\s*"([^"]+)"`)
			matches := re.FindAllStringSubmatch(content, -1)
			for _, m := range matches {
				ctxMap[m[1]] = e.replFmt
			}
		}

		reLocals := regexp.MustCompile(`(\w+)\.Locals\("([^"]+)"\)`)
		content = reLocals.ReplaceAllStringFunc(content, func(s string) string {
			sub := reLocals.FindStringSubmatch(s)
			ctx := sub[1]
			key := sub[2]
			if fmtStr, ok := ctxMap[key]; ok {
				return fmt.Sprintf(fmtStr, ctx)
			}
			return s
		})

		reTypeAssert := regexp.MustCompile(`([\w\.]+FromContext\([^\)]+\))\.\([^\)]+\)`)
		content = reTypeAssert.ReplaceAllString(content, "$1")

		reComma := regexp.MustCompile(`(\w+)\s*,\s*(\w+)\s*:=\s*([\w\.]+FromContext\([^\)]+\))`)
		content = reComma.ReplaceAllString(content, "$1, $2 := $3, true")

		reCtxKey := regexp.MustCompile(`(?m)\s*Context(?:Username|Password|Key):\s*[^,\n}]+,?\s*(//[^\n]*)?\n`)
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
