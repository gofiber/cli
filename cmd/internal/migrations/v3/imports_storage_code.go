package v3

import (
	"go/ast"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	importsRePostgres = regexp.MustCompile(`^github\.com/gofiber/storage/postgres(/v\d+)?$`)
	importsReRedis    = regexp.MustCompile(`^github\.com/gofiber/storage/redis(/v\d+)?$`)
)

// importsStoragePkg is a storage package as one file uses it.
type importsStoragePkg struct {
	// names are the identifiers the file binds to the package.
	names []string
	// fields are the struct fields declared in the file with the Storage type of
	// the package.
	fields []string
	// moved says that the import left the module without a major version, whose
	// Conn() returned another type.
	moved bool
}

// importsStorageFile rewrites the Go code of one file that imports storage modules.
type importsStorageFile struct {
	g   *goSource
	fi  internal.FileInfo
	run *importsStorageScan
	// pg answers what the postgres storages of the file are, nil when the project has
	// none that moves to the v3 line.
	pg *importsPostgresView
	// needShim is the import path of the module package to import, when the file
	// does not import it yet.
	needShim string
	edits    *editSet
	postgres importsStoragePkg
	redis    importsStoragePkg
	usesShim bool
}

func (r *importsStorageScan) migrate(content string, fi internal.FileInfo) string {
	r.refresh()

	hasStorage := strings.Contains(content, importsStorageFamily.prefix)
	callsConn := len(r.moving) > 0 && strings.Contains(content, ".Conn()")
	if !hasStorage && !callsConn {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	moves := importsStorageFamily.moveImports(g, &edits)

	s := &importsStorageFile{g: g, fi: fi, run: r, edits: &edits}
	s.postgres.names = importsStorageNames(g, importsRePostgres, "postgres")
	s.redis.names = importsStorageNames(g, importsReRedis, "redis")
	for _, mv := range moves {
		s.postgres.moved = s.postgres.moved || (mv.name == "postgres" && mv.from == "")
		s.redis.moved = s.redis.moved || (mv.name == "redis" && mv.from == "")
	}
	if len(r.moving) > 0 {
		ix := r.indexes.of(fi)
		ix.packageIn(filepath.Dir(fi.Path)) // the file knows its package once the directory is read
		mf := ix.fileAt(fi.Path)
		s.pg = &importsPostgresView{run: r, g: g, mf: mf, names: s.postgres.names, moved: r.moving[fi.Path]}
	}

	if len(s.postgres.names) > 0 || len(s.redis.names) > 0 || callsConn {
		g.walk(s.collectFields)
		g.walk(s.visit)
	}

	out := edits.apply(content)
	if s.usesShim && s.needShim != "" {
		out = mw1AddImport(out, s.needShim)
	}
	if out != content {
		r.changed = append(r.changed, fi.Path)
	}
	return out
}

// importsStorageNames returns the identifiers the file binds to the imports that match
// re. pkg is the package name of the module.
func importsStorageNames(g *goSource, re *regexp.Regexp, pkg string) []string {
	var names []string
	for _, spec := range importsOf(g, re.MatchString) {
		if !spec.blank() && !spec.dot() {
			names = append(names, spec.local(pkg))
		}
	}
	return names
}

// collectFields finds the struct fields of the file that hold a postgres or
// redis storage, to recognize store.Conn() behind a field.
func (s *importsStorageFile) collectFields(n ast.Node, stack []ast.Node) bool {
	field, ok := n.(*ast.Field)
	if !ok {
		return true
	}
	for _, id := range field.Names {
		if s.typeIs(field.Type, &s.postgres, "Storage", stack) {
			s.postgres.fields = append(s.postgres.fields, id.Name)
		}
		if s.typeIs(field.Type, &s.redis, "Storage", stack) {
			s.redis.fields = append(s.redis.fields, id.Name)
		}
	}
	return true
}

func (s *importsStorageFile) visit(n ast.Node, stack []ast.Node) bool {
	switch x := n.(type) {
	case *ast.CompositeLit:
		// A literal of the postgres Config with the old field name.
		if x.Type != nil && s.typeIs(x.Type, &s.postgres, "Config", stack) {
			for _, elt := range x.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok && isIdentNamed(kv.Key, "SslMode") {
					s.edits.replaceNode(s.g, kv.Key, "SSLMode")
				}
			}
		}
	case *ast.SelectorExpr:
		// The field of a Config value that was assigned to a variable.
		if x.Sel.Name == "SslMode" && s.isConfig(x.X, stack, 0) {
			s.edits.replaceNode(s.g, x.Sel, "SSLMode")
		}
	case *ast.CallExpr:
		s.conn(x, stack)
	}
	return true
}

// pkgRef reports whether e is an identifier that refers to the package, not to
// a variable of the same name.
func (*importsStorageFile) pkgRef(e ast.Expr, p *importsStoragePkg, stack []ast.Node) bool {
	id, ok := e.(*ast.Ident)
	return ok && slices.Contains(p.names, id.Name) && !importsShadowed(id.Name, id.Pos(), stack)
}

// typeIs reports whether e names the type typeName of the package, possibly
// behind pointers.
func (s *importsStorageFile) typeIs(e ast.Expr, p *importsStoragePkg, typeName string, stack []ast.Node) bool {
	for {
		star, ok := e.(*ast.StarExpr)
		if !ok {
			break
		}
		e = star.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == typeName && s.pkgRef(sel.X, p, stack)
}

// importsMaxDepth bounds how far isConfig and isStorage follow variables.
const importsMaxDepth = 3

// isConfig reports whether e is a postgres.Config value: a variable declared as
// one or initialized with a literal of the type.
func (s *importsStorageFile) isConfig(e ast.Expr, stack []ast.Node, depth int) bool {
	if depth > importsMaxDepth {
		return false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return s.isConfig(x.X, stack, depth+1)
	case *ast.UnaryExpr:
		return s.isConfig(x.X, stack, depth+1)
	case *ast.CompositeLit:
		return x.Type != nil && s.typeIs(x.Type, &s.postgres, "Config", stack)
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return s.typeIs(d.typ, &s.postgres, "Config", stack)
		}
		return d.init != nil && s.isConfig(d.init, stack, depth+1)
	}
	return false
}

// isStorage reports whether e is a storage of the package: the result of its
// New, a variable declared as or initialized with one, or a struct field of the
// file that holds one.
func (s *importsStorageFile) isStorage(e ast.Expr, p *importsStoragePkg, stack []ast.Node, depth int) bool {
	if depth > importsMaxDepth {
		return false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return s.isStorage(x.X, p, stack, depth+1)
	case *ast.StarExpr:
		return s.isStorage(x.X, p, stack, depth+1)
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == mw2New && s.pkgRef(sel.X, p, stack)
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return s.typeIs(d.typ, p, "Storage", stack)
		}
		return d.init != nil && s.isStorage(d.init, p, stack, depth+1)
	case *ast.SelectorExpr:
		return slices.Contains(p.fields, x.Sel.Name)
	}
	return false
}

// noteConn reports a Conn() call on a postgres or redis storage whose import
// came from the module without a major version: the connection it returns is
// another type now, which cannot be converted by the CLI.
func (s *importsStorageFile) noteConn(call *ast.CallExpr, stack []ast.Node) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Conn" || len(call.Args) != 0 {
		return
	}

	switch {
	case s.postgres.moved && s.isStorage(sel.X, &s.postgres, stack, 0):
		internal.Notef(s.fi, s.g.line(call.Pos()),
			"`Conn()` returns a *pgxpool.Pool in github.com/gofiber/storage/postgres/v3, it returned a *sql.DB in the module without a major version: adapt the code that uses the connection.")
	case s.redis.moved && s.isStorage(sel.X, &s.redis, stack, 0):
		internal.Notef(s.fi, s.g.line(call.Pos()),
			"`Conn()` returns a redis.UniversalClient in github.com/gofiber/storage/redis/v3, it returned a *redis.Client in the module without a major version: adapt the code that holds it.")
	}
}
