package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var mw1Envvar = newMW1Package("envvar")

// MigrateEnvVarConfig moves the envvar middleware to v3, where ExcludeVars is
// gone and only the variables in ExportVars are exposed.
//
// v2 exposed every variable of the environment except the excluded ones when
// ExportVars was empty. Removing ExcludeVars is safe, v3 exposes less, but an
// app that relied on the full list stops getting it: a config or a New() call
// without ExportVars is reported, with the variables it excluded.
func MigrateEnvVarConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateEnvVarContent)
	if err != nil {
		return fmt.Errorf("failed to migrate EnvVar configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating EnvVar middleware configs")
	return nil
}

// The fields of envvar.Config.
const (
	envvarExportVars  = "ExportVars"
	envvarExcludeVars = "ExcludeVars"
)

// envvarMigration is the state of the rewrite of one file.
type envvarMigration struct {
	g     *goSource
	rep   *mw1Reporter
	edits editSet
	// exported is set when the file assigns ExportVars to a config variable
	exported bool
}

func migrateEnvVarContent(content string, fi internal.FileInfo) string {
	names := mw1Qualifiers(mw1Envvar.names(content))
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	m := &envvarMigration{g: g, rep: newMW1Reporter(g, fi)}
	var unexported []func()
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if lit, isConfig := mw1ConfigLit(x, names); isConfig {
				if report := m.config(lit); report != nil {
					unexported = append(unexported, report)
				}
			}
		case *ast.CallExpr:
			if mw1IsMember(x.Fun, names, "New") && len(x.Args) == 0 {
				unexported = append(unexported, func() { m.reportBareNew(x) })
			}
		case *ast.AssignStmt:
			if _, field, isAssign := mw1AssignedField(x, stack, names); isAssign {
				m.assignment(x, field, stack)
			}
		}
		return true
	})

	// a config that is completed by an assignment is not reported
	if !m.exported {
		for _, report := range unexported {
			report()
		}
	}
	out := m.edits.apply(content)
	m.rep.flush(&m.edits, out, out)
	return out
}

// config removes ExcludeVars from an envvar.Config literal. It returns the
// function that reports the literal when it has no ExportVars.
func (m *envvarMigration) config(lit *ast.CompositeLit) func() {
	var excluded *ast.KeyValueExpr
	index := -1
	exports := false
	for i, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		switch {
		case isIdentNamed(kv.Key, envvarExcludeVars):
			excluded, index = kv, i
		case isIdentNamed(kv.Key, envvarExportVars):
			exports = !isEmptyMap(kv.Value)
		}
	}

	if excluded != nil {
		start, end := mw1ElementSpan(m.g, lit, index)
		m.edits.replace(start, end, "")
	}
	if exports {
		return nil
	}
	vars := excludedNames(excluded)
	return func() { m.reportNoExport(lit, vars) }
}

// isEmptyMap reports whether e is a map literal without entries.
func isEmptyMap(e ast.Expr) bool {
	lit, ok := e.(*ast.CompositeLit)
	return ok && len(lit.Elts) == 0
}

// excludedNames returns the names of the variables a literal ExcludeVars lists.
func excludedNames(kv *ast.KeyValueExpr) []string {
	if kv == nil {
		return nil
	}
	lit, ok := kv.Value.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var vars []string
	for _, elt := range lit.Elts {
		pair, isPair := elt.(*ast.KeyValueExpr)
		if !isPair {
			continue
		}
		if name, isLiteral := mw1StringLiteral(pair.Key); isLiteral {
			vars = append(vars, name)
		}
	}
	return vars
}

func (m *envvarMigration) reportNoExport(lit *ast.CompositeLit, excluded []string) {
	was := "v2 exposed every variable of the environment"
	if len(excluded) > 0 {
		quoted := make([]string, len(excluded))
		for i, name := range excluded {
			quoted[i] = strconv.Quote(name)
		}
		was += " except " + strings.Join(quoted, ", ")
	}
	m.rep.notef(lit, "envvar exposes nothing without ExportVars in v3, %s: list the variables to expose in ExportVars", was)
}

func (m *envvarMigration) reportBareNew(call *ast.CallExpr) {
	m.rep.notef(call, "envvar.New() exposes nothing without ExportVars in v3, v2 exposed every variable of the environment: "+
		"pass envvar.Config{ExportVars: ...} with the variables to expose")
}

// assignment handles `cfg.Field = value` on a variable that holds an
// envvar.Config.
func (m *envvarMigration) assignment(as *ast.AssignStmt, field string, stack []ast.Node) {
	switch field {
	case envvarExportVars:
		m.exported = true
	case envvarExcludeVars:
		if m.isLineStatement(as, stack) {
			start := m.g.offset(as.Pos())
			lineStart := start - len(mw1Indent(m.g.src, start))
			end := m.g.offset(as.End())
			if end < len(m.g.src) && m.g.src[end] == '\n' {
				end++
			}
			m.edits.replace(lineStart, end, "")
			return
		}
		m.rep.notef(as, "envvar.Config has no ExcludeVars in v3: remove this assignment, and list the variables to expose in ExportVars")
	}
}

// isLineStatement reports whether the assignment is a statement of a block that
// takes its line on its own.
func (m *envvarMigration) isLineStatement(as *ast.AssignStmt, stack []ast.Node) bool {
	if len(stack) == 0 || as.Tok != token.ASSIGN {
		return false
	}
	switch stack[len(stack)-1].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
	default:
		return false
	}
	end := mw1SkipSpaces(m.g.src, m.g.offset(as.End()))
	return mw1StartsLine(m.g.src, m.g.offset(as.Pos())) && (end >= len(m.g.src) || m.g.src[end] == '\n')
}
