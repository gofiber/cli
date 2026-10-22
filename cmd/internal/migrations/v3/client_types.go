package v3

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
)

// clientFasthttpAliases are the members of the v2 fiber package that are plain
// aliases of fasthttp: the types of the request, of the response and of the
// arguments, and their pools.
var clientFasthttpAliases = []string{
	nameArgs, clientMemberAcquireArgs, nameReleaseArgs, "Request", nameResponse,
	nameAcquireResponse, nameReleaseResponse, nameRetryIfFunc,
}

// renameAliases points the aliases of fasthttp at the package they come from.
func (f *clientFile) renameAliases() {
	f.g.walk(func(n ast.Node, _ []ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !f.isFiberName(sel.X) || !slices.Contains(clientFasthttpAliases, sel.Sel.Name) {
			return true
		}
		pkg := f.use(clientFasthttp, "fasthttp")
		f.edits.replace(f.edits.begin(), f.g.offset(sel.X.Pos()), f.g.offset(sel.X.End()), pkg)
		f.handled[sel] = true
		return true
	})
}

// clientIsInit reports whether e creates a fiber.Client: AcquireClient(),
// new(fiber.Client) or a literal of the type.
func clientIsInit(e ast.Expr, fiberNames []string) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return clientIsInit(x.X, fiberNames)
	case *ast.UnaryExpr:
		return x.Op == token.AND && clientIsInit(x.X, fiberNames)
	case *ast.CompositeLit:
		return x.Type != nil && isFiberType(x.Type, fiberNames, "Client")
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			id, isIdent := sel.X.(*ast.Ident)
			return isIdent && slices.Contains(fiberNames, id.Name) && sel.Sel.Name == nameAcquireClient
		}
		return isIdentNamed(x.Fun, "new") && len(x.Args) == 1 && isFiberType(x.Args[0], fiberNames, "Client")
	}
	return false
}

// isFiberClientExpr reports whether e is a fiber.Client: a variable or a
// field that was declared or initialized as one.
func (f *clientFile) isFiberClientExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.isFiberClientExpr(x.X)
	case *ast.CallExpr, *ast.CompositeLit, *ast.UnaryExpr:
		return clientIsInit(e, f.fiber)
	case *ast.Ident:
		if d, ok := f.resolve(x); ok {
			return f.isClientDecl(d)
		}
		return f.pkgs.clientVar(f.dir(), f.g.file.Name.Name, x.Name)
	case *ast.SelectorExpr:
		t := f.typeOf(x.X)
		if t == nil {
			return false
		}
		field, ok := f.fieldType(t, x.Sel.Name)
		return ok && isFiberType(field, f.fiber, "Client")
	}
	return false
}

// isClientDecl reports whether a declaration declares a fiber.Client.
func (f *clientFile) isClientDecl(d decl) bool {
	if d.typ != nil {
		return isFiberType(d.typ, f.fiber, "Client")
	}
	return d.init != nil && clientIsInit(d.init, f.fiber)
}

// typeOf returns the type of an expression when it can be told from the
// declarations of the file: a variable, a parameter or a field.
func (f *clientFile) typeOf(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.typeOf(x.X)
	case *ast.Ident:
		d, ok := f.resolve(x)
		if !ok {
			return nil
		}
		if d.typ != nil {
			return d.typ
		}
		if d.init != nil {
			return f.typeOfInit(d.init)
		}
	case *ast.SelectorExpr:
		if t := f.typeOf(x.X); t != nil {
			if ft, ok := f.fieldType(t, x.Sel.Name); ok {
				return ft
			}
		}
	}
	return nil
}

// typeOfInit returns the type of the value an initializer creates.
func (f *clientFile) typeOfInit(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.typeOfInit(x.X)
	case *ast.CompositeLit:
		return x.Type
	case *ast.UnaryExpr:
		if t := f.typeOfInit(x.X); t != nil && x.Op == token.AND {
			return &ast.StarExpr{X: t}
		}
	case *ast.CallExpr:
		if isIdentNamed(x.Fun, "new") && len(x.Args) == 1 {
			return &ast.StarExpr{X: x.Args[0]}
		}
	}
	return nil
}

// fieldType finds the type of the field name of the struct type t, which is
// declared in the file.
func (f *clientFile) fieldType(t ast.Expr, name string) (ast.Expr, bool) {
	for {
		star, ok := t.(*ast.StarExpr)
		if !ok {
			break
		}
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	if !ok {
		return nil, false
	}
	for _, d := range f.g.file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != id.Name {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return nil, false
			}
			for _, field := range st.Fields.List {
				for _, n := range field.Names {
					if n.Name == name {
						return field.Type, true
					}
				}
			}
		}
	}
	return nil, false
}

// clientFieldSetters maps the fields of fiber.Client to the setters of the v3
// client.
var clientFieldSetters = map[string]string{
	clientMemberUserAgent:   "SetUserAgent",
	clientMemberJSONEncoder: "SetJSONMarshal",
	clientMemberJSONDecoder: "SetJSONUnmarshal",
}

// rewriteClients rewrites the creation of fiber.Client values and the type of
// the variables that hold them.
func (f *clientFile) rewriteClients() {
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			f.rewriteClientCall(x)
		case *ast.CompositeLit:
			if x.Type != nil && isFiberType(x.Type, f.fiber, "Client") {
				f.rewriteClientLit(x, stack)
			}
		case *ast.StarExpr:
			if sel, ok := f.fiberSel(x.X); ok && sel.Sel.Name == nameClient {
				f.renameFiberSelector(sel, "client")
			}
		case *ast.FuncType:
			f.renameAgentTypes(x)
		}
		return true
	})
}

// rewriteClientCall rewrites fiber.AcquireClient() and new(fiber.Client).
func (f *clientFile) rewriteClientCall(call *ast.CallExpr) {
	if !clientIsInit(call, f.fiber) {
		return
	}
	pkg := f.use(clientPkgPath, "client")
	f.edits.replace(f.edits.begin(), f.g.offset(call.Pos()), f.g.offset(call.End()), pkg+".New()")
	f.markFiberSelectors(call)
}

// rewriteClientLit rewrites a literal of fiber.Client to a client.New() with
// the setters that stand for its fields.
func (f *clientFile) rewriteClientLit(lit *ast.CompositeLit, stack []ast.Node) {
	var setters []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok {
			return
		}
		switch setter, known := clientFieldSetters[id.Name]; {
		case known:
			setters = append(setters, setter+"("+f.text(kv.Value)+")")
		case id.Name == "NoDefaultUserAgentHeader":
			f.notef(f.line(kv), "NoDefaultUserAgentHeader has no equivalent: the v3 client always sends a User-Agent header (fiber unless SetUserAgent says otherwise)")
		default:
			return
		}
	}

	pkg := f.use(clientPkgPath, "client")
	text := pkg + ".New()"
	if len(setters) > 0 {
		text += "." + strings.Join(setters, ".")
	}
	target := ast.Node(lit)
	if len(stack) > 0 {
		if un, ok := stack[len(stack)-1].(*ast.UnaryExpr); ok && un.Op == token.AND {
			target = un
		}
	}
	f.edits.replace(f.edits.begin(), f.g.offset(target.Pos()), f.g.offset(target.End()), text)
	f.markFiberSelectors(lit)
}

// renameFiberSelector points fiber.X at the package pkgDefault.
func (f *clientFile) renameFiberSelector(sel *ast.SelectorExpr, pkgDefault string) {
	pkg := f.use(clientPkgPath, pkgDefault)
	f.edits.replace(f.edits.begin(), f.g.offset(sel.X.Pos()), f.g.offset(sel.X.End()), pkg)
	f.handled[sel] = true
}

// markFiberSelectors records the fiber members below n as migrated.
func (f *clientFile) markFiberSelectors(n ast.Node) {
	ast.Inspect(n, func(c ast.Node) bool {
		if sel, ok := c.(*ast.SelectorExpr); ok && f.isFiberName(sel.X) {
			f.handled[sel] = true
		}
		return true
	})
}

// dir returns the directory of the file.
func (f *clientFile) dir() string {
	return filepath.Dir(f.fi.Path)
}
