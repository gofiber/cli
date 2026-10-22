package v3

import (
	"fmt"
	"go/ast"
	"maps"
	"path/filepath"
	"slices"
)

// appFileLabel names a file for a note: its path in the project.
func appFileLabel(f *appFile) string {
	if f.fi.Rel != "" {
		return filepath.ToSlash(f.fi.Rel)
	}
	return filepath.Base(f.path)
}

// startValuesIn returns the methods that start an app which f uses as values
// (start := app.Listen) instead of calling them.
func (f *appFile) startValuesIn() []*appValueInfo {
	var out []*appValueInfo
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !slices.Contains(appStartMethods, sel.Sel.Name) {
			return true
		}
		if call, isCall := f.parent[sel].(*ast.CallExpr); isCall && call.Fun == ast.Expr(sel) {
			return true
		}
		if f.kind(sel.X) == appKindApp {
			out = append(out, &appValueInfo{file: f, sel: sel, method: sel.Sel.Name})
		}
		return true
	})
	return out
}

// unknownStart returns the line of the first call of a method that starts an app
// on a value whose type the migration cannot tell (an element of a collection it
// cannot follow, a value behind an interface), 0 when there is none: such a call
// may start an app of the registry, and the registry cannot be asked from it.
func (f *appFile) unknownStart() int {
	line := 0
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if line > 0 || !ok {
			return line == 0
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || !slices.Contains(appStartMethods, sel.Sel.Name) || len(call.Args) < 1 || len(call.Args) > 4 {
			return true
		}
		if f.receiverKind(call, sel) != appKindNone {
			return true
		}
		if id, isIdent := sel.X.(*ast.Ident); isIdent && f.importPathOf(id.Name) != "" {
			if _, bound := f.binding(id); !bound {
				return true // a function of a package
			}
		}
		if callee, found := f.calleeOf(call, 0); found && callee.decl != nil && callee.decl.Recv != nil {
			return true // a method of the project
		}
		if t, _ := f.valueType(sel.X, 0); t != nil {
			return true // a value of a type that is known, and is not an app
		}
		line = f.g.line(call.Pos())
		return true
	})
	return line
}

// unknownStartOf returns the line of the first call in f that starts an app on a
// value of unknown type, once per file.
func (pl *appPlanner) unknownStartOf(f *appFile) int {
	if line, done := pl.unknown[f]; done {
		return line
	}
	line := f.unknownStart()
	pl.unknown[f] = line
	return line
}

// explicitConfig returns the fiber.ListenConfig literal the call passes, nil when
// there is none.
func (s appStart) explicitConfig() *ast.CompositeLit {
	if (s.method != appNameListen && s.method != appNameListener) || len(s.call.Args) != 2 {
		return nil
	}
	lit, ok := s.call.Args[1].(*ast.CompositeLit)
	if !ok || lit.Type == nil || s.file.fiberTypeName(lit.Type) != appNameListenConfig {
		return nil
	}
	return lit
}

// exportedAppAPI returns the names the package exports through which an app
// leaves it, as far as flows says an app does: a function that returns one, a
// variable or a field that holds one.
func (p *appPackage) exportedAppAPI(flows func(appOrigins) bool) []string {
	var out []string

	for _, pf := range p.files {
		for _, d := range pf.g.file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !appIsExported(fn.Name.Name) {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil {
				recv := appReceiverTypeName(fn.Recv)
				if !appIsExported(recv) {
					continue
				}
				name = recv + "." + name
			}
			callee := appCallee{file: pf, typ: fn.Type, body: fn.Body, decl: fn}
			for idx := range appResultCount(fn.Type) {
				if pf.resultKind(fn.Type, idx, 0) == appKindApp && flows(callee.returnOrigins(idx, 0)) {
					out = append(out, name)
					break
				}
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(p.vars)) {
		v := p.vars[name]
		b := v.binding()
		if !appIsExported(name) || b.file.bindingKind(b, 0) != appKindApp {
			continue
		}
		o := appOrigins{complete: true}
		if b.init != nil {
			o.merge(b.file.originsOf(b.init, 0, 0))
		}
		o.merge(b.file.assignedOrigins(b, name, 0))
		if flows(o) {
			out = append(out, name)
		}
	}

	for _, typeName := range slices.Sorted(maps.Keys(p.types)) {
		td := p.types[typeName]
		st, ok := td.spec.Type.(*ast.StructType)
		if !ok || !appIsExported(typeName) {
			continue
		}
		for _, fld := range st.Fields.List {
			if td.file.typeKind(fld.Type, 0) != appKindApp {
				continue
			}
			names := make([]string, 0, len(fld.Names))
			for _, id := range fld.Names {
				names = append(names, id.Name)
			}
			if len(fld.Names) == 0 {
				names = append(names, appBaseTypeName(appSelectorTail(fld.Type)))
			}
			for _, name := range names {
				if appIsExported(name) && flows(td.file.setsOfField(td, name, 0)) {
					out = append(out, fmt.Sprintf("%s.%s", typeName, name))
				}
			}
		}
	}
	return out
}
