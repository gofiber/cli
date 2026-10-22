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

// MigrateTrustedProxyConfig renames the trusted proxy settings of fiber.Config:
// EnableTrustedProxyCheck is TrustProxy in v3 and the list of TrustedProxies
// moves into TrustProxyConfig.Proxies. Literals, assignments to a config
// variable and reads through app.Config() are handled; the list may be any
// expression.
//
// v2 trusted every proxy unless the check was enabled, and v3 trusts none unless
// TrustProxy is set, so a ProxyHeader without the check gets a note.
func MigrateTrustedProxyConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return appMigrateTrustedProxyContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate trusted proxy config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating trusted proxy config")
	return nil
}

func appMigrateTrustedProxyContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "TrustedProxies") &&
		!strings.Contains(content, "EnableTrustedProxyCheck") &&
		!strings.Contains(content, "ProxyHeader") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if x.Type != nil && f.fiberTypeName(x.Type) == appNameConfig {
				appMigrateProxyLiteral(f, &edits, x)
			}
		case *ast.SelectorExpr:
			appMigrateProxySelector(f, &edits, x)
		}
		return true
	})
	return edits.apply(content)
}

const (
	appFieldEnableProxyCheck = "EnableTrustedProxyCheck"
	appFieldTrustedProxies   = "TrustedProxies"
	appFieldProxyHeader      = "ProxyHeader"
	appFieldTrustProxy       = "TrustProxy"
)

// appMigrateProxyLiteral migrates the proxy settings of a fiber.Config literal.
func appMigrateProxyLiteral(f *appFile, edits *editSet, lit *ast.CompositeLit) {
	g := f.g
	_, enable := appKeyedElement(lit, appFieldEnableProxyCheck)
	if enable != nil {
		edits.replaceNode(g, enable.Key, appFieldTrustProxy)
	}

	_, proxies := appKeyedElement(lit, appFieldTrustedProxies)
	if proxies != nil {
		if idx, _ := appKeyedElement(lit, "TrustProxyConfig"); idx >= 0 {
			internal.Notef(f.fi, g.line(proxies.Pos()),
				"TrustedProxies is TrustProxyConfig.Proxies in v3, but this config already sets TrustProxyConfig: merge the two by hand")
		} else {
			prefix, _ := f.fiberQualifier()
			edits.replaceNode(g, proxies, "TrustProxyConfig: "+prefix+"TrustProxyConfig{Proxies: "+g.text(proxies.Value)+"}")
		}
	}

	trusts := f.literalTrustsProxies(lit)
	if _, header := appKeyedElement(lit, appFieldProxyHeader); header != nil && !trusts && !appIsEmptyString(header.Value) {
		internal.Notef(f.fi, g.line(header.Pos()), appProxyHeaderNote)
	}
	if proxies != nil && !trusts {
		internal.Notef(f.fi, g.line(proxies.Pos()), appProxyListNote)
	}
}

const (
	appProxyHeaderNote = "v2 trusted the ProxyHeader and the X-Forwarded-* headers of every client; v3 only trusts them from a proxy listed in TrustProxyConfig, " +
		"so c.IP() is the proxy's address, and c.Scheme(), c.Host() and c.BaseURL() ignore the headers, until TrustProxy is true and TrustProxyConfig names your proxies (Proxies, Loopback, Private)"
	appProxyListNote = "v2 ignored TrustedProxies unless EnableTrustedProxyCheck was true (every client was trusted otherwise); " +
		"v3 only uses TrustProxyConfig when TrustProxy is true, which this config does not set"
)

// literalTrustsProxies reports whether the config turns the trusted proxy check
// on: the literal sets it, or the variable that holds the literal is assigned
// it.
func (f *appFile) literalTrustsProxies(lit *ast.CompositeLit) bool {
	for _, key := range []string{appFieldEnableProxyCheck, appFieldTrustProxy} {
		if _, kv := appKeyedElement(lit, key); kv != nil {
			return !isIdentNamed(kv.Value, appLitFalse)
		}
	}

	src := f.sourceOfLiteral(lit)
	if src.node == nil {
		return false
	}
	return src.assigns(appFieldEnableProxyCheck, appFieldTrustProxy)
}

// appLitFalse is the predeclared identifier false.
const appLitFalse = "false"

// assigns reports whether the variable that holds the source is assigned one of
// the fields.
func (src *appListenSource) assigns(fields ...string) bool {
	found := false
	for _, r := range src.regions() {
		ast.Inspect(r.root, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || found {
				return !found
			}
			id, isIdent := sel.X.(*ast.Ident)
			if isIdent && src.isTheVariable(r.file, id) {
				for _, field := range fields {
					if sel.Sel.Name == field {
						found = true
					}
				}
			}
			return !found
		})
	}
	return found
}

// appIsEmptyString reports whether e is the empty string literal.
func appIsEmptyString(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && (lit.Value == `""` || lit.Value == "``")
}

// appMigrateProxySelector migrates a use of the proxy settings through a config
// variable or app.Config(): cfg.TrustedProxies = ... and the like.
func appMigrateProxySelector(f *appFile, edits *editSet, sel *ast.SelectorExpr) {
	switch sel.Sel.Name {
	case appFieldEnableProxyCheck, appFieldTrustedProxies, appFieldProxyHeader:
	default:
		return
	}
	if f.kind(sel.X) != appKindConfig {
		return
	}

	switch sel.Sel.Name {
	case appFieldEnableProxyCheck:
		edits.replaceNode(f.g, sel.Sel, appFieldTrustProxy)
	case appFieldTrustedProxies:
		edits.replaceNode(f.g, sel.Sel, "TrustProxyConfig.Proxies")
	case appFieldProxyHeader:
		if f.assignedWithoutTrust(sel) {
			internal.Notef(f.fi, f.g.line(sel.Pos()), appProxyHeaderNote)
		}
	}
}

// assignedWithoutTrust reports whether sel is the target of an assignment to a
// config variable that never turns the trusted proxy check on.
func (f *appFile) assignedWithoutTrust(sel *ast.SelectorExpr) bool {
	as, ok := f.parent[sel].(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 || as.Lhs[0] != sel || appIsEmptyString(as.Rhs[0]) {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	b, bound := f.binding(id)
	if !bound {
		return false
	}

	src := &appListenSource{file: b.file, node: b.node, scope: b.scope, name: id.Name}
	if lit, isLit := b.init.(*ast.CompositeLit); isLit {
		src.lit = lit
		if b.file.literalTrustsProxies(lit) {
			return false
		}
	}
	return !src.assigns(appFieldEnableProxyCheck, appFieldTrustProxy)
}
