package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

// A function that returns a fiber.Config whose listener settings cannot move to the
// call that starts the app, because they depend on its arguments or on the branch it
// takes, returns the config of v2 instead. The Config of the registry has the four
// settings next to the embedded fiber.Config, so the code that builds the config
// reads as before, and fibermigrate.New creates the app of one the way NewApp does,
// recording the settings for the app that the Listen call asks for.
//
//	func ForServer(prefork bool) fibermigrate.Config {
//		return fibermigrate.Config{Config: fiber.Config{AppName: "x"}, Prefork: prefork}
//	}
//
// The result of such a function may only be handed to fiber.New, directly or through
// a variable, and have its fields read and assigned: anything else would need the
// type to be the one of v3.

// shimNewV2 turns the fiber.New call c into the call of the registry that takes the
// config of v2.
func shimNewV2(w *appShimWork, c appNew) {
	w.editsOf(c.file).replaceNode(c.file.g, c.call.Fun, w.qualifier(c.file)+appShimNewV2)
}

// returnedSource is the source that stands for all the configs the function of src
// returns, for the checks that look at the settings of the source.
func (pl *appPlanner) returnedSource(src *appListenSource) *appListenSource {
	if src.retFn == nil {
		return src
	}

	merged := *src
	merged.fields = nil
	for _, other := range pl.sources {
		if other.retFn == src.retFn {
			merged.fields = append(merged.fields, other.fields...)
		}
	}
	return &merged
}

// appReturnedLabel names a function that returns the config of the registry.
type appReturnedLabel struct {
	name string
	// exposed is set when other modules can call the function.
	exposed bool
}

// returnedLabels names the function the config of src is returned from, and tells
// whether code outside the module can call it.
func (pl *appPlanner) returnedLabels(mod goModule, src *appListenSource) appReturnedLabel {
	fn := src.retFn
	if fn == nil {
		return appReturnedLabel{}
	}

	name, exported := fn.Name.Name, appIsExported(fn.Name.Name)
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		recv := appBaseTypeName(fn.Recv.List[0].Type)
		name = recv + "." + name
		exported = exported && appIsExported(recv)
	}

	pkg := src.file.ix.pkgOf(src.file)
	if pkg == nil {
		return appReturnedLabel{name: name}
	}
	label := appReturnedLabel{name: pkg.name + "." + name}
	label.exposed = exported && slices.Contains(pl.publicPackages(mod), pkg)
	return label
}

// shimReturned plans the settings of the configs a function returns: its result, the
// literals and the variables it returns, and the fiber.New calls that make apps from
// them all take the config of the registry.
func shimReturned(w *appShimWork, src *appListenSource) string {
	f := src.file
	fn := src.retFn

	w.editsOf(f).replaceNode(f.g, fn.Type.Results.List[0].Type, w.qualifier(f)+appShimConfigType)

	retyped := map[ast.Node]bool{}
	var problem string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if problem == "" {
				problem = shimReturn(w, f, x, retyped)
			}
		}
		return problem == ""
	})
	if problem != "" {
		return problem
	}

	for _, c := range src.creations {
		shimNewV2(w, c)
	}
	return ""
}

// shimReturn plans the config that a return statement hands back.
func shimReturn(w *appShimWork, f *appFile, ret *ast.ReturnStmt, retyped map[ast.Node]bool) string {
	line := f.g.line(ret.Pos())
	if len(ret.Results) != 1 {
		return fmt.Sprintf("line %d returns more than a config", line)
	}

	switch r := ast.Unparen(ret.Results[0]).(type) {
	case *ast.CompositeLit:
		if r.Type == nil || f.fiberTypeName(r.Type) != appNameConfig {
			return fmt.Sprintf("line %d returns a literal that is not a fiber.Config", line)
		}
		return shimWrapLiteral(w, f, r)
	case *ast.Ident:
		return shimReturnedVariable(w, f, r, retyped)
	case *ast.CallExpr:
		if shimWrapCall(w, f, r) {
			return ""
		}
	}
	return fmt.Sprintf("line %d returns %s, which the migration cannot give the settings to", line, f.g.text(ret.Results[0]))
}

// shimWrapCall plans the call of a function that returns a fiber.Config, which the
// config of the registry holds as its embedded fiber.Config. It reports false for any
// other call.
func shimWrapCall(w *appShimWork, f *appFile, call *ast.CallExpr) bool {
	if _, _, isAppConfig := f.appConfigCall(call); isAppConfig {
		return false
	}
	callee, found := f.calleeOf(call, 0)
	if !found || callee.decl == nil || !callee.file.returnsConfig(callee.decl) {
		return false
	}

	g := f.g
	edits := w.editsOf(f)
	edits.replace(g.offset(call.Pos()), g.offset(call.Pos()), w.qualifier(f)+appShimConfigType+"{Config: ")
	edits.replace(g.offset(call.End()), g.offset(call.End()), "}")
	return true
}

// shimReturnedVariable plans the variable that a function returns: it holds the
// config of the registry, which has the fields the code reads and assigns.
func shimReturnedVariable(w *appShimWork, f *appFile, id *ast.Ident, retyped map[ast.Node]bool) string {
	line := f.g.line(id.Pos())
	b, ok := f.binding(id)
	if !ok || b.file != f || b.scope == nil || b.count != 1 || b.isParam() {
		return fmt.Sprintf("line %d returns %s, which is not a variable that the function declares", line, id.Name)
	}
	if retyped[b.node] {
		return ""
	}
	retyped[b.node] = true

	g := f.g
	qual := w.qualifier(f) + appShimConfigType
	switch {
	case b.init != nil:
		switch init := ast.Unparen(b.init).(type) {
		case *ast.CompositeLit:
			if init.Type == nil || f.fiberTypeName(init.Type) != appNameConfig {
				return fmt.Sprintf("%s is declared from %s, which the migration cannot give the settings to", id.Name, g.text(b.init))
			}
			if why := shimWrapLiteral(w, f, init); why != "" {
				return why
			}
		case *ast.CallExpr:
			if !shimWrapCall(w, f, init) {
				return fmt.Sprintf("%s is declared from %s, which the migration cannot give the settings to", id.Name, g.text(b.init))
			}
		default:
			return fmt.Sprintf("%s is declared from %s, which the migration cannot give the settings to", id.Name, g.text(b.init))
		}
		if b.typ != nil {
			w.editsOf(f).replaceNode(g, b.typ, qual)
		}
	case b.typ != nil && f.fiberTypeName(b.typ) == appNameConfig:
		w.editsOf(f).replaceNode(g, b.typ, qual)
	default:
		return id.Name + " is not declared as a fiber.Config or from a literal of one"
	}

	uses := (&appListenSource{file: f, node: b.node, scope: b.scope, name: id.Name}).variableUses()
	switch {
	case uses.escapes:
		return fmt.Sprintf("%s is used in a way the migration does not follow, at line %d", id.Name, uses.file.g.line(uses.at.Pos()))
	case len(uses.creations) > 0:
		return fmt.Sprintf("an app is made from %s in the function that returns it, at line %d", id.Name, uses.file.g.line(uses.creations[0].call.Pos()))
	}
	return ""
}

// shimWrapLiteral plans the fiber.Config literal that a function returns, or that
// initializes the variable it returns: the settings it holds become fields of the
// literal of the registry's Config, and the rest stays in the fiber.Config in it.
func shimWrapLiteral(w *appShimWork, f *appFile, lit *ast.CompositeLit) string {
	g := f.g

	var drop []int
	var held []string
	for i, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return fmt.Sprintf("the fiber.Config literal at line %d is not keyed", g.line(lit.Pos()))
		}
		if id, isIdent := kv.Key.(*ast.Ident); isIdent && appListenerFieldV3(id.Name) != "" {
			drop = append(drop, i)
			held = append(held, id.Name+": "+g.text(kv.Value))
		}
	}

	qual := w.qualifier(f) + appShimConfigType
	edits := w.editsOf(f)
	multi := g.line(lit.Lbrace) != g.line(lit.Rbrace)
	start, end := g.offset(lit.Pos()), g.offset(lit.End())

	switch {
	case len(lit.Elts) == 0:
		edits.replace(start, end, qual+"{}")
		return ""
	case len(drop) == len(lit.Elts) && appOnlyElements(g, lit):
		if multi {
			edits.replace(start, end, qual+"{\n"+strings.Join(held, ",\n")+",\n}")
		} else {
			edits.replace(start, end, qual+"{"+strings.Join(held, ", ")+"}")
		}
		return ""
	}

	if len(drop) > 0 && !appRemoveElements(g, edits, lit, drop, nil) {
		return appLiteralLayoutProblem
	}

	// v2 evaluated the fields in the order they are written. The settings go after
	// the rest of the config, or in front of it when that keeps the order of the
	// fields that can do something; when neither does, the order changes and the
	// note says so.
	listenerFirst := false
	if after := f.orderNote(lit, drop, false); after != "" {
		if f.orderNote(lit, drop, true) == "" {
			listenerFirst = true
		} else {
			w.note(f, g.line(lit.Pos()), after)
		}
	}

	nl, sep, closing := "", ", ", "}"
	if multi {
		nl, sep, closing = "\n", ",\n", ",\n}"
	}
	var prefix, suffix string
	switch {
	case len(held) == 0:
		prefix, suffix = qual+"{"+nl+"Config: ", closing
	case listenerFirst:
		prefix, suffix = qual+"{"+nl+strings.Join(held, sep)+sep+"Config: ", closing
	default:
		prefix, suffix = qual+"{"+nl+"Config: ", sep+strings.Join(held, sep)+closing
	}
	edits.replace(start, start, prefix)
	edits.replace(end, end, suffix)
	return ""
}

// orderNote tells, in words, which settings of the fiber.Config literal change the
// order they are evaluated in when the elements at the indexes in drop leave it for
// a place before (listenerFirst) or after the rest of the literal, and "" when the
// order of everything that can do something stays the same. In v2 the fields were
// evaluated in the order they are written.
func (f *appFile) orderNote(lit *ast.CompositeLit, drop []int, listenerFirst bool) string {
	var moved, others []string
	add := func(list []string, name string) []string {
		if slices.Contains(list, name) {
			return list
		}
		return append(list, name)
	}
	keyOf := func(i int) string {
		if kv, ok := lit.Elts[i].(*ast.KeyValueExpr); ok {
			return f.g.text(kv.Key)
		}
		return ""
	}
	impure := func(i int) bool {
		kv, ok := lit.Elts[i].(*ast.KeyValueExpr)
		return ok && f.impure(kv.Value)
	}

	for _, i := range drop {
		if !impure(i) {
			continue
		}
		for j := range lit.Elts {
			if slices.Contains(drop, j) || !impure(j) {
				continue
			}
			if (!listenerFirst && i < j) || (listenerFirst && j < i) {
				moved = add(moved, keyOf(i))
				others = add(others, keyOf(j))
			}
		}
	}
	if len(moved) == 0 {
		return ""
	}

	when := "after"
	if listenerFirst {
		when = "before"
	}
	return fmt.Sprintf("%s of this fiber.Config is now evaluated %s %s, where v2 evaluated the fields in the order they are written; "+
		"both call functions or receive from channels, so check that the order of their effects does not matter",
		strings.Join(moved, " and "), when, strings.Join(others, " and "))
}

// impure reports whether evaluating e can do more than compute a value: it calls a
// function that is not known to only read, or receives from a channel.
func (f *appFile) impure(e ast.Expr) bool {
	impure := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false // building a closure does nothing
		case *ast.UnaryExpr:
			impure = impure || x.Op == token.ARROW
		case *ast.CallExpr:
			impure = impure || !f.readOnlyCall(x)
		}
		return !impure
	})
	return impure
}

// appReadOnlyPackages are the packages whose functions only compute a value from
// their arguments.
var appReadOnlyPackages = []string{
	mw3StringsPkg, "strconv", "path", "path/filepath", "unicode", "unicode/utf8", "math", "bytes", "slices", "maps", "cmp",
}

// appReadOnlyFuncs are the functions of other packages that only read.
var appReadOnlyFuncs = map[string][]string{
	"os":      {"Getenv", "LookupEnv"},
	"runtime": {"NumCPU"},
	"time":    {"Duration", "ParseDuration"},
}

// readOnlyCall reports whether the call is a conversion, one of the builtin
// functions that only compute, or a call of a function known to only read.
func (f *appFile) readOnlyCall(call *ast.CallExpr) bool {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.ArrayType, *ast.MapType, *ast.InterfaceType, *ast.StructType, *ast.ChanType, *ast.FuncType:
		return true // a conversion
	case *ast.Ident:
		if _, bound := f.binding(fun); bound {
			return false
		}
		switch obj := types.Universe.Lookup(fun.Name).(type) {
		case *types.TypeName:
			return true // a conversion
		case *types.Builtin:
			return !slices.Contains([]string{mw3BuiltinAppend, "copy", "delete", "clear", "close", "panic", "print", "println", "recover"}, obj.Name())
		}
	case *ast.SelectorExpr:
		id, ok := fun.X.(*ast.Ident)
		if !ok {
			return false
		}
		if _, bound := f.binding(id); bound {
			return false
		}
		path := f.importPathOf(id.Name)
		if path == nameFmt {
			return strings.HasPrefix(fun.Sel.Name, "Sprint") // Sprint, Sprintf and Sprintln
		}
		return slices.Contains(appReadOnlyPackages, path) || slices.Contains(appReadOnlyFuncs[path], fun.Sel.Name)
	}
	return false
}
