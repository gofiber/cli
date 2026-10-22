package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// mw1Package identifies one built-in middleware package of Fiber by its import
// path, whatever major version the file imports it at: the module path is
// rewritten to v3 before the middleware migrations run, but the migrations are
// also run on their own.
type mw1Package struct {
	path *regexp.Regexp
	name string
}

func newMW1Package(name string) mw1Package {
	return mw1Package{
		path: regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/` + regexp.QuoteMeta(name) + `$`),
		name: name,
	}
}

// names returns the identifiers src binds to the package, the import alias when
// there is one. A dot import is reported as "." and left to the callers, which
// never treat it as a qualifier.
func (p mw1Package) names(src string) []string {
	return importNames(src, p.path, p.name)
}

// mw1Qualifiers drops the dot import marker from names: what is left can be
// used as the qualifier of a selector expression.
func mw1Qualifiers(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != "." {
			out = append(out, n)
		}
	}
	return out
}

// mw1Selector splits the selector expression x.sel whose operand is a plain
// identifier.
func mw1Selector(e ast.Expr) (x, sel string, ok bool) {
	s, isSel := e.(*ast.SelectorExpr)
	if !isSel {
		return "", "", false
	}
	id, isIdent := s.X.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	return id.Name, s.Sel.Name, true
}

// mw1IsMember reports whether e is `<one of names>.<member>`.
func mw1IsMember(e ast.Expr, names []string, member string) bool {
	x, sel, ok := mw1Selector(e)
	return ok && sel == member && slices.Contains(names, x)
}

// mw1IsMemberOf is mw1IsMember for a member that may be wrapped in parentheses.
func mw1IsMemberOf(e ast.Expr, names []string, member string) bool {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	return mw1IsMember(e, names, member)
}

// mw1ConfigLit reports whether n is a composite literal of the Config type of
// the package bound to names, and returns it.
func mw1ConfigLit(n ast.Node, names []string) (*ast.CompositeLit, bool) {
	lit, ok := n.(*ast.CompositeLit)
	if !ok || lit.Type == nil || !mw1IsMember(lit.Type, names, "Config") {
		return nil, false
	}
	return lit, true
}

// mw1KeyValue returns the element `field: value` of a composite literal.
func mw1KeyValue(lit *ast.CompositeLit, field string) *ast.KeyValueExpr {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if ok && isIdentNamed(kv.Key, field) {
			return kv
		}
	}
	return nil
}

// mw1IsConfigValue reports whether the declaration d is a value of the Config
// type of the package bound to names: declared with that type, built from a
// literal of it, or copied from its ConfigDefault.
func mw1IsConfigValue(d decl, names []string) bool {
	if d.typ != nil {
		e := d.typ
		if star, ok := e.(*ast.StarExpr); ok {
			e = star.X
		}
		return mw1IsMember(e, names, "Config")
	}

	e := d.init
	for e != nil {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.UnaryExpr:
			if x.Op != token.AND {
				return false
			}
			e = x.X
		case *ast.CompositeLit:
			return x.Type != nil && mw1IsMember(x.Type, names, "Config")
		case *ast.SelectorExpr:
			return mw1IsMember(x, names, "ConfigDefault")
		default:
			return false
		}
	}
	return false
}

// mw1AssignedField recognizes `<config>.<field> = <value>` where <config> is a
// variable holding a Config of the package bound to names.
func mw1AssignedField(n ast.Node, stack []ast.Node, names []string) (as *ast.AssignStmt, field string, ok bool) {
	as, isAssign := n.(*ast.AssignStmt)
	if !isAssign || as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return nil, "", false
	}
	sel, isSel := as.Lhs[0].(*ast.SelectorExpr)
	if !isSel {
		return nil, "", false
	}
	id, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return nil, "", false
	}
	d, found := resolveIdent(id.Name, id.Pos(), stack)
	if !found || !mw1IsConfigValue(d, names) {
		return nil, "", false
	}
	return as, sel.Sel.Name, true
}

// mw1Indent returns the whitespace that starts the line the offset is on.
func mw1Indent(src string, offset int) string {
	start := strings.LastIndexByte(src[:offset], '\n') + 1
	end := start
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return src[start:end]
}

// mw1StartsLine reports whether only white space precedes the offset on its
// line.
func mw1StartsLine(src string, offset int) bool {
	start := strings.LastIndexByte(src[:offset], '\n') + 1
	return strings.TrimLeft(src[start:offset], " \t") == ""
}

// mw1SkipSpaces returns the offset of the first byte at or after i that is not
// a space, a tab or the carriage return of a CRLF line break.
func mw1SkipSpaces(src string, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\r') {
		i++
	}
	return i
}

// mw1TrailingComment returns the end of the line comment that starts at the
// first non-blank byte at or after i, or i itself when there is none.
func mw1TrailingComment(src string, i int) int {
	j := mw1SkipSpaces(src, i)
	if !strings.HasPrefix(src[j:], "//") {
		return i
	}
	if nl := strings.IndexByte(src[j:], '\n'); nl >= 0 {
		return j + nl
	}
	return len(src)
}

// mw1ElementSpan returns the byte range that removes element i of a composite
// literal: the element, its comma and the comment that trails it on its line.
// When the element is alone on its lines, the lines go as well.
func mw1ElementSpan(g *goSource, lit *ast.CompositeLit, i int) (start, end int) {
	src := g.src
	elt := lit.Elts[i]
	start = g.offset(elt.Pos())
	end = g.offset(elt.End())

	comma := mw1SkipSpaces(src, end)
	hasComma := comma < len(src) && src[comma] == ','
	if hasComma {
		end = comma + 1
	}
	commentEnd := mw1TrailingComment(src, end)

	lineStart := strings.LastIndexByte(src[:start], '\n') + 1
	aloneBefore := strings.TrimLeft(src[lineStart:start], " \t") == ""
	rest := mw1SkipSpaces(src, commentEnd)
	aloneAfter := rest >= len(src) || src[rest] == '\n'

	switch {
	case aloneBefore && aloneAfter:
		if rest < len(src) {
			rest++ // the newline
		}
		return lineStart, rest
	case hasComma && commentEnd > end:
		return start, commentEnd
	case hasComma:
		return start, mw1SkipSpaces(src, end)
	default:
		return start, end
	}
}

// mw1StatementSpan returns the byte range that removes a statement that takes
// its lines on its own in a block: the statement and its line break. ok is false
// for any other statement.
func mw1StatementSpan(g *goSource, stmt ast.Stmt, stack []ast.Node) (start, end int, ok bool) {
	if len(stack) == 0 {
		return 0, 0, false
	}
	switch stack[len(stack)-1].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
	default:
		return 0, 0, false
	}

	start = g.offset(stmt.Pos())
	end = g.offset(stmt.End())
	rest := mw1TrailingComment(g.src, end)
	after := mw1SkipSpaces(g.src, rest)
	if !mw1StartsLine(g.src, start) || (after < len(g.src) && g.src[after] != '\n') {
		return 0, 0, false
	}
	if after < len(g.src) {
		after++ // the line break
	}
	return start - len(mw1Indent(g.src, start)), after, true
}

// mw1MultiLine reports whether the literal spans more than one line.
func mw1MultiLine(g *goSource, lit *ast.CompositeLit) bool {
	return g.line(lit.Lbrace) != g.line(lit.Rbrace)
}

// mw1AddElements returns the edit (the range it replaces and the text) that
// adds elements to a composite literal. It is written so that no edit to the
// elements of the literal can get in its way: a multi-line literal gets lines of
// their own before the one with the closing brace, and a single-line literal
// gets the elements first, since the last element may go.
func mw1AddElements(g *goSource, lit *ast.CompositeLit, elems []string) (start, end int, text string) {
	if !mw1MultiLine(g, lit) {
		start = g.offset(lit.Lbrace)
		return start, start + 1, "{" + strings.Join(elems, ", ") + ", "
	}

	rbrace := g.offset(lit.Rbrace)
	lineStart := strings.LastIndexByte(g.src[:rbrace], '\n') + 1
	indent := mw1Indent(g.src, rbrace)
	if strings.TrimSpace(g.src[lineStart:rbrace]) == "" {
		indent += "\t" // the brace is alone: the elements are one level deeper
	}
	var lines []byte
	for _, elem := range elems {
		lines = append(lines, indent+elem+",\n"...)
	}
	return lineStart, lineStart, string(lines)
}

// mw1QuoteList renders a []string literal.
func mw1QuoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = strconv.Quote(item)
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}

// mw1StringLiteral evaluates a string literal, an interpreted or a raw one, or a
// concatenation of them.
func mw1StringLiteral(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return mw1StringLiteral(x.X)
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := mw1StringLiteral(x.X)
		r, rok := mw1StringLiteral(x.Y)
		return l + r, lok && rok
	}
	return "", false
}

// mw1StringConsts returns the constants of the file that are declared with a
// string literal, anywhere in the file. A name that is declared more than once
// is left out.
func mw1StringConsts(file *ast.File) map[string]string {
	consts := map[string]string{}
	dup := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, spec := range gd.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, id := range vs.Names {
				if _, seen := consts[id.Name]; seen || dup[id.Name] {
					dup[id.Name] = true
					delete(consts, id.Name)
					continue
				}
				if s, isString := mw1StringLiteral(vs.Values[i]); isString {
					consts[id.Name] = s
				}
			}
		}
		return true
	})
	return consts
}

// mw1FiberName returns the identifier a file uses for the core fiber package.
// needImport is true when the file does not import it, in which case the
// identifier is the package name.
func mw1FiberName(src string) (name string, needImport bool) {
	names := mw1Qualifiers(fiberNames(src))
	if len(names) == 0 {
		return mw1FiberIdent, true
	}
	if slices.Contains(names, mw1FiberIdent) {
		return mw1FiberIdent, false
	}
	return names[0], false
}

// mw1NilIdent is the predeclared nil.
const mw1NilIdent = "nil"

// mw1FiberIdent is the package name of the core module.
const mw1FiberIdent = "fiber"

// mw1FiberCoreImport is the import path v3 code uses for the core package.
const mw1FiberCoreImport = "github.com/gofiber/fiber/v3"

// mw1UsesName reports whether the file, or any other node, mentions an
// identifier called name anywhere (declared or used); it is how a generated
// identifier avoids a clash.
func mw1UsesName(root ast.Node, name string) bool {
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// mw1ImportName returns the identifier the file uses for the package at path,
// and whether the file has to import it first. name is the package name the
// import gets when it has none.
func mw1ImportName(src, path, name string) (ident string, add bool) {
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(path) + `$`)
	for _, n := range importNames(src, re, name) {
		if n != "." {
			return n, false
		}
	}
	return name, true
}

// mw1AddImport adds the import of path to src, next to the imports of the same
// kind: the standard library, Fiber, other modules. goimports regroups what it
// finds, but only after the migrations have run and without knowing about
// packages that are not in the module cache yet.
func mw1AddImport(src, path string) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil && file == nil {
		return addImport(src, path)
	}

	var decl *ast.GenDecl
	for _, d := range file.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT && gd.Lparen.IsValid() {
			decl = gd
			break
		}
	}
	if decl == nil {
		return addImport(src, path)
	}

	quoted := strconv.Quote(path)
	kind := mw1ImportKind(path)
	var neighbor *ast.ImportSpec
	for _, spec := range decl.Specs {
		imp, ok := spec.(*ast.ImportSpec)
		if !ok {
			continue
		}
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && mw1ImportKind(p) == kind {
			neighbor = imp
		}
	}

	switch {
	case neighbor != nil:
		end := mw1LineEnd(src, fset.Position(neighbor.End()).Offset)
		return src[:end] + "\n\t" + quoted + src[end:]
	case len(decl.Specs) == 0:
		open := fset.Position(decl.Lparen).Offset + 1
		return src[:open] + "\n\t" + quoted + src[open:]
	case kind == mw1StdImport:
		// the standard library goes first, in a group of its own
		first := fset.Position(decl.Specs[0].Pos()).Offset
		return src[:first] + quoted + "\n\n\t" + src[first:]
	}

	// another group, after the others
	end := mw1LineEnd(src, fset.Position(decl.Specs[len(decl.Specs)-1].End()).Offset)
	return src[:end] + "\n\n\t" + quoted + src[end:]
}

// mw1LineEnd returns the offset of the end of the line that the offset is on.
func mw1LineEnd(src string, offset int) int {
	if nl := strings.IndexByte(src[offset:], '\n'); nl >= 0 {
		return offset + nl
	}
	return len(src)
}

// The kinds of import, which goimports keeps in groups.
const (
	mw1StdImport = iota
	mw1FiberImport
	mw1OtherImport
)

func mw1ImportKind(path string) int {
	first, _, _ := strings.Cut(path, "/")
	switch {
	case strings.HasPrefix(path, "github.com/gofiber/"):
		return mw1FiberImport
	case strings.Contains(first, "."):
		return mw1OtherImport
	}
	return mw1StdImport
}

// mw1StmtList returns the statements of a block or of a case of a switch or a
// select.
func mw1StmtList(n ast.Node) ([]ast.Stmt, bool) {
	switch x := n.(type) {
	case *ast.BlockStmt:
		return x.List, true
	case *ast.CaseClause:
		return x.Body, true
	case *ast.CommClause:
		return x.Body, true
	}
	return nil, false
}

// mw1FieldsSetAfter returns the fields that the statements after decl assign to
// the variable name, as in `name.Field = value`.
func mw1FieldsSetAfter(list []ast.Stmt, decl ast.Stmt, name string) map[string]bool {
	set := map[string]bool{}
	after := false
	for _, stmt := range list {
		if stmt == decl {
			after = true
			continue
		}
		if !after {
			continue
		}
		ast.Inspect(stmt, func(n ast.Node) bool {
			if assign, isAssign := n.(*ast.AssignStmt); isAssign && len(assign.Lhs) == 1 {
				if sel, isSel := assign.Lhs[0].(*ast.SelectorExpr); isSel && isIdentNamed(sel.X, name) {
					set[sel.Sel.Name] = true
				}
			}
			return true
		})
	}
	return set
}

// mw1InsertStatements adds statements, each on a line of its own, after the
// statement decl.
func mw1InsertStatements(g *goSource, edits *editSet, decl ast.Stmt, stmts []string) {
	if len(stmts) == 0 {
		return
	}
	indent := mw1Indent(g.src, g.offset(decl.Pos()))
	end := g.offset(decl.End())
	edits.replace(end, end, "\n"+indent+strings.Join(stmts, "\n"+indent))
}
