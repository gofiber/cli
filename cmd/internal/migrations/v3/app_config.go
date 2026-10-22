package v3

import (
	"fmt"
	"go/ast"
	"maps"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// appETagImport is the etag middleware, which replaced Config.ETag in v3.
const appETagImport = "github.com/gofiber/fiber/v3/middleware/etag"

// MigrateAppConfig migrates the fiber.Config fields that v3 removed or changed
// and that have an equivalent:
//
//   - ETag: true becomes app.Use(etag.New()) on the app the config creates. v2
//     generated strong ETags (its docs say weak, its code passes weak=false),
//     which is what the middleware does by default.
//   - CompressedFileSuffix: ".gz" becomes CompressedFileSuffixes:
//     map[string]string{"gzip": ".gz"}, the suffix v2 used for gzip files.
//   - fiber.DefaultCompressedFileSuffix is its value, ".fiber.gz".
//
// A config whose app cannot be found keeps the ETag field, so that the compiler
// points at it, and a note says what to do.
func MigrateAppConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return appMigrateAppConfigContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate app config fields: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating app config fields")
	return nil
}

// appConfigRewrite carries what the rewrite of one file needs.
type appConfigRewrite struct {
	f     *appFile
	edits editSet
	imps  appImports
}

func appMigrateAppConfigContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "ETag") && !strings.Contains(content, "CompressedFileSuffix") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	r := &appConfigRewrite{f: f}
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if x.Type != nil && f.fiberTypeName(x.Type) == appNameConfig {
				r.literal(x)
			}
		case *ast.SelectorExpr:
			r.selector(x)
		}
		return true
	})
	return r.imps.apply(r.edits.apply(content))
}

// literal migrates the removed fields of a fiber.Config literal.
func (r *appConfigRewrite) literal(lit *ast.CompositeLit) {
	g := r.f.g
	repl := map[int]string{}
	var drop []int
	var after editSet
	imports := slices.Clone(r.imps.reqs)

	if i, kv := appKeyedElement(lit, "CompressedFileSuffix"); kv != nil {
		repl[i] = `CompressedFileSuffixes: map[string]string{"gzip": ` + g.text(kv.Value) + "}"
	}
	if i, kv := appKeyedElement(lit, "ETag"); kv != nil {
		if why := r.etag(lit, kv, &after); why != "" {
			r.imps.reqs = slices.Clone(imports)
			internal.Notef(r.f.fi, g.line(kv.Pos()),
				"fiber.Config has no ETag field in v3: register app.Use(etag.New()) (%s) on the app this config creates (%s)", appETagImport, why)
		} else {
			drop = append(drop, i)
		}
	}

	var scratch editSet
	if !appRemoveElements(g, &scratch, lit, drop, repl) {
		r.imps.reqs = imports
		internal.Notef(r.f.fi, g.line(lit.Pos()),
			"fiber.Config is laid out in a way that cannot be edited safely: replace ETag and CompressedFileSuffix by hand")
		return
	}
	r.edits.edits = append(r.edits.edits, scratch.edits...)
	r.edits.edits = append(r.edits.edits, after.edits...)
}

// etag plans the middleware that stands for ETag: the value of kv for every
// app the literal creates. It returns why that cannot be done.
func (r *appConfigRewrite) etag(lit *ast.CompositeLit, kv *ast.KeyValueExpr, edits *editSet) string {
	g := r.f.g
	if isIdentNamed(kv.Value, appLitFalse) {
		return "" // nothing was generated
	}

	creations := r.creationsOf(lit)
	if len(creations) == 0 {
		return "this fiber.Config is not handed straight to fiber.New"
	}

	etagQ := r.imps.qualifier(r.f, appETagImport, "etag")
	use := func(recv string) string {
		call := recv + ".Use(" + etagQ + "New())"
		if isIdentNamed(kv.Value, appLitTrue) {
			return call
		}
		return "if " + g.text(kv.Value) + " {\n" + call + "\n}"
	}

	var scratch editSet
	for _, c := range creations {
		if c.file != r.f {
			return "the app is created in another file"
		}
		if why := r.useAfter(c.call, use, &scratch); why != "" {
			return why
		}
	}
	edits.edits = append(edits.edits, scratch.edits...)
	return ""
}

// appLitTrue is the predeclared identifier true.
const appLitTrue = "true"

// creationsOf finds the fiber.New calls that take the literal: it is their
// argument, or the variable that holds it is.
func (r *appConfigRewrite) creationsOf(lit *ast.CompositeLit) []appNew {
	src := r.f.sourceOfLiteral(lit)
	if src.node == nil {
		return src.creations
	}
	uses := src.variableUses()
	if uses.escapes || len(uses.returns) > 0 {
		return nil // the config is also used to create other apps
	}
	return uses.creations
}

// useAfter schedules the statement that registers the middleware on the app the
// fiber.New call creates, right after the statement of the call. use builds
// that statement from the expression naming the app.
func (r *appConfigRewrite) useAfter(call *ast.CallExpr, use func(recv string) string, edits *editSet) string {
	f, g := r.f, r.f.g

	switch p := f.parent[call].(type) {
	case *ast.AssignStmt:
		if len(p.Lhs) != 1 || len(p.Rhs) != 1 || !appIsPureReference(p.Lhs[0]) || !f.inStatementList(p) {
			return "the app is created in a statement that cannot be followed by another"
		}
		at := g.offset(p.End())
		edits.replace(at, at, "\n"+use(g.text(p.Lhs[0])))
	case *ast.ValueSpec:
		decl, ok := f.parent[p].(*ast.GenDecl)
		if !ok || len(p.Names) != 1 || len(p.Values) != 1 {
			return "the app is created in a statement that cannot be followed by another"
		}
		stmt, isStmt := f.parent[decl].(*ast.DeclStmt)
		if !isStmt || !f.inStatementList(stmt) {
			return "the app is created at the package level"
		}
		at := g.offset(stmt.End())
		edits.replace(at, at, "\n"+use(p.Names[0].Name))
	case *ast.ReturnStmt:
		return r.useBeforeReturn(p, call, use, edits)
	default:
		return "the app is created inside a larger expression"
	}
	return ""
}

// inStatementList reports whether stmt sits in a list of statements, where
// another statement can follow it.
func (f *appFile) inStatementList(stmt ast.Stmt) bool {
	switch f.parent[stmt].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		return true
	}
	return false
}

// useBeforeReturn splits `return fiber.New(cfg)` into a variable, the
// middleware and `return app`.
func (r *appConfigRewrite) useBeforeReturn(ret *ast.ReturnStmt, call *ast.CallExpr, use func(recv string) string, edits *editSet) string {
	f, g := r.f, r.f.g
	if len(ret.Results) == 0 || ret.Results[0] != call || !f.inStatementList(ret) {
		return "the app is created in a return statement that cannot be split"
	}

	name := f.freshName(ret, "app", nil)
	edits.replace(g.offset(ret.Pos()), g.offset(call.Pos()), name+" := ")
	at := g.offset(call.End())
	edits.replace(at, at, "\n"+use(name)+"\nreturn "+name)
	return ""
}

// freshName returns a name that nothing in the function around n uses, starting
// from preferred.
func (f *appFile) freshName(n ast.Node, preferred string, taken map[string]bool) string {
	used := map[string]bool{}
	maps.Copy(used, taken)
	if decl := f.enclosingDecl(n); decl != nil {
		ast.Inspect(decl, func(c ast.Node) bool {
			if id, ok := c.(*ast.Ident); ok {
				used[id.Name] = true
			}
			return true
		})
	}

	name := preferred
	for i := 2; used[name] || f.packageLevelNameTaken(name); i++ {
		name = fmt.Sprintf("%s%d", preferred, i)
	}
	return name
}

// selector migrates fiber.DefaultCompressedFileSuffix and the assignment of
// Config.CompressedFileSuffix, and points at the other uses of what was removed.
func (r *appConfigRewrite) selector(sel *ast.SelectorExpr) {
	f, g := r.f, r.f.g
	switch sel.Sel.Name {
	case "DefaultCompressedFileSuffix":
		if id, ok := sel.X.(*ast.Ident); ok && f.isFiberQualifier(id.Name) {
			r.edits.replaceNode(g, sel, `".fiber.gz"`)
		}
	case "CompressedFileSuffix":
		if f.kind(sel.X) != appKindConfig {
			return
		}
		if as, ok := f.parent[sel].(*ast.AssignStmt); ok && len(as.Lhs) == 1 && as.Lhs[0] == sel && len(as.Rhs) == 1 {
			r.edits.replaceNode(g, sel.Sel, "CompressedFileSuffixes")
			r.edits.replaceNode(g, as.Rhs[0], `map[string]string{"gzip": `+g.text(as.Rhs[0])+"}")
			return
		}
		internal.Notef(f.fi, g.line(sel.Pos()), "fiber.Config has CompressedFileSuffixes, a map from encoding to suffix, instead of CompressedFileSuffix")
	case "ETag":
		if f.kind(sel.X) == appKindConfig {
			internal.Notef(f.fi, g.line(sel.Pos()),
				"fiber.Config has no ETag field in v3: register app.Use(etag.New()) (%s) instead", appETagImport)
		}
	}
}
