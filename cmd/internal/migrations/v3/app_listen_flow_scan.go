package v3

import (
	"go/ast"
	"slices"
)

// scanVar follows the variable or parameter: the values it is given, and what the
// code does with it.
func (fl *appFlow) scanVar(b appBinding, class appFlowClass) {
	f := b.file
	if b.loop && (b.rangeX == nil || class != flowFunc) {
		fl.failf(f, b.node, "%s is held by %s, a variable of a loop", class.what(), b.name)
		return
	}

	if b.typ != nil {
		if !fl.retype(f, b.typ, class) {
			return
		}
		fl.claimSiblings(b, class)
		if fld, isField := f.parent[b.typ].(*ast.Field); isField && b.init == nil {
			fl.scanParam(b, fld, class)
			if fl.why != "" {
				return
			}
		}
	}
	if class == flowConfig {
		fl.noteVar(b)
	}

	if b.init != nil {
		if b.count > 1 {
			fl.produceResult(f, b.init, b.index, class)
		} else {
			fl.claimExpr(f, b.init, class, false)
		}
	}
	if b.loop && b.rangeX != nil && !b.rangeKey {
		fl.claimExpr(f, b.rangeX, flowFuncs, false) // the functions come from the slice it ranges over
	}

	scan := func(rf *appFile, root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != b.name {
				return fl.why == ""
			}
			if !rf.isOccurrenceName(id) {
				return true
			}
			if ob, bound := rf.binding(id); bound && ob.node == b.node && ob.file == b.file {
				fl.occurrence(rf, id, class)
			}
			return fl.why == ""
		})
	}
	if b.scope != nil {
		scan(f, b.scope)
		return
	}

	pkg := f.ix.pkgOf(f)
	if pkg == nil {
		fl.failf(f, b.node, "the package of %s could not be read", b.name)
		return
	}
	for _, pf := range pkg.files {
		scan(pf, pf.g.file)
	}
	if appIsExported(b.name) {
		// another package of the project names it pkg.Var
		for _, s := range fl.pl.sites().selectors[b.name] {
			sel, ok := s.node.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if ob, _, isVar := s.file.packageVar(sel); isVar && ob.node == b.node {
				fl.occurrence(s.file, sel, class)
			}
		}
	}
}

// isOccurrenceName reports whether the identifier can be a use of a variable: not
// the name of a field selected, of a field in a literal, or of a declared field.
func (f *appFile) isOccurrenceName(id *ast.Ident) bool {
	switch p := f.parent[id].(type) {
	case *ast.SelectorExpr:
		return p.Sel != id
	case *ast.KeyValueExpr:
		if p.Key == ast.Expr(id) {
			if lit, ok := f.parent[p].(*ast.CompositeLit); ok {
				return !f.isStructLiteral(lit)
			}
		}
	case *ast.Field, *ast.LabeledStmt, *ast.BranchStmt, *ast.ImportSpec, *ast.TypeSpec:
		return false
	}
	return true
}

// isStructLiteral reports whether the literal is of a struct type, whose keys are
// field names.
func (f *appFile) isStructLiteral(lit *ast.CompositeLit) bool {
	if lit.Type == nil {
		return false
	}
	st, _ := f.structOf(lit.Type, 0)
	return st != nil
}

// claimSiblings adds the variables that are declared with the same type
// expression, which cannot be changed for one of them alone.
func (fl *appFlow) claimSiblings(b appBinding, class appFlowClass) {
	f := b.file
	switch p := f.parent[b.typ].(type) {
	case *ast.Field:
		if len(p.Names) < 2 {
			return
		}
		for _, id := range p.Names {
			if id.Name != b.name {
				fl.claimVar(appBinding{file: f, typ: p.Type, name: id.Name, count: 1, node: p.Type, scope: b.scope}, class)
			}
		}
	case *ast.ValueSpec:
		if len(p.Names) < 2 {
			return
		}
		for i, id := range p.Names {
			if id.Name == b.name {
				continue
			}
			d := decl{typ: p.Type}
			switch {
			case len(p.Values) == len(p.Names):
				d.init = p.Values[i]
			case len(p.Values) == 1:
				d.init = p.Values[0]
			}
			fl.claimVar(f.bindingOf(d, id.Name), class)
		}
	}
}

// scanParam follows a parameter to the arguments of the calls of its function.
func (fl *appFlow) scanParam(b appBinding, fld *ast.Field, class appFlowClass) {
	f := b.file
	owner := f.enclosingFuncOfField(fld)
	list := f.fieldList(fld)

	if lit, isLit := owner.(*ast.FuncLit); isLit {
		if class != flowConfig || list != lit.Type.Params {
			fl.failf(f, fld, "%s is a parameter or result of a function literal", class.what())
			return
		}
		fl.funcLit(f, lit)
		fl.use(f, lit, flowFunc)
		return
	}

	fn, isDecl := owner.(*ast.FuncDecl)
	switch {
	case !isDecl || list == nil:
		fl.failf(f, fld, "%s is held in a way the migration does not follow", class.what())
		return
	case list == fn.Recv:
		if class != flowFunc {
			fl.failf(f, fld, "%s is the receiver of a method", class.what())
		}
		return // the functions of a type that has methods are applied through the receiver
	case list != fn.Type.Params:
		fl.failf(f, fld, "%s is a named result of %s", class.what(), f.funcLabel(fn))
		return
	}

	idx := 0
	for _, pf := range list.List {
		if pf == fld {
			for _, id := range pf.Names {
				if id.Name == b.name {
					break
				}
				idx++
			}
			break
		}
		idx += max(len(pf.Names), 1)
	}

	if class == flowConfig {
		fl.noteFunc(f, fn, changeTake)
	}
	calls, refs := fl.callersOf(f, fn)
	for _, c := range calls {
		fl.produceArg(c.file, c.call, idx, class)
	}
	for _, r := range refs {
		// the function is a value: it is one of the functions that take the config
		if class != flowConfig {
			fl.failf(r.file, r.expr, "%s is used as a value", f.funcLabel(fn))
			return
		}
		fl.funcDecl(f, fn)
		fl.use(r.file, r.expr, flowFunc)
	}
}

// produceArg follows the argument idx of the call into the parameter.
func (fl *appFlow) produceArg(f *appFile, call *ast.CallExpr, idx int, class appFlowClass) {
	switch {
	case class == flowFuncs:
		// every argument from the variadic parameter on is one of the functions, or the
		// slice of them that is spread
		if call.Ellipsis.IsValid() {
			if idx < len(call.Args) {
				fl.claimExpr(f, call.Args[len(call.Args)-1], flowFuncs, false)
			}
			return
		}
		for _, arg := range call.Args[min(idx, len(call.Args)):] {
			fl.claimExpr(f, arg, flowFunc, false)
		}
	case call.Ellipsis.IsValid():
		fl.failf(f, call, "the arguments of the call are spread from a slice")
	case idx < len(call.Args):
		fl.claimExpr(f, call.Args[idx], class, false)
	case len(call.Args) == 1 && class == flowConfig:
		fl.produceResult(f, call.Args[0], idx, class)
	default:
		fl.failf(f, call, "the call has no argument for the parameter")
	}
}

// scanResult follows a result of a function: the values it returns, and what its
// callers do with it.
func (fl *appFlow) scanResult(f *appFile, fn *ast.FuncDecl, idx int, class appFlowClass) {
	t := appResultType(fn.Type, idx)
	switch {
	case t == nil || fn.Body == nil:
		fl.failf(f, fn, "%s has no result the migration can follow", f.funcLabel(fn))
		return
	case slices.ContainsFunc(fn.Type.Results.List, func(fld *ast.Field) bool { return len(fld.Names) > 0 }):
		fl.failf(f, fn, "%s has named results", f.funcLabel(fn))
		return
	case !fl.retype(f, t, class):
		return
	}
	if class == flowConfig {
		fl.noteFunc(f, fn, changeReturn)
	}

	want := appResultCount(fn.Type)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch r := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			switch {
			case len(r.Results) == want && idx < len(r.Results):
				fl.claimExpr(f, r.Results[idx], class, false)
			case len(r.Results) == 1 && want > 1:
				fl.produceResult(f, r.Results[0], idx, class)
			default:
				fl.failf(f, r, "%s returns without naming what it returns", f.funcLabel(fn))
			}
		}
		return fl.why == ""
	})

	calls, refs := fl.callersOf(f, fn)
	if len(refs) > 0 {
		fl.failf(refs[0].file, refs[0].expr, "%s is used as a value", f.funcLabel(fn))
		return
	}
	for _, c := range calls {
		fl.useResult(c.file, c.call, idx, want, class)
	}
}

// scanField follows a field of a struct type: the values it is given, and what the
// code does with it.
func (fl *appFlow) scanField(td *appTypeDecl, name string, class appFlowClass) {
	var fld *ast.Field
	st, isStruct := td.spec.Type.(*ast.StructType)
	if isStruct {
		fld = appStructField(st, name)
	}
	if fld == nil {
		fl.failf(td.file, td.spec, "the field %s could not be found", name)
		return
	}
	if len(fld.Names) == 0 {
		fl.failf(td.file, fld, "%s is embedded in %s", name, td.spec.Name.Name)
		return
	}
	if !fl.retype(td.file, fld.Type, class) {
		return
	}
	for _, id := range fld.Names {
		if id.Name != name {
			fl.claimField(td, id.Name, class) // declared together, with one type
		}
	}
	if class == flowConfig {
		fl.noteField(td, name)
	}

	sites := fl.pl.sites()
	for _, s := range sites.literals[td.spec.Name.Name] {
		lit, ok := s.node.(*ast.CompositeLit)
		if !ok || s.file.resolveNamedType(lit.Type) != td {
			continue
		}
		fl.fieldInLiteral(s.file, lit, st, name, class)
	}

	for _, s := range sites.selectors[name] {
		sel, ok := s.node.(*ast.SelectorExpr)
		if !ok || fl.why != "" {
			continue
		}
		if id, isIdent := sel.X.(*ast.Ident); isIdent && s.file.isImportName(id) {
			continue // pkg.Name, not a field
		}
		if !fl.selects(s.file, sel, td, name) {
			continue
		}
		fl.occurrence(s.file, sel, class)
	}
}

// fieldInLiteral follows what a struct literal gives to the field.
func (fl *appFlow) fieldInLiteral(f *appFile, lit *ast.CompositeLit, st *ast.StructType, name string, class appFlowClass) {
	keyed := false
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		keyed = true
		if isIdentNamed(kv.Key, name) {
			fl.claimExpr(f, kv.Value, class, false)
		}
	}
	if keyed || len(lit.Elts) == 0 {
		return
	}

	n := 0
	for _, fld := range st.Fields.List {
		names := fld.Names
		if len(names) == 0 {
			names = []*ast.Ident{{Name: appBaseTypeName(appSelectorTail(fld.Type))}}
		}
		for _, id := range names {
			if id.Name == name && n < len(lit.Elts) {
				fl.claimExpr(f, lit.Elts[n], class, false)
			}
			n++
		}
	}
}

// selects reports whether the selector x.name selects the field name of the struct
// type td. It fails the set when that cannot be told for a name the type shares
// with types of other packages.
func (fl *appFlow) selects(f *appFile, sel *ast.SelectorExpr, td *appTypeDecl, name string) bool {
	if t, tf := f.valueType(sel.X, 0); t != nil {
		rd := tf.resolveNamedType(t)
		return rd != nil && structHolding(rd, name, 0) == td
	}

	// the receiver is of a type that cannot be told: an unexported field can only be
	// selected in the package that declares it
	pkg := td.file.ix.pkgOf(td.file)
	if !appIsExported(name) {
		return f.ix.pkgOf(f) == pkg && pkg != nil && pkg.fieldOwners(name) == 1
	}
	fl.failf(f, sel, "the type of %s could not be told, and the field %s is also the name of a field it may have",
		f.g.text(sel.X), name)
	return false
}

// fieldHolder returns the declaration of the struct type that has the field name
// of the value x: its type, or the type embedded in it that the field is promoted
// from.
func (f *appFile) fieldHolder(x ast.Expr, name string) *appTypeDecl {
	t, tf := f.valueType(x, 0)
	if t == nil {
		return nil
	}
	td := tf.resolveNamedType(t)
	if td == nil {
		return nil
	}
	return structHolding(td, name, 0)
}

// structHolding returns the declaration of the struct type that declares the field
// name for td: td itself, or the embedded type it is promoted from.
func structHolding(td *appTypeDecl, name string, depth int) *appTypeDecl {
	if depth > appMaxDepth {
		return nil
	}
	if td.spec.Assign.IsValid() {
		if inner := td.file.resolveNamedType(td.spec.Type); inner != nil {
			return structHolding(inner, name, depth+1)
		}
		return nil
	}
	st, ok := td.spec.Type.(*ast.StructType)
	if !ok {
		return nil
	}
	if appStructField(st, name) != nil {
		return td
	}
	for _, fld := range st.Fields.List {
		if len(fld.Names) > 0 {
			continue
		}
		if emb := td.file.resolveNamedType(fld.Type); emb != nil {
			if holder := structHolding(emb, name, depth+1); holder != nil {
				return holder
			}
		}
	}
	return nil
}

// funcLabel names a function declaration for a note: server.New, config.Config.Get.
func (f *appFile) funcLabel(fn *ast.FuncDecl) string {
	name := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		name = appBaseTypeName(fn.Recv.List[0].Type) + "." + name
	}
	if pkg := f.ix.pkgOf(f); pkg != nil {
		return pkg.name + "." + name
	}
	return name
}

// noteFunc records that the function takes or returns the config.
func (fl *appFlow) noteFunc(f *appFile, fn *ast.FuncDecl, how string) {
	exported := appIsExported(fn.Name.Name)
	if fn.Recv != nil {
		exported = exported && appIsExported(appReceiverTypeName(fn.Recv))
	}
	fl.addChange(f, appFlowChange{label: f.funcLabel(fn), how: how}, exported)
}

// noteField records that the field holds the config.
func (fl *appFlow) noteField(td *appTypeDecl, name string) {
	label := td.file.ix.pkgLabel(td.file) + td.spec.Name.Name + "." + name
	fl.addChange(td.file, appFlowChange{label: label, how: changeHold}, appIsExported(td.spec.Name.Name) && appIsExported(name))
}

// noteVar records that the variable of the package holds the config.
func (fl *appFlow) noteVar(b appBinding) {
	if b.scope != nil {
		return
	}
	fl.addChange(b.file, appFlowChange{label: b.file.ix.pkgLabel(b.file) + b.name, how: changeHold}, appIsExported(b.name))
}

// pkgLabel is the prefix of a name of the package of f in a note.
func (ix *appIndex) pkgLabel(f *appFile) string {
	if pkg := ix.pkgOf(f); pkg != nil {
		return pkg.name + "."
	}
	return ""
}

func (fl *appFlow) addChange(f *appFile, c appFlowChange, exported bool) {
	if mod, ok := fl.pl.moduleOf(f); ok && exported {
		pkg := f.ix.pkgOf(f)
		c.exposed = pkg != nil && slices.Contains(fl.pl.publicPackages(mod), pkg)
	}
	if !slices.ContainsFunc(fl.changed, func(o appFlowChange) bool { return o.label == c.label && o.how == c.how }) {
		fl.changed = append(fl.changed, c)
	}
}

// fieldList returns the list of parameters, results or receivers the field is in.
func (f *appFile) fieldList(fld *ast.Field) *ast.FieldList {
	list, ok := f.parent[fld].(*ast.FieldList)
	if !ok {
		return nil
	}
	return list
}
