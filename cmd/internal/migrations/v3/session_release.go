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

const releaseComment = "// Important: Manual cleanup required"

// reSessionV3Path matches the import path of the v3 session middleware: the
// release only exists there, and this migration runs after MigrateGoPkgs.
var reSessionV3Path = regexp.MustCompile(`^github\.com/gofiber/fiber/v3/middleware/session$`)

// MigrateSessionRelease adds defer sess.Release() after store.Get() calls
// when using the Store Pattern (legacy pattern).
// This is required in v3 for manual session lifecycle management: v2 released
// a session when it was saved, v3 leaves that to the caller.
//
// Only the following Store methods return *Session from the pool and require Release():
//   - store.Get(c fiber.Ctx) (*Session, error)
//   - store.GetByID(ctx context.Context, id string) (*Session, error)
//
// Middleware handlers do NOT require Release() as the middleware manages the lifecycle.
//
// The store is found through the file's own import of the session package, and
// may be a variable of the function, a package level variable (of any file of
// the package), a parameter, or a field of a struct the package declares.
//
// A session that leaves the function it was gotten in (it is returned, stored,
// sent, or handed to a goroutine) is not released there: whoever uses it next
// would find it reset. Such a Get is reported instead.
func MigrateSessionRelease(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs := newMw3Pkgs()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return mw3SessionReleaseContent(content, fi, pkgs)
	})
	if err != nil {
		return fmt.Errorf("failed to add session Release() calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Adding defer sess.Release() for Store Pattern usage")
	return nil
}

func mw3SessionReleaseContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	// Skip v2 imports - this migration only works with v3.
	if strings.Contains(src, "middleware/session") && len(importNames(src, reSessionV3Path, "session")) == 0 {
		return src
	}
	f, ok := mw3OpenStoreUser(fi, src, pkgs, func(src string) bool {
		return strings.Contains(src, ".Get(") || strings.Contains(src, ".GetByID(")
	})
	if !ok {
		return src
	}

	var edits editSet
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			mw3ReleaseGet(f, &edits, as, stack)
		}
		return true
	})
	return edits.apply(src)
}

// mw3ReleaseGet schedules the defer after a "sess, err := store.Get(c)".
func mw3ReleaseGet(f *mw3SessionFile, edits *editSet, as *ast.AssignStmt, stack []ast.Node) {
	if as.Tok != token.DEFINE || len(as.Lhs) != 2 || len(as.Rhs) != 1 {
		return
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Get" && sel.Sel.Name != "GetByID") || !f.isStore(sel.X, stack) {
		return
	}
	sess, ok := as.Lhs[0].(*ast.Ident)
	if !ok || sess.Name == "_" {
		return
	}

	// The statements the Get sits among; a Get in the init of an if or a for
	// has no place to put the defer.
	if len(stack) == 0 {
		return
	}
	var list []ast.Stmt
	switch parent := stack[len(stack)-1].(type) {
	case *ast.BlockStmt:
		list = parent.List
	case *ast.CaseClause:
		list = parent.Body
	case *ast.CommClause:
		list = parent.Body
	default:
		return
	}

	body, inLoop := mw3EnclosingBody(stack)
	if body == nil || inLoop {
		return
	}
	if mw3CallsMethod(body, sess.Name, "Release") {
		return
	}
	if mw3SessionEscapes(body, as, sess.Name) {
		internal.Notef(f.fi, f.g.line(as.Pos()),
			"the session %s leaves this function (it is returned, stored, sent or handed to a goroutine), so no defer %s.Release() was added. "+
				"v3 does not release a session when it is saved: call Release() where its last user is done with it.", sess.Name, sess.Name)
		return
	}

	target := ast.Stmt(as)
	for i, stmt := range list {
		if stmt == ast.Stmt(as) && i+1 < len(list) && mw3IsErrCheck(list[i+1], as.Lhs[1]) {
			target = list[i+1]
		}
	}

	g := f.g
	at := g.offset(target.End())
	if rest := strings.TrimSpace(g.src[at:mw3LineEnd(g.src, at)]); rest == "" || strings.HasPrefix(rest, "//") {
		at = mw3LineEnd(g.src, at)
	}
	indent := mw3Indent(g.src, g.offset(as.Pos()))
	edits.replace(at, at, "\n"+indent+"defer "+sess.Name+".Release() "+releaseComment)
}

// mw3EnclosingBody returns the body of the innermost function the node whose
// ancestors are stack sits in, and whether a loop sits between them.
func mw3EnclosingBody(stack []ast.Node) (body *ast.BlockStmt, inLoop bool) {
	for _, node := range slices.Backward(stack) {
		switch n := node.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			inLoop = true
		case *ast.FuncDecl:
			return n.Body, inLoop
		case *ast.FuncLit:
			return n.Body, inLoop
		}
	}
	return nil, inLoop
}

// mw3IsErrCheck reports whether stmt is "if err != nil { ... }" for the error
// variable of a Get.
func mw3IsErrCheck(stmt ast.Stmt, errVar ast.Expr) bool {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok || ifStmt.Init != nil {
		return false
	}
	errID, ok := errVar.(*ast.Ident)
	if !ok || errID.Name == "_" {
		return false
	}
	cond, ok := ifStmt.Cond.(*ast.BinaryExpr)
	return ok && cond.Op == token.NEQ && isIdentNamed(cond.X, errID.Name) && isIdentNamed(cond.Y, "nil")
}

// mw3CallsMethod reports whether the node calls name.method() anywhere,
// including in a defer.
func mw3CallsMethod(root ast.Node, name, method string) bool {
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == method && isIdentNamed(sel.X, name) {
			found = true
		}
		return !found
	})
	return found
}

// mw3SessionEscapes reports whether the session variable name, which the
// assignment declares, is used in the body in a way that keeps it alive past
// the end of the function.
func mw3SessionEscapes(body ast.Node, declaration *ast.AssignStmt, name string) bool {
	escapes := false
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if escapes || n == ast.Node(declaration) {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == name && mw3UseEscapes(id, stack) {
			escapes = true
			return false
		}
		stack = append(stack, n)
		return true
	})
	return escapes
}

// mw3UseEscapes reports whether this use of a session variable lets it live on:
// anything but calling a method on it, comparing it, or passing it to a call
// that runs before the function returns.
func mw3UseEscapes(id *ast.Ident, stack []ast.Node) bool {
	// Code that runs later: a goroutine, or a function value that is not
	// called on the spot.
	for i, node := range slices.Backward(stack) {
		switch n := node.(type) {
		case *ast.GoStmt:
			return true
		case *ast.FuncLit:
			if !mw3RunsInPlace(n, stack[:i]) {
				return true
			}
		}
	}

	if len(stack) == 0 {
		return false
	}
	switch parent := stack[len(stack)-1].(type) {
	case *ast.SelectorExpr:
		return false // sess.Save(), or a field named like the variable
	case *ast.BinaryExpr:
		return parent.Op != token.EQL && parent.Op != token.NEQ
	case *ast.KeyValueExpr:
		return parent.Key != ast.Expr(id)
	case *ast.CallExpr:
		if fun, ok := parent.Fun.(*ast.Ident); ok && fun.Name == "append" {
			return true
		}
		return false
	case *ast.AssignStmt:
		if slices.Contains(parent.Lhs, ast.Expr(id)) {
			return false // reassigned or redeclared, not copied
		}
		return !mw3AllBlank(parent.Lhs)
	}
	return true
}

// mw3RunsInPlace reports whether the function literal runs where it is
// written: called at once, or deferred.
func mw3RunsInPlace(lit *ast.FuncLit, ancestors []ast.Node) bool {
	if len(ancestors) == 0 {
		return false
	}
	call, ok := ancestors[len(ancestors)-1].(*ast.CallExpr)
	return ok && call.Fun == ast.Expr(lit)
}

func mw3AllBlank(exprs []ast.Expr) bool {
	for _, e := range exprs {
		if !isIdentNamed(e, "_") {
			return false
		}
	}
	return true
}
