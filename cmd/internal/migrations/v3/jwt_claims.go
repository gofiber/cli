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

const (
	// jwtV5Path is the JWT library contrib/v3/jwt builds on: its tokens and claims
	// are the types of this package.
	jwtV5Path = "github.com/golang-jwt/jwt/v5"
	// jwtMiddlewarePath is the jwt middleware of v3.
	jwtMiddlewarePath = "github.com/gofiber/contrib/v3/jwt"

	jwtStandardClaims   = "StandardClaims"
	jwtRegisteredClaims = "RegisteredClaims"
	jwtPackageName      = "jwt"
	jwtRequestPackage   = "request"
	jwtParserType       = "Parser"
	jwtValidMethod      = "Valid"
)

// jwtClaimsMentionRe finds the files that can import one of the libraries.
var jwtClaimsMentionRe = regexp.MustCompile(`form3tech-oss/jwt-go|dgrijalva/jwt-go|golang-jwt/jwt`)

// jwtLib is a JWT library that the code of a Fiber v2 project uses for the tokens
// and the claims the jwt middleware hands over.
type jwtLib struct {
	// root is the import path of the library.
	root string
	// audienceList is set when the Audience of its StandardClaims is a []string
	// and not a string.
	audienceList bool
}

// jwtLibs are the libraries that moved to golang-jwt/jwt/v5, the most specific
// path first.
var jwtLibs = []jwtLib{
	{root: "github.com/golang-jwt/jwt/v4"},
	{root: "github.com/golang-jwt/jwt"},
	{root: "github.com/form3tech-oss/jwt-go", audienceList: true},
	{root: "github.com/dgrijalva/jwt-go"},
	{root: "gopkg.in/dgrijalva/jwt-go.v3"},
}

// jwtLibOf returns the library an import path belongs to and the rest of the path
// below its root.
func jwtLibOf(path string) (lib jwtLib, sub string, ok bool) {
	for _, l := range jwtLibs {
		rest, found := strings.CutPrefix(path, l.root)
		switch {
		case !found:
			continue
		case rest == "":
			return l, "", true
		case rest[0] != '/':
			continue
		}
		// github.com/golang-jwt/jwt/v4 and /v5 are modules of their own.
		if rest = rest[1:]; hasVersionPrefix(rest) {
			continue
		}
		return l, rest, true
	}
	return jwtLib{}, "", false
}

// jwtV5Of returns the import path in v5 of a package of a library, "" for one that
// v5 does not have.
func jwtV5Of(sub string) string {
	switch sub {
	case "":
		return jwtV5Path
	case jwtRequestPackage:
		return jwtV5Path + "/" + sub
	}
	return ""
}

// MigrateJWTClaims moves the JWT library the code of a project uses to the one the
// jwt middleware of v3 builds on.
//
// The standalone gofiber/jwt middleware handed over the tokens of form3tech-oss/jwt-go
// (v2.2.1 and before) or of golang-jwt/jwt v4, and the project's own code, the
// handlers that read c.Locals("user") and the code that signs the tokens, imports
// that library. contrib/v3/jwt hands over golang-jwt/jwt/v5 (jwtware.FromContext(c)),
// so once the middleware has moved every file of the module that uses the old
// library has to use v5: a token or the claims of one library are no value of the
// other. The files need not import the middleware or Fiber.
//
// The import moves with its name, jwt.StandardClaims is jwt.RegisteredClaims (the
// times of a literal wrapped in jwt.NewNumericDate, Audience a jwt.ClaimStrings,
// Id called ID), jwt.Parser{...} literals become jwt.NewParser(options) and a
// Valid() method of a claims type becomes Validate(), which v5 calls. What v5 took
// away (ValidationError, TimeFunc, the Verify methods of the claims) has no
// rewrite that keeps the code working; each use is reported with what to write.
func MigrateJWTClaims(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := &jwtClaimsRun{byRoot: map[string]*jwtModule{}}
	changed, err := internal.ChangeFileContentInfo(cwd, run.rewrite)
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate the jwt library: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Moving the jwt library of the project to golang-jwt/jwt/v5")
	return nil
}

// jwtClaimsRun is one run of MigrateJWTClaims.
type jwtClaimsRun struct {
	indexes moduleIndexes
	byRoot  map[string]*jwtModule
}

// jwtModule is what the run knows of one Go module.
type jwtModule struct {
	// first is the first file moved, and line its first import.
	first internal.FileInfo
	// libs are the libraries the files used, in the order they were found.
	libs  []string
	line  int
	files int
	// moved is set when the module imports the jwt middleware of v3, and embeds
	// when a file that imports one of the libraries mentions its StandardClaims,
	// which a struct can embed: both are decided the first time a file of the
	// module is looked at, before any is rewritten.
	moved    bool
	embeds   bool
	resolved bool
}

// module returns what the run knows of the module the file is in. A file that is
// in no module stands alone.
func (r *jwtClaimsRun) module(fi internal.FileInfo, g *goSource) *jwtModule {
	root := fi.Path
	mod, inModule := moduleOf(fi.Path, fi.Root)
	if inModule {
		root = mod.root
	}

	m := r.byRoot[root]
	if m == nil {
		m = &jwtModule{}
		r.byRoot[root] = m
	}
	if m.resolved {
		return m
	}

	m.resolved = true
	if !inModule {
		m.moved = jwtImportsPath(g.file, jwtMiddlewarePath)
		m.embeds = jwtImportsOfSource(g.file) && strings.Contains(g.src, jwtStandardClaims)
		return m
	}
	ix := r.indexes.of(fi)
	m.moved = len(ix.importers(jwtMiddlewarePath)) > 0
	if !m.moved {
		return m
	}
	for _, lib := range jwtLibs {
		for _, f := range ix.importers(lib.root) {
			m.embeds = m.embeds || strings.Contains(f.g.src, jwtStandardClaims)
		}
	}
	return m
}

// jwtImportsOfSource reports whether the file imports one of the libraries.
func jwtImportsOfSource(file *ast.File) bool {
	return len(jwtImportsOf(file)) > 0
}

// jwtImportsPath reports whether the file imports the package.
func jwtImportsPath(file *ast.File, path string) bool {
	return slices.ContainsFunc(file.Imports, func(spec *ast.ImportSpec) bool {
		return contribUnquote(spec.Path.Value) == path
	})
}

// jwtImport is an import of a package of one of the libraries.
type jwtImport struct {
	spec *ast.ImportSpec
	// target is the import path in v5, "" for a package v5 does not have.
	target string
	// name is the identifier the file binds the package to.
	name string
	lib  jwtLib
	// base is set for the package of the library itself and not a sub-package.
	base bool
}

// jwtImportsOf lists the imports of the file that belong to one of the libraries.
func jwtImportsOf(file *ast.File) []jwtImport {
	var imports []jwtImport
	for _, spec := range file.Imports {
		path := contribUnquote(spec.Path.Value)
		lib, sub, ok := jwtLibOf(path)
		if !ok {
			continue
		}

		imp := jwtImport{spec: spec, lib: lib, target: jwtV5Of(sub), base: sub == ""}
		switch {
		case spec.Name != nil:
			imp.name = spec.Name.Name
		case sub == "":
			imp.name = jwtPackageName
		default:
			imp.name = defaultImportName(path)
		}
		imports = append(imports, imp)
	}
	return imports
}

// rewrite moves one file.
func (r *jwtClaimsRun) rewrite(content string, fi internal.FileInfo) string {
	if !jwtClaimsMentionRe.MatchString(content) && !strings.Contains(content, jwtStandardClaims) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	imports := jwtImportsOf(g.file)
	if len(imports) == 0 && !strings.Contains(content, jwtStandardClaims) {
		return content
	}
	mod := r.module(fi, g)
	if !mod.moved || (len(imports) == 0 && !mod.embeds) {
		return content
	}

	p := &jwtClaimsPass{fi: fi, g: g, mod: mod, imports: imports}
	p.run()
	out := p.edits.apply(content)
	if out == content {
		return content
	}

	// A file that only follows the rename of an embedded field has no import to count.
	if len(imports) > 0 {
		mod.files++
		if mod.files == 1 {
			mod.first, mod.line = fi, g.line(imports[0].spec.Pos())
		}
	}
	for _, imp := range imports {
		if !slices.Contains(mod.libs, imp.lib.root) {
			mod.libs = append(mod.libs, imp.lib.root)
		}
	}
	return out
}

// report says once per module what moved: the libraries, the number of files and
// what differs at run time between the libraries that no rewrite can keep.
func (r *jwtClaimsRun) report(cwd string) {
	var modules []*jwtModule
	for _, m := range r.byRoot {
		if m.files > 0 {
			modules = append(modules, m)
		}
	}
	slices.SortFunc(modules, func(a, b *jwtModule) int { return strings.Compare(a.first.Rel, b.first.Rel) })

	for _, m := range modules {
		files := "1 file"
		if m.files > 1 {
			files = strconv.Itoa(m.files) + " files"
		}
		internal.AddNote(cwd, internal.Note{
			File: m.first.Rel,
			Line: m.line,
			Message: fmt.Sprintf("the jwt library of this module (%s) moved to %s in %s, because the jwt middleware of v3 (contrib/v3/jwt) "+
				"hands over the tokens and the claims of v5: c.Locals(\"user\") became jwtware.FromContext(c), and the tokens and "+
				"claims of one library are no values of the other. Differences of v5 that no rewrite can keep: the texts of the errors "+
				"of Parse and ParseWithClaims (v4: \"Token is expired\", v5: \"token has invalid claims: token is expired\"), and a "+
				"token whose iat lies in the future is accepted unless the parser is given jwt.WithIssuedAt().",
				strings.Join(m.libs, ", "), jwtV5Path, files),
		})
	}
}

// jwtClaimsPass is the rewrite of one file.
type jwtClaimsPass struct {
	g   *goSource
	mod *jwtModule
	// said holds the reports made already, by kind.
	said    map[string]bool
	imports []jwtImport
	fi      internal.FileInfo
	edits   editSet
}

// run moves the imports and rewrites the use of the packages.
func (p *jwtClaimsPass) run() {
	for _, imp := range p.imports {
		if imp.target == "" {
			p.notef(imp.spec.Pos(), "%s has no counterpart in %s; this import stays on the old library, whose tokens are not those "+
				"of the jwt middleware of v3", contribUnquote(imp.spec.Path.Value), jwtV5Path)
			continue
		}
		p.edits.replaceNode(p.g, imp.spec.Path, strconv.Quote(imp.target))
	}

	libs := map[string]jwtLib{}
	for _, imp := range p.imports {
		switch {
		case !imp.base || imp.target == "" || imp.name == "_":
		case imp.name == ".":
			p.notef(imp.spec.Pos(), "this dot import of the jwt library moved to %s; the names it brought (StandardClaims, "+
				"ValidationError, ...) are not rewritten, see the note about the library", jwtV5Path)
		default:
			libs[imp.name] = imp.lib
		}
	}
	if len(libs) == 0 && !p.mod.embeds {
		return
	}

	p.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			p.selector(x, libs, stack)
		case *ast.CompositeLit:
			p.literal(x, libs, stack)
		case *ast.KeyValueExpr:
			p.key(x, libs, stack)
		case *ast.FuncDecl:
			p.validMethod(x, libs)
		}
		return true
	})
}

func (p *jwtClaimsPass) notef(pos token.Pos, format string, args ...any) {
	internal.Notef(p.fi, p.g.line(pos), format, args...)
}

// notefOncef reports a kind of construct at its first place in the file.
func (p *jwtClaimsPass) notefOncef(kind string, pos token.Pos, format string, args ...any) {
	if p.said[kind] {
		return
	}
	if p.said == nil {
		p.said = map[string]bool{}
	}
	p.said[kind] = true
	p.notef(pos, format, args...)
}

// libOf returns the library the selector's base is the package of, when the name
// is not hidden by a declaration at that place.
func libOf(sel *ast.SelectorExpr, libs map[string]jwtLib, stack []ast.Node) (name string, lib jwtLib, ok bool) {
	id, isID := sel.X.(*ast.Ident)
	if !isID {
		return "", jwtLib{}, false
	}
	lib, ok = libs[id.Name]
	if !ok {
		return "", jwtLib{}, false
	}
	if _, local := resolveIdent(id.Name, id.Pos(), stack); local {
		return "", jwtLib{}, false
	}
	return id.Name, lib, true
}

// jwtRemoved are the names v5 does not have, with what the code uses instead.
var jwtRemoved = map[string]string{
	"TimeFunc": "jwt.TimeFunc is gone in v5: give the parser the clock, jwt.Parse(token, keyFunc, jwt.WithTimeFunc(now)) " +
		"or jwt.NewParser(jwt.WithTimeFunc(now))",
	"DecodeSegment":        "jwt.DecodeSegment is a method of the parser in v5: jwt.NewParser().DecodeSegment(seg)",
	"EncodeSegment":        "jwt.EncodeSegment is a method of the token in v5: token.EncodeSegment(seg)",
	"DecodeStrict":         "jwt.DecodeStrict is the parser option jwt.WithStrictDecoding() in v5",
	"DecodePaddingAllowed": "jwt.DecodePaddingAllowed is the parser option jwt.WithPaddingAllowed() in v5",
}

// jwtValidationErrorAdvice says how the errors of the parser are told apart in v5.
const jwtValidationErrorAdvice = "v5 has no jwt.ValidationError and no ValidationError... flags: the errors of the parser wrap " +
	"jwt.ErrTokenMalformed, ErrTokenUnverifiable, ErrTokenSignatureInvalid, ErrTokenExpired, ErrTokenNotValidYet, ErrTokenUsedBeforeIssued, " +
	"ErrTokenInvalidAudience, ErrTokenInvalidIssuer, ErrTokenInvalidId and ErrTokenInvalidClaims, so ve.Errors&jwt.ValidationErrorExpired != 0 " +
	"is errors.Is(err, jwt.ErrTokenExpired)"

// jwtChangedFields are the fields of the standard claims whose type or name changed
// in RegisteredClaims.
var jwtChangedFields = map[string]bool{"ExpiresAt": true, "IssuedAt": true, "NotBefore": true, "Audience": true, "Id": true}

// jwtVerifyMethods are the methods of the claims that v5 took away.
var jwtVerifyMethods = []string{"VerifyExpiresAt", "VerifyIssuedAt", "VerifyNotBefore", "VerifyAudience", "VerifyIssuer"}

// selector rewrites a use of the library through a selector.
func (p *jwtClaimsPass) selector(sel *ast.SelectorExpr, libs map[string]jwtLib, stack []ast.Node) {
	name := sel.Sel.Name
	if _, _, pkg := libOf(sel, libs, stack); pkg {
		switch {
		case name == jwtStandardClaims:
			p.edits.replaceNode(p.g, sel.Sel, jwtRegisteredClaims)
			p.embedded(sel, stack)
		case strings.HasPrefix(name, "ValidationError") || name == "NewValidationError":
			p.notefOncef("validation", sel.Pos(), "%s", jwtValidationErrorAdvice)
		default:
			if advice, removed := jwtRemoved[name]; removed {
				p.notefOncef(name, sel.Pos(), "%s", advice)
			}
		}
		return
	}

	switch {
	case name == jwtStandardClaims && p.mod.embeds:
		// The embedded field of a claims struct is called after its type.
		p.edits.replaceNode(p.g, sel.Sel, jwtRegisteredClaims)
	case jwtChangedFields[name] && len(libs) > 0 && p.mod.embeds && strings.Contains(strings.ToLower(p.g.text(sel.X)), "claim"):
		p.notefOncef("fields", sel.Pos(), "%s: in v5 the times of the claims (ExpiresAt, IssuedAt, NotBefore) are *jwt.NumericDate, nil for none, "+
			"with the time in .Time and Unix() on it (v4: int64 seconds, 0 for none), Audience is a jwt.ClaimStrings and Id is called ID", p.g.text(sel))
	case name == jwtValidMethod && len(libs) > 0 && p.tokenClaimsValid(sel, libs, stack):
	case name == jwtValidMethod && len(libs) > 0 && jwtIsCalled(sel, stack) && strings.Contains(strings.ToLower(p.g.text(sel.X)), "claim"):
		p.notefOncef("claims-valid", sel.Pos(), "%s: v5 has no Valid() on MapClaims and RegisteredClaims, and the claims types of this module that had "+
			"one call it Validate() now: use claims.Validate() for a type that has it, or jwt.NewValidator(jwt.WithIssuedAt()).Validate(claims)", p.g.text(sel))
	case slices.Contains(jwtVerifyMethods, name) && jwtIsCalled(sel, stack) && len(libs) > 0:
		p.notefOncef("verify", sel.Pos(), "%s: v5 took the Verify methods of the claims away; the parser checks exp, nbf and (with "+
			"jwt.WithIssuedAt()) iat, jwt.WithAudience(aud) and jwt.WithIssuer(iss) check the other two, and the claim of a time "+
			"is read with GetExpirationTime(), GetIssuedAt() or GetNotBefore()", p.g.text(sel))
	case (name == "Sign" || name == "Verify") && jwtIsCalled(sel, stack) && signingMethodOf(sel.X, libs, stack):
		p.notefOncef("signing", sel.Pos(), "%s: in v5 a signing method returns the signature as a []byte and verifies one (v4: the base64url "+
			"string, like Token.Signature, which is a []byte too): encode with base64.RawURLEncoding where the string is needed", p.g.text(sel))
	}
}

// signingMethodOf reports whether the expression is a signing method of one of
// the libraries, jwt.SigningMethodHS256.
func signingMethodOf(x ast.Expr, libs map[string]jwtLib, stack []ast.Node) bool {
	inner, ok := x.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(inner.Sel.Name, "SigningMethod") {
		return false
	}
	_, _, pkg := libOf(inner, libs, stack)
	return pkg
}

// tokenClaimsValid rewrites token.Claims.Valid(), which v5 took away from the
// claims, to the validator that checks what Valid() checked (exp, nbf and iat) and
// calls the Validate() method of claims that have one:
//
//	token.Claims.Valid()  ->  jwt.NewValidator(jwt.WithIssuedAt()).Validate(token.Claims)
func (p *jwtClaimsPass) tokenClaimsValid(sel *ast.SelectorExpr, libs map[string]jwtLib, stack []ast.Node) bool {
	claims, ok := sel.X.(*ast.SelectorExpr)
	if !ok || claims.Sel.Name != "Claims" || !jwtIsCalled(sel, stack) {
		return false
	}
	call, isCall := stack[len(stack)-1].(*ast.CallExpr)
	if !isCall || len(call.Args) != 0 {
		return false
	}

	names := make([]string, 0, len(libs))
	for name := range libs {
		names = append(names, name)
	}
	slices.Sort(names)
	pkg := names[0]
	p.edits.replaceNode(p.g, call, pkg+".NewValidator("+pkg+".WithIssuedAt()).Validate("+p.g.text(claims)+")")
	return true
}

// embedded reports a struct type that embeds the StandardClaims of a library: the
// fields it brings along changed their types, and the code that reads or sets them
// through values of the type cannot be followed from here.
func (p *jwtClaimsPass) embedded(sel *ast.SelectorExpr, stack []ast.Node) {
	if len(stack) == 0 {
		return
	}
	field, ok := stack[len(stack)-1].(*ast.Field)
	if !ok || len(field.Names) != 0 || field.Type != ast.Expr(sel) {
		return
	}

	typ := "a struct"
	for _, n := range slices.Backward(stack) {
		if ts, isType := n.(*ast.TypeSpec); isType {
			typ = ts.Name.Name
			break
		}
	}
	p.notefOncef("embedded", sel.Pos(), "%s embeds the StandardClaims of the old library, which is jwt.RegisteredClaims in v5: its ExpiresAt, "+
		"IssuedAt and NotBefore are *jwt.NumericDate (v4: the Unix seconds as an int64, 0 for none), its Audience is a jwt.ClaimStrings and "+
		"its Id is called ID. Code that reads or sets these fields through a %s has to follow; the compiler lists it", typ, typ)
}

// jwtIsCalled reports whether the selector is the function of a call.
func jwtIsCalled(sel *ast.SelectorExpr, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	call, ok := stack[len(stack)-1].(*ast.CallExpr)
	return ok && call.Fun == ast.Expr(sel)
}

// key renames the key StandardClaims of a struct literal that sets the embedded
// field of that name.
func (p *jwtClaimsPass) key(kv *ast.KeyValueExpr, libs map[string]jwtLib, stack []ast.Node) {
	id, ok := kv.Key.(*ast.Ident)
	if !ok || id.Name != jwtStandardClaims || !p.mod.embeds || len(stack) == 0 {
		return
	}
	lit, ok := stack[len(stack)-1].(*ast.CompositeLit)
	if !ok || isLibType(lit.Type, libs, stack, jwtStandardClaims) {
		return
	}
	p.edits.replaceNode(p.g, id, jwtRegisteredClaims)
}

// isLibType reports whether the type expression is the type name of the library,
// behind the name the file has for it.
func isLibType(t ast.Expr, libs map[string]jwtLib, stack []ast.Node, name string) bool {
	sel, ok := t.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	_, _, pkg := libOf(sel, libs, stack)
	return pkg
}

// literal rewrites the composite literals of the types the libraries differ in.
func (p *jwtClaimsPass) literal(lit *ast.CompositeLit, libs map[string]jwtLib, stack []ast.Node) {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return
	}
	pkg, lib, ok := libOf(sel, libs, stack)
	if !ok {
		return
	}

	switch sel.Sel.Name {
	case jwtStandardClaims:
		p.standardLiteral(lit, pkg, lib)
	case jwtParserType:
		p.parserLiteral(lit, pkg, stack)
	}
}

// standardLiteral rewrites the fields of a StandardClaims literal for
// RegisteredClaims, which holds its times as *jwt.NumericDate and its audience as
// a list.
func (p *jwtClaimsPass) standardLiteral(lit *ast.CompositeLit, pkg string, lib jwtLib) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			p.notef(elt.Pos(), "the fields of jwt.StandardClaims are not all named here; in v5 jwt.RegisteredClaims has other types "+
				"(the times are *jwt.NumericDate, the audience a jwt.ClaimStrings) and a different order of fields, so name the fields")
			return
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}

		switch key.Name {
		case "ExpiresAt", "IssuedAt", "NotBefore":
			if when, isUnix := jwtUnixTime(kv.Value); isUnix {
				p.edits.replaceNode(p.g, kv.Value, pkg+".NewNumericDate("+p.g.text(when)+")")
				continue
			}
			p.notef(kv.Value.Pos(), "%s of jwt.RegisteredClaims is a *jwt.NumericDate in v5 (v4: the Unix seconds as an int64, 0 for none): "+
				"write %s.NewNumericDate(t) for a time.Time and leave the field out when there is none", key.Name, pkg)
		case "Audience":
			p.audience(kv, pkg, lib)
		case "Id":
			p.edits.replaceNode(p.g, key, "ID")
		}
	}
}

// audience wraps the Audience of a StandardClaims literal: a jwt.ClaimStrings in v5
// for the string of golang-jwt and dgrijalva, and for the []string of form3tech.
func (p *jwtClaimsPass) audience(kv *ast.KeyValueExpr, pkg string, lib jwtLib) {
	if !lib.audienceList {
		p.stringAudience(kv.Value, pkg)
		return
	}
	if lit, ok := kv.Value.(*ast.CompositeLit); ok {
		if arr, isArr := lit.Type.(*ast.ArrayType); isArr && arr.Len == nil && isIdentNamed(arr.Elt, "string") {
			p.edits.replaceNode(p.g, lit.Type, pkg+".ClaimStrings")
			return
		}
	}
	p.edits.replaceNode(p.g, kv.Value, pkg+".ClaimStrings("+p.g.text(kv.Value)+")")
}

// stringAudience wraps the audience of the libraries that held a string. An empty
// string was left out of the token by the library (omitempty), and a list that holds
// it is not: the empty string is nil, and so is the result for a string that is only
// known when the program runs.
func (p *jwtClaimsPass) stringAudience(value ast.Expr, pkg string) {
	text := p.g.text(value)
	if lit, ok := ast.Unparen(value).(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if unquoted, err := strconv.Unquote(lit.Value); err == nil {
			if unquoted == "" {
				p.edits.replaceNode(p.g, value, "nil")
				return
			}
			p.edits.replaceNode(p.g, value, pkg+".ClaimStrings{"+text+"}")
			return
		}
	}
	p.edits.replaceNode(p.g, value, "func(aud string) "+pkg+".ClaimStrings {\nif aud == \"\" {\nreturn nil\n}\nreturn "+pkg+".ClaimStrings{aud}\n}("+text+")")
}

// jwtUnixTime returns t of an expression t.Unix(), the Unix seconds of a time.
func jwtUnixTime(e ast.Expr) (when ast.Expr, ok bool) {
	call, isCall := ast.Unparen(e).(*ast.CallExpr)
	if !isCall || len(call.Args) != 0 {
		return nil, false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "Unix" {
		return nil, false
	}
	return sel.X, true
}

// jwtParserOptions are the fields of the parser of v4 and the options of v5 that
// take their place.
var jwtParserOptions = map[string]string{
	"ValidMethods":         "WithValidMethods",
	"UseJSONNumber":        "WithJSONNumber",
	"SkipClaimsValidation": "WithoutClaimsValidation",
}

// parserLiteral rewrites &jwt.Parser{ValidMethods: ...} to jwt.NewParser(options):
// the fields of the parser are private in v5.
func (p *jwtClaimsPass) parserLiteral(lit *ast.CompositeLit, pkg string, stack []ast.Node) {
	if len(lit.Elts) == 0 {
		return
	}

	var options []string
	exact := true
	for _, elt := range lit.Elts {
		option, ok := p.parserOption(elt, pkg)
		exact = exact && ok
		if option != "" {
			options = append(options, option)
		}
	}

	var addr ast.Node
	if len(stack) > 0 {
		if u, ok := stack[len(stack)-1].(*ast.UnaryExpr); ok && u.Op == token.AND && u.X == ast.Expr(lit) {
			addr = u
		}
	}
	if !exact || addr == nil {
		p.notef(lit.Pos(), "the fields of jwt.Parser are private in v5: build the parser with jwt.NewParser(jwt.WithValidMethods(...), "+
			"jwt.WithJSONNumber(), jwt.WithoutClaimsValidation())")
		return
	}
	p.edits.replaceNode(p.g, addr, pkg+".NewParser("+strings.Join(options, ", ")+")")
}

// parserOption returns the option of v5 that stands for a field of a parser
// literal, "" for a field that is left out; ok is false for an element with no
// option.
func (p *jwtClaimsPass) parserOption(elt ast.Expr, pkg string) (option string, ok bool) {
	kv, isKV := elt.(*ast.KeyValueExpr)
	if !isKV {
		return "", false
	}
	key, isID := kv.Key.(*ast.Ident)
	if !isID || jwtParserOptions[key.Name] == "" {
		return "", false
	}
	call := pkg + "." + jwtParserOptions[key.Name]

	if key.Name == "ValidMethods" {
		return call + "(" + p.g.text(kv.Value) + ")", true
	}
	switch {
	case isIdentNamed(ast.Unparen(kv.Value), "true"):
		return call + "()", true
	case isIdentNamed(ast.Unparen(kv.Value), "false"):
		return "", true
	}
	return "", false
}

// validMethod moves the Valid() error method of a claims type of the file to the
// Validate() error method v5 calls. The standard claims need no check of their
// own any more: the parser checks them before it asks the claims.
func (p *jwtClaimsPass) validMethod(fn *ast.FuncDecl, libs map[string]jwtLib) {
	if len(libs) == 0 || fn.Name.Name != jwtValidMethod || fn.Recv == nil || fn.Body == nil || len(fn.Recv.List) != 1 ||
		fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 1 || !isIdentNamed(fn.Type.Results.List[0].Type, "error") {
		return
	}
	typ := importsReceiverType(fn.Recv.List[0].Type)
	field, declared := p.claimsEmbedded(typ, libs)
	if field == "" {
		if declared {
			p.notef(fn.Pos(), "%s has a Valid() error method, which v5 does not call, and embeds no standard claims: if it is used as claims, "+
				"note that jwt.Claims is a set of getters in v5 (GetExpirationTime, GetIssuedAt, GetNotBefore, GetIssuer, GetSubject, GetAudience): "+
				"embed jwt.RegisteredClaims, which has them, and rename the method Validate() error", typ)
		}
		return
	}

	// The standard claims are checked by the parser: the call that asked them first goes.
	body := fn.Body.List
	switch {
	case len(body) > 0 && isStandardCheck(body[0]):
		mw3DeleteStatement(p.g, &p.edits, body[0])
	case len(body) == 1 && isStandardReturn(body[0]):
		if ret, isRet := body[0].(*ast.ReturnStmt); isRet {
			p.edits.replaceNode(p.g, ret.Results[0], "nil")
		}
	case callsStandardValid(fn):
		p.notef(fn.Pos(), "%s calls Valid() of its standard claims in a way this migration does not rewrite; v5 calls Validate() error on "+
			"claims that have it, after the parser checked exp, nbf and the other registered claims, and jwt.RegisteredClaims has no Valid()",
			p.g.text(fn.Name))
		return
	}
	p.edits.replaceNode(p.g, fn.Name, "Validate")
}

// claimsEmbedded returns the name of the field through which the struct type of the
// file embeds the standard claims of a library, "" when it embeds none. declared
// says that the file declares the type as a struct, so that "embeds none" is known.
func (p *jwtClaimsPass) claimsEmbedded(typ string, libs map[string]jwtLib) (field string, declared bool) {
	if typ == "" {
		return "", false
	}
	for _, decl := range p.g.file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, isType := spec.(*ast.TypeSpec)
			if !isType || ts.Name.Name != typ {
				continue
			}
			st, isStruct := ts.Type.(*ast.StructType)
			if !isStruct {
				return "", false
			}
			for _, field := range st.Fields.List {
				sel, isSel := field.Type.(*ast.SelectorExpr)
				if len(field.Names) != 0 || !isSel || (sel.Sel.Name != jwtStandardClaims && sel.Sel.Name != jwtRegisteredClaims) {
					continue
				}
				if id, isID := sel.X.(*ast.Ident); isID {
					if _, known := libs[id.Name]; known {
						return sel.Sel.Name, true
					}
				}
			}
			return "", true
		}
	}
	return "", false
}

// isStandardCheck reports whether the statement is
//
//	if err := x.StandardClaims.Valid(); err != nil { return err }
func isStandardCheck(stmt ast.Stmt) bool {
	ifs, ok := stmt.(*ast.IfStmt)
	if !ok || ifs.Else != nil || ifs.Init == nil {
		return false
	}
	assign, ok := ifs.Init.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || assign.Tok != token.DEFINE {
		return false
	}
	name, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || !isStandardValid(assign.Rhs[0]) {
		return false
	}
	cond, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ || !isIdentNamed(cond.X, name.Name) || !isIdentNamed(cond.Y, "nil") {
		return false
	}
	if len(ifs.Body.List) != 1 {
		return false
	}
	ret, ok := ifs.Body.List[0].(*ast.ReturnStmt)
	return ok && len(ret.Results) == 1 && isIdentNamed(ret.Results[0], name.Name)
}

// isStandardReturn reports whether the statement is return x.StandardClaims.Valid().
func isStandardReturn(stmt ast.Stmt) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	return ok && len(ret.Results) == 1 && isStandardValid(ret.Results[0])
}

// isStandardValid reports whether the expression is x.StandardClaims.Valid() or
// x.RegisteredClaims.Valid().
func isStandardValid(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	valid, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || valid.Sel.Name != jwtValidMethod {
		return false
	}
	std, ok := valid.X.(*ast.SelectorExpr)
	return ok && (std.Sel.Name == jwtStandardClaims || std.Sel.Name == jwtRegisteredClaims)
}

// callsStandardValid reports whether the method calls Valid() of its standard
// claims anywhere.
func callsStandardValid(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && isStandardValid(e) {
			found = true
		}
		return !found
	})
	return found
}
