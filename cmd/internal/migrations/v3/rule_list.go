package v3

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ruleListPackages are the middlewares that took a rule map and now take an
// ordered list. Both deprecated the map for the same reason and their new
// fields are spelled the same, so one migration serves them.
var ruleListPackages = []string{"redirect", "rewrite"}

var reRuleMap = regexp.MustCompile(`(?m)^([ \t]*)Rules:\s*map\[string\]string\{`)

// MigrateRuleList rewrites a Rules map into the RuleList slice that replaced it.
//
//	Rules: map[string]string{"/old": "/new"}
//	RuleList: []redirect.Rule{{From: "/old", To: "/new"}}
//
// The entries are emitted in the order the deprecated map ranked them, not in
// the order they were written: a list is tried top to bottom, so writing
// "/api/*" above the "/api/users" it also matches would change which rule
// answers.
func MigrateRuleList(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, migrateRuleListContent)
	if err != nil {
		return fmt.Errorf("failed to migrate redirect and rewrite rules: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating redirect and rewrite Rules maps to RuleList")
	return nil
}

func migrateRuleListContent(content string) string {
	for _, pkg := range ruleListPackages {
		for _, alias := range collectAliases(content, importRe(pkg), []string{pkg}) {
			pattern := regexp.MustCompile(regexp.QuoteMeta(alias) + `\.Config\{`)
			content = IterateConfigBlocks(content, pattern, func(cfg string) string {
				return rewriteRuleMap(cfg, alias)
			})
		}
	}
	return content
}

func importRe(pkg string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:([\w.]+)\s+)?"github\.com/gofiber/fiber/v3/middleware/` + pkg + `"`)
}

// rewriteRuleMap replaces the one Rules map inside a config block. The block is
// whatever IterateConfigBlocks handed over, so a nested config of another
// package is not reachable from here.
func rewriteRuleMap(cfg, alias string) string {
	loc := reRuleMap.FindStringSubmatchIndex(cfg)
	if loc == nil {
		return cfg
	}

	indent := cfg[loc[2]:loc[3]]
	end := extractBlock(cfg, loc[1], '{', '}')
	if end <= loc[1] {
		return cfg
	}

	rules, ok := parseRuleMap(cfg[loc[1] : end-1])
	if !ok {
		// A key or value this migration cannot read verbatim, a variable for
		// instance. Left alone rather than guessed at; the map still compiles.
		return cfg
	}
	sortRules(rules)

	var b strings.Builder
	fmt.Fprintf(&b, "%sRuleList: []%s.Rule{\n", indent, alias)
	for _, r := range rules {
		fmt.Fprintf(&b, "%s\t{From: %s, To: %s},\n", indent, r.from, r.to)
	}
	fmt.Fprintf(&b, "%s}", indent)

	return cfg[:loc[0]] + b.String() + cfg[end:]
}

type ruleEntry struct {
	from string // the key as written, quotes included
	to   string
	key  string // the key unquoted, for ranking
}

var reRuleEntry = regexp.MustCompile(`^\s*("(?:[^"\\]|\\.)*"|` + "`[^`]*`" + `)\s*:\s*("(?:[^"\\]|\\.)*"|` + "`[^`]*`" + `)\s*,?\s*$`)

// parseRuleMap reads the entries of a map literal body, reporting false when
// any line is something other than a string-to-string pair or a comment.
func parseRuleMap(body string) ([]ruleEntry, bool) {
	var rules []ruleEntry
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		m := reRuleEntry.FindStringSubmatch(line)
		if m == nil {
			return nil, false
		}
		rules = append(rules, ruleEntry{from: m[1], to: m[2], key: unquoteRuleKey(m[1])})
	}
	return rules, len(rules) > 0
}

func unquoteRuleKey(s string) string {
	if len(s) < 2 {
		return s
	}
	inner := s[1 : len(s)-1]
	if s[0] == '`' {
		return inner
	}
	// Only the escapes a path can carry: the ranking reads bytes, so an
	// unhandled escape just ranks by the text as written.
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(inner)
}

// sortRules puts the entries in the order the middleware ranked the map, so
// the list answers each path the way the map did.
func sortRules(rules []ruleEntry) {
	slices.SortFunc(rules, func(a, b ruleEntry) int {
		if d := cmp.Compare(pinnedPrefixLen(b.key), pinnedPrefixLen(a.key)); d != 0 {
			return d
		}
		if d := cmp.Compare(pinnedLen(b.key), pinnedLen(a.key)); d != 0 {
			return d
		}
		if d := cmp.Compare(strings.Count(a.key, "*"), strings.Count(b.key, "*")); d != 0 {
			return d
		}
		return cmp.Compare(a.key, b.key)
	})
}

func pinnedPrefixLen(rule string) int {
	if i := strings.IndexByte(rule, '*'); i >= 0 {
		return i
	}
	return len(rule)
}

func pinnedLen(rule string) int {
	return len(rule) - strings.Count(rule, "*")
}
