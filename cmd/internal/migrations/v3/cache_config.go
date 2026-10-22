package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var mw1Cache = newMW1Package("cache")

// MigrateCacheConfig moves the cache middleware to v3, for the cache.Config
// literals and the variables that hold one:
//
//   - Store and Key become Storage and KeyGenerator, and CacheControl becomes
//     its opposite, DisableCacheControl.
//   - The defaults that changed are set to what they were: v2 sent no
//     Cache-Control header unless CacheControl was set and kept a response for
//     a minute, v3 sends the header and keeps it for five minutes. A config
//     without CacheControl gets DisableCacheControl: true, one without
//     Expiration gets time.Minute, and cache.New() without a config gets both.
//     A config that already uses a field of v3 is left as it is.
func MigrateCacheConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateCacheContent)
	if err != nil {
		return fmt.Errorf("failed to migrate cache configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating cache middleware configs")
	return nil
}

// The fields of cache.Config that the migration reads.
const (
	cacheControl        = "CacheControl"
	cacheDisableControl = "DisableCacheControl"
	cacheExpiration     = "Expiration"
	cacheStore          = "Store"
	cacheStorage        = "Storage"
	cacheKey            = "Key"
	cacheKeyGenerator   = "KeyGenerator"
)

// cacheV3Fields are the fields that only exist in v3: a config that sets one
// is written for v3 and does not get the defaults of v2.
var cacheV3Fields = []string{
	cacheDisableControl, "CacheInvalidator", "KeyHeaders", "KeyCookies", "KeyCredentialHeaders",
	"DisableQueryKeys", "DisableVaryHeaders", "DisableValueRedaction", "AllowStaleCredentialResponses",
}

// cacheMigration is the state of the rewrite of one file.
type cacheMigration struct {
	g         *goSource
	timeName  string
	edits     editSet
	needsTime bool
	usesTime  bool
}

func migrateCacheContent(content string, fi internal.FileInfo) string {
	names := mw1Qualifiers(mw1Cache.names(content))
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	m := &cacheMigration{g: g}
	m.timeName, m.needsTime = mw1ImportName(content, "time", "time")
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if lit, isConfig := mw1ConfigLit(x, names); isConfig {
				m.config(lit)
			}
		case *ast.CallExpr:
			m.bareNew(x, names)
		case *ast.AssignStmt:
			if _, field, isAssign := mw1AssignedField(x, stack, names); isAssign {
				m.assignment(x, field)
			}
		}
		return true
	})

	out := m.edits.apply(content)
	if m.usesTime && m.needsTime {
		out = mw1AddImport(out, "time")
	}
	return out
}

// v2Defaults returns the fields that restore the defaults of v2.
func (m *cacheMigration) v2Defaults(withControl, withExpiration bool) []string {
	var elems []string
	if withControl {
		elems = append(elems, cacheDisableControl+": true")
	}
	if withExpiration {
		elems = append(elems, cacheExpiration+": "+m.timeName+".Minute")
		m.usesTime = true
	}
	return elems
}

// bareNew gives cache.New() without a config the config of v2.
func (m *cacheMigration) bareNew(call *ast.CallExpr, names []string) {
	if len(call.Args) != 0 || !mw1IsMember(call.Fun, names, "New") {
		return
	}
	pkg, _, _ := mw1Selector(call.Fun)
	text := pkg + ".Config{" + strings.Join(m.v2Defaults(true, true), ", ") + "}"
	m.edits.replace(m.g.offset(call.Rparen), m.g.offset(call.Rparen), text)
}

// config rewrites a cache.Config literal.
func (m *cacheMigration) config(lit *ast.CompositeLit) {
	fields := map[string]*ast.KeyValueExpr{}
	index := map[string]int{}
	for i, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if key, isIdent := kv.Key.(*ast.Ident); isIdent {
				fields[key.Name] = kv
				index[key.Name] = i
			}
		}
	}

	// the deprecated field wins in v2 when both are set
	rename := func(from, to string) {
		if fields[from] == nil {
			return
		}
		if fields[to] != nil {
			start, end := mw1ElementSpan(m.g, lit, index[to])
			m.edits.replace(start, end, "")
		}
		m.edits.replaceNode(m.g, fields[from].Key, to)
	}
	rename(cacheStore, cacheStorage)
	rename(cacheKey, cacheKeyGenerator)

	if kv := fields[cacheControl]; kv != nil {
		m.edits.replaceNode(m.g, kv.Key, cacheDisableControl)
		m.edits.replaceNode(m.g, kv.Value, m.invert(kv.Value))
	}

	for _, name := range cacheV3Fields {
		if fields[name] != nil {
			return
		}
	}
	elems := m.v2Defaults(fields[cacheControl] == nil, fields[cacheExpiration] == nil)
	if len(elems) > 0 {
		offset, text := mw1AppendElements(m.g, lit, elems)
		m.edits.replace(offset, offset, text)
	}
}

// assignment rewrites `cfg.Field = value` on a variable that holds a
// cache.Config.
func (m *cacheMigration) assignment(as *ast.AssignStmt, field string) {
	sel, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok {
		return
	}
	switch field {
	case cacheControl:
		m.edits.replaceNode(m.g, sel.Sel, cacheDisableControl)
		m.edits.replaceNode(m.g, as.Rhs[0], m.invert(as.Rhs[0]))
	case cacheStore:
		m.edits.replaceNode(m.g, sel.Sel, cacheStorage)
	case cacheKey:
		m.edits.replaceNode(m.g, sel.Sel, cacheKeyGenerator)
	}
}

// invert returns the opposite of a boolean expression.
func (m *cacheMigration) invert(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		switch x.Name {
		case litTrue:
			return litFalse
		case litFalse:
			return litTrue
		}
		return "!" + x.Name
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			inner := x.X
			for {
				p, isParen := inner.(*ast.ParenExpr)
				if !isParen {
					break
				}
				inner = p.X
			}
			return m.g.text(inner)
		}
	case *ast.SelectorExpr, *ast.CallExpr, *ast.IndexExpr, *ast.ParenExpr:
		return "!" + m.g.text(e)
	}
	return "!(" + m.g.text(e) + ")"
}
