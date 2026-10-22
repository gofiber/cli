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
// returns []any now, which cannot be fixed without knowing what the keys are
// used for, so the calls are reported.
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
	f           *mw3SessionFile
	fi          internal.FileInfo
	contextName string
	imports     mw3ImportEdits
	edits       editSet
}

func mw3SessionMethodsContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	f, ok := mw3OpenStoreUser(fi, src, pkgs, func(src string) bool {
		return strings.Contains(src, ".SetExpiry(") || strings.Contains(src, ".Reset(") ||
			strings.Contains(src, ".Delete(") || strings.Contains(src, ".Keys(")
	})
	if !ok {
		return src
	}

	p := &mw3SessionMethodsPass{f: f, fi: fi}
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			p.call(call, stack)
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

	switch {
	case sel.Sel.Name == "SetExpiry" && len(call.Args) == 1:
		if p.f.isSession(sel.X, stack) {
			p.edits.replaceNode(g, sel.Sel, "SetIdleTimeout")
			return
		}
		if !mw3TypeIsShown(sel.X, stack) {
			internal.Notef(p.fi, g.line(call.Pos()),
				"if %s is a session, SetExpiry is called SetIdleTimeout in v3.", g.text(sel.X))
		}
	case sel.Sel.Name == "Reset" && len(call.Args) == 0 && p.f.isStore(sel.X, stack):
		p.edits.replace(g.offset(call.Lparen)+1, g.offset(call.Rparen), p.context(stack))
	case sel.Sel.Name == nameDelete && len(call.Args) == 1 && p.f.isStore(sel.X, stack):
		start := g.offset(call.Args[0].Pos())
		p.edits.replace(start, start, p.context(stack)+", ")
	case sel.Sel.Name == "Keys" && len(call.Args) == 0 && p.f.isSession(sel.X, stack):
		internal.Notef(p.fi, g.line(call.Pos()),
			"%s returns []any in v3 (v2: []string): convert the keys where you need strings, for instance with fmt.Sprint(key) or key.(string).", g.text(call))
	}
}

// context returns the expression for the context a store call takes: the
// request context of the handler the call is in, or context.Background().
func (p *mw3SessionMethodsPass) context(stack []ast.Node) string {
	for _, node := range slices.Backward(stack) {
		var ft *ast.FuncType
		switch n := node.(type) {
		case *ast.FuncLit:
			ft = n.Type
		case *ast.FuncDecl:
			ft = n.Type
		default:
			continue
		}
		// Only the function the call is in: a closure it starts may outlive
		// the request, and the context with it.
		for _, field := range ft.Params.List {
			if !isFiberType(field.Type, p.f.fiber, "Ctx") {
				continue
			}
			for _, name := range field.Names {
				if name.Name != "_" {
					return name.Name
				}
			}
		}
		break
	}

	if p.contextName == "" {
		p.contextName = mw3ImportName(p.f.g.src, "context")
		if p.contextName == "" {
			declared := mw3DeclaredNames(p.f.g.file)
			p.contextName = mw3FreeName(func(name string) bool {
				_, taken := declared[name]
				return taken
			}, "context", "stdcontext")
			p.imports.require("context", p.contextName)
		}
	}
	return p.contextName + ".Background()"
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
