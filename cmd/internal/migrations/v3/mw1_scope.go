package v3

import (
	"go/ast"
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

// mw1IsConfigLit reports whether n is a composite literal of the Config type
// of the package bound to names.
func mw1IsConfigLit(n ast.Node, names []string) bool {
	lit, ok := n.(*ast.CompositeLit)
	return ok && lit.Type != nil && mw1IsMember(lit.Type, names, "Config")
}

// mw1KeyValue returns the element `field: value` of a composite literal and its
// index.
func mw1KeyValue(lit *ast.CompositeLit, field string) (kv *ast.KeyValueExpr, index int) {
	for i, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if isIdentNamed(kv.Key, field) {
			return kv, i
		}
	}
	return nil, -1
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
// a space or a tab.
func mw1SkipSpaces(src string, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
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

// mw1MultiLine reports whether the literal spans more than one line.
func mw1MultiLine(g *goSource, lit *ast.CompositeLit) bool {
	return g.line(lit.Lbrace) != g.line(lit.Rbrace)
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

// mw1FiberIdent is the package name of the core module.
const mw1FiberIdent = "fiber"

// mw1FiberCoreImport is the import path v3 code uses for the core package.
const mw1FiberCoreImport = "github.com/gofiber/fiber/v3"

// mw1UsesName reports whether the file mentions an identifier called name
// anywhere (declared or used); it is how a generated identifier avoids a clash.
func mw1UsesName(file *ast.File, name string) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}
