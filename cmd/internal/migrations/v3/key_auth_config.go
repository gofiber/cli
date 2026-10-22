package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// The sources a v2 KeyLookup can name.
const (
	mw2SourceHeader = "header"
	mw2SourceQuery  = "query"
	mw2SourceForm   = "form"
	mw2SourceParam  = "param"
	mw2SourceCookie = "cookie"
)

// MigrateKeyAuthConfig replaces the KeyLookup and AuthScheme fields of the keyauth
// config with an Extractor. The pair is translated the way v2 read it, so the
// same requests are accepted: an empty AuthScheme only defaulted to "Bearer" when
// KeyLookup was empty too, and the scheme was stripped from any header, not only
// from Authorization. A lookup that cannot be translated faithfully is left as
// it is, so that the build fails here, and a note says what to write instead.
func MigrateKeyAuthConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs := newMW2Packages()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateKeyAuthContent(content, fi, pkgs)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate keyauth configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating keyauth middleware configs")
	return nil
}

func migrateKeyAuthContent(content string, fi internal.FileInfo, pkgs *mw2Packages) string {
	if !strings.Contains(content, "KeyLookup") && !strings.Contains(content, "AuthScheme") {
		return content
	}
	names := importNames(content, mw2KeyAuth, "keyauth")
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	r := &mw2KeyAuthRewriter{
		g:          g,
		fi:         fi,
		names:      names,
		fiber:      fiberNames(content),
		extractors: mw2FirstName(content, mw2Extractors, "extractors"),
		consts:     pkgs.of(filepath.Dir(fi.Path), g.file.Name.Name),
		done:       map[ast.Stmt]bool{},
	}
	if r.extractors == "" {
		r.extractors = "extractors"
		if mw2NameTaken(g, r.consts, r.extractors) {
			r.extractors = "fiberextractors"
		}
	}
	g.walk(r.visit)

	if len(r.edits.edits) == 0 {
		return content
	}
	alias := ""
	if r.extractors != "extractors" {
		alias = r.extractors
	}
	imports := []mw2Import{{alias: alias, path: "github.com/gofiber/fiber/v3/extractors"}}
	if r.needsFiber {
		imports = append(imports, mw2Import{path: "github.com/gofiber/fiber/v3"})
	}
	return mw2AddImports(r.edits.apply(content), imports...)
}

// mw2KeyAuthRewriter rewrites the keyauth lookup fields of one file.
type mw2KeyAuthRewriter struct {
	g      *goSource
	consts *mw2Package
	// done holds the assignment statements that were rewritten together with a sibling.
	done map[ast.Stmt]bool
	// extractors is the identifier of the extractors package in the output.
	extractors string
	fi         internal.FileInfo
	edits      editSet
	// names are the identifiers the file binds to the keyauth package, fiber those it binds to the core package.
	names []string
	fiber []string
	// needsFiber is set when the output names fiber.Ctx in a file that does not import the core package.
	needsFiber bool
}

// mw2KeyAuthField is the value of KeyLookup or AuthScheme.
type mw2KeyAuthField struct {
	// text is the source text of the value.
	text string
	// value is the constant string, valid when known is set.
	value string
	// set tells whether the field is given at all.
	set bool
	// known tells whether the value is a constant string.
	known bool
}

func (r *mw2KeyAuthRewriter) visit(n ast.Node, stack []ast.Node) bool {
	if lit, ok := mw2ConfigLit(n, r.names, "Config"); ok {
		r.literal(lit)
		return true
	}
	if as, ok := n.(*ast.AssignStmt); ok {
		r.assignment(as, stack)
	}
	return true
}

// field evaluates the value of a KeyLookup or AuthScheme element.
func (r *mw2KeyAuthRewriter) field(value ast.Expr) mw2KeyAuthField {
	f := mw2KeyAuthField{set: true, text: r.g.text(value)}
	f.value, f.known = mw2ConstString(value, r.consts.constValue, 0)
	return f
}

// literal rewrites a keyauth.Config literal.
func (r *mw2KeyAuthRewriter) literal(lit *ast.CompositeLit) {
	if !mw2IsKeyed(lit) {
		return
	}
	_, lookupKV := mw2Field(lit, "KeyLookup")
	schemeIdx, schemeKV := mw2Field(lit, "AuthScheme")
	if lookupKV == nil && schemeKV == nil {
		return
	}

	var lookup, scheme mw2KeyAuthField
	if lookupKV != nil {
		lookup = r.field(lookupKV.Value)
	}
	if schemeKV != nil {
		scheme = r.field(schemeKV.Value)
	}
	expr, problem := r.extractor(lookup, scheme)

	first := lookupKV
	if first == nil {
		first = schemeKV
	}
	if problem != "" {
		internal.Notef(r.fi, r.g.line(first.Pos()), "keyauth %s", problem)
		return
	}

	r.edits.replaceNode(r.g, first, "Extractor: "+expr)
	if lookupKV != nil && schemeKV != nil {
		mw2RemoveElement(r.g, &r.edits, lit, schemeIdx)
	}
}

// assignment rewrites the assignments to the lookup fields of a keyauth config
// variable. The ones of a variable are translated together, so they must all sit
// in one statement list; anything else is left for the user.
func (r *mw2KeyAuthRewriter) assignment(as *ast.AssignStmt, stack []ast.Node) {
	target, field, ok := r.lookupAssignment(as, stack)
	if !ok || r.done[as] {
		return
	}

	list := mw2StmtList(stack)
	if !slices.Contains(list, ast.Stmt(as)) {
		return
	}
	var group []*ast.AssignStmt
	for _, s := range list {
		if sib, ok := s.(*ast.AssignStmt); ok {
			if t, _, ok := r.lookupAssignment(sib, stack); ok && t == target {
				group = append(group, sib)
			}
		}
	}
	for _, sib := range group {
		r.done[sib] = true
	}

	var lookupAs, schemeAs *ast.AssignStmt
	for _, sib := range group {
		sel, ok := sib.Lhs[0].(*ast.SelectorExpr)
		if !ok {
			return
		}
		slot := &lookupAs
		if sel.Sel.Name == "AuthScheme" {
			slot = &schemeAs
		}
		if *slot != nil {
			internal.Notef(r.fi, r.g.line(sib.Pos()), "%s is assigned more than once; set Extractor by hand instead of KeyLookup and AuthScheme (extractors.FromHeader, FromQuery, FromCookie, FromForm, FromParam, FromAuthHeader)", field)
			return
		}
		*slot = sib
	}

	var lookup, scheme mw2KeyAuthField
	if lookupAs != nil {
		lookup = r.field(lookupAs.Rhs[0])
	}
	if schemeAs != nil {
		scheme = r.field(schemeAs.Rhs[0])
	}
	// A copy of ConfigDefault starts out with the Bearer scheme in place, which still applies to the lookup that replaces its header.
	if lookupAs != nil && schemeAs == nil && r.copiesDefault(as, stack) {
		scheme = mw2KeyAuthField{set: true, known: true, value: "Bearer", text: `"Bearer"`}
	}
	expr, problem := r.extractor(lookup, scheme)

	first := lookupAs
	if first == nil {
		first = schemeAs
	}
	if problem != "" {
		internal.Notef(r.fi, r.g.line(first.Pos()), "keyauth %s", problem)
		return
	}

	r.edits.replaceNode(r.g, first, target+".Extractor = "+expr)
	if lookupAs != nil && schemeAs != nil {
		mw2RemoveStmt(r.g, &r.edits, schemeAs)
	}
}

// lookupAssignment reports whether as assigns KeyLookup or AuthScheme of a
// keyauth config held in a variable, and returns the variable's text.
func (r *mw2KeyAuthRewriter) lookupAssignment(as *ast.AssignStmt, stack []ast.Node) (target, field string, ok bool) {
	if as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return "", "", false
	}
	sel, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "KeyLookup" && sel.Sel.Name != "AuthScheme") {
		return "", "", false
	}
	if _, isVar := sel.X.(*ast.Ident); !isVar || !mw2IsTyped(sel.X, r.names, "Config", stack, 0) {
		return "", "", false
	}
	return r.g.text(sel.X), r.g.text(sel), true
}

// copiesDefault reports whether the variable assigned by as was initialized with keyauth.ConfigDefault.
func (r *mw2KeyAuthRewriter) copiesDefault(as *ast.AssignStmt, stack []ast.Node) bool {
	sel, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	return ok && d.init != nil && mw2IsSelector(d.init, r.names, "ConfigDefault")
}

// extractor translates a KeyLookup and AuthScheme pair to the v3 extractor that
// accepts the same requests. When it cannot, the problem is reported instead.
func (r *mw2KeyAuthRewriter) extractor(lookup, scheme mw2KeyAuthField) (expr, problem string) {
	if !lookup.set {
		// v2 reads Authorization when KeyLookup is empty, and then defaults an empty scheme to Bearer.
		switch {
		case !scheme.known:
			return "", fmt.Sprintf("AuthScheme %s is not a constant, and v2 replaced an empty scheme by \"Bearer\". It was left as it is; set Extractor to extractors.FromAuthHeader(<scheme>) by hand", scheme.text)
		case scheme.value == "":
			return r.call("FromAuthHeader", strconv.Quote("Bearer")), ""
		}
		return r.call("FromAuthHeader", strconv.Quote(scheme.value)), ""
	}

	if !lookup.known {
		return "", fmt.Sprintf("KeyLookup %s is not a constant string, so it was left as it is; set Extractor by hand (extractors.FromHeader, FromQuery, FromCookie, FromForm or FromParam)", lookup.text)
	}
	// v2 split the value at every colon and used the first two parts.
	parts := strings.Split(lookup.value, ":")
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" || strings.ContainsAny(lookup.value, ", ") {
		return "", fmt.Sprintf("KeyLookup %s is not <source>:<name>, which is the only form v2 accepted, so it was left as it is; set Extractor by hand (extractors.Chain combines several lookups)", strconv.Quote(lookup.value))
	}
	source, name := parts[0], parts[1]

	switch source {
	case mw2SourceQuery:
		return r.call("FromQuery", strconv.Quote(name)), ""
	case mw2SourceForm:
		return r.call("FromForm", strconv.Quote(name)), ""
	case mw2SourceParam:
		return r.call("FromParam", strconv.Quote(name)), ""
	case mw2SourceCookie:
		return r.call("FromCookie", strconv.Quote(name)), ""
	case mw2SourceHeader:
		return r.header(name, scheme)
	}
	return "", fmt.Sprintf("KeyLookup %s names the source %q, which v2 handled as a header lookup; it was left as it is, set Extractor by hand", strconv.Quote(lookup.value), source)
}

// header translates a header lookup with an optional scheme.
func (r *mw2KeyAuthRewriter) header(name string, scheme mw2KeyAuthField) (expr, problem string) {
	authorization := strings.EqualFold(name, "Authorization")

	switch {
	case scheme.set && !scheme.known && authorization:
		// An empty scheme returns the whole header in v3 as well, like in v2.
		return r.call("FromAuthHeader", scheme.text), ""
	case scheme.set && !scheme.known:
		return "", fmt.Sprintf("AuthScheme %s is not a constant and is applied to the header %s, which no v3 extractor does. It was left as it is; write an extractors.FromCustom that strips the scheme", scheme.text, name)
	case !scheme.set || scheme.value == "":
		return r.call("FromHeader", strconv.Quote(name)), ""
	case authorization:
		return r.call("FromAuthHeader", strconv.Quote(scheme.value)), ""
	}

	// v2 stripped the scheme from any header. FromAuthHeader only reads Authorization, so the rule is spelled out.
	ctx := r.ctxType()
	header, quoted := strconv.Quote(name), strconv.Quote(scheme.value)
	body := "func(c " + ctx + ") (string, error) {\n" +
		"auth := c.Get(" + header + ")\n" +
		"if len(auth) > len(" + quoted + ")+1 && auth[:len(" + quoted + ")] == " + quoted + " {\n" +
		"return auth[len(" + quoted + ")+1:], nil\n" +
		"}\n" +
		"return \"\", " + r.extractors + ".ErrNotFound\n" +
		"}"
	return r.call("FromCustom", header+", "+body), ""
}

// ctxType names fiber.Ctx the way the file spells the core package, importing it when the file does not.
func (r *mw2KeyAuthRewriter) ctxType() string {
	if len(r.fiber) == 0 {
		r.needsFiber = true
		return "fiber.Ctx"
	}
	if r.fiber[0] == "." {
		return "Ctx"
	}
	return r.fiber[0] + ".Ctx"
}

// call renders a call of the extractors package.
func (r *mw2KeyAuthRewriter) call(fn, args string) string {
	return r.extractors + "." + fn + "(" + args + ")"
}

// mw2StmtList returns the statements of the closest enclosing block, case clause
// or select clause of the node whose ancestors are stack.
func mw2StmtList(stack []ast.Node) []ast.Stmt {
	for _, n := range slices.Backward(stack) {
		switch x := n.(type) {
		case *ast.BlockStmt:
			return x.List
		case *ast.CaseClause:
			return x.Body
		case *ast.CommClause:
			return x.Body
		}
	}
	return nil
}

// mw2RemoveStmt schedules the removal of a statement; a statement that is alone on its line takes the line with it.
func mw2RemoveStmt(g *goSource, edits *editSet, stmt ast.Stmt) {
	start, end := g.offset(stmt.Pos()), g.offset(stmt.End())
	src := g.src
	lineStart := strings.LastIndexByte(src[:start], '\n') + 1
	next := mw2SkipBlanks(src, end)
	if strings.HasPrefix(src[next:], "//") {
		next = mw2LineEnd(src, next)
	}
	if strings.TrimSpace(src[lineStart:start]) == "" && (next >= len(src) || src[next] == '\n' || src[next] == '\r') {
		edits.replace(mw2CommentsAbove(src, lineStart), min(mw2LineEnd(src, next)+1, len(src)), "")
		return
	}
	edits.replace(start, end, "")
}
