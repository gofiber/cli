package v3

import (
	"fmt"
	"go/ast"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var mw1Favicon = newMW1Package("favicon")

var mw1NetHTTP = regexp.MustCompile(`^net/http$`)

// MigrateFaviconConfig moves favicon.Config to v3, where FileSystem is an
// io/fs.FS and no longer an http.FileSystem:
//
//   - http.FS(fsys) becomes fsys and http.Dir(dir) becomes os.DirFS(dir). Any
//     other file system is left as it is, and reported, since an
//     http.FileSystem cannot be used as an fs.FS.
//   - The names an fs.FS opens are slash separated and relative, without a
//     leading / or ./, and v3 panics at start-up on File: "/favicon.ico", which
//     http.Dir and http.FS accepted. A literal File is cleaned up accordingly.
func MigrateFaviconConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateFaviconContent)
	if err != nil {
		return fmt.Errorf("failed to migrate favicon configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating favicon middleware configs")
	return nil
}

// faviconMigration is the state of the rewrite of one file.
type faviconMigration struct {
	g       *goSource
	rep     *mw1Reporter
	httpPkg []string
	osName  string
	edits   editSet
	needsOS bool
	usesOS  bool
	// io/fs is imported for the type of a variable that was converted
	needsIOFS bool
	usesIOFS  bool
}

func migrateFaviconContent(content string, fi internal.FileInfo) string {
	names := mw1Qualifiers(mw1Favicon.names(content))
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	m := &faviconMigration{g: g, rep: newMW1Reporter(g, fi)}
	m.httpPkg = mw1Qualifiers(importNames(content, mw1NetHTTP, "http"))
	m.osName, m.needsOS = mw1ImportName(content, "os", "os")
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if lit, isConfig := mw1ConfigLit(n, names); isConfig {
			m.config(lit, stack)
		} else if as, field, isAssign := mw1AssignedField(n, stack, names); isAssign && field == faviconFileSystem {
			m.fileSystem(as.Rhs[0], stack)
		}
		return true
	})

	applied := m.edits.apply(content)
	out := applied
	if m.usesOS && m.needsOS {
		out = mw1AddImport(out, "os")
	}
	if m.usesIOFS && m.needsIOFS {
		out = mw1AddImport(out, "io/fs")
	}
	m.rep.flush(&m.edits, applied, out)
	return out
}

const (
	faviconFileSystem = "FileSystem"
	faviconFile       = "File"
)

// config rewrites a favicon.Config literal.
func (m *faviconMigration) config(lit *ast.CompositeLit, stack []ast.Node) {
	fs := mw1KeyValue(lit, faviconFileSystem)
	if fs == nil || !m.fileSystem(fs.Value, stack) {
		return
	}

	// the file is opened through the file system, whose names are relative
	file := mw1KeyValue(lit, faviconFile)
	if file == nil {
		return
	}
	name, isLiteral := mw1StringLiteral(file.Value)
	if !isLiteral {
		m.rep.notef(file, "favicon.Config.File must be a relative path without a leading / or ./ now that FileSystem is an fs.FS: "+
			"v3 panics at start-up otherwise, check %s", m.g.text(file.Value))
		return
	}
	if cleaned := strings.TrimPrefix(path.Clean("/"+name), "/"); name != "" && cleaned != "" && cleaned != name {
		m.edits.replaceNode(m.g, file.Value, strconv.Quote(cleaned))
	}
}

// fileSystem rewrites the value of FileSystem. It reports whether the value is
// an fs.FS afterwards.
func (m *faviconMigration) fileSystem(e ast.Expr, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return m.fileSystem(x.X, stack)
	case *ast.CallExpr:
		pkg, fn, ok := mw1Selector(x.Fun)
		if !ok || len(x.Args) != 1 {
			break
		}
		switch {
		case slices.Contains(m.httpPkg, pkg) && fn == "FS":
			m.edits.replaceNode(m.g, x, m.g.text(x.Args[0]))
			return true
		case slices.Contains(m.httpPkg, pkg) && fn == "Dir":
			m.edits.replaceNode(m.g, x, m.osName+".DirFS("+m.g.text(x.Args[0])+")")
			m.usesOS = true
			return true
		case pkg == m.osName && fn == "DirFS":
			return true // v3 already
		}
	case *ast.Ident:
		if x.Name == mw1NilIdent || m.declaredAsFS(x, stack) || m.convertVariable(x, stack) {
			return true
		}
	}

	m.rep.notef(e, "favicon.Config.FileSystem is an fs.FS in v3, not an http.FileSystem, and %s cannot be converted (left as it is): "+
		"os.DirFS(dir) replaces http.Dir(dir) and an embed.FS is one itself", m.g.text(e))
	return false
}

// convertVariable turns the declaration of a variable that only the favicon
// middleware uses, an http.Dir or http.FS held as an http.FileSystem, into the
// file system of v3. It reports whether it did.
func (m *faviconMigration) convertVariable(id *ast.Ident, stack []ast.Node) bool {
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	if !ok || countIdent(m.g.file, id.Name) != 2 {
		return false
	}
	call, isCall := d.init.(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return false
	}
	pkg, fn, isSel := mw1Selector(call.Fun)
	if !isSel || !slices.Contains(m.httpPkg, pkg) || (fn != "FS" && fn != "Dir") {
		return false
	}
	if d.typ != nil {
		typePkg, name, isType := mw1Selector(d.typ)
		if !isType || !slices.Contains(m.httpPkg, typePkg) || name != "FileSystem" {
			return false
		}
		fsName, needsFS := mw1ImportName(m.g.src, "io/fs", "fs")
		m.edits.replaceNode(m.g, d.typ, fsName+".FS")
		m.usesIOFS, m.needsIOFS = true, needsFS
	}
	m.fileSystem(call, stack)
	return true
}

// declaredAsFS reports whether the variable is declared as an fs.FS: with the
// type, or from os.DirFS or an embedded file system.
func (m *faviconMigration) declaredAsFS(id *ast.Ident, stack []ast.Node) bool {
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	if !ok {
		return false
	}
	if d.typ != nil {
		// fs.FS and embed.FS
		_, name, isSel := mw1Selector(d.typ)
		return isSel && name == "FS"
	}
	call, isCall := d.init.(*ast.CallExpr)
	if !isCall {
		return false
	}
	pkg, fn, isSel := mw1Selector(call.Fun)
	return isSel && pkg == m.osName && fn == "DirFS"
}
