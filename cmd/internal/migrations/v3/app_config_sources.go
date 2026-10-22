package v3

import (
	"fmt"
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
	// retFn is the function the config is returned from, nil otherwise: the literal
	// of a return statement, or a local variable that the function returns.
	retFn *ast.FuncDecl
	name  string
	// escapeWhy says where the config is used in a way that cannot be followed, for
	// the configs a function returns.
	escapeWhy string
	fields    []*appSourceField
	creations []appNew
	// sels are the reads and assignments of fields that code makes on the config
	// where the function hands it to a variable or a selector.
	sels []*ast.SelectorExpr
	// escapes is set when the config is also used in a way that cannot be
	// followed, so that settings moved to the apps found would be lost for
	// the others.
	escapes bool
	// branches is set when the function returns more than one config, so that its
	// callers get one of several.
	branches bool
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
	// file is the file the node is in: a package-level config is also set and
	// read from the other files of its package.
	file *appFile
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
			if len(x.Values) == len(x.Names) {
				for i, value := range x.Values {
					if src := f.sourceOfConfigCall(value, x.Names[i].Name, f.specScope(x)); src != nil {
						out = append(out, src)
					}
				}
			}
		case *ast.AssignStmt:
			if len(x.Rhs) == 1 {
				if id, isDefine := appSingleDefine(x, x.Rhs[0]); isDefine {
					if src := f.sourceOfConfigCall(x.Rhs[0], id.Name, f.parent[x]); src != nil {
						out = append(out, src)
					}
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

// sourceOfConfigCall returns the source for the variable name that is declared with
// the result of a call of a function that returns a fiber.Config: the code adds the
// settings to the config the function built, which does not set any of them itself.
func (f *appFile) sourceOfConfigCall(rhs ast.Expr, name string, scope ast.Node) *appListenSource {
	call, ok := rhs.(*ast.CallExpr)
	if !ok || name == "_" || f.isNewCall(call) {
		return nil
	}
	if _, _, isAppConfig := f.appConfigCall(call); isAppConfig {
		return nil // the config of an app is read, which has its own handling
	}
	callee, found := f.calleeOf(call, 0)
	if !found || callee.decl == nil || !callee.file.returnsConfig(callee.decl) || appSetsListenerFields(callee.decl) {
		return nil
	}
	return &appListenSource{file: f, node: call, scope: scope, name: name}
}

// appSetsListenerFields reports whether the body of the function names one of the
// settings that v3 moved: it builds a config that is a source of its own.
func appSetsListenerFields(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok && appListenerFieldV3(id.Name) != "" {
				found = true
			}
		case *ast.SelectorExpr:
			if appListenerFieldV3(x.Sel.Name) != "" {
				found = true
			}
		}
		return !found
	})
	return found
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
		if fn, ok := f.enclosingFuncOf(p).(*ast.FuncDecl); ok && len(p.Results) == 1 && f.returnsConfig(fn) {
			src.retFn = fn
		}
	}
	if len(src.creations) == 0 && src.node == nil && src.retFn == nil {
		src.escape(f, lit, "the config is "+f.useOf(lit))
	}
	return src
}

// returnsConfig reports whether the function has one result, unnamed, of type
// fiber.Config.
func (f *appFile) returnsConfig(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 || len(fn.Type.Results.List[0].Names) != 0 {
		return false
	}
	t := fn.Type.Results.List[0].Type
	if _, isPointer := t.(*ast.StarExpr); isPointer {
		return false
	}
	return f.fiberTypeName(t) == appNameConfig
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
				fld.occs = append(fld.occs, appOccurrence{node: kv, file: src.file, elt: i})
			}
		}
	}
	switch {
	case src.node != nil:
		uses := src.variableUses()
		src.creations = append(src.creations, uses.creations...)
		if uses.escapes {
			src.escape(uses.file, uses.at, "the config is "+uses.file.useOf(uses.at))
		}
		src.returnedVariable(uses)
		src.collectVariableUses(field)
		if src.retFn != nil {
			src.callUses()
		}
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

// appVarUses is what the code does with the variable that holds a config.
type appVarUses struct {
	// at is the first use that cannot be followed, and file the file it is in.
	at   *ast.Ident
	file *appFile
	// creations are the fiber.New calls that take the variable.
	creations []appNew
	// returns are the statements that return the variable.
	returns []*ast.ReturnStmt
	// sels are the reads and assignments of its fields.
	sels []*ast.SelectorExpr
	// escapes is set when the variable is used in any other way.
	escapes bool
}

// variableUses finds the fiber.New calls that take the variable which holds the
// source, the statements that return it, and whether the variable is used in any
// other way than to read or assign one of its fields.
func (src *appListenSource) variableUses() appVarUses {
	var uses appVarUses
	for _, r := range src.regions() {
		ast.Inspect(r.root, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || !src.isTheVariable(r.file, id) {
				return true
			}
			switch p := r.file.parent[id].(type) {
			case *ast.CallExpr:
				if r.file.isNewCall(p) && len(p.Args) == 1 && p.Args[0] == id {
					uses.creations = append(uses.creations, appNew{file: r.file, call: p})
					return true
				}
			case *ast.SelectorExpr:
				if p.X == id {
					uses.sels = append(uses.sels, p)
					return true
				}
			case *ast.ValueSpec:
				if slices.Contains(p.Names, id) {
					return true // the declaration of a package-level variable
				}
			case *ast.ReturnStmt:
				if len(p.Results) == 1 {
					uses.returns = append(uses.returns, p)
					return true
				}
			}
			if !uses.escapes {
				uses.at, uses.file = id, r.file
			}
			uses.escapes = true
			return true
		})
	}
	return uses
}

// returnedVariable works out what it means that the code returns the variable that
// holds the source: when a function returns it, the source is the config of that
// function, and the apps are made from what the callers of the function get.
func (src *appListenSource) returnedVariable(uses appVarUses) {
	if len(uses.returns) == 0 {
		return
	}
	var fn *ast.FuncDecl
	for _, ret := range uses.returns {
		decl, ok := src.file.enclosingFuncOf(ret).(*ast.FuncDecl)
		if !ok || !src.file.returnsConfig(decl) || (fn != nil && decl != fn) {
			src.escape(src.file, ret, "the config is returned from a function that cannot be followed")
			return
		}
		fn = decl
	}
	switch {
	case src.scope == nil:
		src.escape(src.file, uses.returns[0], "the config is a variable of the package, which the functions that return it share")
	case len(uses.creations) > 0:
		src.escape(src.file, uses.creations[0].call, "an app is created from the config in the function that returns it")
	case uses.escapes:
		src.escape(uses.file, uses.at, "the config is used in a way the migration does not follow")
	default:
		src.retFn = fn
	}
}

// escape records that the config is used in a way that cannot be followed, and
// where, for the first such use.
func (src *appListenSource) escape(f *appFile, n ast.Node, why string) {
	src.escapes = true
	if src.escapeWhy == "" {
		src.escapeWhy = fmt.Sprintf("%s, at line %d of %s", why, f.g.line(n.Pos()), appFileLabel(f))
	}
}

// callUses finds the fiber.New calls that take the config a function returns:
// the calls of the function that are handed to fiber.New, directly or through a
// variable. Any other use of its result, and any reference to the function that is
// not a call, means the config escapes.
func (src *appListenSource) callUses() {
	fn := src.retFn
	src.branches = appReturnCount(fn) != 1

	for _, pf := range src.file.ix.moduleFiles() {
		ast.Inspect(pf.g.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if appCallsName(x, fn.Name.Name) {
					src.followCall(pf, x)
				}
			case *ast.Ident, *ast.SelectorExpr:
				if pf.referencesFunc(x, fn) {
					src.escape(pf, x, fn.Name.Name+" is used as a value")
				}
			}
			return true
		})
	}
}

// followCall classifies a call of a function or method of the name of the one the
// config is returned from.
func (src *appListenSource) followCall(pf *appFile, call *ast.CallExpr) {
	fn := src.retFn
	callee, found := pf.calleeOf(call, 0)
	switch {
	case found && callee.decl != fn:
		return // another function of the same name
	case !found:
		// a method of another type, or of a type the project does not declare, is
		// not it; a receiver that cannot be traced may be
		if fn.Recv != nil && !pf.foreignReceiver(call) && pf.feedsNew(call) {
			src.escape(pf, call, "a call of "+fn.Name.Name+" whose receiver could not be traced may hand its config to fiber.New")
		}
		return
	}

	switch p := pf.parent[call].(type) {
	case *ast.CallExpr:
		if pf.isNewCall(p) && len(p.Args) == 1 && p.Args[0] == call {
			src.creations = append(src.creations, appNew{file: pf, call: p})
			return
		}
	case *ast.AssignStmt:
		if id, isDefine := appSingleDefine(p, call); isDefine {
			held := &appListenSource{file: pf, node: call, scope: pf.parent[p], name: id.Name}
			uses := held.variableUses()
			src.creations = append(src.creations, uses.creations...)
			src.sels = append(src.sels, uses.sels...)
			switch {
			case uses.escapes:
				src.escape(uses.file, uses.at, "the result of "+fn.Name.Name+" is held in a variable that is used in a way the migration does not follow")
			case len(uses.returns) > 0:
				src.escape(pf, uses.returns[0], "the result of "+fn.Name.Name+" is returned on")
			}
			return
		}
	case *ast.SelectorExpr:
		if p.X == ast.Expr(call) {
			src.sels = append(src.sels, p)
			return
		}
	}
	src.escape(pf, call, "the result of "+fn.Name.Name+" is "+pf.useOf(call))
}

// useOf says, in words that follow "is", what the code does with the value of the
// expression e, from the node it is the child of.
func (f *appFile) useOf(e ast.Expr) string {
	switch p := f.parent[e].(type) {
	case *ast.ReturnStmt:
		return "returned by another function"
	case *ast.CallExpr:
		if p.Fun != e {
			return "passed to " + f.g.text(p.Fun)
		}
	case *ast.KeyValueExpr:
		if p.Value == e {
			return "stored in the field " + f.g.text(p.Key) + " of a struct, slice or map"
		}
	case *ast.CompositeLit:
		return "stored in a struct, slice or map"
	case *ast.AssignStmt:
		if len(p.Lhs) == 1 && len(p.Rhs) == 1 && p.Rhs[0] == e {
			return "assigned to " + f.g.text(p.Lhs[0])
		}
		return "assigned to a variable or field the migration does not follow"
	case *ast.ValueSpec:
		return "assigned to a variable or field the migration does not follow"
	case *ast.SendStmt:
		return "sent on a channel"
	}
	return "used in a way the migration does not follow"
}

// feedsNew reports whether the result of the call is handed to fiber.New, or held
// by a variable declared with it: the shapes of the call of a function that returns
// a config.
func (f *appFile) feedsNew(call *ast.CallExpr) bool {
	switch p := f.parent[call].(type) {
	case *ast.CallExpr:
		return f.isNewCall(p)
	case *ast.AssignStmt:
		_, isDefine := appSingleDefine(p, call)
		return isDefine
	}
	return false
}

// foreignReceiver reports whether the receiver of a method call is known to be a
// value of a type that the project does not declare, so that the method is not one
// of the project's.
func (f *appFile) foreignReceiver(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if id, isIdent := sel.X.(*ast.Ident); isIdent && f.isImportName(id) {
		return true // a name of another package is not a method
	}
	t, tf := f.valueType(sel.X, 0)
	if t == nil {
		return false
	}
	switch x := appStripType(t).(type) {
	case *ast.SelectorExpr:
		id, isIdent := x.X.(*ast.Ident)
		return isIdent && tf.packageNamed(id.Name) == nil
	case *ast.Ident:
		return tf.resolveNamedType(x) == nil
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType:
		return true
	}
	return false
}

// isImportName reports whether id names an import of the file rather than a
// variable.
func (f *appFile) isImportName(id *ast.Ident) bool {
	if _, bound := f.binding(id); bound {
		return false
	}
	return f.importPathOf(id.Name) != ""
}

// referencesFunc reports whether the identifier or selector n names the function
// fn without calling it, which hands the function to code that cannot be followed.
func (f *appFile) referencesFunc(n ast.Node, fn *ast.FuncDecl) bool {
	var name string
	switch x := n.(type) {
	case *ast.Ident:
		name = x.Name
	case *ast.SelectorExpr:
		name = x.Sel.Name
	}
	if name != fn.Name.Name {
		return false
	}

	switch p := f.parent[n].(type) {
	case *ast.CallExpr:
		if p.Fun == n {
			return false // a call
		}
	case *ast.FuncDecl:
		return false // the declaration
	case *ast.SelectorExpr:
		if p.Sel == n {
			return false // handled as the selector
		}
	case *ast.KeyValueExpr:
		if p.Key == n {
			return false // the name of a field
		}
	case *ast.Field, *ast.ValueSpec, *ast.TypeSpec, *ast.ImportSpec:
		return false
	}

	switch x := n.(type) {
	case *ast.Ident:
		if _, bound := f.binding(x); bound {
			return false
		}
		pkg := f.ix.pkgOf(f)
		return fn.Recv == nil && pkg != nil && pkg.funcs[name] != nil && pkg.funcs[name].decl == fn
	case *ast.SelectorExpr:
		call := &ast.CallExpr{Fun: x}
		if callee, found := f.calleeOf(call, 0); found {
			return callee.decl == fn
		}
		if t, tf := f.valueType(x.X, 0); t != nil {
			if st, _ := tf.structOf(t, 0); st != nil && appStructField(st, name) != nil {
				return false // a field of the name, not the method
			}
		}
		return fn.Recv != nil && !f.foreignReceiver(call)
	}
	return false
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

			occ := appOccurrence{node: sel, file: r.file, elt: -1}
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

// appProblemConditional is why an assignment to a field of the config cannot
// move to the call that starts the app. A source whose only problem is this one
// can keep its settings in a fiber.ListenConfig variable (see variableProblem).
const appProblemConditional = "it is assigned conditionally or outside the function that creates the config"

// assignProblem says why `cfg.Field = value` cannot move, "" when it can: it
// has to run unconditionally, before the app is created, and not depend on the
// config itself.
func (src *appListenSource) assignProblem(as *ast.AssignStmt) string {
	if src.retFn != nil {
		return "" // the apps are made by the callers of the function, which see no assignment
	}
	if src.scope == nil || src.file.parent[as] != src.scope {
		return appProblemConditional
	}
	for _, c := range src.creations {
		if c.file != src.file || as.End() > c.call.Pos() {
			return "it is assigned after fiber.New, which already copied the config"
		}
	}
	if src.dependsOnConfig(as) {
		return "its value depends on the config"
	}
	return ""
}

// dependsOnConfig reports whether the value assigned by as reads the variable
// that holds the config.
func (src *appListenSource) dependsOnConfig(as *ast.AssignStmt) bool {
	found := false
	ast.Inspect(as.Rhs[0], func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && src.isTheVariable(src.file, id) {
			found = true
		}
		return !found
	})
	return found
}

// wantsVariable reports whether the settings of the source can only move if they
// are kept in a fiber.ListenConfig variable: a local variable holds the config
// and the one thing in the way is an assignment that runs conditionally.
func (src *appListenSource) wantsVariable() bool {
	conditional := false
	for _, fld := range src.fields {
		switch fld.problem {
		case "":
		case appProblemConditional:
			conditional = true
		default:
			return false
		}
	}
	return conditional && src.node != nil && src.scope != nil && src.retFn == nil
}

// declStatement returns the statement that declares the variable holding the
// config, nil when another statement cannot follow it.
func (src *appListenSource) declStatement() ast.Stmt {
	// src.node is the literal, or the type of `var cfg fiber.Config`
	f := src.file
	for p := f.parent[src.node]; p != nil; p = f.parent[p] {
		if stmt, ok := p.(ast.Stmt); ok {
			if f.inStatementList(stmt) {
				return stmt
			}
			return nil
		}
	}
	return nil
}

// variableProblem says why the settings of the source cannot live in a
// fiber.ListenConfig variable declared right after the config, "" when they can.
// Every app made from the config and every call that starts one must sit in the
// code the variable is visible in, and every assignment to a setting must be
// plain code of the function that declares the config, run before the app is
// created, so that the variable has the value the config had.
func (src *appListenSource) variableProblem(decl ast.Stmt, linked []*appStartInfo) string {
	f := src.file
	inScope := func(file *appFile, n ast.Node) bool {
		return file == f && n.Pos() >= decl.End() && n.End() <= src.scope.End()
	}
	for _, c := range src.creations {
		if !inScope(c.file, c.call) {
			return "the app is created outside the code that sees the config variable"
		}
	}
	for _, s := range linked {
		if !inScope(s.file, s.call) {
			return "the call that starts the app is outside the code that sees the config variable"
		}
	}

	fn := f.enclosingFuncOf(decl)
	for _, fld := range src.fields {
		for _, occ := range fld.occs {
			if occ.elt >= 0 {
				continue
			}
			as := occ.assign
			switch {
			case as == nil:
				return "it is read from the config"
			case f.enclosingFuncOf(as) != fn:
				return "it is assigned inside a function literal"
			case src.dependsOnConfig(as):
				return "its value depends on the config"
			}
			for _, c := range src.creations {
				if as.End() > c.call.Pos() {
					return "it is assigned after fiber.New, which already copied the config"
				}
			}
		}
	}
	return ""
}
