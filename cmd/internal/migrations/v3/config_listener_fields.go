package v3

import (
	"cmp"
	"fmt"
	"go/ast"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateConfigListenerFields moves the settings that fiber.Config lost in v3
// (Prefork, Network, DisableStartupMessage, EnablePrintRoutes) to the
// fiber.ListenConfig of the call that starts the app the config creates.
//
// The link between a config and the call that starts its app is followed
// through the code (variables, functions, struct fields, other files and
// packages of the project). A config whose app cannot be traced to a start
// call keeps its settings, so that the compiler points at them, and a note
// tells where they have to go.
func MigrateConfigListenerFields(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	plan, err := appPlanListenerFields(cwd)
	if err != nil {
		return fmt.Errorf("failed to plan the migration of listener related config fields: %w", err)
	}

	changed, err := internal.ChangeFileContentInfo(cwd, plan.rewrite)
	if err != nil {
		return fmt.Errorf("failed to migrate listener related config fields: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating listener related config fields")
	return nil
}

// appLiteralLayoutProblem is why the literal of a fiber.Config is left alone.
const appLiteralLayoutProblem = "the fiber.Config literal is laid out in a way that cannot be edited safely"

// appListenPlan holds the edits of the whole project, worked out before any
// file is written so that a config and the call that starts its app are always
// changed together.
type appListenPlan struct {
	files map[string]*appFilePlan
}

// appFilePlan is what happens to one file.
type appFilePlan struct {
	src   string
	edits editSet
	imps  appImports
}

func (p *appListenPlan) fileOf(f *appFile) *appFilePlan {
	fp := p.files[f.path]
	if fp == nil {
		fp = &appFilePlan{src: f.g.src}
		p.files[f.path] = fp
	}
	return fp
}

// rewrite applies the planned edits to the file.
func (p *appListenPlan) rewrite(content string, fi internal.FileInfo) string {
	fp := p.files[fi.Path]
	if fp == nil || fp.src != content {
		return content
	}
	if appEditsOverlap(fp.edits.edits) {
		// an edit that lies inside another is lost: that must not go unnoticed
		internal.Notef(fi, 0, "some changes to the listener settings of this file overlap and one of them was not made (please report this)")
	}
	return fp.imps.apply(fp.edits.apply(content))
}

// appEditsOverlap reports whether an edit lies inside another, which editSet.apply
// leaves out.
func appEditsOverlap(edits []textEdit) bool {
	sorted := slices.Clone(edits)
	slices.SortStableFunc(sorted, func(a, b textEdit) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end))
	})

	pos := 0
	for _, e := range sorted {
		if e.start < pos {
			return true
		}
		pos = e.end
	}
	return false
}

// appStartInfo is a call that starts an app and where the app comes from.
type appStartInfo struct {
	appStart
	origins appOrigins
}

// appPlanner works out the plan.
type appPlanner struct {
	plan *appListenPlan
	// shims are the registries of the modules, by the directory of their go.mod.
	shims map[string]*appShimModule
	// mods remembers the module of a directory.
	mods map[string]appModuleOf
	// unknown remembers the first call on a value of unknown type of a file.
	unknown map[*appFile]int
	// reads are the reads of removed settings through the config of an app, by the
	// directory of the go.mod of their module.
	reads map[string]*appModuleReads
	// blocked has the modules whose reads the registry cannot serve, with the reason.
	blocked map[string]string
	// returned remembers what became of the configs a function returns.
	returned map[*ast.FuncDecl]*appReturned
	// followed are the reads and assignments of settings that the migration handled
	// where a config it follows is held by a variable or handed on.
	followed map[*ast.SelectorExpr]bool
	// unreadable is set when a file that may start an app does not parse.
	unreadable bool
	root       string
	files      []*appFile
	starts     []*appStartInfo
	values     []*appValueInfo
	sources    []*appListenSource
}

// appPlanListenerFields reads the project and plans the migration.
func appPlanListenerFields(cwd string) (*appListenPlan, error) {
	ix := newAppIndex(cwd)

	var files []*appFile
	unreadable := false
	_, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		// only a file that mentions fiber or starts an app can matter, and the
		// file of an earlier run is not the project's code
		if !strings.Contains(content, "fiber") && !strings.Contains(content, ".Listen") || isShimFile(fi.Path, content) {
			return content
		}
		if f, ok := ix.openFile(fi, content); ok {
			files = append(files, f)
		} else if strings.Contains(content, "Listen") {
			unreadable = true // it may start an app
		}
		return content
	})
	if err != nil {
		return nil, fmt.Errorf("read the project: %w", err)
	}

	pl := &appPlanner{
		plan:     &appListenPlan{files: map[string]*appFilePlan{}},
		shims:    map[string]*appShimModule{},
		mods:     map[string]appModuleOf{},
		unknown:  map[*appFile]int{},
		reads:    map[string]*appModuleReads{},
		blocked:  map[string]string{},
		returned: map[*ast.FuncDecl]*appReturned{},
		followed: map[*ast.SelectorExpr]bool{},
		root:     cwd,
		files:    files,
		// a file that cannot be read and starts an app somewhere makes "nothing starts
		// it" a guess
		unreadable: unreadable,
	}
	pl.collectReads()
	for _, f := range files {
		pl.sources = append(pl.sources, f.configSources()...)
	}
	if len(pl.sources) > 0 {
		for _, f := range files {
			for _, s := range f.startsIn() {
				pl.starts = append(pl.starts, &appStartInfo{appStart: s, origins: f.originsOf(s.sel.X, 0, 0)})
			}
			for _, v := range f.startValuesIn() {
				v.origins = f.originsOf(v.sel.X, 0, 0)
				pl.values = append(pl.values, v)
			}
		}

		for _, src := range pl.sources {
			reason := pl.move(src)
			appNoteLeftovers(src, reason)
		}
		pl.finishShims()
	}
	pl.finishReads()
	pl.noteUntracked(files)
	return pl.plan, nil
}

// noteUntracked points at the settings that code reads or assigns through a
// fiber.Config the migration does not follow, such as a struct field or a
// parameter: they stay, and v3 does not compile them.
func (pl *appPlanner) noteUntracked(files []*appFile) {
	followed := map[ast.Node]bool{}
	for _, mr := range pl.reads {
		for _, r := range mr.reads {
			followed[r.sel] = true
		}
	}
	for _, src := range pl.sources {
		for _, fld := range src.fields {
			for _, occ := range fld.occs {
				followed[occ.node] = true
			}
		}
	}
	for sel := range pl.followed {
		followed[sel] = true
	}

	for _, f := range files {
		ast.Inspect(f.g.file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || followed[sel] {
				return true
			}
			if v3 := appListenerFieldV3(sel.Sel.Name); v3 != "" && f.kind(sel.X) == appKindConfig {
				internal.Notef(f.fi, f.g.line(sel.Pos()),
					"fiber.Config has no %s field in v3: set %s in the fiber.ListenConfig of the call that starts the app (this fiber.Config is not one the migration follows)",
					sel.Sel.Name, v3)
			}
			return true
		})
	}
}

// appMovePlan is the work that moving the settings of one source takes.
type appMovePlan struct {
	removal editSet
	starts  []appMoveStart
}

type appMoveStart struct {
	start   *appStartInfo
	rewrite appStartRewrite
}

// movableFields returns the settings of the source that can move.
func (src *appListenSource) movableFields() []*appSourceField {
	var movable []*appSourceField
	for _, fld := range src.fields {
		if fld.problem == "" {
			movable = append(movable, fld)
		}
	}
	return movable
}

// appInPlace is the plan for moving the settings of one source to the calls that
// start its apps. It is worked out before anything is added to the plan of the
// project, because that plan is another one when the settings cannot all move
// that way.
type appInPlace struct {
	// commit adds the plan to the plan of the project, nil when there is nothing
	// to add.
	commit func()
	// reason says why the settings that stay could not move, when it is the same
	// for all of them.
	reason string
	// dropped are the settings the plan removes because they have no effect.
	dropped []*appSourceField
	// never is set when no call starts one of the apps at all.
	never bool
}

func (ip appInPlace) apply() {
	if ip.commit != nil {
		ip.commit()
	}
}

// hasLeftovers reports whether some setting of the source stays where it is
// after the plan.
func (src *appListenSource) hasLeftovers(ip appInPlace) bool {
	return slices.ContainsFunc(src.fields, func(fld *appSourceField) bool {
		return len(fld.occs) > 0 && !slices.Contains(ip.dropped, fld) && (fld.problem != "" || ip.reason != "")
	})
}

// move plans the move of every setting of the source that can move, and
// returns why none could when it cannot.
//
// A setting moves to the call that starts the app. When that is not possible for
// all of them, the settings stay with the app they belong to (see carry), or are
// removed from an app that is never started (see dropDead). Only when neither can
// be done do the settings that can move move, and the others stay with a note.
func (pl *appPlanner) move(src *appListenSource) string {
	// the code reads the settings of the apps of the module from their configs, which
	// only the registry can give back
	if mod, ok := pl.moduleOf(src.file); ok && pl.reads[mod.root] != nil {
		commit, refusal := pl.carry(src)
		if commit != nil {
			commit()
			for _, fld := range src.fields {
				fld.problem = ""
			}
			return ""
		}
		if pl.blocked[mod.root] == "" {
			pl.blocked[mod.root] = refusal.why
		}
	}

	ip := pl.moveInPlace(src)
	if !src.hasLeftovers(ip) {
		ip.apply()
		return ip.reason
	}

	if ip.never {
		if commit, dropped := pl.dropDead(src); commit != nil {
			commit()
			ip.dropped = append(ip.dropped, dropped...)
			if !src.hasLeftovers(ip) {
				return ip.reason
			}
			_, refusal := pl.carry(src)
			return src.explain(ip.reason, refusal)
		}
	}

	commit, refusal := pl.carry(src)
	if commit != nil {
		commit()
		for _, fld := range src.fields {
			fld.problem = ""
		}
		return ""
	}

	if refusal.nobody {
		if drop, dropped := pl.dropUnstarted(src); drop != nil {
			drop()
			ip.dropped = append(ip.dropped, dropped...)
			if !src.hasLeftovers(ip) {
				return ip.reason
			}
		}
	}

	ip.apply()
	return src.explain(ip.reason, refusal)
}

// explain adds to the reasons why the settings stay the reason why the registry
// does not keep them with the app, when that is not obvious from the code.
func (src *appListenSource) explain(reason string, refusal appShimRefusal) string {
	if !refusal.tell {
		return reason
	}

	suffix := "; the registry in " + moduleShimDir + ", which would keep it with the app, is not an option: " + refusal.why
	for _, fld := range src.fields {
		if fld.problem != "" {
			fld.problem += suffix
		}
	}
	if reason != "" {
		reason += suffix
	}
	return reason
}

// moveInPlace plans the move of the settings of the source to the calls that
// start its apps.
func (pl *appPlanner) moveInPlace(src *appListenSource) appInPlace {
	if len(src.movableFields()) == 0 && !src.wantsVariable() {
		return appInPlace{}
	}
	if src.escapes && src.escapeWhy != "" {
		return appInPlace{reason: src.escapeWhy}
	}
	if len(src.creations) == 0 {
		return appInPlace{reason: "this fiber.Config is not handed straight to fiber.New"}
	}
	if src.escapes {
		return appInPlace{reason: "this fiber.Config is also passed on to code that could create another app with it"}
	}
	if src.branches {
		return appInPlace{reason: "this fiber.Config is one of several that the function returns"}
	}
	if src.node != nil && src.retFn != nil {
		return appInPlace{reason: "this fiber.Config is returned from a function, away from the app it creates"}
	}

	linked, never, reason := pl.linkedStarts(src)
	if reason != "" {
		if never && src.onlyInTests() {
			ip := pl.dropCosmetic(src, src.movableFields(), reason)
			ip.never = true
			return ip
		}
		return appInPlace{reason: reason, never: never}
	}

	// assignments that run conditionally keep working on a variable of their own
	if src.wantsVariable() {
		commit, why := pl.moveToVariable(src, linked)
		if why == "" {
			return appInPlace{commit: commit}
		}
		for _, fld := range src.fields {
			if fld.problem == appProblemConditional {
				fld.problem += ", and a fiber.ListenConfig variable cannot stand in for it: " + why
			}
		}
	}

	// a setting moves only if its value means the same at every call
	for _, s := range linked {
		if !s.file.walked {
			return appInPlace{reason: fmt.Sprintf("the call that starts it, in %s, is not part of the migration", s.file.path)}
		}
		for _, fld := range src.movableFields() {
			imps := appImports{reqs: slices.Clone(pl.plan.fileOf(s.file).imps.reqs)}
			if why := src.file.valueProblem(fld.value, s.file, s.call, &imps); why != "" {
				fld.problem = fmt.Sprintf("its value cannot move to the call that starts the app, at line %d: %s", s.file.g.line(s.call.Pos()), why)
			}
		}
	}
	movable := src.movableFields()
	if len(movable) == 0 {
		return appInPlace{}
	}

	var work appMovePlan
	if reason = src.planRemoval(movable, &work.removal); reason != "" {
		return appInPlace{reason: reason}
	}

	extras := make([]appListenField, 0, len(movable))
	for _, fld := range movable {
		extras = append(extras, appListenField{name: fld.v3, value: src.file.g.text(fld.value)})
	}

	imports := map[*appFile]appImports{}
	for _, s := range linked {
		imps, seen := imports[s.file]
		if !seen {
			imps = appImports{reqs: slices.Clone(pl.plan.fileOf(s.file).imps.reqs)}
		}
		for _, fld := range movable {
			src.file.valueProblem(fld.value, s.file, s.call, &imps) // records the imports it needs
		}
		rw := s.rewrite(extras, imps)
		if rw.problem != "" {
			return appInPlace{reason: fmt.Sprintf("the call that starts the app, at line %d, cannot take them: %s", s.file.g.line(s.call.Pos()), rw.problem)}
		}
		imports[s.file] = rw.imports
		work.starts = append(work.starts, appMoveStart{start: s, rewrite: rw})
	}

	return appInPlace{commit: func() { pl.commit(src, &work, imports) }}
}

// moveToVariable plans to keep the settings of the source in a fiber.ListenConfig
// variable declared right after the config: the settings of the literal move
// into it, the assignments to them are made to it, and the calls that start the
// apps take it. It returns why that cannot be done, and nothing to commit then.
func (pl *appPlanner) moveToVariable(src *appListenSource, linked []*appStartInfo) (commit func(), why string) {
	f := src.file
	g := f.g
	decl := src.declStatement()
	if decl == nil {
		return nil, "the config is declared in a statement that cannot be followed by another"
	}
	if why := src.variableProblem(decl, linked); why != "" {
		return nil, why
	}
	for _, s := range linked {
		if !s.file.walked {
			return nil, fmt.Sprintf("the call that starts it, in %s, is not part of the migration", s.file.path)
		}
	}

	var work appMovePlan
	name := f.freshName(decl, "listenCfg", nil)
	names := make([]string, len(src.fields))
	byElement := map[int]*appSourceField{}
	for i, fld := range src.fields {
		names[i] = fld.v3
		for _, occ := range fld.occs {
			if occ.elt >= 0 {
				byElement[occ.elt] = fld
			} else {
				work.removal.replaceNode(g, occ.node, name+"."+fld.v3)
			}
		}
	}

	// the settings the literal holds become the literal of the variable
	var drop []int
	var held []string
	if src.lit != nil {
		for i, el := range src.lit.Elts {
			fld := byElement[i]
			if kv, ok := el.(*ast.KeyValueExpr); ok && fld != nil {
				drop = append(drop, i)
				held = append(held, fld.v3+": "+g.text(kv.Value))
			}
		}
	}
	if len(drop) > 0 && !appRemoveElements(g, &work.removal, src.lit, drop, nil) {
		return nil, appLiteralLayoutProblem
	}

	imports := map[*appFile]appImports{}
	importsOf := func(file *appFile) appImports {
		if imps, seen := imports[file]; seen {
			return imps
		}
		return appImports{reqs: slices.Clone(pl.plan.fileOf(file).imps.reqs)}
	}
	srcImports := importsOf(f)
	declText := name + " := " + srcImports.fiberQualifier(f) + "ListenConfig{" + strings.Join(held, ", ") + "}"
	imports[f] = srcImports
	at := g.offset(decl.End())
	work.removal.replace(at, at, "\n"+declText)

	for _, s := range linked {
		rw := s.rewriteVar(name, names, importsOf(s.file))
		if rw.problem != "" {
			return nil, fmt.Sprintf("the call that starts the app, at line %d, cannot take them: %s", s.file.g.line(s.call.Pos()), rw.problem)
		}
		imports[s.file] = rw.imports
		work.starts = append(work.starts, appMoveStart{start: s, rewrite: rw})
	}

	for _, fld := range src.fields {
		fld.problem = ""
	}
	return func() { pl.commit(src, &work, imports) }, ""
}

// commit adds the planned work to the plan.
func (pl *appPlanner) commit(src *appListenSource, work *appMovePlan, imports map[*appFile]appImports) {
	srcPlan := pl.plan.fileOf(src.file)
	srcPlan.edits.edits = append(srcPlan.edits.edits, work.removal.edits...)

	for _, ms := range work.starts {
		fp := pl.plan.fileOf(ms.start.file)
		fp.edits.edits = append(fp.edits.edits, ms.rewrite.edits...)
		for _, note := range ms.rewrite.notes {
			internal.Notef(ms.start.file.fi, ms.start.file.g.line(ms.start.call.Pos()), "%s", note)
		}
	}
	for f, imps := range imports {
		pl.plan.fileOf(f).imps = imps
	}
}

// planRemoval schedules the removal of the movable settings from the source.
func (src *appListenSource) planRemoval(movable []*appSourceField, edits *editSet) string {
	var drop []int
	var assigns []*ast.AssignStmt
	for _, fld := range movable {
		for _, occ := range fld.occs {
			if occ.elt >= 0 {
				drop = append(drop, occ.elt)
			} else {
				assigns = append(assigns, occ.assign)
			}
		}
	}

	for _, stmt := range src.file.removedWith(assigns) {
		if !appRemoveStmt(src.file.g, edits, stmt) {
			for _, fld := range movable {
				for _, occ := range fld.occs {
					if occ.assign != nil && occ.assign == stmt {
						return fmt.Sprintf("the assignment of %s shares its line with other code", fld.v2)
					}
				}
			}
			return "a statement that sets a setting shares its line with other code"
		}
	}
	if len(drop) > 0 && !appRemoveElements(src.file.g, edits, src.lit, drop, nil) {
		return appLiteralLayoutProblem
	}
	return ""
}

// removedWith returns the statements to remove when the assignments go: the
// assignments themselves, and instead of them the `if` that nothing but them is
// left in, when its condition does nothing but compute a value.
func (f *appFile) removedWith(assigns []*ast.AssignStmt) []ast.Stmt {
	gone := map[ast.Stmt]bool{}
	for _, as := range assigns {
		gone[as] = true
	}

	for changed := true; changed; {
		changed = false
		for stmt := range gone {
			block, ok := f.parent[stmt].(*ast.BlockStmt)
			if !ok {
				continue
			}
			ifs, ok := f.parent[block].(*ast.IfStmt)
			if !ok || ifs.Body != block || ifs.Else != nil || ifs.Init != nil || gone[ifs] || f.impure(ifs.Cond) {
				continue
			}
			if slices.ContainsFunc(block.List, func(s ast.Stmt) bool { return !gone[s] }) {
				continue
			}
			for _, s := range block.List {
				delete(gone, s)
			}
			gone[ifs] = true
			changed = true
			break
		}
	}

	out := make([]ast.Stmt, 0, len(gone))
	for stmt := range gone {
		out = append(out, stmt)
	}
	slices.SortFunc(out, func(a, b ast.Stmt) int { return int(a.Pos()) - int(b.Pos()) })
	return out
}

// linkedStarts finds the calls that start the apps the source creates. It
// fails, with the reason, unless every app is started by a call that starts no
// other app.
//
// never is set when the reason is that no call starts one of the apps at all,
// as opposed to a call that might start it.
func (pl *appPlanner) linkedStarts(src *appListenSource) (linked []*appStartInfo, never bool, reason string) {
	creations := src.creations
	for _, s := range pl.starts {
		hit, miss := 0, 0
		for _, n := range s.origins.news {
			if slices.Contains(creations, n) {
				hit++
			} else {
				miss++
			}
		}
		switch {
		case hit == 0:
		case !s.origins.complete:
			return nil, false, fmt.Sprintf("the call at line %d may start it, but where its app comes from could not be traced completely", s.file.g.line(s.call.Pos()))
		case miss > 0:
			return nil, false, fmt.Sprintf("the call at line %d may start this app or another one", s.file.g.line(s.call.Pos()))
		default:
			linked = append(linked, s)
		}
	}

	for _, c := range creations {
		started := slices.ContainsFunc(linked, func(s *appStartInfo) bool { return slices.Contains(s.origins.news, c) })
		if !started {
			return nil, true, fmt.Sprintf("no Listen call that starts the app created at line %d could be found", c.file.g.line(c.call.Pos()))
		}
	}
	return linked, false, ""
}

// onlyInTests reports whether every app of the source is created in a test
// file, where nothing outside the package can start it.
func (src *appListenSource) onlyInTests() bool {
	return !slices.ContainsFunc(src.creations, func(c appNew) bool { return !strings.HasSuffix(c.file.path, "_test.go") })
}

// appCosmeticFields are the settings that only change what Listen prints.
var appCosmeticFields = []string{appFieldQuiet, appFieldPrintRoutes}

// dropCosmetic plans the removal of the settings that only change what Listen
// prints from a source whose apps are never started: they have no effect there.
// The others stay, with the reason.
func (pl *appPlanner) dropCosmetic(src *appListenSource, movable []*appSourceField, reason string) appInPlace {
	var cosmetic []*appSourceField
	for _, fld := range movable {
		if slices.Contains(appCosmeticFields, fld.v2) {
			cosmetic = append(cosmetic, fld)
		}
	}
	if len(cosmetic) == 0 {
		return appInPlace{reason: reason}
	}

	var removal editSet
	if src.planRemoval(cosmetic, &removal) != "" {
		return appInPlace{reason: reason}
	}
	return appInPlace{reason: reason, dropped: cosmetic, commit: func() {
		fp := pl.plan.fileOf(src.file)
		fp.edits.edits = append(fp.edits.edits, removal.edits...)
		for _, fld := range cosmetic {
			fld.dropped = true
		}
	}}
}

// appNoteLeftovers tells the user about the settings that stay where they are:
// every setting of a source that could not move, and the settings that cannot
// move whatever the source.
func appNoteLeftovers(src *appListenSource, reason string) {
	for _, fld := range src.fields {
		why := fld.problem
		if why == "" {
			why = reason
		}
		if why == "" || fld.dropped || !src.file.walked || len(fld.occs) == 0 {
			continue
		}
		for _, occ := range fld.occs {
			if !occ.file.walked {
				continue
			}
			internal.Notef(occ.file.fi, occ.file.g.line(occ.node.Pos()),
				"fiber.Config has no %s field in v3: set %s in the fiber.ListenConfig of the call that starts this app (%s)",
				fld.v2, fld.v3, why)
		}
	}
}
