package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// contribPasetoMentionRe finds the files that import the paseto middleware.
var contribPasetoMentionRe = regexp.MustCompile(`github\.com/gofiber/contrib/(?:v3/)?paseto\b`)

// The sources a token is read from, as v2 spelled them.
const (
	contribSourceHeader = "header"
	contribSourceCookie = "cookie"
	contribSourceQuery  = "query"
	contribSourceParam  = "param"
	contribSourceForm   = "form"
)

// pasetoLookupConsts are the constants of the v2 paseto package that name the
// source of a token.
var pasetoLookupConsts = map[string]string{
	"LookupHeader": contribSourceHeader,
	"LookupCookie": contribSourceCookie,
	"LookupQuery":  contribSourceQuery,
	"LookupParam":  contribSourceParam,
}

// fiberHeaderConsts are the header constants a token lookup is written with.
var fiberHeaderConsts = map[string]string{
	"HeaderAuthorization":      contribAuthorizationHeader,
	"HeaderProxyAuthorization": "Proxy-Authorization",
}

// MigratePasetoExtractor moves the token lookup of the paseto middleware to the
// Extractor of v3 and drops the fields that are gone (ContextKey).
//
// The extractor repeats what the v2 middleware did: a lookup without a prefix
// reads the bare value (the default of v2 was the bare token in the
// Authorization header, the default of v3 wants "Bearer <token>"), and a prefix
// is expected in front of the token wherever it is read. A config that has no
// faithful extractor is left alone (it does not compile, which is the point) and
// reported.
func MigratePasetoExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migratePasetoContent)
	if err != nil {
		return fmt.Errorf("failed to migrate paseto extractor config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating paseto middleware configs")
	return nil
}

func migratePasetoContent(content string, fi internal.FileInfo) string {
	if !contribPasetoMentionRe.MatchString(content) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	names := contribNames(g, "paseto")
	if len(names) == 0 {
		return content
	}

	f := newContribFile(g, fi)
	for _, c := range f.findConfigs(names) {
		f.pasetoConfig(c, names)
	}
	f.pasetoErrors(names)
	return f.apply()
}

// pasetoConfig migrates one pasetoware.Config.
func (f *contribFile) pasetoConfig(c *contribConfig, names []string) {
	defer c.finish()

	// A config that has its Extractor is a v3 config already.
	if c.get("Extractor") != nil {
		return
	}

	t, p := c.get("TokenLookup"), c.get("TokenPrefix")
	parts, reason := f.pasetoLookup(c, names)
	switch {
	case reason != "":
		at := t
		if at == nil {
			at = p
		}
		f.notef(at.pos(), "paseto Config: %s; v3 reads the token with Config.Extractor instead of TokenLookup and TokenPrefix "+
			"(extractors.FromAuthHeader, FromCookie, FromQuery, ...), set it by hand", reason)
	case t != nil:
		c.replace("TokenLookup", "Extractor", f.renderExtractors(parts))
		c.remove("TokenPrefix")
	case p != nil:
		c.replace("TokenPrefix", "Extractor", f.renderExtractors(parts))
	default:
		// v2 took the bare token of the Authorization header here, v3 wants a
		// Bearer scheme in front of it.
		c.add("Extractor", f.renderExtractors(parts))
	}

	c.remove("ContextKey")
}

// pasetoErrors moves the references to pasetoware.ErrIncorrectTokenPrefix, which v3
// removed: a token without the prefix is a missing token there, and the
// ErrorHandler receives ErrMissingToken for it.
func (f *contribFile) pasetoErrors(names []string) {
	reported := false
	f.g.walk(func(n ast.Node, _ []ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "ErrIncorrectTokenPrefix" {
			return true
		}
		if id, isIdent := sel.X.(*ast.Ident); !isIdent || !slices.Contains(names, id.Name) {
			return true
		}

		f.edits.replaceNode(f.g, sel.Sel, "ErrMissingToken")
		if !reported {
			reported = true
			f.notef(sel.Pos(), "ErrIncorrectTokenPrefix does not exist in the v3 paseto module: a token without the prefix is reported as ErrMissingToken, which this refers to now")
		}
		return true
	})
}

// pasetoLookup works out the extractor a v2 config stands for; reason says why
// there is no faithful translation.
func (f *contribFile) pasetoLookup(c *contribConfig, names []string) (parts []contribExtractor, reason string) {
	t, p := c.get("TokenLookup"), c.get("TokenPrefix")
	if !c.settled("TokenLookup", "TokenPrefix") {
		return nil, "TokenLookup or TokenPrefix is set conditionally"
	}

	prefix := contribString{text: `""`, known: true}
	if p != nil {
		prefix = f.stringOf(p.value, names)
	}

	source, name := contribSourceHeader, contribAuthorizationHeader
	if t != nil {
		var ok bool
		if source, name, ok = f.pasetoSource(t.value, names); !ok {
			return nil, "TokenLookup is not an array of string constants"
		}
	}

	var plain contribExtractor
	switch source {
	case contribSourceQuery:
		plain = contribExtractor{fn: nameFromQuery, arg: strconv.Quote(name)}
	case contribSourceParam:
		plain = contribExtractor{fn: nameFromParam, arg: strconv.Quote(name)}
	case contribSourceCookie:
		plain = contribExtractor{fn: "FromCookie", arg: strconv.Quote(name)}
	default:
		// v2 read every source it did not know from a header.
		e, why := f.contribHeaderExtractor(name, prefix, false)
		if why != "" {
			return nil, fmt.Sprintf("v2 expected the TokenPrefix (%s) in front of the token in the %s header, %s", prefix.text, name, why)
		}
		return []contribExtractor{e}, ""
	}

	switch {
	case !prefix.known:
		return nil, fmt.Sprintf("v2 expected the TokenPrefix (%s) in front of the token in the %s %s, %s", prefix.text, source, name, contribNotConstant)
	case prefix.value == "":
		return []contribExtractor{plain}, ""
	}
	e, why := f.stripExtractor(source, name, prefix.value, false)
	if why != "" {
		return nil, fmt.Sprintf("v2 expected the TokenPrefix (%s) in front of the token in the %s %s, %s", prefix.text, source, name, why)
	}
	return []contribExtractor{e}, ""
}

// pasetoSource reads the [2]string{source, name} of a TokenLookup, with the
// defaults of v2 for the parts that are empty.
func (f *contribFile) pasetoSource(e ast.Expr, names []string) (source, name string, ok bool) {
	lit, isLit := e.(*ast.CompositeLit)
	if !isLit || len(lit.Elts) > 2 {
		return "", "", false
	}

	var values [2]string
	for i, elt := range lit.Elts {
		if _, keyed := elt.(*ast.KeyValueExpr); keyed {
			return "", "", false
		}
		v, ok := f.constString(elt, names)
		if !ok {
			return "", "", false
		}
		values[i] = v
	}

	source, name = values[0], values[1]
	if source == "" {
		source = contribSourceHeader
	}
	if name == "" {
		name = contribAuthorizationHeader
	}
	return source, name, true
}

// stringOf describes a string the config sets.
func (f *contribFile) stringOf(e ast.Expr, pasetoNames []string) contribString {
	v, ok := f.constString(e, pasetoNames)
	return contribString{text: f.g.text(e), value: v, known: ok}
}

// constString evaluates the string constants that token lookups are written with:
// literals, their concatenation, the Lookup constants of the paseto package, the
// Authorization header constant of fiber and constants of the file.
func (f *contribFile) constString(e ast.Expr, pasetoNames []string) (string, bool) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.constString(x.X, pasetoNames)
	case *ast.BasicLit:
		return contribStringLit(x)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		left, ok := f.constString(x.X, pasetoNames)
		if !ok {
			return "", false
		}
		right, ok := f.constString(x.Y, pasetoNames)
		return left + right, ok
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		if slices.Contains(pasetoNames, id.Name) {
			v, ok := pasetoLookupConsts[x.Sel.Name]
			return v, ok
		}
		if slices.Contains(fiberNames(f.g.src), id.Name) {
			v, ok := fiberHeaderConsts[x.Sel.Name]
			return v, ok
		}
	case *ast.Ident:
		return f.packageConst(x.Name)
	}
	return "", false
}

// packageConst returns the value of a string constant declared at the top level
// of the file.
func (f *contribFile) packageConst(name string) (string, bool) {
	for _, d := range f.g.file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			if i := slices.IndexFunc(vs.Names, func(id *ast.Ident) bool { return id.Name == name }); i >= 0 {
				return contribStringLit(vs.Values[i])
			}
		}
	}
	return "", false
}
