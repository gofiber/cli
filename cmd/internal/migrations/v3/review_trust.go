package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// The trusted proxy settings changed in a way that no rewrite can follow: v2
// trusted the X-Forwarded-* headers of every client unless the check was on, v3
// trusts none unless TrustProxy is true and the peer is listed. A project that
// reads the scheme or host of a request, or uses the earlydata middleware, and
// never configures the trust, sees the proxy's scheme and host behind a reverse
// proxy. The review collects the facts from every file and reports once.

// reviewTrustReads are the methods of the context that read X-Forwarded-Proto or
// X-Forwarded-Host in v3 when the peer is trusted.
var reviewTrustReads = []string{"Scheme", reviewReadSecure, reviewReadHost, "Hostname", "BaseURL", "Subdomains"}

const (
	reviewReadHost         = "Host"
	reviewReadSecure       = "Secure"
	reviewTrustProxy       = "TrustProxy"
	reviewTrustProxyConfig = "TrustProxyConfig"
	reviewConfigType       = "Config"
	reviewProxyHeader      = "ProxyHeader"
	reviewNewFiberApp      = "NewWithCustomCtx"
)

var reviewEarlyDataPath = reviewMiddlewarePath("earlydata")

// reviewPlace is a place in the project.
type reviewPlace struct {
	file string
	line int
}

func (p reviewPlace) known() bool { return p.file != "" }

func (p reviewPlace) String() string { return p.file + ":" + strconv.Itoa(p.line) }

// first remembers the place of pos in the file unless one is known already.
func (p *reviewPlace) first(rf *reviewFile, pos token.Pos) {
	if !p.known() {
		*p = reviewPlace{file: rf.fi.Rel, line: rf.g.line(pos)}
	}
}

// reviewTrust is what the review knows about the trusted proxy settings of the
// project, from all of its files.
type reviewTrust struct {
	// config is the first fiber.Config literal, call the first fiber.New call, bare the
	// first app that is created without a word about the trust: fiber.New() or a literal
	// of the config that sets none of its fields.
	config, call, bare reviewPlace
	// read is the first read of the scheme or host of a request, early the first
	// import of the earlydata middleware.
	read, early reviewPlace
	// header is set when a config sets a ProxyHeader: the migration of the trusted
	// proxy settings notes such a config that does not trust its proxies.
	header bool
	// list is set when the project mentions a TrustProxyConfig.
	list bool
	// on is set when a config sets TrustProxy to true, maybe when it sets it from
	// an expression that may be false.
	on, maybe bool
}

// setTrust records the value a config gives TrustProxy: true is on, false says
// nothing (the default), anything else may be either.
func (t *reviewTrust) setTrust(value ast.Expr) {
	switch {
	case isIdentNamed(value, appLitTrue):
		t.on = true
	case isIdentNamed(value, appLitFalse):
	default:
		t.maybe = true
	}
}

// collectTrust adds what the node says about the trusted proxy settings.
func (rf *reviewFile) collectTrust(n ast.Node, stack []ast.Node) {
	t := &rf.r.trust
	switch x := n.(type) {
	case *ast.ImportSpec:
		if path, err := strconv.Unquote(x.Path.Value); err == nil && reviewEarlyDataPath.MatchString(path) {
			t.early.first(rf, x.Pos())
		}
	case *ast.CompositeLit:
		rf.collectTrustLiteral(t, x)
	case *ast.AssignStmt:
		rf.collectTrustAssign(t, x, stack)
	case *ast.SelectorExpr:
		if x.Sel.Name == reviewTrustProxyConfig {
			t.list = true
		}
	case *ast.CallExpr:
		rf.collectTrustCall(t, x, stack)
	}
}

// collectTrustLiteral reads the trust settings of a fiber.Config literal.
func (rf *reviewFile) collectTrustLiteral(t *reviewTrust, lit *ast.CompositeLit) {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != reviewConfigType {
		return
	}
	if _, path, bound := rf.packageOf(sel.X); !bound || path != reviewCorePath {
		return
	}

	t.config.first(rf, lit.Pos())
	for _, elt := range lit.Elts {
		kv, isKV := elt.(*ast.KeyValueExpr)
		if !isKV {
			continue
		}
		switch {
		case isIdentNamed(kv.Key, reviewTrustProxy):
			t.setTrust(kv.Value)
		case isIdentNamed(kv.Key, reviewTrustProxyConfig):
			t.list = true
		case isIdentNamed(kv.Key, reviewProxyHeader):
			t.header = t.header || !appIsEmptyString(kv.Value)
		}
	}
}

// collectTrustAssign reads cfg.TrustProxy = ... and cfg.ProxyHeader = ... for a
// config value.
func (rf *reviewFile) collectTrustAssign(t *reviewTrust, as *ast.AssignStmt, stack []ast.Node) {
	for i, lhs := range as.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != reviewTrustProxy && sel.Sel.Name != reviewProxyHeader) || !rf.isConfigValue(sel.X, stack, 0) {
			continue
		}

		var value ast.Expr
		if len(as.Lhs) == len(as.Rhs) {
			value = as.Rhs[i]
		}
		switch {
		case sel.Sel.Name == reviewTrustProxy && value != nil:
			t.setTrust(value)
		case sel.Sel.Name == reviewTrustProxy:
			t.maybe = true
		case value == nil || !appIsEmptyString(value):
			t.header = true
		}
	}
}

// collectTrustCall notes the first fiber.New and the first read of the scheme or
// host of a request.
func (rf *reviewFile) collectTrustCall(t *reviewTrust, call *ast.CallExpr, stack []ast.Node) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if id, isIdent := sel.X.(*ast.Ident); isIdent && slices.Contains(rf.core, id.Name) && (sel.Sel.Name == mw2New || sel.Sel.Name == reviewNewFiberApp) {
		t.call.first(rf, call.Pos())
		if rf.isBareApp(call) {
			t.bare.first(rf, call.Pos())
		}
		return
	}
	if !t.read.known() && slices.Contains(reviewTrustReads, sel.Sel.Name) && rf.isCtxMethod(sel, stack) {
		t.read.first(rf, sel.Sel.Pos())
	}
}

// reviewTrustFields are the fields of the config that tell something about the trust.
var reviewTrustFields = []string{reviewTrustProxy, reviewTrustProxyConfig, appFieldEnableProxyCheck, appFieldTrustedProxies}

// isBareApp reports whether the app is created without a word about the trust: no config, or a literal of
// the config that sets none of the fields of the trust.
func (rf *reviewFile) isBareApp(call *ast.CallExpr) bool {
	switch len(call.Args) {
	case 0:
		return true
	case 1:
		lit, ok := call.Args[0].(*ast.CompositeLit)
		if !ok || !isFiberType(lit.Type, rf.core, reviewConfigType) {
			return false
		}
		for _, elt := range lit.Elts {
			if kv, isKV := elt.(*ast.KeyValueExpr); isKV {
				if key, isIdent := kv.Key.(*ast.Ident); isIdent && slices.Contains(reviewTrustFields, key.Name) {
					return false
				}
			}
		}
		return true
	}
	return false
}

// isConfigValue reports whether e is a fiber.Config: a literal of it, a variable
// declared as one or initialized with one, or what app.Config() returns.
func (rf *reviewFile) isConfigValue(e ast.Expr, stack []ast.Node, depth int) bool {
	if depth > reviewMaxDepth {
		return false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return rf.isConfigValue(x.X, stack, depth+1)
	case *ast.StarExpr:
		return rf.isConfigValue(x.X, stack, depth+1)
	case *ast.UnaryExpr:
		return x.Op == token.AND && rf.isConfigValue(x.X, stack, depth+1)
	case *ast.CompositeLit:
		return isFiberType(x.Type, rf.core, reviewConfigType)
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return isFiberType(d.typ, rf.core, reviewConfigType)
		}
		return d.init != nil && rf.isConfigValue(d.init, stack, depth+1)
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == reviewConfigType && rf.isRouterExpr(sel.X, stack, depth+1)
	}
	return false
}

// Text of the note about the trusted proxy settings.
const (
	reviewTrustIntro = "v2 trusted the X-Forwarded-* headers of every client unless EnableTrustedProxyCheck was on; " +
		"v3 ignores them unless TrustProxy is true and the peer is listed in TrustProxyConfig (Proxies, Loopback, Private, LinkLocal or UnixSocket)."
	reviewTrustNever = "No fiber.Config of this project sets TrustProxy."
	reviewTrustMaybe = "TrustProxy is set from an expression here: whenever it is false, v2 trusted every client and v3 trusts none."
	reviewTrustEmpty = "TrustProxy is true, but no TrustProxyConfig is set, so v3 trusts no peer."
	reviewTrustMixed = "Other apps of this project trust their proxies, the one created at %s does not."
)

// report leaves the note, once for the project, when the code depends on the
// trust and nothing configures it. The config migration notes a config that
// sets a ProxyHeader without trusting its proxies at the header already.
func (t *reviewTrust) report(root string) {
	if !t.read.known() && !t.early.known() {
		return
	}

	var cause string
	switch {
	case t.on && t.list && !t.bare.known():
		return
	case t.on && t.list:
		cause = fmt.Sprintf(reviewTrustMixed, t.bare)
	case t.on:
		cause = reviewTrustEmpty
	case t.header:
		return
	case t.maybe:
		cause = reviewTrustMaybe
	default:
		cause = reviewTrustNever
	}

	parts := []string{reviewTrustIntro, cause}
	if t.read.known() {
		parts = append(parts, "Behind a reverse proxy, c.Scheme(), c.Secure(), c.Host(), c.Hostname(), c.BaseURL() and c.Subdomains() (first read at "+t.read.String()+
			") now describe the connection to the proxy, not the client's request: X-Forwarded-Proto and X-Forwarded-Host are ignored.")
	}
	if t.early.known() {
		parts = append(parts, "The earlydata middleware (imported at "+t.early.String()+") answers 425 Too Early to every request with Early-Data: 1 unless the peer is trusted.")
	}

	at := t.config
	switch {
	case t.on && t.list:
		at = t.bare
	case t.config.known():
	case t.call.known():
		at = t.call
	case t.read.known():
		at = t.read
	default:
		at = t.early
	}
	internal.AddNote(root, internal.Note{File: at.file, Line: at.line, Message: strings.Join(parts, " ")})
}
