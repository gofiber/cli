package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateMiddlewareLocals(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
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

	reImport := regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:([\w\.]+)\s+)?"github\.com/gofiber/fiber/(?:v2|v3)/middleware/([\w]+)"`)

	// first pass: collect context key mappings across all files
	_, err := internal.ChangeFileContent(cwd, func(content string) string {
		imports := map[string]string{}
		for _, m := range reImport.FindAllStringSubmatch(content, -1) {
			alias := m[1]
			pkg := m[2]
			if alias == "" {
				alias = pkg
			}
			imports[alias] = pkg
		}

		for alias, pkg := range imports {
			for _, e := range extractors {
				if e.pkg != pkg {
					continue
				}
				re := regexp.MustCompile(alias + `\.Config{[^}]*` + e.field + `:\s*"([^"]+)"`)
				matches := re.FindAllStringSubmatch(content, -1)
				for _, m := range matches {
					ctxMap[m[1]] = e.replFmt
				}
			}
		}

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to gather middleware locals: %w", err)
	}

	// second pass: perform replacements and clean up
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		imports := map[string]string{}
		for _, m := range reImport.FindAllStringSubmatch(content, -1) {
			alias := m[1]
			pkg := m[2]
			if alias == "" {
				alias = pkg
			}
			imports[alias] = pkg
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

		for alias := range imports {
			reCfg := regexp.MustCompile(alias + `\.Config{[^}]*}`)
			content = reCfg.ReplaceAllStringFunc(content, func(cfg string) string {
				cfg = removeConfigField(cfg, "ContextKey")
				cfg = removeConfigField(cfg, "ContextUsername")
				cfg = removeConfigField(cfg, "ContextPassword")
				return cfg
			})
		}

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
