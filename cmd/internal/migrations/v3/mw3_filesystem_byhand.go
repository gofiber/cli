package v3

import (
	"go/ast"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3FSStored is the handler of a filesystem.New call that a variable holds.
type mw3FSStored struct {
	call *ast.CallExpr
	name string
}

// remember notes that the filesystem.New call is the value of a variable: stack
// holds the ancestors of the filesystem.New selector, whose parent is the call.
func (p *mw3FSPass) remember(call *ast.CallExpr, stack []ast.Node) {
	if len(stack) < 2 {
		return
	}

	switch parent := stack[len(stack)-2].(type) {
	case *ast.ValueSpec:
		if len(parent.Names) != len(parent.Values) {
			return
		}
		for i, value := range parent.Values {
			if value == ast.Expr(call) {
				p.stored = append(p.stored, mw3FSStored{call: call, name: parent.Names[i].Name})
			}
		}
	case *ast.AssignStmt:
		if len(parent.Lhs) != len(parent.Rhs) {
			return
		}
		for i, value := range parent.Rhs {
			if id, ok := parent.Lhs[i].(*ast.Ident); ok && value == ast.Expr(call) {
				p.stored = append(p.stored, mw3FSStored{call: call, name: id.Name})
			}
		}
	}
}

// calledByHand reports the handlers that a variable holds and that the code calls
// itself, after it set the path of the request: the handler of v2 served the path of
// the request, and the static handler of v3 serves what follows the prefix of the
// route that matched, so a path set by c.Path() is cut by a prefix that is not its
// own and the file is not found.
func (p *mw3FSPass) calledByHand() {
	if len(p.stored) == 0 {
		return
	}

	g := p.g
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		handler, ok := call.Fun.(*ast.Ident)
		if !ok || !p.holdsHandler(handler, stack) {
			return true
		}
		ctx, ok := call.Args[0].(*ast.Ident)
		if !ok {
			return true
		}

		if setter := mw3PathSetter(ctx.Name, call, stack); setter != nil {
			internal.Notef(p.fi, g.line(call.Pos()),
				"%s(%s) runs the static handler by hand after %s set another path: the static middleware of v3 looks the file up below the prefix "+
					"of the route that matched (the first argument of static.New, or the route itself when it is empty) and cuts that many bytes from the path, "+
					"so the path set by hand is cut wrongly and the file is not found. Serve the file with %s.SendFile(path, fiber.SendFile{FS: fsys}), "+
					"or register the handler on a route that matches the path.",
				handler.Name, ctx.Name, g.text(setter), ctx.Name)
		}
		return true
	})
}

// holdsHandler reports whether the identifier is a variable that a filesystem.New
// call initialized.
func (p *mw3FSPass) holdsHandler(id *ast.Ident, stack []ast.Node) bool {
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	if !ok || d.init == nil {
		return false
	}
	for _, stored := range p.stored {
		if stored.name == id.Name && d.init == ast.Expr(stored.call) {
			return true
		}
	}
	return false
}

// mw3PathSetter returns the call that sets the path of the context called ctx
// (ctx.Path(p)) before the call in the function that holds it.
func mw3PathSetter(ctx string, call *ast.CallExpr, stack []ast.Node) *ast.CallExpr {
	var body ast.Node
	for i := len(stack) - 1; i >= 0 && body == nil; i-- {
		switch fn := stack[i].(type) {
		case *ast.FuncDecl:
			if fn.Body != nil {
				body = fn.Body
			}
		case *ast.FuncLit:
			body = fn.Body
		}
	}
	if body == nil {
		return nil
	}

	var setter *ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || setter != nil || c.Pos() >= call.Pos() || len(c.Args) != 1 {
			return setter == nil
		}
		if sel, isSel := c.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "Path" && isIdentNamed(sel.X, ctx) {
			setter = c
		}
		return setter == nil
	})
	return setter
}
