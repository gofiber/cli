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

// cacheDefaultNote is the note for a copy of cache.ConfigDefault that cannot be
// completed with the defaults of v2.
const cacheDefaultNote = "cache.ConfigDefault is not what it was in v2 (it sends Cache-Control headers and keeps responses for five minutes): " +
	"set DisableCacheControl and Expiration on the copy"

// cacheMigration is the state of the rewrite of one file.
type cacheMigration struct {
	g         *goSource
	rep       *mw1Reporter
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

	m := &cacheMigration{g: g, rep: newMW1Reporter(g, fi)}
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
			} else {
				m.defaultCopy(x, stack, names)
			}
		case *ast.ValueSpec:
			if len(x.Values) == 1 && mw1IsMember(x.Values[0], names, "ConfigDefault") {
				m.rep.notef(x, cacheDefaultNote)
			}
		}
		return true
	})

	applied := m.edits.apply(content)
	out := applied
	if m.usesTime && m.needsTime {
		out = mw1AddImport(out, "time")
	}
	m.rep.flush(&m.edits, applied, out)
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

// defaultCopy restores the defaults of v2 in a config that starts as a copy of
// cache.ConfigDefault, which v3 changed: the statements follow the declaration,
// unless the code sets the field itself afterwards.
func (m *cacheMigration) defaultCopy(as *ast.AssignStmt, stack []ast.Node, names []string) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 || !mw1IsMember(as.Rhs[0], names, "ConfigDefault") {
		return
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok || as.Tok != token.DEFINE || len(stack) == 0 {
		return
	}
	block, ok := stack[len(stack)-1].(*ast.BlockStmt)
	if !ok || !mw1StartsLine(m.g.src, m.g.offset(as.Pos())) {
		m.rep.notef(as, cacheDefaultNote)
		return
	}

	// the fields that the code sets after the declaration
	set := map[string]bool{}
	after := false
	for _, stmt := range block.List {
		if stmt == ast.Stmt(as) {
			after = true
			continue
		}
		if !after {
			continue
		}
		ast.Inspect(stmt, func(n ast.Node) bool {
			if assign, isAssign := n.(*ast.AssignStmt); isAssign && len(assign.Lhs) == 1 {
				if sel, isSel := assign.Lhs[0].(*ast.SelectorExpr); isSel && isIdentNamed(sel.X, id.Name) {
					set[sel.Sel.Name] = true
				}
			}
			return true
		})
	}

	var stmts []string
	if !set[cacheControl] && !set[cacheDisableControl] {
		stmts = append(stmts, id.Name+"."+cacheDisableControl+" = true")
	}
	if !set[cacheExpiration] {
		stmts = append(stmts, id.Name+"."+cacheExpiration+" = "+m.timeName+".Minute")
		m.usesTime = true
	}
	if len(stmts) == 0 {
		return
	}
	indent := mw1Indent(m.g.src, m.g.offset(as.Pos()))
	end := m.g.offset(as.End())
	m.edits.replace(end, end, "\n"+indent+strings.Join(stmts, "\n"+indent))
}
