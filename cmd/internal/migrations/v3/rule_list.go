package v3

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
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
var ruleListPackages = []mw3RuleListPackage{
	{name: "redirect", patternBytes: `*.[](){}+?^|\$`},
	{name: "rewrite", patternBytes: "*", quotedKeys: true},
}

type mw3RuleListPackage struct {
	name string
	// patternBytes are the key bytes ranked as syntax rather than path text:
	// redirect compiles a key as a regexp, rewrite reads only "*".
	patternBytes string
	// quotedKeys is whether the middleware takes every byte of a key but "*"
	// for itself: a key written with regexp syntax matches that text in v3.
	quotedKeys bool
}

// mw3RuleRegexpSyntax are the bytes of a key that v2 compiled as a regular
// expression and that rewrite v3 reads as the path text they spell.
const mw3RuleRegexpSyntax = `[](){}+?^|\$`

// MigrateRuleList rewrites a Rules map into the RuleList slice that replaced it.
//
//	Rules: map[string]string{"/old": "/new"}
//	RuleList: []redirect.Rule{{From: "/old", To: "/new"}}
//
// The entries are emitted in the order the deprecated map ranked them, not in
// the order they were written: a list is tried top to bottom, so writing
// "/api/*" above the "/api/users" it also matches would change which rule
// answers.
//
// A rewrite key that relies on regular-expression syntax is dead in v3 whether
// it sits in the map or the list, since the middleware quotes the key: such a
// map is left as it is and reported.
func MigrateRuleList(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateRuleListContent)
	if err != nil {
		return fmt.Errorf("failed to migrate redirect and rewrite rules: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating redirect and rewrite Rules maps to RuleList")
	return nil
}

func migrateRuleListContent(content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "Rules") || !strings.Contains(content, "middleware/") {
		return content
	}

	type target struct {
		pkg   mw3RuleListPackage
		names []string
	}
	var targets []target
	for _, pkg := range ruleListPackages {
		re := regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/` + pkg.name + `$`)
		if names := importNames(content, re, pkg.name); len(names) > 0 {
			targets = append(targets, target{pkg: pkg, names: names})
		}
	}
	if len(targets) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || lit.Type == nil {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != mw3Config {
			return true
		}
		pkgID, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if _, shadowed := resolveIdent(pkgID.Name, pkgID.Pos(), stack); shadowed {
			return true
		}
		for _, t := range targets {
			if slices.Contains(t.names, pkgID.Name) {
				rewriteRuleMap(g, &edits, fi, lit, pkgID.Name, t.pkg)
			}
		}
		return true
	})
	return edits.apply(content)
}

// rewriteRuleMap replaces the Rules map of a config literal with the list.
func rewriteRuleMap(g *goSource, edits *editSet, fi internal.FileInfo, lit *ast.CompositeLit, alias string, pkg mw3RuleListPackage) {
	kv := mw3Field(lit, "Rules")
	if kv == nil {
		return
	}
	mapLit, ok := kv.Value.(*ast.CompositeLit)
	if !ok || !mw3IsStringMap(mapLit.Type) {
		return
	}

	rules, ok := parseRuleMap(mapLit)
	if !ok {
		// A key or value this migration cannot read verbatim, a variable for
		// instance. Left alone rather than guessed at; the map still compiles.
		return
	}

	if pkg.quotedKeys {
		var dead []string
		first := token.NoPos
		for _, r := range rules {
			if strings.ContainsAny(r.key, mw3RuleRegexpSyntax) {
				dead = append(dead, r.from)
				if first == token.NoPos {
					first = r.pos
				}
			}
		}
		if len(dead) > 0 {
			internal.Notef(fi, g.line(first),
				"the %s Rules key %s uses regular-expression syntax, which v3 no longer reads: a key is path text now, where only \"*\" is a wildcard, "+
					"so the rule would not match what it did. Spell it with \"*\" or do the rewrite in a middleware of your own. The Rules map was left as it is.",
				pkg.name, strings.Join(dead, ", "))
			return
		}
	}
	sortRules(rules, pkg.patternBytes)

	// Rules may share its line with the config's opening or another field.
	indent := mw3Indent(g.src, g.offset(kv.Pos()))
	var b strings.Builder
	fmt.Fprintf(&b, "RuleList: []%s.Rule{\n", alias)
	for _, r := range rules {
		fmt.Fprintf(&b, "%s\t{From: %s, To: %s},\n", indent, r.from, r.to)
	}
	fmt.Fprintf(&b, "%s}", indent)

	edits.replaceNode(g, kv, b.String())
}

// mw3IsStringMap reports whether the type is map[string]string.
func mw3IsStringMap(typ ast.Expr) bool {
	m, ok := typ.(*ast.MapType)
	return ok && isIdentNamed(m.Key, "string") && isIdentNamed(m.Value, "string")
}

type ruleEntry struct {
	from string // the key as written, quotes included
	to   string
	key  string // the key unquoted, for ranking
	pos  token.Pos
}

// parseRuleMap reads the entries of a map literal. It reports false when an
// entry is anything other than a string literal pair, which is how a computed
// key or value is left alone rather than guessed at.
func parseRuleMap(lit *ast.CompositeLit) ([]ruleEntry, bool) {
	var rules []ruleEntry
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, false
		}
		key, keyOK := kv.Key.(*ast.BasicLit)
		value, valueOK := kv.Value.(*ast.BasicLit)
		if !keyOK || !valueOK {
			return nil, false
		}
		// Ranked by the string the middleware sees, so "\x2a" counts as the
		// wildcard it is.
		unquoted, ok := mw3UnquoteString(key)
		if !ok {
			return nil, false
		}
		if _, ok := mw3UnquoteString(value); !ok {
			return nil, false
		}
		rules = append(rules, ruleEntry{from: key.Value, to: value.Value, key: unquoted, pos: key.Pos()})
	}
	return rules, len(rules) > 0
}

// sortRules puts the entries in the order the middleware ranked the map, so
// the list answers each path the way the map did.
func sortRules(rules []ruleEntry, patternBytes string) {
	slices.SortFunc(rules, func(a, b ruleEntry) int {
		if d := cmp.Compare(pinnedPrefixLen(b.key, patternBytes), pinnedPrefixLen(a.key, patternBytes)); d != 0 {
			return d
		}
		if d := cmp.Compare(pinnedLen(b.key, patternBytes), pinnedLen(a.key, patternBytes)); d != 0 {
			return d
		}
		if d := cmp.Compare(strings.Count(a.key, "*"), strings.Count(b.key, "*")); d != 0 {
			return d
		}
		return cmp.Compare(a.key, b.key)
	})
}

func pinnedPrefixLen(rule, patternBytes string) int {
	if i := strings.IndexAny(rule, patternBytes); i >= 0 {
		return i
	}
	return len(rule)
}

func pinnedLen(rule, patternBytes string) int {
	n := 0
	for i := range len(rule) {
		if strings.IndexByte(patternBytes, rule[i]) < 0 {
			n++
		}
	}
	return n
}
