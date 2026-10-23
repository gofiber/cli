package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// the names of the otel middleware that this migration asks about
const (
	otelWithServerName             = "WithServerName"
	otelWithCustomAttributes       = "WithCustomAttributes"
	otelWithCustomMetricAttributes = "WithCustomMetricAttributes"
	otelOptionType                 = "Option"

	// otelAttributePath is the package of the attributes that the options take.
	otelAttributePath = "go.opentelemetry.io/otel/attribute"
	// otelServerNameKey is the key that WithServerName set.
	otelServerNameKey = "http.server_name"
)

// MigrateOtelOptions replaces the options of the otel middleware that its v3
// module no longer has. The v2 module (otelfiber 1.0) had one:
//
//   - WithServerName(name) put the attribute http.server_name on every span and on
//     every metric. The v3 module has no such option (the stable HTTP conventions
//     it follows have no server name), and its two options for attributes of the
//     caller put the same attribute where it was:
//
//     WithCustomAttributes(func(fiber.Ctx) []attribute.KeyValue {
//     return []attribute.KeyValue{attribute.String("http.server_name", name)}
//     })
//
//     and the same with WithCustomMetricAttributes for the metrics.
//
// The two options keep one function each, and the last one given wins, so the
// rewrite is made where it cannot replace attributes the project sets (the option
// stands in the list of options of the call or of a []otelfiber.Option literal
// and nothing next to it sets custom attributes) and where the name is a value,
// not a call that v2 evaluated once. Anywhere else the option is left, which stops
// the build, with a note that says what to write.
func MigrateOtelOptions(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateOtelContent)
	if err != nil {
		return fmt.Errorf("failed to migrate the otel options: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating otel options")
	return nil
}

// otelFile is a file that imports the otel middleware.
type otelFile struct {
	*contribFile
	// rep holds the notes until the edits are known, which move the lines below them.
	rep *mw1Reporter
	// names are the identifiers of the file for the middleware.
	names []string
	// customs is set when the file passes custom attributes to the middleware
	// anywhere.
	customs bool
}

func migrateOtelContent(content string, fi internal.FileInfo) string {
	if !strings.Contains(content, otelWithServerName) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	names := contribNames(g, "otel")
	if len(names) == 0 {
		return content
	}

	o := &otelFile{contribFile: newContribFile(g, fi), rep: newMW1Reporter(g, fi), names: names}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if sel, isMember := o.member(n, stack); isMember {
			o.customs = o.customs || sel.Sel.Name == otelWithCustomAttributes || sel.Sel.Name == otelWithCustomMetricAttributes
		}
		return true
	})
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if call, isCall := n.(*ast.CallExpr); isCall {
			if sel, isMember := o.member(call.Fun, stack); isMember && sel.Sel.Name == otelWithServerName {
				o.serverName(call, sel, stack)
			}
		}
		return true
	})
	out := o.apply()
	o.rep.flush(&o.edits, out, out)
	return out
}

// member returns e as pkg.Name when pkg is the middleware here, and not a variable
// that has the name of the import.
func (o *otelFile) member(e ast.Node, stack []ast.Node) (*ast.SelectorExpr, bool) {
	x, isExpr := e.(ast.Expr)
	if !isExpr {
		return nil, false
	}
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	id, ok := sel.X.(*ast.Ident)
	return sel, ok && isPackageName(id, o.names, stack)
}

// memberNamed is member for the names given.
func (o *otelFile) memberNamed(e ast.Node, stack []ast.Node, names ...string) bool {
	sel, ok := o.member(e, stack)
	return ok && slices.Contains(names, sel.Sel.Name)
}

// serverName migrates one call of WithServerName, sel being its callee.
func (o *otelFile) serverName(call *ast.CallExpr, sel *ast.SelectorExpr, stack []ast.Node) {
	if reason := o.cannotRewrite(call, stack); reason != "" {
		o.rep.notef(call, "otel: WithServerName is not in the v3 module. It put the attribute %s on the spans and on the metrics, "+
			"and WithCustomAttributes and WithCustomMetricAttributes can put it there with attribute.String(%q, name) (left as it is: %s)",
			otelServerNameKey, otelServerNameKey, reason)
		return
	}

	pkg := o.g.text(sel.X)
	fiberPkg := o.fiberName()
	attr := o.importName(otelAttributePath, "attribute", "otelattribute")
	name := o.g.text(call.Args[0])

	option := func(member string) string {
		return pkg + "." + member + "(func(" + fiberPkg + ".Ctx) []" + attr + ".KeyValue {\n" +
			"return []" + attr + ".KeyValue{" + attr + ".String(" + strconv.Quote(otelServerNameKey) + ", " + name + ")}\n" +
			"})"
	}
	o.edits.replaceNode(o.g, call, option(otelWithCustomAttributes)+",\n"+option(otelWithCustomMetricAttributes))
}

// cannotRewrite returns what keeps the call of WithServerName from being
// rewritten, "" when nothing does.
func (o *otelFile) cannotRewrite(call *ast.CallExpr, stack []ast.Node) string {
	if len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return "the call does not have the one argument of v2"
	}
	if !otelIsValue(call.Args[0]) {
		return "the name is the result of a call, which v2 made once: make it first, then pass it"
	}

	others, inLiteral, ok := o.siblings(call, stack)
	if !ok {
		return "the option is neither an argument of Middleware or New nor an element of a []Option literal"
	}
	for _, other := range others {
		if c, isCall := other.(*ast.CallExpr); isCall && o.memberNamed(c.Fun, stack, otelWithCustomAttributes, otelWithCustomMetricAttributes) {
			return "the options next to it set custom attributes already, and the last option of a kind replaces the others: add the name to them"
		}
		if !o.isOptionCall(other, stack) {
			return "an option next to it is not written here, and it may set custom attributes, which the last option of a kind would replace"
		}
	}
	if inLiteral && o.customs {
		return "this file sets custom attributes elsewhere, and the list may be passed on together with them, when the last option of a kind replaces the others"
	}
	return ""
}

// siblings returns the other elements of the list of options the call stands in:
// the arguments of Middleware (or New) and the elements of a []Option literal.
func (o *otelFile) siblings(call *ast.CallExpr, stack []ast.Node) (others []ast.Expr, inLiteral, ok bool) {
	if len(stack) == 0 {
		return nil, false, false
	}

	var list []ast.Expr
	switch parent := stack[len(stack)-1].(type) {
	case *ast.CallExpr:
		if parent.Ellipsis.IsValid() || !o.memberNamed(parent.Fun, stack[:len(stack)-1], "Middleware", "New") {
			return nil, false, false
		}
		list = parent.Args
	case *ast.CompositeLit:
		arr, isArray := parent.Type.(*ast.ArrayType)
		if !isArray || arr.Len != nil || !o.memberNamed(arr.Elt, stack[:len(stack)-1], otelOptionType) {
			return nil, false, false
		}
		list, inLiteral = parent.Elts, true
	default:
		return nil, false, false
	}

	for _, e := range list {
		if e != ast.Expr(call) {
			others = append(others, e)
		}
	}
	return others, inLiteral, slices.Contains(list, ast.Expr(call))
}

// isOptionCall reports whether e is a call of a function of the middleware.
func (o *otelFile) isOptionCall(e ast.Expr, stack []ast.Node) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	_, ok = o.member(call.Fun, stack)
	return ok
}

// otelIsValue reports whether e is a value that can be read again at every
// request without a difference: a literal, a name, a field, and the sum of them.
func otelIsValue(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING
	case *ast.Ident:
		return true
	case *ast.ParenExpr:
		return otelIsValue(x.X)
	case *ast.SelectorExpr:
		return otelIsValue(x.X)
	case *ast.BinaryExpr:
		return x.Op == token.ADD && otelIsValue(x.X) && otelIsValue(x.Y)
	}
	return false
}
