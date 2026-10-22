package v3

import (
	"fmt"
	"go/ast"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var mw1CSRF = newMW1Package("csrf")

// MigrateCSRFConfig moves the csrf middleware to v3, for what the file imports
// from the csrf package:
//
//   - Expiration (and the deprecated CookieExpires) becomes IdleTimeout,
//     SessionKey and HandlerContextKey go, the deprecated Cookie struct becomes
//     the Cookie* fields.
//   - KeyLookup (and the deprecated TokenLookup) becomes an Extractor of the
//     extractors package, and so do the csrf.CsrfFrom* helpers and a function
//     literal or a named function given as Extractor. A lookup with no
//     equivalent (the CSRF cookie itself, or a value that is not known) stays
//     as it is, and is reported.
//   - The error variables that v3 renamed or merged, and the handler that is
//     read back from the context (csrf.CSRFHandler) are migrated.
func MigrateCSRFConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	idx := newMW1Index(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateCSRFContent(content, fi, idx)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate CSRF configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating CSRF middleware configs")
	return nil
}

const (
	csrfExtractorsPath = "github.com/gofiber/fiber/v3/extractors"
	// the v2 fields of csrf.Config that are migrated
	csrfExpiration        = "Expiration"
	csrfIdleTimeout       = "IdleTimeout"
	csrfCookieExpires     = "CookieExpires"
	csrfCookie            = "Cookie"
	csrfKeyLookup         = "KeyLookup"
	csrfTokenLookup       = "TokenLookup"
	csrfExtractor         = "Extractor"
	csrfSessionKey        = "SessionKey"
	csrfHandlerContextKey = "HandlerContextKey"
	// csrfHeaderName is csrf.HeaderName, the header the default lookup reads
	csrfHeaderName = "X-Csrf-Token"
)

// csrfMigration is the state of the rewrite of one file.
type csrfMigration struct {
	g    *goSource
	rep  *mw1Reporter
	idx  *mw1Index
	path string
	// customExtractors are the function literals that were given to
	// extractors.FromCustom: inside them the missing token is
	// extractors.ErrNotFound.
	customExtractors map[*ast.FuncLit]bool
	extractorsName   string
	timeName         string
	names            []string
	edits            editSet
	needsExtractors  bool
	usesExtractors   bool
	needsTime        bool
	usesTime         bool
}

func migrateCSRFContent(content string, fi internal.FileInfo, idx *mw1Index) string {
	names := mw1Qualifiers(mw1CSRF.names(content))
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	m := &csrfMigration{g: g, rep: newMW1Reporter(g, fi), idx: idx, path: fi.Path, names: names, customExtractors: map[*ast.FuncLit]bool{}}
	m.extractorsName, m.needsExtractors = mw1ImportName(content, csrfExtractorsPath, "extractors")
	m.timeName, m.needsTime = mw1ImportName(content, "time", "time")

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if lit, isConfig := mw1ConfigLit(x, names); isConfig {
				m.config(lit, stack)
			}
		case *ast.CallExpr:
			m.newCall(x)
		case *ast.ValueSpec:
			m.valueSpec(x, stack)
		case *ast.AssignStmt:
			if m.handlerAssignment(x, stack) {
				return false
			}
			m.assignment(x, stack)
			m.defaultCopy(x, stack)
		case *ast.TypeAssertExpr:
			return !m.handlerAssertion(x, stack)
		case *ast.SelectorExpr:
			m.selector(x, stack)
		}
		return true
	})

	applied := m.edits.apply(content)
	out := applied
	if m.usesExtractors && m.needsExtractors {
		out = mw1AddImport(out, csrfExtractorsPath)
	}
	if m.usesTime && m.needsTime {
		out = mw1AddImport(out, "time")
	}
	m.rep.flush(&m.edits, applied, out)
	return out
}

// config rewrites the fields of a csrf.Config literal.
func (m *csrfMigration) config(lit *ast.CompositeLit, stack []ast.Node) {
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
	remove := func(name string) {
		start, end := mw1ElementSpan(m.g, lit, index[name])
		m.edits.replace(start, end, "")
	}

	// the deprecated CookieExpires wins over Expiration in v2
	switch {
	case fields[csrfExpiration] != nil && fields[csrfCookieExpires] != nil:
		m.edits.replaceNode(m.g, fields[csrfExpiration].Key, csrfIdleTimeout)
		m.notef(fields[csrfCookieExpires], "CookieExpires and Expiration are both set and CookieExpires wins in v2: keep the one you mean as IdleTimeout and remove the other")
	case fields[csrfExpiration] != nil:
		m.edits.replaceNode(m.g, fields[csrfExpiration].Key, csrfIdleTimeout)
	case fields[csrfCookieExpires] != nil:
		m.edits.replaceNode(m.g, fields[csrfCookieExpires].Key, csrfIdleTimeout)
	}

	if fields[csrfSessionKey] != nil {
		remove(csrfSessionKey)
	}
	if fields[csrfHandlerContextKey] != nil {
		remove(csrfHandlerContextKey)
	}
	if kv := fields[csrfCookie]; kv != nil {
		m.cookie(lit, kv, index[csrfCookie], fields)
	}

	m.lookup(lit, fields, index)

	if kv := fields[csrfExtractor]; kv != nil {
		m.extractor(kv.Value, stack)
	}
	m.defaultLifetime(lit, fields, stack)
}

// notef leaves a note on the line of the node.
func (m *csrfMigration) notef(at ast.Node, format string, args ...any) {
	m.rep.notef(at, format, args...)
}

// assignment rewrites `cfg.Field = value` on a variable that holds a
// csrf.Config.
func (m *csrfMigration) assignment(as *ast.AssignStmt, stack []ast.Node) {
	_, field, ok := mw1AssignedField(as, stack, m.names)
	if !ok {
		return
	}
	sel, isSel := as.Lhs[0].(*ast.SelectorExpr)
	if !isSel {
		return
	}

	switch field {
	case csrfExpiration, csrfCookieExpires:
		m.edits.replaceNode(m.g, sel.Sel, csrfIdleTimeout)
	case csrfKeyLookup, csrfTokenLookup:
		if text, ok := m.lookupExtractor(as.Rhs[0]); ok {
			if text == "" {
				// the empty lookup is the default one
				text = m.extractorsName + ".FromHeader(" + strconv.Quote(csrfHeaderName) + ")"
			}
			m.edits.replaceNode(m.g, sel.Sel, csrfExtractor)
			m.edits.replaceNode(m.g, as.Rhs[0], text)
			m.usesExtractors = true
		} else {
			m.notef(as, "csrf.Config has no %s in v3: set the Extractor instead, with extractors.FromHeader, FromForm, FromQuery or FromParam", field)
		}
	case csrfExtractor:
		m.extractor(as.Rhs[0], stack)
	case csrfSessionKey, csrfHandlerContextKey:
		if start, end, found := mw1StatementSpan(m.g, as, stack); found {
			m.edits.replace(start, end, "")
		} else {
			m.notef(as, "csrf.Config has no %s in v3: remove this assignment", field)
		}
	case csrfCookie:
		m.notef(as, "csrf.Config has no Cookie in v3: set CookieName, CookieDomain, CookiePath, CookieSecure, CookieHTTPOnly and CookieSameSite (left as it is)")
	}
}
