package v3

import (
	"fmt"
	"go/ast"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	_ "embed" // the shim is embedded as text

	"github.com/gofiber/cli/cmd/internal"
)

// The registry shim carries the listener settings of a fiber.Config that cannot
// move to the call that starts the app, because the code that sets them is not
// the call, nor in its reach: a condition, a value that a function only knows, an
// app that another function starts. The settings travel with the app instead.
//
// In the migrated code a fiber.ListenConfig variable sits next to the fiber.Config
// variable and takes over its settings, fiber.New(cfg) becomes
// fiberMigrateNew(cfg, cfgListen), which records the settings for the app, and the
// calls that can start such an app ask for them:
//
//	app.Listen(addr, fiberMigrateListenConfig(app))
//
// The source of the shim is a package of its own, listenshim, which is built and
// tested like any other.

//go:embed listenshim/fiber_migrate_listen.go
var appShimEmbedded string

// The calls that start an app through an expression, such as newApp().Listen(addr),
// go through two functions that take the app as an argument. They are a file of
// their own, which only the packages that have such a call get.
//
//go:embed listenshim/fiber_migrate_listen_calls.go
var appShimCallsEmbedded string

const (
	appShimFile      = "fiber_migrate_listen.go"
	appShimCallsFile = "fiber_migrate_listen_calls.go"
	appShimMarker    = `This file was added by "fiber migrate"`
	appShimPackage   = "package listenshim"
	appShimFiberImp  = `"github.com/gofiber/fiber/v3"`

	// appMainPackage is the name of a package that nothing imports.
	appMainPackage = "main"

	appShimNew      = "fiberMigrateNew"
	appShimConfig   = "fiberMigrateListenConfig"
	appShimListen   = "fiberMigrateListen"
	appShimListener = "fiberMigrateListener"
)

// appShimGenerated describes the shim file as the packages of a project that
// imports fiber under fiberPath get it.
func appShimGenerated(fiberPath string) generatedFile {
	return appShimFileOf(appShimFile, appShimEmbedded, fiberPath)
}

// appShimCallsGenerated describes the file of the functions that start an app
// through an expression.
func appShimCallsGenerated(fiberPath string) generatedFile {
	return appShimFileOf(appShimCallsFile, appShimCallsEmbedded, fiberPath)
}

func appShimFileOf(name, embedded, fiberPath string) generatedFile {
	src := strings.Replace(embedded, appShimPackage, "package "+generatedPkgPlaceholder, 1)
	src = strings.Replace(src, appShimFiberImp, `"`+fiberPath+`"`, 1)

	var idents []string
	if g, ok := parseSource(embedded); ok {
		for _, d := range g.file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					idents = append(idents, d.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						idents = append(idents, spec.Name.Name)
					case *ast.ValueSpec:
						for _, id := range spec.Names {
							idents = append(idents, id.Name)
						}
					}
				}
			}
		}
	}
	return generatedFile{name: name, marker: appShimMarker, source: src, idents: idents}
}

// isShimFile reports whether the file is one of the shim, which the migration
// never reads as the project's own code.
func isShimFile(path, content string) bool {
	return (strings.HasSuffix(path, appShimFile) || strings.HasSuffix(path, appShimCallsFile)) && strings.Contains(content, appShimMarker)
}

// appShimNote is the note that tells about the shim file, once for every package
// that gets one.
const appShimNote = "Prefork, Network, DisableStartupMessage and EnablePrintRoutes moved from fiber.Config to the fiber.ListenConfig that Listen takes in v3. " +
	"Some apps of this package get them away from the call that starts the app, so " + appShimFile + " was added to carry them: " +
	appShimNew + " records them for the app and " + appShimConfig + " hands them to the Listen or Listener call that starts it, in this package only " +
	"(an app handed to another package starts without them). Pass a fiber.ListenConfig to Listen yourself when you can, and delete the file once nothing uses it"

// appShimPkg is what the shim of one package has to do.
type appShimPkg struct {
	pkg *appPackage
	// anchor is a file of the package, which says where the shim goes.
	anchor *appFile
	// first is the first app the package makes with the shim, where its note goes.
	first *appNew
	seen  map[*ast.CallExpr]bool
	// starts are the calls that ask the registry for the settings.
	starts []*appStartInfo
	// checked and available say whether the shim file exists, or could be added,
	// once that was asked; the same for the file of the functions that start an
	// app through an expression.
	checked        bool
	available      bool
	callsChecked   bool
	callsAvailable bool
}

func (pl *appPlanner) shimOf(pkg *appPackage, anchor *appFile) *appShimPkg {
	if sp := pl.shims[pkg]; sp != nil {
		return sp
	}
	sp := &appShimPkg{pkg: pkg, anchor: anchor, seen: map[*ast.CallExpr]bool{}}
	pl.shims[pkg] = sp
	return sp
}

// ensure makes sure the package has the shim file, adding it on first use.
func (sp *appShimPkg) ensure() bool {
	if !sp.checked {
		sp.checked = true
		path := sp.anchor.fiberImportPath()
		sp.available = ensureGeneratedFile(sp.anchor.fi, sp.pkg.name, appShimGenerated(path))
	}
	return sp.available
}

// ensureCalls makes sure the package has the file of the functions that start an
// app through an expression.
func (sp *appShimPkg) ensureCalls() bool {
	if !sp.callsChecked {
		sp.callsChecked = true
		path := sp.anchor.fiberImportPath()
		sp.callsAvailable = ensureGeneratedFile(sp.anchor.fi, sp.pkg.name, appShimCallsGenerated(path))
	}
	return sp.callsAvailable
}

// appShimRefusal says why the shim cannot carry the settings of a source. The
// user is told about the reasons that are not obvious from the code.
type appShimRefusal struct {
	why  string
	tell bool
}

// appShimWork is what carrying the settings of one source takes: the edits and
// imports of every file it touches, kept apart from the plan until the work is
// known to be possible.
type appShimWork struct {
	pl      *appPlanner
	edits   map[*appFile]*editSet
	imports map[*appFile]*appImports
}

func newShimWork(pl *appPlanner) *appShimWork {
	return &appShimWork{pl: pl, edits: map[*appFile]*editSet{}, imports: map[*appFile]*appImports{}}
}

func (w *appShimWork) editsOf(f *appFile) *editSet {
	if es := w.edits[f]; es != nil {
		return es
	}
	es := &editSet{}
	w.edits[f] = es
	return es
}

func (w *appShimWork) importsOf(f *appFile) *appImports {
	if imps := w.imports[f]; imps != nil {
		return imps
	}
	imps := &appImports{reqs: slices.Clone(w.pl.plan.fileOf(f).imps.reqs)}
	w.imports[f] = imps
	return imps
}

func (w *appShimWork) commit() {
	for f, es := range w.edits {
		fp := w.pl.plan.fileOf(f)
		fp.edits.edits = append(fp.edits.edits, es.edits...)
	}
	for f, imps := range w.imports {
		w.pl.plan.fileOf(f).imps = *imps
	}
}

// carry plans to keep the settings of the source with the apps it creates: every
// setting, whether it could move or not, so that the code reads one way.
//
// It returns the function that adds the work to the plan, or why it cannot be done.
func (pl *appPlanner) carry(src *appListenSource) (commit func(), refusal appShimRefusal) {
	quiet := func(why string) (func(), appShimRefusal) { return nil, appShimRefusal{why: why} }
	tell := func(why string) (func(), appShimRefusal) { return nil, appShimRefusal{why: why, tell: true} }

	switch {
	case len(src.creations) == 0:
		return quiet("this fiber.Config is not handed straight to fiber.New")
	case src.escapes:
		return quiet("this fiber.Config is also passed on to code that could create another app with it")
	case src.retFn != nil:
		return quiet("this fiber.Config comes from a function, away from the app it creates")
	case src.node == nil && (src.lit == nil || len(src.creations) != 1):
		return quiet("this fiber.Config is not a variable or a literal handed to fiber.New")
	}

	f := src.file
	pkg := f.ix.pkgOf(f)
	switch {
	case pkg == nil || !f.walked:
		return quiet("the file is not part of the migration")
	case strings.HasSuffix(pkg.name, "_test"):
		return tell("the config is in an external test package, which cannot share a file with the package it tests")
	case src.node != nil && src.scope == nil && pkg.name != appMainPackage && appIsExported(src.name):
		return tell(src.name + " is an exported variable, which other packages can create apps with")
	}
	for _, c := range src.creations {
		if c.file.ix.pkgOf(c.file) != pkg || !c.file.walked {
			return quiet("the app is created in a file that is not part of this package or the migration")
		}
	}

	if why := appExportedApp(pkg, src.creations); why != "" {
		return tell(why)
	}
	wrap, refusal := pl.shimStarts(src, pkg)
	if refusal.why != "" {
		return nil, refusal
	}

	w := newShimWork(pl)
	var why string
	if src.node != nil {
		why = src.shimVariable(w)
	} else {
		why = src.shimLiteral(w)
	}
	if why != "" {
		return tell(why)
	}

	for _, s := range wrap {
		if rw := s.rewriteShim(appImports{}, ""); rw.problem != "" {
			return tell(fmt.Sprintf("the call at line %d cannot ask for them: %s", s.file.g.line(s.call.Pos()), rw.problem))
		}
	}

	sp := pl.shimOf(pkg, f)
	if !sp.ensure() {
		return tell(appShimFile + " cannot be added to the package: a file or a name of the project is in the way")
	}
	if slices.ContainsFunc(wrap, func(s *appStartInfo) bool { return !appPureReceiver(s.sel.X) }) && !sp.ensureCalls() {
		return tell(appShimCallsFile + " cannot be added to the package: a file or a name of the project is in the way")
	}

	return func() {
		w.commit()
		for _, s := range wrap {
			if !sp.seen[s.call] {
				sp.seen[s.call] = true
				sp.starts = append(sp.starts, s)
			}
		}
		if sp.first == nil {
			sp.first = &src.creations[0]
		}
	}, appShimRefusal{}
}

// shimStarts finds the calls that can start the apps of the source and take their
// settings from the registry: those that start them, and in the package every call
// whose app cannot be traced, which may be one of them. It fails when a call of
// another package starts one of the apps, which the shim of this package cannot
// reach.
func (pl *appPlanner) shimStarts(src *appListenSource, pkg *appPackage) ([]*appStartInfo, appShimRefusal) {
	var wrap []*appStartInfo
	for _, s := range pl.starts {
		hit := slices.ContainsFunc(s.origins.news, func(n appNew) bool { return slices.Contains(src.creations, n) })
		same := s.file.ix.pkgOf(s.file) == pkg
		switch {
		case hit && !same:
			return nil, appShimRefusal{
				why:  fmt.Sprintf("the app is started in %s, which is another package, and the shim of this package does not reach it", appFileLabel(s.file)),
				tell: true,
			}
		case hit, same && !s.origins.complete:
			wrap = append(wrap, s)
		}
	}
	// a start the shim cannot see would run without the settings
	for _, pf := range pkg.files {
		if line := pf.methodValueOfStart(); line > 0 {
			return nil, appShimRefusal{
				why:  fmt.Sprintf("%s uses a method that starts an app as a value, at line %d", appFileLabel(pf), line),
				tell: true,
			}
		}
		if line := pf.unknownStart(); line > 0 {
			return nil, appShimRefusal{
				why:  fmt.Sprintf("the call at line %d of %s may start an app, but the type of its receiver is unknown", line, appFileLabel(pf)),
				tell: true,
			}
		}
	}
	// an app handed to a function of another package that starts apps would start
	// without them
	for _, pf := range pkg.files {
		if line, callee := pl.handedToStarter(pf, pkg); line > 0 {
			return nil, appShimRefusal{
				why:  fmt.Sprintf("the call at line %d of %s hands an app to %s, which starts apps in another package", line, appFileLabel(pf), callee),
				tell: true,
			}
		}
	}
	if len(wrap) == 0 {
		return nil, appShimRefusal{why: "no Listen call of this package starts the app"}
	}

	// a call that sets the same setting itself says two things
	for _, s := range wrap {
		lit := s.explicitConfig()
		if lit == nil {
			continue
		}
		for _, fld := range src.fields {
			if idx, _ := appKeyedElement(lit, fld.v3); idx >= 0 {
				return nil, appShimRefusal{
					why:  fmt.Sprintf("the fiber.ListenConfig of the call at line %d already sets %s", s.file.g.line(s.call.Pos()), fld.v3),
					tell: true,
				}
			}
		}
	}

	return wrap, appShimRefusal{}
}

// handedToStarter returns the line of the first call in f that hands an app to a
// function of another package of the project that starts it, or hands it on to one
// that does, and the name of that function. The line is 0 when there is none. The
// app starts there without the settings the shim keeps.
func (pl *appPlanner) handedToStarter(f *appFile, pkg *appPackage) (line int, name string) {
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if line > 0 || !ok {
			return line == 0
		}
		callee, found := f.calleeOf(call, 0)
		if !found || callee.decl == nil || callee.file.ix.pkgOf(callee.file) == pkg {
			return true
		}
		if slices.ContainsFunc(call.Args, func(arg ast.Expr) bool { return f.kind(arg) == appKindApp }) && pl.startsItsApp(callee, 0) {
			line, name = f.g.line(call.Pos()), callee.decl.Name.Name
		}
		return true
	})
	return line, name
}

// startsItsApp reports whether a function starts an app it is given: one of its
// own calls does, or it hands an app to a function that does.
func (pl *appPlanner) startsItsApp(callee appCallee, depth int) bool {
	if depth > 3 || callee.decl == nil || callee.body == nil {
		return false
	}
	for _, s := range pl.starts {
		if s.file == callee.file && callee.file.enclosingDecl(s.call) == callee.decl {
			return true
		}
	}

	starts := false
	ast.Inspect(callee.body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if starts || !ok {
			return !starts
		}
		inner, found := callee.file.calleeOf(call, 0)
		if !found || inner.decl == nil || inner.decl == callee.decl {
			return true
		}
		if slices.ContainsFunc(call.Args, func(arg ast.Expr) bool { return callee.file.kind(arg) == appKindApp }) {
			starts = pl.startsItsApp(inner, depth+1)
		}
		return true
	})
	return starts
}

// appFileLabel names a file for a note: its path in the project.
func appFileLabel(f *appFile) string {
	if f.fi.Rel != "" {
		return filepath.ToSlash(f.fi.Rel)
	}
	return filepath.Base(f.path)
}

// unknownStart returns the line of the first call of a method that starts an app
// on a value whose type the migration cannot tell (an element of a slice, a
// variable of a loop), 0 when there is none: such a call may start an app of the
// shim.
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

// methodValueOfStart returns the line of the first place that uses a method that
// starts an app without calling it (start := app.Listen), 0 when there is none.
func (f *appFile) methodValueOfStart() int {
	line := 0
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if line > 0 || !ok || !slices.Contains(appStartMethods, sel.Sel.Name) {
			return line == 0
		}
		if call, isCall := f.parent[sel].(*ast.CallExpr); isCall && call.Fun == ast.Expr(sel) {
			return true
		}
		if f.kind(sel.X) == appKindApp {
			line = f.g.line(sel.Pos())
		}
		return true
	})
	return line
}

// exportedApp tells whether the apps the creations make can leave the package
// through what it exports: a function that returns one, a variable or a field
// that holds one. It returns what does. A main package exports nothing.
func appExportedApp(pkg *appPackage, creations []appNew) string {
	if pkg.name == appMainPackage {
		return ""
	}
	flows := func(o appOrigins) bool {
		return !o.complete || slices.ContainsFunc(o.news, func(n appNew) bool { return slices.Contains(creations, n) })
	}

	for _, pf := range pkg.files {
		for _, d := range pf.g.file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !appIsExported(fn.Name.Name) {
				continue
			}
			callee := appCallee{file: pf, typ: fn.Type, body: fn.Body, decl: fn}
			for idx := range appResultCount(fn.Type) {
				if pf.resultKind(fn.Type, idx, 0) == appKindApp && flows(callee.returnOrigins(idx, 0)) {
					return fn.Name.Name + " is exported and returns an app that may be one of them, so the app can be started from other packages"
				}
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(pkg.vars)) {
		v := pkg.vars[name]
		b := v.binding()
		if !appIsExported(name) || b.file.bindingKind(b, 0) != appKindApp {
			continue
		}
		o := appOrigins{complete: true}
		if b.init != nil {
			o.merge(b.file.originsOf(b.init, 0, 0))
		}
		o.merge(b.file.assignedOrigins(b, name, 0))
		if b.init == nil && len(o.news) == 0 {
			o.complete = false
		}
		if flows(o) {
			return name + " is an exported variable that may hold one of them, so the app can be started from other packages"
		}
	}

	for _, typeName := range slices.Sorted(maps.Keys(pkg.types)) {
		td := pkg.types[typeName]
		st, ok := td.spec.Type.(*ast.StructType)
		if !ok {
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
					return td.spec.Name.Name + "." + name + " is an exported field that may hold one of them, so the app can be started from other packages"
				}
			}
		}
	}
	return ""
}

// shimHeld splits the listener settings of the literal of the source off: the
// elements to drop from the literal, and the fields of the fiber.ListenConfig
// they become, in the order they are written.
func (src *appListenSource) shimHeld() (drop []int, held []string) {
	byElement := map[int]*appSourceField{}
	for _, fld := range src.fields {
		for _, occ := range fld.occs {
			if occ.elt >= 0 {
				byElement[occ.elt] = fld
			}
		}
	}
	if src.lit == nil {
		return nil, nil
	}
	g := src.file.g
	for i, el := range src.lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok && byElement[i] != nil {
			drop = append(drop, i)
			held = append(held, byElement[i].v3+": "+g.text(kv.Value))
		}
	}
	return drop, held
}

// shimVariable plans the settings of a config held by a variable: a variable of
// type fiber.ListenConfig is declared next to it and takes over the settings of its
// literal, every assignment and read of a setting goes to it, and the apps that
// are made from the config are made with it.
func (src *appListenSource) shimVariable(w *appShimWork) string {
	f := src.file
	g := f.g

	var name string
	var at int
	var declare func(init string) string
	if src.scope != nil {
		decl := src.declStatement()
		if decl == nil {
			return "the config is declared in a statement that cannot be followed by another"
		}
		name = f.freshName(decl, src.name+"Listen", nil)
		at = appAfterStatement(g.src, g.offset(decl.End()))
		declare = func(init string) string { return name + " := " + init }
	} else {
		gd := f.topLevelDecl(src.node)
		if gd == nil {
			return "the config is declared in a way that cannot be followed by another declaration"
		}
		name = f.freshPackageName(src.name + "Listen")
		at = appAfterStatement(g.src, g.offset(gd.End()))
		declare = func(init string) string { return "var " + name + " = " + init }
	}

	drop, held := src.shimHeld()
	edits := w.editsOf(f)
	if len(drop) > 0 && !appRemoveElements(g, edits, src.lit, drop, nil) {
		return appLiteralLayoutProblem
	}
	listenCfg := w.importsOf(f).fiberQualifier(f) + "ListenConfig{" + strings.Join(held, ", ") + "}"
	edits.replace(at, at, "\n"+declare(listenCfg))

	for _, fld := range src.fields {
		for _, occ := range fld.occs {
			if occ.elt < 0 {
				w.editsOf(occ.file).replaceNode(occ.file.g, occ.node, name+"."+fld.v3)
			}
		}
	}

	for _, c := range src.creations {
		shimCreation(w.editsOf(c.file), c, name)
	}
	return ""
}

// shimLiteral plans the settings of a config that is a literal handed to
// fiber.New: they become the fiber.ListenConfig that fiberMigrateNew takes next to it.
func (src *appListenSource) shimLiteral(w *appShimWork) string {
	f := src.file
	c := src.creations[0]
	if c.file != f || len(c.call.Args) != 1 || c.call.Args[0] != ast.Expr(src.lit) {
		return "the fiber.Config literal is not the only argument of fiber.New"
	}

	drop, held := src.shimHeld()
	edits := w.editsOf(f)
	if len(drop) > 0 && !appRemoveElements(f.g, edits, src.lit, drop, nil) {
		return appLiteralLayoutProblem
	}
	shimCreation(edits, c, w.importsOf(f).fiberQualifier(f)+"ListenConfig{"+strings.Join(held, ", ")+"}")
	return ""
}

// shimCreation turns the fiber.New call c into the call that records the
// settings, the expression listenCfg, for the app.
func shimCreation(edits *editSet, c appNew, listenCfg string) {
	g := c.file.g
	edits.replaceNode(g, c.call.Fun, appShimNew)
	at := g.offset(c.call.Args[0].End())
	edits.replace(at, at, ", "+listenCfg)
}

// finishShims adds the calls that ask the registry for the settings, once every
// source has been planned: a call that can start apps of several sources is
// changed once.
func (pl *appPlanner) finishShims() {
	for _, pkg := range pl.shimOrder() {
		sp := pl.shims[pkg]
		if sp.first != nil {
			note := appShimNote
			if sp.callsChecked && sp.callsAvailable {
				note += " (" + appShimCallsFile + " has the functions for the calls that start an app through an expression)"
			}
			internal.Notef(sp.first.file.fi, sp.first.file.g.line(sp.first.call.Pos()), "%s", note)
		}
		for _, s := range sp.starts {
			fp := pl.plan.fileOf(s.file)

			// the call replaces its receiver, which can hold an app that another edit
			// makes: the receiver is rewritten with those edits, and they are left out
			receiver, rest := "", fp.edits.edits
			if !appPureReceiver(s.sel.X) {
				receiver, rest = appEditedText(fp.edits.edits, s.file.g, s.sel.X)
			}

			rw := s.rewriteShim(appImports{reqs: slices.Clone(fp.imps.reqs)}, receiver)
			if rw.problem != "" {
				internal.Notef(s.file.fi, s.file.g.line(s.call.Pos()),
					"this call may start an app whose listener settings %s keeps, and it cannot ask for them: %s", appShimFile, rw.problem)
				continue
			}
			rest = append(rest, rw.edits...)
			fp.edits.edits = rest
			fp.imps = rw.imports
			for _, note := range rw.notes {
				internal.Notef(s.file.fi, s.file.g.line(s.call.Pos()), "%s", note)
			}
		}
	}
}

// appEditedText returns the text of the node n with the edits that lie inside it
// applied, and the edits that do not.
func appEditedText(edits []textEdit, g *goSource, n ast.Node) (text string, rest []textEdit) {
	start, end := g.offset(n.Pos()), g.offset(n.End())

	var inside editSet
	for _, e := range edits {
		if e.start >= start && e.end <= end {
			inside.replace(e.start-start, e.end-start, e.text)
		} else {
			rest = append(rest, e)
		}
	}
	return inside.apply(g.src[start:end]), rest
}

// shimOrder lists the packages that have a shim in the order of their directories.
func (pl *appPlanner) shimOrder() []*appPackage {
	pkgs := make([]*appPackage, 0, len(pl.shims))
	for pkg := range pl.shims {
		pkgs = append(pkgs, pkg)
	}
	slices.SortFunc(pkgs, func(a, b *appPackage) int {
		if c := strings.Compare(a.dir, b.dir); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})
	return pkgs
}

// rewriteShim turns the call that starts an app into the form that asks the
// registry for the settings of the app. A receiver that is a plain variable or
// field is named again in the call; any other receiver goes to a function of the
// shim, so that the expression is evaluated once. receiver is the text of that
// receiver when other edits changed it, empty to take it from the source.
func (s appStart) rewriteShim(imports appImports, receiver string) appStartRewrite {
	out := appStartRewrite{imports: imports}
	g := s.file.g

	if s.call.Ellipsis.IsValid() {
		out.problem = "the arguments are spread from a slice"
		return out
	}
	recv := receiver
	if recv == "" {
		recv = g.text(s.sel.X)
	}
	if !s.file.isPlainApp(s.sel.X) {
		field := s.file.embeddedAppField(s.sel.X)
		if field == "" {
			out.problem = "its receiver is not a *fiber.App but a type that holds one in a way the shim cannot name"
			return out
		}
		recv += "." + field
	}

	args := s.call.Args
	var cfg string // the config the call already has, empty when it has none
	switch s.method {
	case appNameListen, appNameListener:
		switch len(args) {
		case 1:
		case 2:
			if call, ok := args[1].(*ast.CallExpr); ok && isIdentNamed(call.Fun, appShimConfig) {
				return out // it asks for them already
			}
			cfg = g.text(args[1])
		default:
			out.problem = "it does not take an address and an optional fiber.ListenConfig"
			return out
		}
	default:
		base, ok := s.tlsBase(&out)
		if !ok {
			return out
		}
		cfg = out.imports.fiberQualifier(s.file) + "ListenConfig{" + appJoinFields(base) + "}"
	}

	addr := g.text(args[0])
	if !appPureReceiver(s.sel.X) {
		helper := appShimListen
		if s.method == appNameListener {
			helper = appShimListener
		}
		call := helper + "(" + recv + ", " + addr
		if cfg != "" {
			call += ", " + cfg
		}
		out.edits = append(out.edits, textEdit{start: g.offset(s.call.Pos()), end: g.offset(s.call.End()), text: call + ")"})
		return out
	}

	ask := appShimConfig + "(" + recv
	if cfg != "" {
		ask += ", " + cfg
	}
	ask += ")"

	method := s.method
	if slices.Contains(appTLSMethods, method) {
		method = appNameListen
	}
	end := g.offset(args[len(args)-1].End())
	switch {
	case !slices.Contains(appTLSMethods, s.method) && len(args) == 1:
		out.edits = append(out.edits, textEdit{start: end, end: end, text: ", " + ask})
	case !slices.Contains(appTLSMethods, s.method):
		out.edits = append(out.edits, textEdit{start: g.offset(args[1].Pos()), end: end, text: ask})
	default:
		out.edits = append(out.edits, textEdit{start: g.offset(s.sel.Sel.Pos()), end: end, text: method + "(" + addr + ", " + ask})
	}
	return out
}

// appPureReceiver reports whether e can be written twice without changing what
// the program does: a variable, a field of one, an element of a collection
// picked by a name or a literal.
func appPureReceiver(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return appPureReceiver(x.X)
	case *ast.ParenExpr:
		return appPureReceiver(x.X)
	case *ast.StarExpr:
		return appPureReceiver(x.X)
	case *ast.IndexExpr:
		return appPureReceiver(x.X) && appPureReceiver(x.Index)
	case *ast.BasicLit:
		return true
	}
	return false
}

// isPlainApp reports whether e is a *fiber.App itself rather than a type of the
// project that embeds one, whose value fiberMigrateListenConfig cannot take.
func (f *appFile) isPlainApp(e ast.Expr) bool {
	t, tf := f.valueType(e, 0)
	if t == nil {
		return true // an app made by fiber.New
	}
	return tf.fiberTypeName(t) == ctxTypeApp
}

// embeddedAppField returns the name of the field of type *fiber.App that the
// struct type of e embeds, empty when e is not a value of such a struct.
func (f *appFile) embeddedAppField(e ast.Expr) string {
	t, tf := f.valueType(e, 0)
	if t == nil {
		return ""
	}
	td := tf.resolveNamedType(t)
	if td == nil {
		return ""
	}
	st, ok := td.spec.Type.(*ast.StructType)
	if !ok {
		return ""
	}
	for _, fld := range st.Fields.List {
		if len(fld.Names) == 0 && td.file.fiberTypeName(fld.Type) == ctxTypeApp {
			return appBaseTypeName(appSelectorTail(fld.Type))
		}
	}
	return ""
}

// appAfterStatement returns where a statement that ends at end can be followed by
// another on the next line: the end of its line, when only a comment is left on it,
// so that the comment stays with the statement.
func appAfterStatement(src string, end int) int {
	lineEnd := len(src)
	if i := strings.IndexByte(src[end:], '\n'); i >= 0 {
		lineEnd = end + i
	}

	tail := strings.TrimSpace(src[end:lineEnd])
	switch {
	case tail == "", strings.HasPrefix(tail, "//"):
	case strings.HasPrefix(tail, "/*") && strings.HasSuffix(tail, "*/") && strings.Count(tail, "*/") == 1:
	default:
		return end
	}
	if lineEnd > end && src[lineEnd-1] == '\r' {
		lineEnd--
	}
	return lineEnd
}

// topLevelDecl returns the declaration at the top of the file that holds n.
func (f *appFile) topLevelDecl(n ast.Node) *ast.GenDecl {
	var found *ast.GenDecl
	for p := f.parent[n]; p != nil; p = f.parent[p] {
		if gd, ok := p.(*ast.GenDecl); ok && f.parent[gd] == ast.Node(f.g.file) {
			found = gd
		}
	}
	return found
}

// freshPackageName returns a name that nothing in the package uses, starting from
// preferred: a package-level declaration named like it would be hidden by any
// function that uses the name for something else.
func (f *appFile) freshPackageName(preferred string) string {
	used := map[string]bool{}
	if pkg := f.ix.pkgOf(f); pkg != nil {
		for _, pf := range pkg.files {
			ast.Inspect(pf.g.file, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					used[id.Name] = true
				}
				return true
			})
		}
	}

	name := preferred
	for i := 2; used[name] || f.packageLevelNameTaken(name); i++ {
		name = fmt.Sprintf("%s%d", preferred, i)
	}
	return name
}
