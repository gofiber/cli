package v3

import (
	"fmt"
	"go/ast"
	"regexp"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3CoreMonitorPath matches the import path of the monitor middleware of the core
// module at any major version; v3 moved it to the contrib module.
var mw3CoreMonitorPath = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/monitor$`)

const mw3MonitorPath = "github.com/gofiber/contrib/v3/monitor"

// mw3ChartJsURL is the name of the field that v3 spells ChartJSURL.
const mw3ChartJsURL = "ChartJsURL"

// mw3MonitorIgnored are the fields that contrib/v3/monitor still has for source
// compatibility, but no longer reads: its dashboard takes no custom head markup
// and loads neither fonts nor Chart.js.
var mw3MonitorIgnored = []string{"CustomHead", "FontURL", "ChartJSURL"}

// MigrateMonitorImport moves the monitor middleware to the contrib module where
// v3 has it, under the name the file imported it by. ChartJsURL of its config is
// spelled ChartJSURL there, and that field and CustomHead and FontURL no longer
// have an effect, which is reported.
func MigrateMonitorImport(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, mw3MonitorContent)
	if err != nil {
		return fmt.Errorf("failed to migrate monitor import: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating monitor middleware import")
	return nil
}

func mw3MonitorContent(src string, fi internal.FileInfo) string {
	if !strings.Contains(src, "middleware/monitor") {
		return src
	}
	names := importNames(src, mw3CoreMonitorPath, "monitor")
	if len(names) == 0 {
		return src
	}
	g, ok := parseFile(fi, src)
	if !ok {
		return src
	}

	var edits editSet
	for _, spec := range g.file.Imports {
		if path, err := strconv.Unquote(spec.Path.Value); err == nil && mw3CoreMonitorPath.MatchString(path) {
			edits.replaceNode(g, spec.Path, strconv.Quote(mw3MonitorPath))
		}
	}

	m := &mw3MonitorPass{g: g, fi: fi, names: names, edits: &edits}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			if n.Type != nil && m.isMember(n.Type, mw3Config, stack) {
				m.configLit(n)
			}
		case *ast.SelectorExpr:
			m.field(n, stack)
		}
		return true
	})
	return edits.apply(src)
}

// mw3MonitorPass is the rewrite of one file by MigrateMonitorImport.
type mw3MonitorPass struct {
	g     *goSource
	edits *editSet
	fi    internal.FileInfo
	// names are the identifiers the file binds to the monitor package.
	names []string
}

// isMember reports whether e is monitor.member.
func (m *mw3MonitorPass) isMember(e ast.Expr, member string, stack []ast.Node) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != member {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || !slices.Contains(m.names, id.Name) {
		return false
	}
	_, shadowed := resolveIdent(id.Name, id.Pos(), stack)
	return !shadowed
}

// configLit migrates a monitor.Config literal.
func (m *mw3MonitorPass) configLit(lit *ast.CompositeLit) {
	var ignored []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		if key.Name == mw3ChartJsURL {
			m.edits.replaceNode(m.g, key, "ChartJSURL")
			ignored = append(ignored, "ChartJSURL")
		} else if slices.Contains(mw3MonitorIgnored, key.Name) {
			ignored = append(ignored, key.Name)
		}
	}
	m.reportIgnored(lit, ignored)
}

// field migrates cfg.ChartJsURL on a variable that holds a monitor config.
func (m *mw3MonitorPass) field(sel *ast.SelectorExpr, stack []ast.Node) {
	name := sel.Sel.Name
	if name != mw3ChartJsURL && !slices.Contains(mw3MonitorIgnored, name) {
		return
	}
	if !m.isConfigVar(sel.X, stack) {
		return
	}
	if name == mw3ChartJsURL {
		m.edits.replaceNode(m.g, sel.Sel, "ChartJSURL")
		name = "ChartJSURL"
	}
	m.reportIgnored(sel, []string{name})
}

// isConfigVar reports whether e is a variable that holds a monitor Config.
func (m *mw3MonitorPass) isConfigVar(e ast.Expr, stack []ast.Node) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	if !ok {
		return false
	}
	typ := d.typ
	for {
		star, isStar := typ.(*ast.StarExpr)
		if !isStar {
			break
		}
		typ = star.X
	}
	if typ != nil {
		return m.isMember(typ, mw3Config, stack)
	}

	init := d.init
	if un, isUnary := init.(*ast.UnaryExpr); isUnary {
		init = un.X
	}
	switch x := init.(type) {
	case *ast.CompositeLit:
		return x.Type != nil && m.isMember(x.Type, mw3Config, stack)
	case *ast.SelectorExpr:
		return m.isMember(x, mw3ConfigDefault, stack)
	}
	return false
}

// reportIgnored tells the user that the fields are accepted and do nothing.
func (m *mw3MonitorPass) reportIgnored(at ast.Node, fields []string) {
	if len(fields) == 0 {
		return
	}
	slices.Sort(fields)
	fields = slices.Compact(fields)
	internal.Notef(m.fi, m.g.line(at.Pos()),
		"monitor.Config %s: github.com/gofiber/contrib/v3/monitor still accepts it, but its dashboard has no use for it any more "+
			"(no custom head markup, no external fonts, no Chart.js), so it has no effect.", strings.Join(fields, ", "))
}
