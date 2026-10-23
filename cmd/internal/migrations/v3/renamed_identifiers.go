package v3

import (
	"fmt"
	"go/ast"
	"regexp"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	renameFiberPath       = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+$`)
	renameStringsPath     = regexp.MustCompile(`^strings$`)
	renameEncryptPath     = middlewarePath("encryptcookie")
	renameHealthcheckPath = middlewarePath("healthcheck")
	renameLimiterPath     = middlewarePath("limiter")
	renameProxyPath       = middlewarePath("proxy")
	// The monitor middleware moved to contrib in v3; the file has the import of v2 or the
	// one of v3, depending on whether the move was made yet.
	renameMonitorPath = regexp.MustCompile(`^github\.com/gofiber/(?:fiber/v\d+/middleware|contrib/v\d+)/monitor$`)
)

// renamedIdentifierHints are the texts a file holds when it may hold one of the identifiers
// that MigrateRenamedIdentifiers changes. The other files are not parsed.
var renamedIdentifierHints = []string{
	"ConstraintGuid", "HeaderFeaturePolicy", "IndexRune", "GenerateKey(", "DefaultLivenessEndpoint",
	"DefaultReadinessEndpoint", "HealthChecker", "LimiterHandler", "FixedWindow", "SlidingWindow",
	"ChartJsURL", "TlsConfig", ".Format(",
}

// MigrateRenamedIdentifiers changes the identifiers of Fiber's packages that v3 renamed or
// removed, where the new spelling is mechanical. Each is found through the import of its
// package (or the type of the context), so a name that merely looks alike is left alone:
//
//   - fiber.ConstraintGuid is fiber.ConstraintGUID, fiber.HeaderFeaturePolicy is the header
//     it named, "Feature-Policy", and fiber.IndexRune(s, r) is strings.ContainsRune(s, r).
//   - encryptcookie.GenerateKey() is GenerateKey(32), the length v2 always used (v3 panics
//     for a length other than 16, 24 or 32 and has no default).
//   - healthcheck.DefaultLivenessEndpoint and DefaultReadinessEndpoint lost their Default,
//     and the types HealthChecker and HealthCheckerHandler are the function types they
//     were: func(fiber.Ctx) bool and func(func(fiber.Ctx) bool) fiber.Handler.
//   - limiter.LimiterHandler is limiter.Handler, and the New of the fixed and the sliding
//     window takes a *limiter.Config.
//   - the ChartJsURL of the monitor config is ChartJSURL, and the TlsConfig of the proxy
//     config is TLSConfig.
//   - c.Format(body) of a context, which negotiated the content type of the body in v2, is
//     c.AutoFormat(body): Format takes ResFmt handlers in v3.
func MigrateRenamedIdentifiers(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, migrateRenamedIdentifiersContent)
	if err != nil {
		return fmt.Errorf("failed to migrate renamed identifiers: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating identifiers that v3 renamed")
	return nil
}

func migrateRenamedIdentifiersContent(content string) string {
	if !slices.ContainsFunc(renamedIdentifierHints, func(hint string) bool { return strings.Contains(content, hint) }) {
		return content
	}
	src, ok := parseForEdits(content)
	if !ok {
		return content
	}

	r := &identifierRenamer{
		src:         src,
		content:     content,
		fiber:       src.importName(renameFiberPath),
		strings:     src.importName(renameStringsPath),
		encrypt:     src.importName(renameEncryptPath),
		healthcheck: src.importName(renameHealthcheckPath),
		limiter:     src.importName(renameLimiterPath),
		proxy:       src.importName(renameProxyPath),
		monitor:     src.importName(renameMonitorPath),
	}
	ast.Inspect(src.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			r.selector(n)
		case *ast.CallExpr:
			r.call(n)
		case *ast.CompositeLit:
			r.literal(n)
		}
		return true
	})

	out := src.apply()
	if r.needsStrings && out != content {
		out = addImport(out, "strings")
	}
	return out
}

// identifierRenamer holds the names a file gives the packages whose identifiers are
// changed, "" for a package the file does not import.
type identifierRenamer struct {
	src     *sourceEdits
	content string

	fiber, strings, encrypt, healthcheck, limiter, proxy, monitor string

	// needsStrings says a call was turned into one of strings, which the file does not import.
	needsStrings bool
}

// is reports whether e names the package the file knows as name.
func (r *identifierRenamer) is(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && name != "" && id.Name == name
}

// selector changes pkg.Name where the package renamed or removed it.
func (r *identifierRenamer) selector(sel *ast.SelectorExpr) {
	switch {
	case r.is(sel.X, r.fiber):
		switch sel.Sel.Name {
		case "ConstraintGuid":
			r.src.replace(sel.Sel, "ConstraintGUID")
		case "HeaderFeaturePolicy":
			r.src.replace(sel, `"Feature-Policy"`)
		}
	case r.is(sel.X, r.healthcheck):
		switch sel.Sel.Name {
		case "DefaultLivenessEndpoint":
			r.src.replace(sel.Sel, "LivenessEndpoint")
		case "DefaultReadinessEndpoint":
			r.src.replace(sel.Sel, "ReadinessEndpoint")
		case "HealthChecker":
			if r.fiber != "" {
				r.src.replace(sel, "func("+r.fiber+".Ctx) bool")
			}
		case "HealthCheckerHandler":
			if r.fiber != "" {
				r.src.replace(sel, "func(func("+r.fiber+".Ctx) bool) "+r.fiber+".Handler")
			}
		}
	case r.is(sel.X, r.limiter):
		if sel.Sel.Name == "LimiterHandler" {
			r.src.replace(sel.Sel, "Handler")
		}
	}
}

// call changes the calls that v3 changed the name or the arguments of.
func (r *identifierRenamer) call(call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	switch {
	case r.is(sel.X, r.fiber) && sel.Sel.Name == "IndexRune":
		name := r.strings
		if name == "" {
			name = "strings"
			r.needsStrings = true
		}
		r.src.replace(sel, name+".ContainsRune")
	case r.is(sel.X, r.encrypt) && sel.Sel.Name == "GenerateKey" && len(call.Args) == 0:
		r.src.insertAt(call.Rparen, "32")
	case sel.Sel.Name == "New" && len(call.Args) == 1 && r.isWindow(sel.X):
		switch call.Args[0].(type) {
		case *ast.Ident, *ast.SelectorExpr, *ast.CompositeLit:
			r.src.insertAt(call.Args[0].Pos(), "&")
		}
	case sel.Sel.Name == "Format" && len(call.Args) == 1 && r.isCtx(sel.X) && !isResFmt(call.Args[0]):
		r.src.replace(sel.Sel, "AutoFormat")
	}
}

// isWindow reports whether e is a limiter.FixedWindow{} or limiter.SlidingWindow{} literal.
func (r *identifierRenamer) isWindow(e ast.Expr) bool {
	lit, ok := e.(*ast.CompositeLit)
	if !ok || len(lit.Elts) != 0 {
		return false
	}
	typ, ok := lit.Type.(*ast.SelectorExpr)
	return ok && r.is(typ.X, r.limiter) && (typ.Sel.Name == "FixedWindow" || typ.Sel.Name == "SlidingWindow")
}

// isCtx reports whether e is, or is called on, a fiber.Ctx of the file.
func (r *identifierRenamer) isCtx(e ast.Expr) bool {
	ident := GetBaseIdent(e)
	return ident != nil && isFiberCtx(r.content, ident.Name)
}

// isResFmt reports whether e is a ResFmt of v3, the argument of its Format.
func isResFmt(e ast.Expr) bool {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	switch t := lit.Type.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "ResFmt"
	case *ast.Ident:
		return t.Name == "ResFmt"
	default:
		return false
	}
}

// literal renames the fields of the monitor and the proxy config that v3 spells differently.
func (r *identifierRenamer) literal(lit *ast.CompositeLit) {
	typ, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || typ.Sel.Name != "Config" {
		return
	}

	var from, to string
	switch {
	case r.is(typ.X, r.monitor):
		from, to = "ChartJsURL", "ChartJSURL"
	case r.is(typ.X, r.proxy):
		from, to = "TlsConfig", "TLSConfig"
	default:
		return
	}

	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == from {
				r.src.replace(key, to)
			}
		}
	}
}
