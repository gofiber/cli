package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

type ctxRepl struct {
	pkg       string
	replFmt   string
	isDefault bool
}

func parseMiddlewareImports(content string, reImport *regexp.Regexp) map[string]string {
	imports := map[string]string{}
	for _, m := range reImport.FindAllStringSubmatch(content, -1) {
		alias := m[1]
		pkg := m[2]
		if alias == "" {
			alias = pkg
		}
		imports[alias] = pkg
	}
	return imports
}

func MigrateMiddlewareLocals(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ctxMap := map[string][]ctxRepl{}

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
		imports := parseMiddlewareImports(content, reImport)

		for alias, pkg := range imports {
			for _, e := range extractors {
				if e.pkg != pkg {
					continue
				}
				reCfg := regexp.MustCompile(regexp.QuoteMeta(alias) + `\.Config{`)
				matches := reCfg.FindAllStringIndex(content, -1)
				for _, m := range matches {
					start := m[0]
					end := extractBlock(content, m[1], '{', '}')
					cfg := content[start:end]
					reField := regexp.MustCompile(e.field + `:\s*"([^"]+)"`)
					for _, fm := range reField.FindAllStringSubmatch(cfg, -1) {
						ctxMap[fm[1]] = append(ctxMap[fm[1]], ctxRepl{pkg: e.pkg, replFmt: e.replFmt})
					}
				}
			}
		}

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to gather middleware locals: %w", err)
	}

	defaults := map[string][]ctxRepl{
		"requestid":    {{pkg: "requestid", replFmt: "requestid.FromContext(%s)", isDefault: true}},
		"csrf":         {{pkg: "csrf", replFmt: "csrf.TokenFromContext(%s)", isDefault: true}},
		"csrf_handler": {{pkg: "csrf", replFmt: "csrf.HandlerFromContext(%s)", isDefault: true}},
		"session":      {{pkg: "session", replFmt: "session.FromContext(%s)", isDefault: true}},
		"username":     {{pkg: "basicauth", replFmt: "basicauth.UsernameFromContext(%s)", isDefault: true}},
		"password":     {{pkg: "basicauth", replFmt: "basicauth.PasswordFromContext(%s)", isDefault: true}},
		"token":        {{pkg: "keyauth", replFmt: "keyauth.TokenFromContext(%s)", isDefault: true}},
	}
	for key, repls := range defaults {
		for _, r := range repls {
			exists := false
			for _, existing := range ctxMap[key] {
				if existing.pkg == r.pkg {
					exists = true
					break
				}
			}
			if !exists {
				ctxMap[key] = append(ctxMap[key], r)
			}
		}
	}

	// second pass: perform replacements and clean up
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		imports := parseMiddlewareImports(content, reImport)

		reLocals := regexp.MustCompile(`(\w+)\.Locals\("([^"]+)"\)`)
		content = reLocals.ReplaceAllStringFunc(content, func(s string) string {
			sub := reLocals.FindStringSubmatch(s)
			ctx := sub[1]
			key := sub[2]
			repls, ok := ctxMap[key]
			if !ok {
				return s
			}

			var custom, defs []ctxRepl
			for _, r := range repls {
				if r.isDefault {
					defs = append(defs, r)
				} else {
					custom = append(custom, r)
				}
			}

			choose := func(r ctxRepl) string { return fmt.Sprintf(r.replFmt, ctx) }

			if len(custom) == 1 {
				return choose(custom[0])
			}
			if len(custom) > 1 {
				for _, r := range custom {
					for _, pkg := range imports {
						if pkg == r.pkg {
							return choose(r)
						}
					}
				}
				return s
			}

			if len(defs) == 1 {
				return choose(defs[0])
			}
			for _, r := range defs {
				for _, pkg := range imports {
					if pkg == r.pkg {
						return choose(r)
					}
				}
			}
			return s
		})

		reTypeAssert := regexp.MustCompile(`([\w\.]+FromContext\([^\)]+\))\.\([^\)]+\)`)
		content = reTypeAssert.ReplaceAllString(content, "$1")

		reComma := regexp.MustCompile(`(\w+)\s*,\s*(\w+)\s*:=\s*([\w\.]+FromContext\([^\)]+\))(?:\s*,\s*true)*`)
		content = reComma.ReplaceAllString(content, "$1, $2 := $3, true")

		for alias := range imports {
			reCfg := regexp.MustCompile(regexp.QuoteMeta(alias) + `\.Config{`)
			matches := reCfg.FindAllStringIndex(content, -1)
			if len(matches) == 0 {
				continue
			}
			var b strings.Builder
			last := 0
			for _, m := range matches {
				if _, err := b.WriteString(content[last:m[0]]); err != nil {
					return content
				}
				start := m[0]
				end := extractBlock(content, m[1], '{', '}')
				cfg := content[start:end]
				cfg = removeConfigField(cfg, "ContextKey")
				cfg = removeConfigField(cfg, "ContextUsername")
				cfg = removeConfigField(cfg, "ContextPassword")
				if _, err := b.WriteString(cfg); err != nil {
					return content
				}
				last = end
			}
			if _, err := b.WriteString(content[last:]); err != nil {
				return content
			}
			content = b.String()
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
