package v3

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// appStaticImport is the static middleware, which replaced app.Static in v3.
const appStaticImport = "github.com/gofiber/fiber/v3/middleware/static"

// MigrateStaticRoutes turns app.Static, which v3 removed, into the static
// middleware:
//
//	app.Static("/prefix", "./public", fiber.Static{Index: "a.html"})
//	-> app.Get("/prefix*", static.New("./public", static.Config{IndexNames: []string{"a.html"}}))
//
// The prefix and the root may be any expression, the config any expression of
// the type fiber.Static, which becomes static.Config wherever it is written.
// Calls on groups and routers are migrated too; calls on other types are not
// touched.
func MigrateStaticRoutes(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return appMigrateStaticContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate static usages: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating app.Static usage")
	return nil
}

// appStaticRewrite carries what the rewrite of one file needs.
type appStaticRewrite struct {
	f     *appFile
	edits editSet
	imps  appImports
}

func appMigrateStaticContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "Static") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	r := &appStaticRewrite{f: f}
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, isSel := x.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == appNameStatic && f.receiverKind(x, sel).isRouter() {
				r.call(x, sel)
			}
		case *ast.SelectorExpr:
			r.selector(x)
		case *ast.KeyValueExpr:
			r.indexKey(x)
		}
		return true
	})
	return r.imps.apply(r.edits.apply(content))
}

// call rewrites app.Static(prefix, root, config).
func (r *appStaticRewrite) call(call *ast.CallExpr, sel *ast.SelectorExpr) {
	f, g := r.f, r.f.g
	args := call.Args
	if call.Ellipsis.IsValid() || len(args) < 2 || len(args) > 3 {
		internal.Notef(f.fi, g.line(call.Pos()),
			"Static was removed in v3 and this call could not be migrated: use app.Get(prefix+\"*\", static.New(root, static.Config{...}))")
		return
	}

	path, root := args[0], args[1]
	pkg := r.imps.qualifier(f, appStaticImport, "static")

	r.edits.replaceNode(g, sel.Sel, appMethodGet)
	r.edits.replaceNode(g, path, r.pathText(path))

	// the root is wrapped in static.New( ... ), possibly with a default for an
	// empty one: v2 served the working directory then, whereas static.New("")
	// serves the root of the file system
	open, closing := pkg+"New(", ""
	switch {
	case appIsEmptyString(root):
		r.edits.replaceNode(g, root, open+`"."`)
		open = ""
	case !f.provablyNotEmpty(root, 0):
		open += r.imps.qualifier(f, "cmp", "cmp") + "Or("
		closing = `, ".")`
	}
	if open != "" {
		r.edits.replace(g.offset(root.Pos()), g.offset(root.Pos()), open)
	}
	if closing != "" {
		r.edits.replace(g.offset(root.End()), g.offset(root.End()), closing)
	}
	last := args[len(args)-1]
	r.edits.replace(g.offset(last.End()), g.offset(last.End()), ")")
}

// pathText returns the route of the static middleware for the prefix of
// app.Static: the prefix and a wildcard, which / and * already are.
func (r *appStaticRewrite) pathText(path ast.Expr) string {
	value, constant := r.f.constString(path, 0)
	switch {
	case constant && appIsLiteralConcat(path):
		return strconv.Quote(appStaticRoute(value))
	case constant && strings.HasSuffix(value, "*"):
		return r.f.g.text(path)
	default:
		return r.f.g.text(path) + ` + "*"`
	}
}

// appStaticRoute returns the route that serves the files below prefix.
func appStaticRoute(prefix string) string {
	switch {
	case prefix == "":
		return "/*"
	case strings.HasSuffix(prefix, "*"):
		return prefix
	default:
		return prefix + "*"
	}
}

// appIsLiteralConcat reports whether e is made of string literals and +.
func appIsLiteralConcat(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return appIsLiteralConcat(x.X)
	case *ast.BinaryExpr:
		return appIsLiteralConcat(x.X) && appIsLiteralConcat(x.Y)
	}
	return false
}

// selector rewrites the uses of the type fiber.Static, which is static.Config
// now, and of its Index field.
func (r *appStaticRewrite) selector(sel *ast.SelectorExpr) {
	f := r.f
	if id, ok := sel.X.(*ast.Ident); ok && f.isFiberQualifier(id.Name) && sel.Sel.Name == appNameStatic {
		r.edits.replaceNode(f.g, sel, r.imps.qualifier(f, appStaticImport, "static")+"Config")
		return
	}

	if sel.Sel.Name != "Index" {
		return
	}
	t, tf := f.valueType(sel.X, 0)
	if t == nil || tf.fiberTypeName(t) != appNameStatic {
		return
	}

	if as, ok := f.parent[sel].(*ast.AssignStmt); ok && len(as.Lhs) == 1 && as.Lhs[0] == sel && len(as.Rhs) == 1 {
		r.edits.replaceNode(f.g, sel.Sel, "IndexNames")
		r.wrapIndex(as.Rhs[0])
		return
	}
	internal.Notef(f.fi, f.g.line(sel.Pos()), "static.Config has IndexNames, a list, instead of Index")
}

// indexKey rewrites Index: "x" in a fiber.Static literal to IndexNames.
func (r *appStaticRewrite) indexKey(kv *ast.KeyValueExpr) {
	f := r.f
	if !isIdentNamed(kv.Key, "Index") {
		return
	}
	lit, ok := f.parent[kv].(*ast.CompositeLit)
	if !ok || lit.Type == nil || f.fiberTypeName(lit.Type) != appNameStatic {
		return
	}
	r.edits.replaceNode(f.g, kv.Key, "IndexNames")
	r.wrapIndex(kv.Value)
}

// wrapIndex turns the single index file into a list.
func (r *appStaticRewrite) wrapIndex(value ast.Expr) {
	g := r.f.g
	r.edits.replace(g.offset(value.Pos()), g.offset(value.Pos()), "[]string{")
	r.edits.replace(g.offset(value.End()), g.offset(value.End()), "}")
}
