package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// contribJWTMentionRe finds the files that import the jwt middleware.
var contribJWTMentionRe = regexp.MustCompile(`github\.com/gofiber/(?:contrib/(?:v3/)?jwt|jwt)\b`)

// contribAuthorizationHeader is the header the Authorization extractors read.
const contribAuthorizationHeader = "Authorization"

// contribExtractor is a call of the extractors package: FromCookie("jwt").
type contribExtractor struct {
	fn  string
	arg string
}

// contribString is a string a config sets: the text of its expression and, when
// the expression is a constant the migration can evaluate, its value.
type contribString struct {
	text  string
	value string
	known bool
}

// contribHeaderExtractor returns the extractor of a header whose value carries
// the scheme in front of the token, and ok is false when no extractor of the
// extractors package reads such a header. Only the Authorization header has one
// that strips a scheme, and it takes whatever the scheme is; any other header can
// be read when there is no scheme.
func contribHeaderExtractor(name string, scheme contribString) (e contribExtractor, ok bool) {
	switch {
	case strings.EqualFold(name, contribAuthorizationHeader):
		return contribExtractor{fn: "FromAuthHeader", arg: scheme.text}, true
	case scheme.known && scheme.value == "":
		return contribExtractor{fn: "FromHeader", arg: strconv.Quote(name)}, true
	}
	return contribExtractor{}, false
}

// renderExtractors writes the extractors as one expression, importing the
// extractors package.
func (f *contribFile) renderExtractors(parts []contribExtractor) string {
	pkg := f.extractorsName()
	exprs := make([]string, len(parts))
	for i, p := range parts {
		exprs[i] = pkg + "." + p.fn + "(" + p.arg + ")"
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	return pkg + ".Chain(" + strings.Join(exprs, ", ") + ")"
}

// MigrateJWTExtractor moves the token lookup of the jwt middleware to the
// Extractor of v3, drops the fields that are gone (ContextKey), renames Filter to
// Next and moves the references to the error the middleware used to report for a
// missing token.
//
// v2 applied the AuthScheme to every header lookup, and defaulted it to Bearer
// only when TokenLookup was empty; the extractor repeats exactly that, so the
// credentials a client has to send do not change. A config that has no faithful
// extractor is left alone (it does not compile, which is the point) and reported.
func MigrateJWTExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateJWTContent)
	if err != nil {
		return fmt.Errorf("failed to migrate jwt extractor config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating jwt middleware configs")
	return nil
}

func migrateJWTContent(content string, fi internal.FileInfo) string {
	if !contribJWTMentionRe.MatchString(content) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	names := contribNames(g, "jwt")
	if len(names) == 0 {
		return content
	}

	f := newContribFile(g, fi)
	for _, c := range f.findConfigs(names, "Config") {
		f.jwtConfig(c)
	}
	f.jwtErrors(contribImportsOf(g, "jwt"))
	return f.apply()
}

// jwtConfig migrates one jwtware.Config.
func (f *contribFile) jwtConfig(c *contribConfig) {
	defer c.finish()

	// A config that has its Extractor is a v3 config already.
	if c.get("Extractor") != nil {
		return
	}

	parts, reason := f.jwtLookup(c)
	t, a := c.get("TokenLookup"), c.get("AuthScheme")
	switch {
	case reason != "":
		at := t
		if at == nil {
			at = a
		}
		f.notef(at.pos(), "jwt Config: %s; v3 reads the token with Config.Extractor instead of TokenLookup and AuthScheme "+
			"(extractors.FromAuthHeader, FromCookie, FromQuery, Chain, ...), set it by hand", reason)
	case len(parts) > 0 && t != nil:
		c.setExtractor("TokenLookup", f.renderExtractors(parts))
		c.remove("AuthScheme")
	case len(parts) > 0:
		c.setExtractor("AuthScheme", f.renderExtractors(parts))
	default:
		// The scheme is the one v3 defaults to.
		c.remove("AuthScheme")
	}

	c.remove("ContextKey")
	c.rename("Filter", "Next")
}

// jwtLookup works out the extractors a v2 config stands for. parts is empty when
// the default extractor of v3 does the same, reason says why there is no
// faithful translation.
func (f *contribFile) jwtLookup(c *contribConfig) (parts []contribExtractor, reason string) {
	t, a := c.get("TokenLookup"), c.get("AuthScheme")
	if !c.settled("TokenLookup", "AuthScheme") {
		return nil, "TokenLookup or AuthScheme is set conditionally"
	}

	scheme := contribString{text: `""`, known: true}
	if a != nil {
		scheme = f.stringOf(a.value, nil)
	}

	// Without a TokenLookup v2 reads the Authorization header, and falls back to
	// the Bearer scheme when there is no AuthScheme either.
	if t == nil {
		switch {
		case a == nil:
			return nil, ""
		case !scheme.known:
			return nil, "AuthScheme is not a string constant"
		case scheme.value == "" || strings.EqualFold(scheme.value, "Bearer"):
			return nil, ""
		}
		return []contribExtractor{{fn: "FromAuthHeader", arg: scheme.text}}, ""
	}

	lookup := f.stringOf(t.value, nil)
	if !lookup.known {
		return nil, "TokenLookup is not a string constant"
	}
	for root := range strings.SplitSeq(lookup.value, ",") {
		// v2 read the source and the name of "<source>:<name>" this way.
		src := strings.Split(strings.TrimSpace(root), ":")
		if len(src) < 2 {
			return nil, fmt.Sprintf("TokenLookup %q is not made of <source>:<name> parts", lookup.value)
		}
		name := strconv.Quote(src[1])

		switch src[0] {
		case contribSourceHeader:
			e, ok := contribHeaderExtractor(src[1], scheme)
			if !ok {
				return nil, fmt.Sprintf("v2 expected the AuthScheme (%s) in front of the token in the %s header, which no built-in extractor reads", scheme.text, src[1])
			}
			parts = append(parts, e)
		case contribSourceQuery:
			parts = append(parts, contribExtractor{fn: "FromQuery", arg: name})
		case contribSourceParam:
			parts = append(parts, contribExtractor{fn: "FromParam", arg: name})
		case contribSourceCookie:
			parts = append(parts, contribExtractor{fn: "FromCookie", arg: name})
		case contribSourceForm:
			parts = append(parts, contribExtractor{fn: "FromForm", arg: name})
		default:
			// v2 skipped the sources it did not know.
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Sprintf("TokenLookup %q names no source that v2 reads", lookup.value)
	}
	return parts, ""
}

// jwtErrors moves the references to jwtware.ErrJWTMissingOrMalformed. v2 gave
// that error to the ErrorHandler when the request carried no token; v3 gives it
// extractors.ErrNotFound, whatever the text of the error says. A comparison with
// the error follows it, any other use of the error wants its text, which
// ErrMissingToken has.
func (f *contribFile) jwtErrors(imports []contribImport) {
	names := make([]string, len(imports))
	messages := map[string]bool{}
	for i, imp := range imports {
		names[i] = imp.name
		// The two modules did not spell the message alike.
		if imp.legacy {
			messages["Missing or malformed JWT"] = true
		} else {
			messages["missing or malformed JWT"] = true
		}
	}

	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok || x.Sel.Name != "ErrJWTMissingOrMalformed" || !slices.Contains(names, id.Name) {
				return true
			}
			if contribComparesError(stack) {
				f.edits.replaceNode(f.g, x, f.extractorsName()+".ErrNotFound")
			} else {
				f.edits.replaceNode(f.g, x.Sel, "ErrMissingToken")
			}
		case *ast.BinaryExpr:
			f.jwtMessageComparison(x, messages)
		}
		return true
	})
}

// jwtMessageComparison rewrites err.Error() == "missing or malformed JWT", which
// v3 never makes true because the error it hands over says "value not found".
func (f *contribFile) jwtMessageComparison(x *ast.BinaryExpr, messages map[string]bool) {
	if x.Op != token.EQL && x.Op != token.NEQ {
		return
	}

	call, lit := x.X, x.Y
	if _, ok := contribStringLit(call); ok {
		call, lit = lit, call
	}
	msg, ok := contribStringLit(lit)
	if !ok || !messages[msg] {
		return
	}
	errCall, ok := call.(*ast.CallExpr)
	if !ok || len(errCall.Args) != 0 {
		return
	}
	sel, ok := errCall.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" {
		return
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}

	errorsPkg, ok := f.stdName("errors")
	if !ok {
		f.notef(x.Pos(), "the jwt ErrorHandler no longer receives an error that says %q (it is extractors.ErrNotFound); "+
			"compare with errors.Is(err, extractors.ErrNotFound)", msg)
		return
	}
	text := errorsPkg + ".Is(" + id.Name + ", " + f.extractorsName() + ".ErrNotFound)"
	if x.Op == token.NEQ {
		text = "!" + text
	}
	f.edits.replaceNode(f.g, x, text)
}

// contribComparesError reports whether the node on top of the stack is compared
// to an error: errors.Is(err, X), err == X, a case of a switch on the error, or
// the text of X compared to the text of another error.
func contribComparesError(stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}

	switch p := stack[len(stack)-1].(type) {
	case *ast.CallExpr:
		sel, ok := p.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == "errors" && sel.Sel.Name == "Is"
	case *ast.BinaryExpr:
		return p.Op == token.EQL || p.Op == token.NEQ
	case *ast.CaseClause:
		return true
	case *ast.SelectorExpr:
		// X.Error() as an operand of a comparison, or as a case of a switch.
		if p.Sel.Name != "Error" || len(stack) < 3 {
			return false
		}
		if _, ok := stack[len(stack)-2].(*ast.CallExpr); !ok {
			return false
		}
		switch gp := stack[len(stack)-3].(type) {
		case *ast.BinaryExpr:
			return gp.Op == token.EQL || gp.Op == token.NEQ
		case *ast.CaseClause:
			return true
		}
	}
	return false
}
