package v3

import (
	"fmt"
	"go/ast"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateLoggerConfig moves the logger middleware to its v3 config: Config.Output
// is called Stream, and Data.Timestamp, an atomic.Value in v2, is a plain string,
// so data.Timestamp.Load().(string) is data.Timestamp. Config literals and
// variables, and the Data parameters of custom tags, are found through the import
// path and the declared types.
func MigrateLoggerConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateLoggerConfigContent)
	if err != nil {
		return fmt.Errorf("failed to migrate logger configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating logger middleware configs")
	return nil
}

func migrateLoggerConfigContent(content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "Output") && !strings.Contains(content, "Timestamp") {
		return content
	}
	names := importNames(content, mw2Logger, "logger")
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			migrateLoggerLiteral(g, &edits, fi, x, names)
		case *ast.SelectorExpr:
			if x.Sel.Name == "Output" && mw2IsTyped(x.X, names, mw2ConfigType, stack, 0) {
				edits.replaceNode(g, x.Sel, "Stream")
			}
		case *ast.TypeAssertExpr:
			migrateLoggerTimestamp(g, &edits, x, names, stack)
		}
		return true
	})
	return edits.apply(content)
}

// migrateLoggerLiteral renames the Output key of a logger.Config literal.
func migrateLoggerLiteral(g *goSource, edits *editSet, fi internal.FileInfo, n *ast.CompositeLit, names []string) {
	lit, ok := mw2ConfigLit(n, names)
	if !ok || !mw2IsKeyed(lit) {
		return
	}
	_, output := mw2Field(lit, "Output")
	if output == nil {
		return
	}
	if _, stream := mw2Field(lit, "Stream"); stream != nil {
		internal.Notef(fi, g.line(output.Pos()), "logger.Config sets both Output and Stream; Output no longer exists, merge it into Stream by hand")
		return
	}
	edits.replaceNode(g, output.Key, "Stream")
}

// migrateLoggerTimestamp turns data.Timestamp.Load().(string) on a *logger.Data
// into data.Timestamp.
func migrateLoggerTimestamp(g *goSource, edits *editSet, assert *ast.TypeAssertExpr, names []string, stack []ast.Node) {
	if !isIdentNamed(assert.Type, "string") {
		return
	}
	load, ok := assert.X.(*ast.CallExpr)
	if !ok || len(load.Args) != 0 {
		return
	}
	loadSel, ok := load.Fun.(*ast.SelectorExpr)
	if !ok || loadSel.Sel.Name != "Load" {
		return
	}
	field, ok := loadSel.X.(*ast.SelectorExpr)
	if !ok || field.Sel.Name != "Timestamp" {
		return
	}
	data, ok := field.X.(*ast.Ident)
	if !ok {
		return
	}
	d, ok := resolveIdent(data.Name, data.Pos(), stack)
	if !ok || d.typ == nil || !isFiberType(d.typ, names, "Data") {
		return
	}
	edits.replaceNode(g, assert, g.text(field))
}
