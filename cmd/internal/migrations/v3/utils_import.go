package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

const (
	importsUtilsModule    = "github.com/gofiber/utils/v2"
	importsTestifyRequire = "github.com/stretchr/testify/require"
	importsSyncAtomic     = "sync/atomic"
)

// importsReFiberUtils matches the import path of the utils package of the core
// fiber module at any major version. Fiber v2 shipped the helpers there, v3
// moved them to a module of their own.
var importsReFiberUtils = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/utils$`)

// importsUtilsRenames lists the helpers of the v2 utils package that
// github.com/gofiber/utils/v2 offers under another name. Every other helper
// keeps its name and signature: Trim, TrimLeft, TrimRight and EqualFold take the
// same arguments (they became generic over strings and byte slices), so the
// calls stay as they are.
var importsUtilsRenames = map[string]string{
	"GetString":        "UnsafeString",
	"GetBytes":         "UnsafeBytes",
	"ImmutableString":  "CopyString",
	"AddTrailingSlash": "AddTrailingSlashString",
	"TrimBytes":        "Trim",
	"TrimLeftBytes":    "TrimLeft",
	"TrimRightBytes":   "TrimRight",
	"EqualFoldBytes":   "EqualFold",
}

// MigrateUtilsImport moves the code that uses the utils package of Fiber v2 to
// github.com/gofiber/utils/v2 and renames the helpers that changed. It only
// touches files that import the Fiber utils package, and only through the name
// that file gives it.
func MigrateUtilsImport(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, importsMigrateUtils)
	if err != nil {
		return fmt.Errorf("failed to migrate utils imports: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating utils imports")
	return nil
}

func importsMigrateUtils(content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "/utils") {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	specs := importsOf(g, importsReFiberUtils.MatchString)
	if len(specs) == 0 {
		return content
	}

	m := &importsUtilsMigration{g: g, fi: fi, atomic: importsNameOf(g, importsSyncAtomic, "atomic")}
	for _, s := range specs {
		importsRewritePath(g, &m.edits, s, importsUtilsModule)
		switch {
		case s.blank():
		case s.dot():
			m.dot = true
		default:
			m.names = append(m.names, s.local("utils"))
		}
	}
	g.walk(m.visit)

	out := m.edits.apply(content)
	if m.addRequire {
		g2, ok := parseSource(out)
		if !ok {
			return content
		}
		var imp editSet
		importsAdd(g2, &imp, m.requireAlias, importsTestifyRequire)
		out = imp.apply(out)
	}
	return out
}

// importsUtilsMigration rewrites the uses of the Fiber utils package in one file.
type importsUtilsMigration struct {
	g  *goSource
	fi internal.FileInfo
	// atomic is the identifier of sync/atomic in the file, empty when it is not
	// imported.
	atomic string
	// require is the identifier of testify's require package, once AssertEqual
	// needs it. addRequire says that the file does not import it yet, under
	// requireAlias when the plain name is taken.
	require      string
	requireAlias string
	// names are the identifiers the file binds to the package, dot is set when
	// it is also dot imported.
	names      []string
	edits      editSet
	dot        bool
	addRequire bool
}

func (m *importsUtilsMigration) visit(n ast.Node, stack []ast.Node) bool {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if ok && slices.Contains(m.names, id.Name) && !importsShadowed(id.Name, id.Pos(), stack) {
			m.member(x, x.Sel, id.Name, stack)
			return false
		}
	case *ast.Ident:
		// A dot import makes the helpers plain identifiers. Only call and
		// atomic.LoadUint32(&Timestamp) positions are recognized: anywhere else
		// the identifier may well be something else.
		if m.dot && importsUtilsCalled(x, stack) && !importsShadowed(x.Name, x.Pos(), stack) {
			m.member(x, x, "", stack)
		}
	}
	return true
}

// importsUtilsCalled reports whether id is the function of a call, or the operand of
// the address-of in the single argument of a call.
func importsUtilsCalled(id *ast.Ident, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	switch parent := stack[len(stack)-1].(type) {
	case *ast.CallExpr:
		return parent.Fun == id
	case *ast.UnaryExpr:
		return parent.Op == token.AND && id.Name == "Timestamp"
	}
	return false
}

// member rewrites one use of a helper. ref is the whole reference
// (utils.Name or Name), ident the identifier that carries the helper's name and
// qualifier the identifier of the package, empty for a dot import.
func (m *importsUtilsMigration) member(ref ast.Expr, ident *ast.Ident, qualifier string, stack []ast.Node) {
	name := ident.Name
	switch {
	case importsUtilsRenames[name] != "":
		m.edits.replaceNode(m.g, ident, importsUtilsRenames[name])
	case name == "AssertEqual":
		// AssertEqual stopped the test on a failure, require does too.
		m.edits.replaceNode(m.g, ref, m.requireName()+".Equal")
	case name == "Timestamp":
		m.timestamp(ref, qualifier, stack)
	case name == "MaxStringLen":
		internal.Notef(m.fi, m.g.line(ref.Pos()),
			"`utils.MaxStringLen` does not exist in %s (it only applied to Go before 1.20); remove the check.", importsUtilsModule)
	}
}

// timestamp rewrites the use of the utils.Timestamp variable, a function in
// github.com/gofiber/utils/v2.
func (m *importsUtilsMigration) timestamp(ref ast.Expr, qualifier string, stack []ast.Node) {
	call := qualifier + ".Timestamp()"
	if qualifier == "" {
		call = "Timestamp()"
	}

	var parent, grand ast.Node
	if len(stack) > 0 {
		parent = stack[len(stack)-1]
	}
	if len(stack) > 1 {
		grand = stack[len(stack)-2]
	}

	switch p := parent.(type) {
	case *ast.UnaryExpr:
		// atomic.LoadUint32(&utils.Timestamp) is the documented way to read it.
		if load, ok := grand.(*ast.CallExpr); ok && m.isAtomicLoad(load) && len(load.Args) == 1 {
			m.edits.replaceNode(m.g, load, call)
			return
		}
	case *ast.AssignStmt:
		if slices.Contains(p.Lhs, ref) {
			break
		}
		m.edits.replace(m.g.offset(ref.End()), m.g.offset(ref.End()), "()")
		return
	case *ast.IncDecStmt:
	default:
		m.edits.replace(m.g.offset(ref.End()), m.g.offset(ref.End()), "()")
		return
	}

	internal.Notef(m.fi, m.g.line(ref.Pos()),
		"`utils.Timestamp` is the function `utils.Timestamp()` in %s and cannot be written or addressed; read it with `utils.Timestamp()`.", importsUtilsModule)
}

// isAtomicLoad reports whether call is sync/atomic's LoadUint32.
func (m *importsUtilsMigration) isAtomicLoad(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && m.atomic != "" && sel.Sel.Name == "LoadUint32" && isIdentNamed(sel.X, m.atomic)
}

// requireName returns the identifier of testify's require package, arranging
// for the import when the file has none.
func (m *importsUtilsMigration) requireName() string {
	if m.require != "" {
		return m.require
	}

	m.require = importsNameOf(m.g, importsTestifyRequire, "require")
	if m.require != "" {
		return m.require
	}

	m.require, m.addRequire = "require", true
	if importsBinds(m.g, m.require) {
		m.require = "testifyrequire"
		m.requireAlias = m.require
	}
	return m.require
}
