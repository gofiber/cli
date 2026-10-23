package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"net/url"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// contribJWTPaths are the paths the jwt middleware is imported by, the standalone module of v2
// included.
var contribJWTPaths = append(contribPathsOf("jwt"), contribLegacyJWTPath)

// contribAuthorizationHeader is the header the Authorization extractors read.
const contribAuthorizationHeader = "Authorization"

// contribExtractor is a call of the extractors package: FromCookie("jwt"), or,
// when strip is set, an extractors.FromCustom that takes a prefix off the value.
type contribExtractor struct {
	strip *contribStrip
	fn    string
	arg   string
}

// contribStrip describes the extractor of a source whose value carries a prefix
// in front of the token. Only the Authorization header has an extractor that
// takes such a prefix off, so any other lookup gets a function that does what v2
// did.
type contribStrip struct {
	source string
	name   string
	// prefix is what stands in front of the token, the space after it included.
	prefix string
	// fold is set when v2 matched the prefix without regard to case and trimmed
	// the token (the jwt middleware), and not when it matched it exactly (paseto).
	fold bool
}

// contribString is a string a config sets: the text of its expression and, when
// the expression is a constant the migration can evaluate, its value.
type contribString struct {
	text  string
	value string
	known bool
}

// The reasons a prefix in front of a token cannot be taken off by a function
// written into the config.
const (
	contribNotConstant = "which is only translated when it is a constant"
	contribStringsUsed = "but this file uses the name strings for something else, and the function that takes it off needs that package"
)

// contribHeaderExtractor returns the extractor of a header whose value carries
// the scheme in front of the token. reason says why there is none. The
// Authorization header has an extractor that takes any scheme off; any other
// header is read as it is when there is no scheme, and otherwise by a function
// that takes the scheme off the way v2 did, for which the scheme must be a
// constant. fold tells how v2 matched it.
func (f *contribFile) contribHeaderExtractor(name string, scheme contribString, fold bool) (e contribExtractor, reason string) {
	switch {
	case strings.EqualFold(name, contribAuthorizationHeader):
		return contribExtractor{fn: "FromAuthHeader", arg: scheme.text}, ""
	case !scheme.known:
		return contribExtractor{}, contribNotConstant
	case scheme.value == "":
		return contribExtractor{fn: nameFromHeader, arg: strconv.Quote(name)}, ""
	}
	return f.stripExtractor(contribSourceHeader, name, scheme.value, fold)
}

// stripExtractor returns the extractor that reads a source and takes the prefix
// and the space after it off the value; reason says why it cannot be written.
func (f *contribFile) stripExtractor(source, name, prefix string, fold bool) (e contribExtractor, reason string) {
	if fold && !f.hasStd(mw3StringsPkg) {
		return contribExtractor{}, contribStringsUsed
	}
	return contribExtractor{strip: &contribStrip{source: source, name: name, prefix: prefix + " ", fold: fold}}, ""
}

// renderExtractors writes the extractors as one expression, importing the
// extractors package.
func (f *contribFile) renderExtractors(parts []contribExtractor) string {
	exprs := make([]string, len(parts))
	for i, p := range parts {
		exprs[i] = f.renderExtractor(p)
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	return f.extractorsName() + ".Chain(" + strings.Join(exprs, ", ") + ")"
}

// renderExtractor writes one extractor of the extractors package: a call of one of
// its functions, or the function that takes a prefix off, written out.
func (f *contribFile) renderExtractor(e contribExtractor) string {
	pkg := f.extractorsName()
	if e.strip == nil {
		return pkg + "." + e.fn + "(" + e.arg + ")"
	}
	s := e.strip

	read := map[string]string{
		contribSourceHeader: appMethodGet, contribSourceCookie: "Cookies", contribSourceQuery: ctxFnQuery, contribSourceParam: "Params",
	}[s.source]
	name := strconv.Quote(s.name)

	// Both conditions are the ones of v2: paseto compared the prefix exactly, jwt
	// without regard to case and with a token of at least two characters.
	cond, result := "len(value) > len(prefix) && value[:len(prefix)] == prefix", "value[len(prefix):]"
	if s.fold {
		strs, _ := f.stdName(mw3StringsPkg)
		cond = "len(value) > len(prefix)+1 && " + strs + ".EqualFold(value[:len(prefix)], prefix)"
		result = strs + ".TrimSpace(value[len(prefix):])"
	}

	return strings.Join([]string{
		pkg + ".FromCustom(" + name + ", func(c " + f.fiberName() + ".Ctx) (string, error) {",
		"const prefix = " + strconv.Quote(s.prefix),
		"value := c." + read + "(" + name + ")",
		"if " + cond + " {",
		"return " + result + ", nil",
		"}",
		"return \"\", " + pkg + ".ErrNotFound",
		"})",
	}, "\n")
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
	if !mentionsPath(content, contribJWTPaths...) {
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
	for _, c := range f.findConfigs(names) {
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
		c.replace("TokenLookup", "Extractor", f.renderExtractors(parts))
		c.remove("AuthScheme")
	case len(parts) > 0:
		c.replace("AuthScheme", "Extractor", f.renderExtractors(parts))
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
			e, why := f.contribHeaderExtractor(src[1], scheme, true)
			if why != "" {
				return nil, fmt.Sprintf("v2 expected the AuthScheme (%s) in front of the token in the %s header, %s", scheme.text, src[1], why)
			}
			parts = append(parts, e)
		case contribSourceQuery:
			parts = append(parts, contribExtractor{fn: nameFromQuery, arg: name})
		case contribSourceParam:
			parts = append(parts, contribExtractor{fn: nameFromParam, arg: name})
		case contribSourceCookie:
			parts = append(parts, contribExtractor{fn: "FromCookie", arg: name})
		case contribSourceForm:
			parts = append(parts, contribExtractor{fn: nameFromForm, arg: name})
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

// jwtLegacyOnly are the settings of the standalone gofiber/jwt module that
// contrib/v3/jwt has no field for (key sets are JWKSetURLs there, and the
// refresh of the keys is not configurable), the key sets last: when one of the
// others goes with them, that is what the note is about.
var jwtLegacyOnly = []string{
	"SigningKeys", "KeyRefreshSuccessHandler", "KeyRefreshErrorHandler",
	"KeyRefreshInterval", "KeyRefreshRateLimit", "KeyRefreshTimeout", "KeyRefreshUnknownKID", "KeySetURL", nameKeySetURLs,
}

// jwtKeySetCompanions are the settings that change what a key set does in the
// standalone module and that contrib/v3/jwt has no counterpart for: it refreshes
// the key sets in one way (every hour and on a kid it has not seen), and its
// SigningKeys hold keys of another type that it uses next to the key sets.
var jwtKeySetCompanions = []string{
	"SigningKeys", "KeyRefreshSuccessHandler", "KeyRefreshErrorHandler",
	"KeyRefreshInterval", "KeyRefreshRateLimit", "KeyRefreshTimeout", "KeyRefreshUnknownKID",
}

// jwtLegacyAlgs are the algorithm constants of the standalone module.
var jwtLegacyAlgs = []string{
	"HS256", "HS384", "HS512", "ES256", "ES384", "ES512", "RS256", "RS384", "RS512", "PS256", "PS384", "PS512",
}

// jwtLegacy migrates what a file written for the standalone gofiber/jwt module
// needs besides the path of its import: the key of the config and the message of
// the error for a missing token. It runs while the import is moved, since
// afterwards nothing tells which module the file used. The JWT library of the
// project moves with MigrateJWTClaims, in every file that uses it.
func (f *contribFile) jwtLegacy() {
	var legacy []contribImport
	var names []string
	for _, imp := range contribImportsOf(f.g, "jwt") {
		if imp.legacy {
			legacy = append(legacy, imp)
			names = append(names, imp.name)
		}
	}
	if len(legacy) == 0 {
		return
	}

	for _, c := range f.findConfigs(names) {
		f.jwtLegacyConfig(c)
	}
	f.jwtErrors(legacy)
}

// jwtLegacyConfig moves the key of a config of the standalone gofiber/jwt module.
// It took the key itself and the algorithm next to it (HS256 when none was
// given); contrib/v3/jwt takes a SigningKey that holds both.
func (f *contribFile) jwtLegacyConfig(c *contribConfig) {
	defer c.finish()

	moved := f.jwtLegacyKeySets(c)
	for _, field := range jwtLegacyOnly {
		if field == moved {
			continue
		}
		if s := c.get(field); s != nil {
			f.notef(s.pos(), "jwt Config: %s of the standalone gofiber/jwt module has no counterpart in contrib/v3/jwt "+
				"(SigningKey takes a jwtware.SigningKey{Key: ..., JWTAlg: ...}, key sets are JWKSetURLs); move it by hand", field)
			return
		}
	}

	key, method := c.get("SigningKey"), c.get("SigningMethod")
	if key != nil {
		if lit, ok := key.value.(*ast.CompositeLit); !ok || lit.Type == nil || !contribIsPkgType(lit.Type, []string{c.pkg}, "SigningKey") {
			alg := strconv.Quote("HS256")
			if method != nil {
				alg = f.jwtLegacyAlg(method.value, c.pkg)
			}
			f.edits.replaceNode(f.g, key.value, c.pkg+".SigningKey{Key: "+f.g.text(key.value)+", JWTAlg: "+alg+"}")
		}
	}
	c.remove("SigningMethod")
}

// jwtLegacyKeySets writes the key sets of the standalone module as JWKSetURLs of
// contrib/v3/jwt and returns the field it moved, "" when it left them. The module
// appended KeySetURL to KeySetURLs, so
//
//	KeySetURLs: urls  ->  JWKSetURLs: urls
//	KeySetURL:  url   ->  JWKSetURLs: []string{url}
//
// where nothing next to the key sets changes what they do: the refresh settings
// and SigningKeys have no counterpart, and a URL that may be empty (the module
// ignored an empty KeySetURL and fell back to the key given next to it, where
// contrib/v3/jwt refuses an empty URL) or that cannot be a URL of v3 (absolute,
// http or https) is left for the user.
func (f *contribFile) jwtLegacyKeySets(c *contribConfig) string {
	one, many := c.get("KeySetURL"), c.get(nameKeySetURLs)
	if (one == nil) == (many == nil) {
		return ""
	}
	field, slot := "KeySetURL", one
	if one == nil {
		field, slot = nameKeySetURLs, many
	}
	if !c.settled(field) || slices.ContainsFunc(jwtKeySetCompanions, func(other string) bool { return c.get(other) != nil }) {
		return ""
	}

	if field == nameKeySetURLs {
		if lit, ok := slot.value.(*ast.CompositeLit); ok && !jwtAbsoluteURLs(lit) {
			return ""
		}
		c.rename(field, "JWKSetURLs")
	} else {
		if u, isLiteral := contribStringLit(slot.value); isLiteral {
			if !jwtAbsoluteURL(u) {
				return ""
			}
		} else if c.get("SigningKey") != nil || c.get("KeyFunc") != nil {
			return ""
		}
		c.replace(field, "JWKSetURLs", "[]string{"+f.g.text(slot.value)+"}")
	}

	f.notef(slot.pos(), "jwt Config: %s is JWKSetURLs in contrib/v3/jwt. The v3 module downloads the key sets when the middleware is created, so the application "+
		"stops at startup when one of them cannot be reached (the standalone module downloaded them with the first request), refreshes them every hour and "+
		"when a token names a kid it has not seen, and accepts absolute http and https URLs only", field)
	return field
}

// jwtAbsoluteURL reports whether u is a URL that contrib/v3/jwt accepts for a key set.
func jwtAbsoluteURL(u string) bool {
	parsed, err := url.Parse(u)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

// jwtAbsoluteURLs reports whether every URL written as a string literal in the
// list is one that contrib/v3/jwt accepts.
func jwtAbsoluteURLs(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		if u, isLiteral := contribStringLit(elt); isLiteral && !jwtAbsoluteURL(u) {
			return false
		}
	}
	return true
}

// jwtLegacyAlg writes the algorithm that SigningMethod names: the constants of the
// standalone module are strings now.
func (f *contribFile) jwtLegacyAlg(e ast.Expr, pkg string) string {
	if sel, ok := e.(*ast.SelectorExpr); ok && isIdentNamed(sel.X, pkg) && slices.Contains(jwtLegacyAlgs, sel.Sel.Name) {
		return strconv.Quote(sel.Sel.Name)
	}
	return f.g.text(e)
}
