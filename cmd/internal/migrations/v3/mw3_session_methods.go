package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateSessionMethods adapts the calls on a session and on a session store
// whose signature changed in v3:
//
//	sess.SetExpiry(d)  ->  sess.SetIdleTimeout(d)
//	store.Reset()      ->  store.Reset(c)
//	store.Delete(id)   ->  store.Delete(c, id)
//
// The store methods take a context for the storage call: the request context
// when the call sits in a handler, context.Background() elsewhere. Session.Keys
// returns []any now, where the keys of a v2 session were strings: the call is
// wrapped in a function literal that converts them, so that the result is the
// []string the program was written for.
//
// A store that the program keeps behind an interface of its own gets the same:
// the interface that asks for the methods of the v2 store, the types that have
// them, and the calls through both take the context (see mw3StoreTypes). A
// receiver may also be a type that embeds a store, or a store of another package
// of the module, and a call that a type the source does not show may have
// declared is reported instead of rewritten.
func MigrateSessionMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs := newMw3Pkgs()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return mw3SessionMethodsContent(content, fi, pkgs)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating session methods")
	return nil
}

// mw3SessionMethodsPass is the rewrite of one file by MigrateSessionMethods.
type mw3SessionMethodsPass struct {
	f     *mw3SessionFile
	types *mw3StoreTypes
	// env tells which types are the request context of Fiber: an alias of it, a
	// struct that embeds it, here or in another package of the module. It is built
	// when the first store call asks for a context.
	env *ctxEnv
	// added are the functions that were given a context to pass on, by the name of
	// the parameter.
	added    map[*ast.FuncType]string
	imports  mw3ImportEdits
	edits    editSet
	fi       internal.FileInfo
	envReady bool
}

func mw3SessionMethodsContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	f, ok := mw3OpenStoreUser(fi, src, pkgs, func(src string) bool {
		return strings.Contains(src, "SetExpiry(") || strings.Contains(src, "Reset(") ||
			strings.Contains(src, "Delete(") || strings.Contains(src, "Keys(")
	})
	if !ok {
		return src
	}

	// What stands in for the store is decided before any file is rewritten.
	p := &mw3SessionMethodsPass{f: f, fi: fi, types: pkgs.storeTypes(fi), added: map[*ast.FuncType]string{}}
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch n := n.(type) {
		case *ast.TypeSpec:
			p.interfaceSpec(n, stack)
		case *ast.FuncDecl:
			p.standIn(n, stack)
		case *ast.CallExpr:
			p.call(n, stack)
		}
		return true
	})

	out := p.edits.apply(src)
	if out == src {
		return src
	}
	return p.imports.apply(out)
}

func (p *mw3SessionMethodsPass) call(call *ast.CallExpr, stack []ast.Node) {
	g := p.f.g
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || call.Ellipsis.IsValid() {
		return
	}

	switch name := sel.Sel.Name; {
	case name == mw3SetExpiry && len(call.Args) == 1:
		m, isSession := p.f.providedBy(sel.X, name, mw3KindSession, stack)
		switch {
		case isSession && !m.unsure:
			p.edits.replaceNode(g, sel.Sel, mw3SetIdle)
		case isSession:
			p.noteUnsure(call, name, "SetExpiry is called SetIdleTimeout in v3")
		case !m.user && !mw3TypeIsShown(sel.X, stack):
			internal.Notef(p.fi, g.line(call.Pos()),
				"if %s is a session, SetExpiry is called SetIdleTimeout in v3.", g.text(sel.X))
		}
	case name == mw3Reset && len(call.Args) == 0 && p.isStore(call, sel, stack, "Reset takes a context in v3: Reset(ctx)"):
		p.edits.replace(g.offset(call.Lparen)+1, g.offset(call.Rparen), p.context(call, stack))
	case name == nameDelete && len(call.Args) == 1 && p.isStore(call, sel, stack, "Delete takes a context in v3: Delete(ctx, id)"):
		start := g.offset(call.Args[0].Pos())
		p.edits.replace(start, start, p.context(call, stack)+", ")
	case name == mw3KeysMethod && len(call.Args) == 0:
		m, isSession := p.f.providedBy(sel.X, name, mw3KindSession, stack)
		switch {
		case isSession && !m.unsure:
			p.keys(call, stack)
		case isSession:
			p.noteUnsure(call, name, "Keys returns []any in v3 (v2: []string)")
		}
	}
}

// isStore reports whether the method called by the selector is one of a session
// store. A call that may be, because a type that the source does not show sits
// above the store in what the receiver embeds, is reported with what changed.
func (p *mw3SessionMethodsPass) isStore(call *ast.CallExpr, sel *ast.SelectorExpr, stack []ast.Node, change string) bool {
	m, ok := p.f.providedBy(sel.X, sel.Sel.Name, mw3KindStore, stack)
	if ok && m.unsure {
		p.noteUnsure(call, sel.Sel.Name, change)
		return false
	}
	return ok
}

// noteUnsure reports a call that is the method of a session type if no type
// above it declares the same name.
func (p *mw3SessionMethodsPass) noteUnsure(call *ast.CallExpr, method, change string) {
	internal.Notef(p.fi, p.f.g.line(call.Pos()),
		"%s: if this is the %s of the session type that its receiver embeds, %s. It cannot be told here, "+
			"because a type that the receiver embeds above it may declare %s as well.",
		p.f.g.text(call.Fun), method, change, method)
}

// keys makes the keys of a session the strings they were in v2:
//
//	sess.Keys()  ->  func(keys []any) []string { ... fmt.Sprint(key) ... }(sess.Keys())
//
// fmt.Sprint hands a string key back as it is and cannot panic on any other
// kind. The call stays as it is where a slice of any does as well, which the
// program cannot tell from one of strings: its length is all that is taken, or it
// is passed to a function literal that takes []any, which also is what the
// rewritten call looks like.
func (p *mw3SessionMethodsPass) keys(call *ast.CallExpr, stack []ast.Node) {
	g := p.f.g
	if mw3KeysNeedNoStrings(call, stack) {
		return
	}
	pkg := p.f.importAt(&p.imports, "fmt", call.Pos(), stack, mw3KeysFuncNames, "fmt", "stdfmt")
	start, end := g.offset(call.Pos()), g.offset(call.End())
	p.edits.replace(start, start, strings.Join([]string{
		"func(keys []any) []string {",
		"names := make([]string, len(keys))",
		"for i, key := range keys {",
		"names[i] = " + pkg + ".Sprint(key)",
		"}",
		"return names",
		"}(",
	}, "\n"))
	p.edits.replace(end, end, ")")
}

// mw3KeysFuncNames are the names the function literal of keys declares: fmt must
// not be called one of them.
var mw3KeysFuncNames = []string{"keys", "names", "i", mw3KeyVar}

// mw3KeyVar is the variable of the generated function that holds one key.
const mw3KeyVar = "key"

// mw3KeysNeedNoStrings reports whether the call of Keys, whose ancestors are
// stack, is used in a way that does not care what the keys are: len(sess.Keys()),
// or the argument of a function literal that is called at once and takes a slice
// of any for it.
func mw3KeysNeedNoStrings(call *ast.CallExpr, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	outer, ok := stack[len(stack)-1].(*ast.CallExpr)
	if !ok || outer.Ellipsis.IsValid() {
		return false
	}
	idx := slices.Index(outer.Args, ast.Expr(call))
	if idx < 0 {
		return false
	}

	if isIdentNamed(outer.Fun, "len") && len(outer.Args) == 1 {
		_, shadowed := resolveIdent("len", outer.Pos(), stack)
		return !shadowed
	}
	lit, ok := outer.Fun.(*ast.FuncLit)
	if !ok {
		return false
	}
	i := 0
	for _, field := range lit.Type.Params.List {
		for range max(len(field.Names), 1) {
			if i == idx {
				return mw3IsSliceOfAny(field.Type)
			}
			i++
		}
	}
	return false
}

// mw3IsSliceOfAny reports whether the type is []any or []interface{}.
func mw3IsSliceOfAny(t ast.Expr) bool {
	arr, ok := t.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	if iface, isIface := arr.Elt.(*ast.InterfaceType); isIface {
		return iface.Methods == nil || len(iface.Methods.List) == 0
	}
	return isIdentNamed(arr.Elt, "any")
}

// context returns the expression for the context the store call takes: the
// request context of the handler the call is in, or context.Background().
func (p *mw3SessionMethodsPass) context(call *ast.CallExpr, stack []ast.Node) string {
	for _, node := range slices.Backward(stack) {
		var ft *ast.FuncType
		var recv *ast.FieldList
		switch n := node.(type) {
		case *ast.FuncLit:
			ft = n.Type
		case *ast.FuncDecl:
			ft, recv = n.Type, n.Recv
		default:
			continue
		}
		// Only the function the call is in: a closure it starts may outlive
		// the request, and the context with it.
		if name := p.added[ft]; name != "" {
			return name
		}
		if name := p.requestContext(ft, recv); name != "" {
			return name
		}
		break
	}

	return p.f.importAt(&p.imports, "context", call.Pos(), stack, nil, "context", "stdcontext") + ".Background()"
}

// requestContext returns the name of the parameter of the function, or of its
// receiver, that holds the context of the request: a fiber.Ctx, a context.Context,
// or a type that is the fiber context under another name (an alias, a struct that
// embeds it) wherever the program declares it.
func (p *mw3SessionMethodsPass) requestContext(ft *ast.FuncType, recv *ast.FieldList) string {
	for _, fields := range []*ast.FieldList{ft.Params, recv} {
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			if !isFiberType(field.Type, p.f.fiber, "Ctx") && !p.isContext(field.Type) && !p.isFiberContext(field.Type) {
				continue
			}
			for _, name := range field.Names {
				if name.Name != "_" {
					return name.Name
				}
			}
		}
	}
	return ""
}

// isFiberContext reports whether the type is the request context of Fiber under
// another name, as the environment of the context migrations tells.
func (p *mw3SessionMethodsPass) isFiberContext(t ast.Expr) bool {
	if !p.envReady {
		p.envReady = true
		p.env = p.f.pkgs.contexts().envFor(p.fi, p.f.g, p.f.fiber)
	}
	return p.env != nil && p.env.typeIs(t, ctxTypeCtx) == ctxYes
}

// isContext reports whether the type is context.Context.
func (p *mw3SessionMethodsPass) isContext(t ast.Expr) bool {
	sel, ok := t.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Context" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == mw3ImportName(p.f.g.src, "context")
}

// interfaceSpec gives the methods of an interface that asks for the v2 session
// store a context.
func (p *mw3SessionMethodsPass) interfaceSpec(spec *ast.TypeSpec, stack []ast.Node) {
	it, ok := spec.Type.(*ast.InterfaceType)
	if !ok {
		return
	}
	if _, store := p.types.ifaces[mw3TypeKey{p.f.pkgOf(), spec.Name.Name}]; !store {
		return
	}
	for _, m := range it.Methods.List {
		ft, isFunc := m.Type.(*ast.FuncType)
		if !isFunc || len(m.Names) != 1 || !slices.Contains(mw3ContextMethods, m.Names[0].Name) ||
			!mw3IsStoreMethod(m.Names[0].Name, ft, p.f.env) {
			continue
		}
		p.addContext(ft, nil, stack)
	}
}

// standIn gives the methods of a type that has the methods of the v2 session
// store a context, as the interface has.
func (p *mw3SessionMethodsPass) standIn(d *ast.FuncDecl, stack []ast.Node) {
	if d.Recv == nil || len(d.Recv.List) != 1 || !slices.Contains(mw3ContextMethods, d.Name.Name) {
		return
	}
	recv := mw3KindOfType(d.Recv.List[0].Type, nil)
	if recv.base != mw3KindNamed {
		return
	}
	if !p.types.stands(mw3TypeKey{p.f.pkgOf(), recv.name}, d.Name.Name) || !mw3IsStoreMethod(d.Name.Name, d.Type, p.f.env) {
		return
	}
	p.addContext(d.Type, d.Recv, stack)
}

// addContext adds the parameter ctx context.Context in front of the parameters of
// a method, or a context.Context in front of unnamed ones. recv is the receiver
// of the method, whose name the parameter cannot have.
func (p *mw3SessionMethodsPass) addContext(ft *ast.FuncType, recv *ast.FieldList, stack []ast.Node) {
	g := p.f.g
	pkg := p.f.importAt(&p.imports, "context", ft.Pos(), stack, nil, "context", "stdcontext")

	named := len(ft.Params.List) == 0 || len(ft.Params.List[0].Names) > 0
	name := ""
	if named {
		name = mw3FreeName(func(name string) bool {
			_, inParams := declInFieldList(name, ft.Params)
			_, inRecv := declInFieldList(name, recv)
			return inParams || inRecv
		}, "ctx", "sessionCtx")
		p.added[ft] = name
	}

	text := pkg + ".Context"
	if named {
		text = name + " " + text
	}
	if len(ft.Params.List) > 0 {
		text += ", "
	}
	at := g.offset(ft.Params.Opening) + 1
	if len(ft.Params.List) > 0 {
		at = g.offset(ft.Params.List[0].Pos())
	}
	p.edits.replace(at, at, text)
}

// mw3TypeIsShown reports whether the file shows what type the expression is
// of: a literal, or a variable declared with a type or with a literal.
func mw3TypeIsShown(e ast.Expr, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return mw3TypeIsShown(x.X, stack)
	case *ast.CompositeLit, *ast.BasicLit:
		return true
	case *ast.UnaryExpr:
		return x.Op == token.AND && mw3TypeIsShown(x.X, stack)
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		return d.typ != nil || (d.init != nil && mw3TypeIsShown(d.init, stack))
	}
	return false
}
