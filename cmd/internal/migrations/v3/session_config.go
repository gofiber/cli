package v3

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateSessionConfig renames the session Expiration to IdleTimeout and keeps
// the lifetime of v2 sessions: Expiration defaulted to 24 hours there, while
// IdleTimeout defaults to 30 minutes in v3, so a config that does not set it
// gets IdleTimeout: 24 * time.Hour.
//
// It rewrites session.Config literals, assignments to the fields of a config or
// a store, and session.New calls, in files that import the fiber session
// middleware under whatever name.
func MigrateSessionConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs := newMw3Pkgs()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return mw3SessionConfigContent(content, fi, pkgs)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating session middleware configs")
	return nil
}

// mw3SessionV3Fields are the Config fields that only exist in v3. A config that
// sets one of them is already written for v3 and keeps the lifetime it names.
var mw3SessionV3Fields = []string{"IdleTimeout", "AbsoluteTimeout", "Extractor", cacheStore, "Next", "ErrorHandler"}

// mw3SessionConfigPass is the rewrite of one file by MigrateSessionConfig.
type mw3SessionConfigPass struct {
	f       *mw3SessionFile
	fi      internal.FileInfo
	imports mw3ImportEdits
	edits   editSet
}

func mw3SessionConfigContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	f, ok := mw3OpenSessionFile(fi, src, pkgs)
	if !ok {
		return src
	}

	p := &mw3SessionConfigPass{f: f, fi: fi}
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			if lit, ok := f.isConfigLit(n, stack); ok && !p.isV3Config(stack) {
				p.configLit(lit, stack)
			}
		case *ast.CallExpr:
			if call, name, ok := f.isStoreCall(n, stack); ok && name == mw3SessionNew && !mw3IsUseArgument(call, stack) {
				p.newCall(call, stack)
			}
		case *ast.SelectorExpr:
			p.selector(n, stack)
		case *ast.ValueSpec:
			p.zeroConfig(n, stack)
		}
		return true
	})

	out := p.edits.apply(src)
	if out == src {
		return src
	}
	return p.imports.apply(out)
}

// isV3Config reports whether the literal whose ancestors are stack is the
// config of a v3 constructor: NewStore, or a New that is handed to Use. A file
// that was moved to v3 by hand keeps the lifetime it chose there.
func (p *mw3SessionConfigPass) isV3Config(stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	call, ok := stack[len(stack)-1].(*ast.CallExpr)
	if !ok {
		return false
	}
	outer := stack[:len(stack)-1]
	if p.f.isMember(call.Fun, mw3SessionNewStore, outer) {
		return true
	}
	return p.f.isMember(call.Fun, mw3SessionNew, outer) && mw3IsUseArgument(call, outer)
}

// idleTimeout is the field that keeps the v2 lifetime, written at the node whose
// position and ancestors are given.
func (p *mw3SessionConfigPass) idleTimeout(at mw3Site) string {
	return "IdleTimeout: " + p.v2Lifetime(at)
}

// v2Lifetime is the expression for the lifetime v2 sessions had by default.
func (p *mw3SessionConfigPass) v2Lifetime(at mw3Site) string {
	return "24 * " + p.f.importAt(&p.imports, "time", at.pos, at.stack, nil, "time", "stdtime") + ".Hour"
}

// configLit migrates a session.Config literal.
func (p *mw3SessionConfigPass) configLit(lit *ast.CompositeLit, stack []ast.Node) {
	g := p.f.g
	if slices.ContainsFunc(lit.Elts, func(e ast.Expr) bool { _, keyed := e.(*ast.KeyValueExpr); return !keyed }) {
		return
	}

	if kv := mw3Field(lit, "CookieSameSite"); kv != nil {
		p.sameSite(kv, stack)
	}

	if kv := mw3Field(lit, "Expiration"); kv != nil {
		p.edits.replaceNode(g, kv.Key, "IdleTimeout")
		return
	}
	for _, name := range mw3SessionV3Fields {
		if kv := mw3Field(lit, name); kv != nil {
			return
		}
	}
	mw3AddElement(g, &p.edits, lit, p.idleTimeout(mw3Site{pos: lit.Pos(), stack: stack}))
}

// sameSite keeps the cookie v2 sent for a "disabled" SameSite. v2 knew only
// strict, none and, for anything else, lax; v3 takes disabled for no attribute
// at all, which is weaker than the cookie the application had.
func (p *mw3SessionConfigPass) sameSite(kv *ast.KeyValueExpr, stack []ast.Node) {
	g := p.f.g
	disabled := false
	if sel, ok := kv.Value.(*ast.SelectorExpr); ok && sel.Sel.Name == "CookieSameSiteDisabled" {
		id, isIdent := sel.X.(*ast.Ident)
		disabled = isIdent && slices.Contains(p.f.fiber, id.Name)
	} else if value, ok := p.f.strings().value(kv.Value, stack); ok {
		disabled = strings.EqualFold(value, "disabled")
	}
	if !disabled {
		return
	}

	p.edits.replaceNode(g, kv.Value, `"Lax"`)
	p.imports.releaseIn(g, kv.Value)
	internal.Notef(p.fi, g.line(kv.Pos()),
		`CookieSameSite %s sent SameSite=Lax in v2, where only strict and none were told apart; v3 would send no SameSite attribute, so it was set to "Lax" to keep the cookie you had. `+
			`Use "Disabled" to leave the attribute out.`, g.text(kv.Value))
}

// newCall keeps the lifetime of a store made by session.New, whose config is
// not a literal that configLit sees.
func (p *mw3SessionConfigPass) newCall(call *ast.CallExpr, stack []ast.Node) {
	g := p.f.g
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	pkgName := g.text(sel.X)
	config := pkgName + ".Config{" + p.idleTimeout(mw3Site{pos: call.Pos(), stack: stack}) + "}"

	if len(call.Args) == 0 {
		p.edits.replace(g.offset(call.Lparen)+1, g.offset(call.Rparen), config)
		return
	}
	if call.Ellipsis.IsValid() {
		return
	}

	arg := call.Args[0]
	if _, isLit := p.f.isConfigLit(arg, stack); isLit {
		return
	}
	if p.f.isMember(arg, mw3ConfigDefault, stack) {
		p.edits.replaceNode(g, arg, config)
		return
	}
	// Any other config was built where this rewrite saw it: from a literal, from
	// ConfigDefault or as a zero value, and has its lifetime there. What is
	// left is a config that comes from another package.
	if mw3FromOtherPackage(arg, stack) {
		internal.Notef(p.fi, g.line(arg.Pos()),
			"%s: v2 sessions lasted 24 hours unless Expiration said otherwise, v3 sessions idle out after 30 minutes unless IdleTimeout says otherwise. "+
				"Make sure the config sets IdleTimeout (24 * time.Hour keeps the v2 lifetime).", g.text(arg))
	}
}

// mw3FromOtherPackage reports whether the expression is a call of a function
// that another package declares, or a variable initialized with one.
func mw3FromOtherPackage(e ast.Expr, stack []ast.Node) bool {
	if id, ok := e.(*ast.Ident); ok {
		d, found := resolveIdent(id.Name, id.Pos(), stack)
		if !found || d.init == nil {
			return false
		}
		e = d.init
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, isVariable := resolveIdent(id.Name, id.Pos(), stack)
	return !isVariable
}

// selector renames the Expiration field of a config or store, and keeps the
// lifetime of a config copied from ConfigDefault.
func (p *mw3SessionConfigPass) selector(sel *ast.SelectorExpr, stack []ast.Node) {
	g := p.f.g
	if sel.Sel.Name == cacheExpiration && p.f.isConfig(sel.X, stack) {
		p.edits.replaceNode(g, sel.Sel, "IdleTimeout")
		return
	}
	if p.f.isMember(sel, mw3ConfigDefault, stack) {
		p.configDefault(sel, stack)
	}
}

// configDefault handles a use of session.ConfigDefault, whose Expiration was
// 24 hours and whose IdleTimeout is 30 minutes.
func (p *mw3SessionConfigPass) configDefault(sel *ast.SelectorExpr, stack []ast.Node) {
	g := p.f.g
	if len(stack) >= 1 {
		if call, ok := stack[len(stack)-1].(*ast.CallExpr); ok && len(call.Args) > 0 && call.Args[0] == ast.Expr(sel) {
			if _, _, isNew := p.f.isStoreCall(call, stack[:len(stack)-1]); isNew {
				return // newCall replaces it
			}
		}
	}

	if name := mw3InitializedName(sel, stack); name != "" && p.keepLifetimeAfter(name, stack) {
		return
	}

	internal.Notef(p.fi, g.line(sel.Pos()),
		"%s lasts 30 minutes in v3 (v2: 24 hours). Set IdleTimeout to 24 * time.Hour on the config you build from it to keep the v2 lifetime.", g.text(sel))
}

// zeroConfig handles a session.Config variable that is declared without a
// value: a zero config has no IdleTimeout, which v3 takes for 30 minutes.
func (p *mw3SessionConfigPass) zeroConfig(spec *ast.ValueSpec, stack []ast.Node) {
	if spec.Type == nil || len(spec.Values) > 0 || !p.f.isMember(spec.Type, mw3Config, stack) {
		return
	}
	for _, name := range spec.Names {
		if name.Name == "_" {
			continue
		}
		if !p.keepLifetimeAfter(name.Name, stack) {
			internal.Notef(p.fi, p.f.g.line(spec.Pos()),
				"%s is a zero session.Config: v2 sessions lasted 24 hours by default, v3 sessions idle out after 30 minutes. Set IdleTimeout to 24 * time.Hour to keep the v2 lifetime.", name.Name)
		}
	}
}

// keepLifetimeAfter makes the declaration statement that the node whose
// ancestors are stack sits in set IdleTimeout on the variable, unless something
// after it does. It reports whether the declaration is one it can do that after.
func (p *mw3SessionConfigPass) keepLifetimeAfter(name string, stack []ast.Node) bool {
	g := p.f.g
	stmt, list, ok := mw3StatementOf(stack)
	if !ok {
		return false
	}
	switch stmt.(type) {
	case *ast.AssignStmt, *ast.DeclStmt:
	default:
		return false
	}
	if !mw3AssignsField(list, stmt, name, "Expiration", "IdleTimeout") {
		end := g.offset(stmt.End())
		p.edits.replace(end, end, "\n"+mw3Indent(g.src, g.offset(stmt.Pos()))+name+".IdleTimeout = "+p.v2Lifetime(mw3Site{pos: stmt.End(), stack: stack}))
	}
	return true
}

// mw3InitializedName returns the variable that the declaration it is the value
// of gives sel to, or "" when sel is not the value of a declaration.
func mw3InitializedName(sel *ast.SelectorExpr, stack []ast.Node) string {
	if len(stack) == 0 {
		return ""
	}
	switch p := stack[len(stack)-1].(type) {
	case *ast.AssignStmt:
		if len(p.Lhs) != len(p.Rhs) {
			return ""
		}
		for i, rhs := range p.Rhs {
			if id, ok := p.Lhs[i].(*ast.Ident); ok && rhs == ast.Expr(sel) && id.Name != "_" {
				return id.Name
			}
		}
	case *ast.ValueSpec:
		if len(p.Names) != len(p.Values) {
			return ""
		}
		for i, v := range p.Values {
			if v == ast.Expr(sel) && p.Names[i].Name != "_" {
				return p.Names[i].Name
			}
		}
	}
	return ""
}

// mw3StatementOf returns the statement of a block that holds the node whose
// ancestors are stack, and the statements of that block.
func mw3StatementOf(stack []ast.Node) (stmt ast.Stmt, list []ast.Stmt, ok bool) {
	for i := len(stack) - 1; i > 0; i-- {
		switch c := stack[i-1].(type) {
		case *ast.BlockStmt:
			list = c.List
		case *ast.CaseClause:
			list = c.Body
		case *ast.CommClause:
			list = c.Body
		default:
			continue
		}
		stmt, ok = stack[i].(ast.Stmt)
		return stmt, list, ok
	}
	return nil, nil, false
}

// mw3AssignsField reports whether a statement after stmt assigns one of the
// fields of the variable.
func mw3AssignsField(list []ast.Stmt, stmt ast.Stmt, variable string, fields ...string) bool {
	found := false
	for _, later := range list {
		if later.Pos() <= stmt.Pos() {
			continue
		}
		ast.Inspect(later, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return !found
			}
			for _, lhs := range as.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if ok && isIdentNamed(sel.X, variable) && slices.Contains(fields, sel.Sel.Name) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}
