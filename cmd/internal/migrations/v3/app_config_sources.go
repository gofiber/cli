package v3

import (
	"go/ast"
	"go/token"
	"slices"
)

// appListenSource is a fiber.Config as it is written in the code: an inline
// literal handed to fiber.New, or a variable that holds one and is filled in
// field by field. fields are the settings it carries that v3 moved to
// fiber.ListenConfig.
type appListenSource struct {
	file *appFile
	// lit is the literal, nil for a variable that is only assigned to.
	lit *ast.CompositeLit
	// node and scope identify the variable that holds the config (the same
	// values as appBinding.node and appBinding.scope); node is nil for an inline
	// or loose literal.
	node ast.Node
	// scope is nil for a package-level variable.
	scope ast.Node
	// retFn is the function the literal is returned from, nil otherwise.
	retFn     *ast.FuncDecl
	name      string
	fields    []*appSourceField
	creations []appNew
	// escapes is set when the config is also used in a way that cannot be
	// followed, so that settings moved to the apps found would be lost for
	// the others.
	escapes bool
}

// appSourceField is one setting of a source that moves to fiber.ListenConfig.
type appSourceField struct {
	// value is the value to move: the last one written, nil when the field
	// cannot move.
	value ast.Expr
	// problem says why the field cannot move, empty when it can.
	problem string
	v2      string
	v3      string
	occs    []appOccurrence
	// dropped is set when the setting was removed because it has no effect.
	dropped bool
}

// appOccurrence is a place that sets or reads a setting.
type appOccurrence struct {
	assign *ast.AssignStmt
	node   ast.Node
	// elt is the index of the element in the literal, -1 for an assignment or a
	// read.
	elt int
}

// configSources returns the fiber.Config values written in f that carry
// listener settings, with the fiber.New calls that take them.
func (f *appFile) configSources() []*appListenSource {
	var out []*appListenSource
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if x.Type != nil && f.fiberTypeName(x.Type) == appNameConfig {
				out = append(out, f.sourceOfLiteral(x))
			}
		case *ast.ValueSpec:
			if len(x.Values) == 0 && x.Type != nil && f.fiberTypeName(x.Type) == appNameConfig {
				for _, name := range x.Names {
					src := &appListenSource{file: f, node: x.Type, scope: f.specScope(x), name: name.Name}
					out = append(out, src)
				}
			}
		}
		return true
	})

	var keep []*appListenSource
	for _, src := range out {
		src.collect()
		if len(src.fields) > 0 {
			keep = append(keep, src)
		}
	}
	return keep
}

// sourceOfLiteral works out what a fiber.Config literal is used for.
func (f *appFile) sourceOfLiteral(lit *ast.CompositeLit) *appListenSource {
	src := &appListenSource{file: f, lit: lit}
	switch p := f.parent[lit].(type) {
	case *ast.CallExpr:
		if f.isNewCall(p) && len(p.Args) == 1 && p.Args[0] == lit {
			src.creations = []appNew{{file: f, call: p}}
		}
	case *ast.AssignStmt:
		if id, ok := appSingleDefine(p, lit); ok {
			src.node, src.scope, src.name = lit, f.parent[p], id.Name
		}
	case *ast.ValueSpec:
		for i, id := range p.Names {
			if len(p.Names) == len(p.Values) && p.Values[i] == lit {
				src.node, src.scope, src.name = lit, f.specScope(p), id.Name
			}
		}
	case *ast.ReturnStmt:
		if fn, ok := f.enclosingFuncOf(p).(*ast.FuncDecl); ok && appResultCount(fn.Type) == 1 && len(p.Results) == 1 {
			src.retFn = fn
		}
	}
	return src
}

// enclosingFuncOf returns the innermost function declaration or literal around
// n.
func (f *appFile) enclosingFuncOf(n ast.Node) ast.Node {
	for p := f.parent[n]; p != nil; p = f.parent[p] {
		switch p.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return p
		}
	}
	return nil
}

// appSingleDefine returns the variable that `v := rhs` declares.
func appSingleDefine(as *ast.AssignStmt, rhs ast.Expr) (*ast.Ident, bool) {
	if as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 || as.Rhs[0] != rhs {
		return nil, false
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	return id, ok
}

// collect finds the fiber.New calls that take the source and the listener
// settings it carries.
func (src *appListenSource) collect() {
	byName := map[string]*appSourceField{}
	field := func(v2 string) *appSourceField {
		if fld := byName[v2]; fld != nil {
			return fld
		}
		fld := &appSourceField{v2: v2, v3: appListenerFieldV3(v2)}
		byName[v2] = fld
		src.fields = append(src.fields, fld)
		return fld
	}

	if src.lit != nil {
		for i, el := range src.lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if id, isIdent := kv.Key.(*ast.Ident); isIdent && appListenerFieldV3(id.Name) != "" {
				fld := field(id.Name)
				fld.occs = append(fld.occs, appOccurrence{node: kv, elt: i})
			}
		}
	}
	switch {
	case src.node != nil:
		creations, escapes := src.variableUses()
		src.creations = append(src.creations, creations...)
		src.escapes = src.escapes || escapes
		src.collectVariableUses(field)
	case src.retFn != nil:
		src.callUses()
	}

	slices.SortFunc(src.fields, func(a, b *appSourceField) int { return appListenerFieldIndex(a.v2) - appListenerFieldIndex(b.v2) })
	for _, fld := range src.fields {
		src.decide(fld)
	}
}

func appListenerFieldIndex(v2 string) int {
	for i, f := range appListenerFieldNames {
		if f.v2 == v2 {
			return i
		}
	}
	return len(appListenerFieldNames)
}

// regions returns the code a variable of the source is visible in.
func (src *appListenSource) regions() []appRegion {
	if src.scope != nil {
		return []appRegion{{file: src.file, root: src.scope}}
	}
	pkg := src.file.ix.pkgOf(src.file)
	if pkg == nil {
		return nil
	}
	var out []appRegion
	for _, pf := range pkg.files {
		out = append(out, appRegion{file: pf, root: pf.g.file})
	}
	return out
}

// appRegion is a piece of code to search.
type appRegion struct {
	file *appFile
	root ast.Node
}

// isTheVariable reports whether id refers to the variable that holds the source.
func (src *appListenSource) isTheVariable(rf *appFile, id *ast.Ident) bool {
	if id.Name != src.name {
		return false
	}
	b, ok := rf.binding(id)
	return ok && b.node == src.node && b.file == src.file
}

// variableUses finds the fiber.New calls that take the variable which holds the
// source, and whether the variable is used in any other way than to read or
// assign one of its fields.
func (src *appListenSource) variableUses() (creations []appNew, escapes bool) {
	for _, r := range src.regions() {
		ast.Inspect(r.root, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || !src.isTheVariable(r.file, id) {
				return true
			}
			switch p := r.file.parent[id].(type) {
			case *ast.CallExpr:
				if r.file.isNewCall(p) && len(p.Args) == 1 && p.Args[0] == id {
					creations = append(creations, appNew{file: r.file, call: p})
					return true
				}
			case *ast.SelectorExpr:
				if p.X == id {
					return true
				}
			case *ast.ValueSpec:
				if slices.Contains(p.Names, id) {
					return true // the declaration of a package-level variable
				}
			}
			escapes = true
			return true
		})
	}
	return creations, escapes
}

// callUses finds the fiber.New calls that take the config a function returns:
// the calls of the function that are handed to fiber.New, directly or through a
// variable. Any other use of its result means the config escapes.
func (src *appListenSource) callUses() {
	if appReturnCount(src.retFn) != 1 {
		src.escapes = true
		return
	}

	name := src.retFn.Name.Name
	for _, pf := range src.file.ix.moduleFiles() {
		ast.Inspect(pf.g.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !appCallsName(call, name) {
				return true
			}
			if callee, found := pf.calleeOf(call, 0); !found || callee.decl != src.retFn {
				return true
			}

			switch p := pf.parent[call].(type) {
			case *ast.CallExpr:
				if pf.isNewCall(p) && len(p.Args) == 1 && p.Args[0] == call {
					src.creations = append(src.creations, appNew{file: pf, call: p})
					return true
				}
			case *ast.AssignStmt:
				if id, isDefine := appSingleDefine(p, call); isDefine {
					held := &appListenSource{file: pf, node: call, scope: pf.parent[p], name: id.Name}
					creations, escapes := held.variableUses()
					src.creations = append(src.creations, creations...)
					src.escapes = src.escapes || escapes
					return true
				}
			}
			src.escapes = true
			return true
		})
	}
}

// appCallsName reports whether call is a call of a function or method called
// name.
func appCallsName(call *ast.CallExpr, name string) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == name
	case *ast.SelectorExpr:
		return fun.Sel.Name == name
	}
	return false
}

// appReturnCount counts the return statements of a function, leaving out the
// function literals inside it.
func appReturnCount(fn *ast.FuncDecl) int {
	n := 0
	ast.Inspect(fn.Body, func(c ast.Node) bool {
		switch c.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			n++
		}
		return true
	})
	return n
}

// collectVariableUses finds what the code does with the variable that holds the
// source: the settings it assigns or reads.
func (src *appListenSource) collectVariableUses(field func(string) *appSourceField) {
	for _, r := range src.regions() {
		ast.Inspect(r.root, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, isIdent := sel.X.(*ast.Ident)
			if !isIdent || appListenerFieldV3(sel.Sel.Name) == "" || !src.isTheVariable(r.file, id) {
				return true
			}

			occ := appOccurrence{node: sel, elt: -1}
			if as, isAssign := r.file.parent[sel].(*ast.AssignStmt); isAssign && as.Tok == token.ASSIGN &&
				len(as.Lhs) == 1 && len(as.Rhs) == 1 && as.Lhs[0] == sel {
				occ.assign = as
			}
			fld := field(sel.Sel.Name)
			fld.occs = append(fld.occs, occ)
			return true
		})
	}
}

// decide works out whether a setting can move to the call that starts the app,
// and which value it has there.
func (src *appListenSource) decide(fld *appSourceField) {
	type written struct {
		value ast.Expr
		pos   token.Pos
	}
	var last written

	for _, occ := range fld.occs {
		if occ.elt >= 0 {
			kv, ok := occ.node.(*ast.KeyValueExpr)
			if !ok {
				fld.problem = "it is written in a way that cannot be moved"
				return
			}
			if kv.Pos() >= last.pos {
				last = written{pos: kv.Pos(), value: kv.Value}
			}
			continue
		}
		if occ.assign == nil {
			fld.problem = "it is read from the config"
			return
		}
		if why := src.assignProblem(occ.assign); why != "" {
			fld.problem = why
			return
		}
		if occ.assign.Pos() >= last.pos {
			last = written{pos: occ.assign.Pos(), value: occ.assign.Rhs[0]}
		}
	}
	fld.value = last.value
}

// assignProblem says why `cfg.Field = value` cannot move, "" when it can: it
// has to run unconditionally, before the app is created, and not depend on the
// config itself.
func (src *appListenSource) assignProblem(as *ast.AssignStmt) string {
	if src.scope == nil || src.file.parent[as] != src.scope {
		return "it is assigned conditionally or outside the function that creates the config"
	}
	for _, c := range src.creations {
		if c.file != src.file || as.End() > c.call.Pos() {
			return "it is assigned after fiber.New, which already copied the config"
		}
	}

	dependsOnConfig := false
	ast.Inspect(as.Rhs[0], func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && src.isTheVariable(src.file, id) {
			dependsOnConfig = true
		}
		return !dependsOnConfig
	})
	if dependsOnConfig {
		return "its value depends on the config"
	}
	return ""
}
