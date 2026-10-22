package v3

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var mw1CORS = newMW1Package("cors")

// corsListFields are the fields of cors.Config that are comma separated
// strings in v2 and string slices in v3.
var corsListFields = []string{"AllowOrigins", "AllowMethods", "AllowHeaders", "ExposeHeaders"}

// MigrateCORSConfig turns the comma separated strings of cors.Config into the
// string slices of v3. A literal becomes a slice literal, strings.Join of a
// slice becomes the slice, and any other expression is split at run time the
// way v2 did, dropping the empty entries so that an empty value keeps meaning
// "the default".
//
// An empty string means the default in v2 (all origins, the default methods,
// the headers the request asks for), which in v3 is a missing field, where
// []string{""} would panic or send an empty header.
func MigrateCORSConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateCORSContent)
	if err != nil {
		return fmt.Errorf("failed to migrate CORS configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating CORS middleware configs")
	return nil
}

// corsMigration is the state of the rewrite of one file.
type corsMigration struct {
	g            *goSource
	stringsName  string
	edits        editSet
	needsStrings bool
}

func migrateCORSContent(content string, fi internal.FileInfo) string {
	names := mw1Qualifiers(mw1CORS.names(content))
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	m := &corsMigration{g: g}
	m.stringsName, m.needsStrings = mw1ImportName(content, "strings", "strings")
	usedStrings := false
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if lit, isConfig := mw1ConfigLit(n, names); isConfig {
			usedStrings = m.literal(lit, stack) || usedStrings
		} else if as, field, isAssign := mw1AssignedField(n, stack, names); isAssign && slices.Contains(corsListFields, field) {
			usedStrings = m.assignment(as, stack) || usedStrings
		}
		return true
	})

	out := m.edits.apply(content)
	if usedStrings && m.needsStrings {
		out = mw1AddImport(out, "strings")
	}
	return out
}

// literal rewrites the list fields of a cors.Config literal. It reports whether
// the rewrite uses the strings package.
func (m *corsMigration) literal(lit *ast.CompositeLit, stack []ast.Node) (usesStrings bool) {
	for i, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		field, isIdent := kv.Key.(*ast.Ident)
		if !isIdent || !slices.Contains(corsListFields, field.Name) {
			continue
		}

		v := m.convert(kv.Value, stack)
		switch {
		case v.keep:
		case v.empty:
			start, end := mw1ElementSpan(m.g, lit, i)
			m.edits.replace(start, end, "")
		default:
			m.edits.replaceNode(m.g, kv.Value, v.text)
			usesStrings = usesStrings || v.usesStrings
		}
	}
	return usesStrings
}

// assignment rewrites `cfg.AllowOrigins = value`. It reports whether the
// rewrite uses the strings package.
func (m *corsMigration) assignment(as *ast.AssignStmt, stack []ast.Node) (usesStrings bool) {
	v := m.convert(as.Rhs[0], stack)
	switch {
	case v.keep:
		return false
	case v.empty:
		m.edits.replaceNode(m.g, as.Rhs[0], "nil")
		return false
	}
	m.edits.replaceNode(m.g, as.Rhs[0], v.text)
	return v.usesStrings
}

// corsValue is the outcome of the conversion of one expression.
type corsValue struct {
	text string
	// keep is set when the expression already is a slice.
	keep bool
	// empty is set when the expression is the empty string, a missing field in v3.
	empty bool
	// usesStrings is set when text calls the strings package.
	usesStrings bool
}

// convert returns the v3 value of a v2 comma separated string expression.
func (m *corsMigration) convert(e ast.Expr, stack []ast.Node) corsValue {
	if isSliceExpr(e, stack) {
		return corsValue{keep: true}
	}

	if s, ok := mw1StringLiteral(e); ok {
		items := splitList(s)
		if len(items) == 0 {
			return corsValue{empty: true}
		}
		return corsValue{text: mw1QuoteList(items)}
	}

	// strings.Join(list, ",") is the list
	if call, ok := e.(*ast.CallExpr); ok && m.isJoinWithComma(call) {
		return corsValue{text: m.g.text(call.Args[0])}
	}

	return corsValue{
		text: m.stringsName + ".FieldsFunc(" + m.g.text(e) + ", func(r rune) bool { return r == ',' || r == ' ' })",
		// FieldsFunc drops the empty entries, which is what makes an empty
		// value mean the default again.
		usesStrings: true,
	}
}

// isJoinWithComma recognizes strings.Join(list, ",") and the same with a space.
func (m *corsMigration) isJoinWithComma(call *ast.CallExpr) bool {
	if len(call.Args) != 2 || !mw1IsMember(call.Fun, []string{m.stringsName}, "Join") {
		return false
	}
	sep, ok := mw1StringLiteral(call.Args[1])
	return ok && strings.TrimSpace(sep) == ","
}

// splitList splits a comma separated list and drops the blanks.
func splitList(s string) []string {
	var items []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			items = append(items, part)
		}
	}
	return items
}

// isSliceExpr reports whether e is evidently a slice: a slice literal, a
// conversion to a slice, the result of the functions that split a string, nil,
// or a variable declared with one of those.
func isSliceExpr(e ast.Expr, stack []ast.Node) bool {
	return isSliceExprDepth(e, stack, 0)
}

func isSliceExprDepth(e ast.Expr, stack []ast.Node, depth int) bool {
	if depth > 3 {
		return false
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return isSliceExprDepth(x.X, stack, depth+1)
	case *ast.CompositeLit:
		_, isArray := x.Type.(*ast.ArrayType)
		return isArray
	case *ast.Ident:
		if x.Name == mw1NilIdent {
			return true
		}
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			_, isArray := d.typ.(*ast.ArrayType)
			return isArray
		}
		return d.init != nil && isSliceExprDepth(d.init, stack, depth+1)
	case *ast.CallExpr:
		if _, isArray := x.Fun.(*ast.ArrayType); isArray {
			return true
		}
		if id, ok := x.Fun.(*ast.Ident); ok {
			return id.Name == "append" || id.Name == "make" && len(x.Args) > 0 && isArrayType(x.Args[0])
		}
		pkg, fn, ok := mw1Selector(x.Fun)
		if !ok {
			return false
		}
		return pkg == "slices" || pkg == "strings" && slices.Contains([]string{"Split", "SplitN", "SplitAfter", "SplitAfterN", "Fields", "FieldsFunc"}, fn)
	}
	return false
}

func isArrayType(e ast.Expr) bool {
	_, ok := e.(*ast.ArrayType)
	return ok
}
