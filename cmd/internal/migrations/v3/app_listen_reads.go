package v3

import (
	"go/ast"
	"go/token"
	"slices"

	"github.com/gofiber/cli/cmd/internal"
)

// appRead is a read of one of the settings that v3 removed from fiber.Config,
// through the config of an app: app.Config().Prefork. The registry keeps the
// settings of the apps it made, and ConfigOf gives them back with the rest of the
// config, in a value that has the four fields v2 had.
type appRead struct {
	file *appFile
	// sel is the read, nil for a config that is only handed on.
	sel *ast.SelectorExpr
	configRef
}

// configRef is where a config comes from: the call app.Config(), or the variable
// that was declared with it.
type configRef struct {
	// call is the app.Config() that the read goes through, or that initializes the
	// variable it reads or hands on.
	call *ast.CallExpr
	// recv is the app.
	recv ast.Expr
	// news are the fiber.New calls that make an app from the config: the settings
	// go with it in v2, so they become calls of the registry's New.
	news []*ast.CallExpr
}

// appConfigCall returns the app of the call recv.Config(), when recv is one: an
// app, or a type that embeds one and declares no Config of its own.
func (f *appFile) appConfigCall(e ast.Expr) (call *ast.CallExpr, recv ast.Expr, ok bool) {
	call, isCall := ast.Unparen(e).(*ast.CallExpr)
	if !isCall || len(call.Args) != 0 {
		return nil, nil, false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != appNameConfig || f.callKind(call, 0, 0) != appKindConfig {
		return nil, nil, false
	}
	if callee, found := f.calleeOf(call, 0); found && callee.decl != nil && callee.decl.Recv != nil {
		return nil, nil, false // a Config the project declares
	}
	return call, sel.X, true
}

// readsIn returns the reads of removed settings that f makes through the config of
// an app: those of a call (app.Config().Prefork) and those of a variable that holds
// the result of one and is only used to read and assign fields, or to make another
// app. It also returns the apps that are made from the config of another, which
// take the settings with them.
func (f *appFile) readsIn() []*appRead {
	var reads []*appRead
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if appListenerFieldV3(x.Sel.Name) == "" {
				return true
			}
			if ref, found := f.configOfSelector(x.X); found {
				reads = append(reads, &appRead{file: f, sel: x, configRef: ref})
			}
		case *ast.CallExpr:
			if f.isNewCall(x) && len(x.Args) == 1 {
				if ref, found := f.configOfSelector(x.Args[0]); found {
					reads = append(reads, &appRead{file: f, configRef: ref})
				}
			}
		}
		return true
	})
	return reads
}

// configOfSelector tells whether e is the config of an app: the call
// app.Config(), or a variable declared as cfg := app.Config() whose every use is a
// selector (a read or an assignment of a field) or the argument of fiber.New, so
// that the variable can hold the config with the settings in its place. It returns
// the call, the app, and the fiber.New calls that the variable is handed to.
func (f *appFile) configOfSelector(e ast.Expr) (configRef, bool) {
	if call, recv, ok := f.appConfigCall(e); ok {
		ref := configRef{call: call, recv: recv}
		if p, isNew := f.parent[ast.Unparen(e)].(*ast.CallExpr); isNew && f.isNewCall(p) && len(p.Args) == 1 {
			ref.news = append(ref.news, p)
		}
		return ref, true
	}
	if id, isIdent := ast.Unparen(e).(*ast.Ident); isIdent {
		return f.heldConfig(id)
	}
	return configRef{}, false
}

// heldConfig is configOfSelector for a variable.
func (f *appFile) heldConfig(id *ast.Ident) (configRef, bool) {
	b, bound := f.binding(id)
	if !bound || b.file != f || b.init == nil || b.count != 1 || b.scope == nil {
		return configRef{}, false
	}
	call, recv, isConfig := f.appConfigCall(b.init)
	if !isConfig {
		return configRef{}, false
	}
	ref := configRef{call: call, recv: recv}

	onlyFields := true
	ast.Inspect(b.scope, func(n ast.Node) bool {
		use, isIdent := n.(*ast.Ident)
		if !isIdent || use.Name != id.Name || !onlyFields {
			return onlyFields
		}
		if ub, same := f.binding(use); !same || ub.node != b.node {
			return true
		}
		switch p := f.parent[use].(type) {
		case *ast.SelectorExpr:
			onlyFields = p.X == ast.Expr(use)
		case *ast.CallExpr:
			onlyFields = f.isNewCall(p) && len(p.Args) == 1 && p.Args[0] == ast.Expr(use)
			if onlyFields {
				ref.news = append(ref.news, p)
			}
		case *ast.AssignStmt:
			// the declaration itself
			onlyFields = p.Tok == token.DEFINE && len(p.Rhs) == 1 && p.Rhs[0] == b.init && slices.Contains(p.Lhs, ast.Expr(use))
		default:
			onlyFields = false
		}
		return onlyFields
	})
	if !onlyFields {
		return configRef{}, false
	}
	return ref, true
}

// appModuleReads are the reads of one module.
type appModuleReads struct {
	mod   goModule
	reads []*appRead
}

// collectReads finds the reads of removed settings through the config of an app.
func (pl *appPlanner) collectReads() {
	for _, f := range pl.files {
		if !f.walked {
			continue
		}
		mod, ok := pl.moduleOf(f)
		if !ok {
			continue
		}
		for _, r := range f.readsIn() {
			mr := pl.reads[mod.root]
			if mr == nil {
				mr = &appModuleReads{mod: mod}
				pl.reads[mod.root] = mr
			}
			mr.reads = append(mr.reads, r)
		}
	}
}

// finishReads turns the reads into reads of the config the registry gives back, in
// the modules whose apps the registry keeps the settings of, and says which reads
// stay where the registry could not be used.
func (pl *appPlanner) finishReads() {
	roots := make([]string, 0, len(pl.reads))
	for root := range pl.reads {
		roots = append(roots, root)
	}
	slices.Sort(roots)

	for _, root := range roots {
		mr := pl.reads[root]
		if why := pl.blocked[root]; why != "" {
			for _, r := range mr.reads {
				internal.Notef(r.file.fi, r.file.g.line(r.sel.Pos()),
					"fiber.Config has no %s field in v3: read the setting from the fiber.ListenConfig of the call that starts the app "+
						"(the registry in %s, which keeps the settings of the apps of this module, could not be used: %s)",
					r.sel.Sel.Name, moduleShimDir, why)
			}
			continue
		}

		// a config that is only handed on has no settings to give back in a module
		// whose apps never had any
		if !slices.ContainsFunc(mr.reads, func(r *appRead) bool { return r.sel != nil }) && pl.shims[root] == nil {
			continue
		}

		sp := pl.shimOf(mr.mod, mr.reads[0].file.fi)
		if sp.path == "" || !sp.ensure() {
			for _, r := range mr.reads {
				if r.sel != nil {
					internal.Notef(r.file.fi, r.file.g.line(r.sel.Pos()),
						"fiber.Config has no %s field in v3: the registry in %s could not be added to the module to give it back", r.sel.Sel.Name, moduleShimDir)
				}
			}
			continue
		}

		done := map[*ast.CallExpr]bool{}
		for _, r := range mr.reads {
			if done[r.call] {
				continue
			}
			done[r.call] = true

			fp := pl.plan.fileOf(r.file)
			recv, rest := appEditedText(fp.edits.edits, r.file.g, r.recv)
			text, ok := r.file.appText(recv, r.recv)
			if !ok {
				if r.sel != nil {
					internal.Notef(r.file.fi, r.file.g.line(r.sel.Pos()),
						"fiber.Config has no %s field in v3: the receiver is not a *fiber.App but a type that holds one in a way the registry cannot name", r.sel.Sel.Name)
				}
				continue
			}

			imps := appImports{reqs: slices.Clone(fp.imps.reqs)}
			shim := imps.qualifier(r.file, sp.path, moduleShimPkg)
			g := r.file.g
			rest = append(rest, textEdit{start: g.offset(r.call.Pos()), end: g.offset(r.call.End()), text: shim + appShimConfigOf + "(" + text + ")"})
			for _, newCall := range r.news {
				if !done[newCall] {
					done[newCall] = true
					rest = append(rest, textEdit{start: g.offset(newCall.Fun.Pos()), end: g.offset(newCall.Fun.End()), text: shim + appShimNewV2})
				}
			}
			fp.edits.edits = rest
			fp.imps = imps
		}
	}
}
