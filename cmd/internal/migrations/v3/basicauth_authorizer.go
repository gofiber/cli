package v3

import (
	"fmt"
	"go/ast"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var mw1Basicauth = newMW1Package("basicauth")

// MigrateBasicauthAuthorizer gives the Authorizer of basicauth.Config the
// request context that v3 passes as its third argument: a function literal gets
// the parameter, any other function (a declared function, a method value, a
// variable) is wrapped.
func MigrateBasicauthAuthorizer(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateBasicauthAuthorizerContent)
	if err != nil {
		return fmt.Errorf("failed to migrate basicauth authorizer: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating basicauth authorizer")
	return nil
}

// basicauthAuthorizerParams is the number of parameters of a v2 Authorizer; the
// one of v3 takes the context as well.
const basicauthAuthorizerParams = 2

// authorizerMigration is the state of the rewrite of one file.
type authorizerMigration struct {
	g          *goSource
	rep        *mw1Reporter
	fiber      string
	edits      editSet
	needsFiber bool
	usesFiber  bool
}

func migrateBasicauthAuthorizerContent(content string, fi internal.FileInfo) string {
	names := mw1Qualifiers(mw1Basicauth.names(content))
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	m := &authorizerMigration{g: g, rep: newMW1Reporter(g, fi)}
	m.fiber, m.needsFiber = mw1FiberName(content)
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if lit, isConfig := mw1ConfigLit(n, names); isConfig {
			if kv := mw1KeyValue(lit, "Authorizer"); kv != nil {
				m.rewrite(kv.Value)
			}
		} else if as, field, isAssign := mw1AssignedField(n, stack, names); isAssign && field == "Authorizer" {
			m.rewrite(as.Rhs[0])
		}
		return true
	})

	applied := m.edits.apply(content)
	out := applied
	if m.usesFiber && m.needsFiber {
		out = mw1AddImport(out, mw1FiberCoreImport)
	}
	m.rep.flush(&m.edits, applied, out)
	return out
}

// rewrite gives one Authorizer expression the third parameter.
func (m *authorizerMigration) rewrite(e ast.Expr) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		m.rewrite(x.X)
	case *ast.FuncLit:
		if mw1ParamCount(x.Type) != basicauthAuthorizerParams {
			return
		}
		offset, text := mw1AppendParam(m.g, x.Type, m.fiber+".Ctx")
		m.edits.replace(offset, offset, text)
		m.usesFiber = true
	case *ast.Ident:
		if x.Name == mw1NilIdent {
			return
		}
		m.wrap(e)
	case *ast.SelectorExpr:
		m.wrap(e)
	default:
		m.rep.notef(e,
			"the Authorizer of basicauth.Config takes the request context as a third argument in v3: wrap %s into a func(user, pass string, c %s.Ctx) bool",
			m.g.text(e), m.fiber)
	}
}

// wrap replaces a function value with a function literal that calls it, unless
// the file shows that the function already takes the context.
func (m *authorizerMigration) wrap(e ast.Expr) {
	if mw1DeclaredWithParams(m.g.file, e, basicauthAuthorizerParams+1) {
		return
	}

	user, pass := mw1FreeNames(e, "user", "pass")
	m.edits.replaceNode(m.g, e, fmt.Sprintf("func(%s, %s string, _ %s.Ctx) bool { return %s(%s, %s) }",
		user, pass, m.fiber, m.g.text(e), user, pass))
	m.usesFiber = true
}

// mw1FreeNames returns names for two parameters of a closure that do not shadow
// anything the expression refers to: the given ones when they are free.
func mw1FreeNames(e ast.Expr, first, second string) (a, b string) {
	used := map[string]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for i := 0; ; i++ {
		a, b = first, second
		if i > 0 {
			suffix := "Arg"
			if i > 1 {
				suffix += strconv.Itoa(i)
			}
			a, b = first+suffix, second+suffix
		}
		if !used[a] && !used[b] {
			return a, b
		}
	}
}
