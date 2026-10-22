package v3

import (
	"fmt"
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
	return fp.imps.apply(fp.edits.apply(content))
}

// appStartInfo is a call that starts an app and where the app comes from.
type appStartInfo struct {
	appStart
	origins appOrigins
}

// appPlanner works out the plan.
type appPlanner struct {
	plan    *appListenPlan
	starts  []*appStartInfo
	sources []*appListenSource
}

// appPlanListenerFields reads the project and plans the migration.
func appPlanListenerFields(cwd string) (*appListenPlan, error) {
	ix := newAppIndex(cwd)

	var files []*appFile
	_, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		// only a file that mentions fiber or starts an app can matter
		if !strings.Contains(content, "fiber") && !strings.Contains(content, ".Listen") {
			return content
		}
		if f, ok := ix.openFile(fi, content); ok {
			files = append(files, f)
		}
		return content
	})
	if err != nil {
		return nil, fmt.Errorf("read the project: %w", err)
	}

	pl := &appPlanner{plan: &appListenPlan{files: map[string]*appFilePlan{}}}
	for _, f := range files {
		pl.sources = append(pl.sources, f.configSources()...)
	}
	if len(pl.sources) == 0 {
		return pl.plan, nil
	}
	for _, f := range files {
		for _, s := range f.startsIn() {
			pl.starts = append(pl.starts, &appStartInfo{appStart: s, origins: f.originsOf(s.sel.X, 0, 0)})
		}
	}

	for _, src := range pl.sources {
		reason := pl.move(src)
		noteLeftovers(src, reason)
	}
	return pl.plan, nil
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

// move plans the move of every setting of the source that can move, and
// returns why none could when it cannot.
func (pl *appPlanner) move(src *appListenSource) string {
	if len(src.movableFields()) == 0 {
		return ""
	}
	if len(src.creations) == 0 {
		return "this fiber.Config is not handed straight to fiber.New"
	}
	if src.escapes {
		return "this fiber.Config is also passed on to code that could create another app with it"
	}

	linked, never, reason := pl.linkedStarts(src)
	if reason != "" {
		if never && src.onlyInTests() {
			return pl.dropCosmetic(src, src.movableFields(), reason)
		}
		return reason
	}

	// a setting moves only if its value means the same at every call
	for _, s := range linked {
		if !s.file.walked {
			return fmt.Sprintf("the call that starts it, in %s, is not part of the migration", s.file.path)
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
		return ""
	}

	var work appMovePlan
	if reason = src.planRemoval(movable, &work.removal); reason != "" {
		return reason
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
			return fmt.Sprintf("the call that starts the app, at line %d, cannot take them: %s", s.file.g.line(s.call.Pos()), rw.problem)
		}
		imports[s.file] = rw.imports
		work.starts = append(work.starts, appMoveStart{start: s, rewrite: rw})
	}

	pl.commit(src, &work, imports)
	return ""
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
	for _, fld := range movable {
		for _, occ := range fld.occs {
			switch {
			case occ.elt >= 0:
				drop = append(drop, occ.elt)
			case !appRemoveStmt(src.file.g, edits, occ.assign):
				return fmt.Sprintf("the assignment of %s shares its line with other code", fld.v2)
			}
		}
	}
	if len(drop) > 0 && !appRemoveElements(src.file.g, edits, src.lit, drop, nil) {
		return "the fiber.Config literal is laid out in a way that cannot be edited safely"
	}
	return ""
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

// dropCosmetic removes the settings that only change what Listen prints from a
// source whose apps are never started: they have no effect there. The others
// stay, with the reason.
func (pl *appPlanner) dropCosmetic(src *appListenSource, movable []*appSourceField, reason string) string {
	var cosmetic []*appSourceField
	for _, fld := range movable {
		if slices.Contains(appCosmeticFields, fld.v2) {
			cosmetic = append(cosmetic, fld)
		}
	}
	if len(cosmetic) == 0 {
		return reason
	}

	var removal editSet
	if src.planRemoval(cosmetic, &removal) != "" {
		return reason
	}
	fp := pl.plan.fileOf(src.file)
	fp.edits.edits = append(fp.edits.edits, removal.edits...)
	for _, fld := range cosmetic {
		fld.dropped = true
	}
	return reason
}

// noteLeftovers tells the user about the settings that stay where they are:
// every setting of a source that could not move, and the settings that cannot
// move whatever the source.
func noteLeftovers(src *appListenSource, reason string) {
	for _, fld := range src.fields {
		why := fld.problem
		if why == "" {
			why = reason
		}
		if why == "" || fld.dropped || !src.file.walked || len(fld.occs) == 0 {
			continue
		}
		for _, occ := range fld.occs {
			internal.Notef(src.file.fi, src.file.g.line(occ.node.Pos()),
				"fiber.Config has no %s field in v3: set %s in the fiber.ListenConfig of the call that starts this app (%s)",
				fld.v2, fld.v3, why)
		}
	}
}
