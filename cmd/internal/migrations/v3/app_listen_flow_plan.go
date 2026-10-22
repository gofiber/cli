package v3

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// appVisited is a Go file the walk of the project came across, whether or not the
// migration opened it: a file that names neither fiber nor a Listen call can still
// hold a literal of an alias of fiber.Config, or call a function that takes one.
type appVisited struct {
	fi  internal.FileInfo
	src string
}

// editable reports whether the migration may change the file, opening it when it
// was only read to understand the project.
func (pl *appPlanner) editable(f *appFile) bool {
	if f.walked {
		return true
	}
	v, seen := pl.visited[f.path]
	if !seen || v.src != f.g.src {
		return false
	}
	opened, ok := pl.ix.openFile(v.fi, v.src)
	return ok && opened.walked
}

// carryFlow plans to keep the settings of a config that is not handed straight to
// fiber.New with the config, which becomes the registry's: see app_listen_flow.go.
func (pl *appPlanner) carryFlow(src *appListenSource) (commit func(), refusal appShimRefusal) {
	fl := pl.newFlow(src.file)
	switch {
	case src.retFn != nil:
		fl.claimResult(src.file, src.retFn, 0, flowConfig)
	case src.node != nil:
		b, ok := src.file.sourceBinding(src)
		if !ok {
			return nil, appShimRefusal{why: "the variable that holds the config could not be traced to its declaration"}
		}
		fl.claimVar(b, flowConfig)
	case src.lit != nil:
		fl.claimExpr(src.file, src.lit, flowConfig, true)
	default:
		return nil, appShimRefusal{why: "this fiber.Config is not a variable or a literal"}
	}
	fl.run()
	return fl.finish()
}

// sourceBinding describes the variable the source is held by.
func (f *appFile) sourceBinding(src *appListenSource) (appBinding, bool) {
	var d decl
	switch p := f.parent[src.node].(type) {
	case *ast.ValueSpec:
		d.typ = p.Type
		for i, id := range p.Names {
			if id.Name != src.name {
				continue
			}
			switch {
			case len(p.Values) == len(p.Names):
				d.init = p.Values[i]
			case len(p.Values) == 1:
				d.init = p.Values[0]
			}
		}
	case *ast.AssignStmt:
		init, ok := src.node.(ast.Expr)
		if !ok {
			return appBinding{}, false
		}
		d.init = init
	default:
		return appBinding{}, false
	}
	b := f.bindingOf(d, src.name)
	return b, b.node == src.node
}

// flowOf returns the set of places that took the source, nil when none did.
func (pl *appPlanner) flowOf(src *appListenSource) *appFlow {
	for _, fl := range pl.flows {
		switch {
		case src.retFn != nil && fl.keys[appFlowKey{kind: flowResult, node: src.retFn}]:
			return fl
		case src.node != nil && fl.keys[appFlowKey{kind: flowVar, node: src.node, name: src.name}]:
			return fl
		case src.lit != nil && fl.lits[src.lit] != nil:
			return fl
		}
	}
	return nil
}

// finish plans the change of every place of the set, once they are all known. It
// returns the function that adds the work to the plan, or why it cannot be done.
func (fl *appFlow) finish() (commit func(), refusal appShimRefusal) {
	pl := fl.pl
	if fl.why != "" {
		return nil, appShimRefusal{why: fl.why, tell: true, flow: true}
	}
	tell := func(why string) (func(), appShimRefusal) {
		return nil, appShimRefusal{why: why, tell: true, flow: true}
	}
	if len(fl.news) == 0 {
		return tell("no fiber.New call of this module makes an app from this config")
	}

	mod, inModule := pl.moduleOf(fl.seed)
	if !inModule {
		return tell("the file is not in a module, and the registry is a package of the module")
	}

	created := &appListenSource{file: fl.seed, creations: fl.news, fields: fl.settings()}
	wrap, values, refusal := pl.shimStarts(created, mod)
	if refusal.nobody {
		// the settings have no effect on an app nobody starts
		if commit, why := fl.drop(mod); commit != nil {
			return commit, appShimRefusal{}
		} else if why != "" {
			refusal.why += "; and they cannot be dropped: " + why
		}
	}
	if refusal.why != "" {
		refusal.flow = true
		return nil, refusal
	}

	files := fl.files()
	for _, f := range files {
		fm, ok := pl.moduleOf(f)
		switch {
		case !ok || fm.root != mod.root:
			return tell(fmt.Sprintf("the config goes between %s and %s, which belong to different Go modules: "+
				"each module gets a registry of its own, and two of them cannot exchange values", appFileLabel(fl.seed), appFileLabel(f)))
		case !pl.editable(f):
			return tell(fmt.Sprintf("%s holds the config and is not part of the migration", appFileLabel(f)))
		}
	}

	sp := pl.shimOf(mod, files[0].fi)
	if sp.path == "" {
		return tell("the file is not in a module, and the registry is a package of the module")
	}

	w := newShimWork(pl, sp.path)
	for _, node := range sortedByPos(fl.types) {
		f := fl.types[node]
		w.editsOf(f).replaceNode(f.g, node, w.qualifier(f)+appShimConfigType)
	}
	for _, lit := range sortedByPos(fl.lits) {
		if why := shimWrapLiteral(w, fl.lits[lit], lit); why != "" {
			return tell(why)
		}
	}
	for _, c := range fl.news {
		shimNewV2(w, c)
	}
	for _, s := range wrap {
		if rw := s.rewriteShim(appImports{}, "", sp.path); rw.problem != "" {
			return tell(fmt.Sprintf("the call at line %d cannot ask for them: %s", s.file.g.line(s.call.Pos()), rw.problem))
		}
	}
	if !sp.ensure() {
		return tell("the directory " + moduleShimDir + " holds a Go file that is not the registry's")
	}

	// edits of the plan that overlap these would lose one of the two
	for f, es := range w.edits {
		var planned []textEdit
		if fp := pl.plan.files[f.path]; fp != nil {
			planned = fp.edits.edits
		}
		if appEditsOverlap(slices.Concat(planned, es.edits)) {
			return tell(fmt.Sprintf("its changes overlap other changes of %s", appFileLabel(f)))
		}
	}

	exposed := pl.exposure(mod, fl.news)
	first := fl.firstNew()
	return func() {
		w.commit()
		for sel := range fl.sels {
			pl.followed[sel] = true
		}
		for _, s := range wrap {
			if !sp.seen[s.call] {
				sp.seen[s.call] = true
				sp.starts = append(sp.starts, s)
			}
		}
		for _, v := range values {
			if !sp.seen[v.sel] {
				sp.seen[v.sel] = true
				sp.values = append(sp.values, v)
			}
		}
		for _, name := range exposed {
			if !slices.Contains(sp.exposed, name) {
				sp.exposed = append(sp.exposed, name)
			}
		}
		for _, c := range fl.changed {
			sp.recordChange(c)
		}
		if sp.first == nil {
			sp.first = &first
		}
		pl.flows = append(pl.flows, fl)
	}, appShimRefusal{}
}

// files returns the files the change of the set touches, in the order of their
// paths.
func (fl *appFlow) files() []*appFile {
	seen := map[*appFile]bool{}
	for _, f := range fl.types {
		seen[f] = true
	}
	for _, f := range fl.lits {
		seen[f] = true
	}
	for _, c := range fl.news {
		seen[c.file] = true
	}
	out := slices.Collect(maps.Keys(seen))
	slices.SortFunc(out, func(a, b *appFile) int { return cmp.Compare(a.path, b.path) })
	return out
}

// firstNew returns the fiber.New call of the set that comes first in the project.
func (fl *appFlow) firstNew() appNew {
	first := fl.news[0]
	for _, c := range fl.news[1:] {
		if c.file.path < first.file.path || (c.file.path == first.file.path && c.call.Pos() < first.call.Pos()) {
			first = c
		}
	}
	return first
}

// settings returns the settings that the config of the set carries, as the
// fields of a source: the ones its literals set and its code reads and assigns.
func (fl *appFlow) settings() []*appSourceField {
	names := map[string]bool{}
	for lit := range fl.lits {
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if id, isIdent := kv.Key.(*ast.Ident); isIdent && appListenerFieldV3(id.Name) != "" {
					names[id.Name] = true
				}
			}
		}
	}
	for sel := range fl.sels {
		names[sel.Sel.Name] = true
	}

	out := make([]*appSourceField, 0, len(names))
	for name := range names {
		out = append(out, &appSourceField{v2: name, v3: appListenerFieldV3(name)})
	}
	slices.SortFunc(out, func(a, b *appSourceField) int { return appListenerFieldIndex(a.v2) - appListenerFieldIndex(b.v2) })
	return out
}

// sortedByPos returns the keys of the map in the order of their files and
// positions.
func sortedByPos[K interface {
	ast.Node
	comparable
}](m map[K]*appFile) []K {
	out := slices.Collect(maps.Keys(m))
	slices.SortFunc(out, func(a, b K) int {
		return cmp.Or(cmp.Compare(m[a].path, m[b].path), cmp.Compare(a.Pos(), b.Pos()))
	})
	return out
}

// recordChange remembers a declaration whose type changes, for the note of the
// module.
func (sp *appShimModule) recordChange(c appFlowChange) {
	list, exposed := &sp.configs, &sp.exposedConfigs
	switch c.how {
	case "take":
		list, exposed = &sp.takes, &sp.exposedTypes
	case "hold":
		list, exposed = &sp.holds, &sp.exposedTypes
	}
	if !slices.Contains(*list, c.label) {
		*list = append(*list, c.label)
	}
	if c.exposed && !slices.Contains(*exposed, c.label) {
		*exposed = append(*exposed, c.label)
	}
}

// drop plans the removal of the settings the set carries, for the apps it makes
// that no call of the module starts: they take effect only when an app is started,
// and the app is for tests or for a program of another module. It returns why they
// cannot be taken out when they cannot.
func (fl *appFlow) drop(mod goModule) (commit func(), why string) {
	pl := fl.pl
	switch {
	case pl.unreadable:
		return nil, "a file of the project that may start an app does not parse"
	case len(pl.exposure(mod, fl.news)) > 0:
		return nil, "an app of the set can leave the module through a package other modules import"
	}

	edits := map[*appFile]*editSet{}
	editsOf := func(f *appFile) *editSet {
		if edits[f] == nil {
			edits[f] = &editSet{}
		}
		return edits[f]
	}
	var names []string
	var first ast.Node
	var firstFile *appFile
	add := func(f *appFile, n ast.Node, name string) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
		if first == nil {
			first, firstFile = n, f
		}
	}

	for _, lit := range sortedByPos(fl.lits) {
		f := fl.lits[lit]
		var idx []int
		for i, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if id, isIdent := kv.Key.(*ast.Ident); isIdent && appListenerFieldV3(id.Name) != "" {
				if !f.droppableValue(kv.Value) {
					return nil, fmt.Sprintf("the value of %s, at line %d of %s, cannot be removed with it", id.Name, f.g.line(kv.Pos()), appFileLabel(f))
				}
				idx = append(idx, i)
				add(f, kv, id.Name)
			}
		}
		if len(idx) > 0 && !appRemoveElements(f.g, editsOf(f), lit, idx, nil) {
			return nil, appLiteralLayoutProblem
		}
	}

	assigns := map[*appFile][]*ast.AssignStmt{}
	for _, sel := range sortedByPos(fl.sels) {
		f := fl.sels[sel]
		as, ok := f.parent[sel].(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 || as.Lhs[0] != ast.Expr(sel) {
			return nil, fmt.Sprintf("the code reads %s, at line %d of %s", sel.Sel.Name, f.g.line(sel.Pos()), appFileLabel(f))
		}
		if !f.droppableValue(as.Rhs[0]) {
			return nil, fmt.Sprintf("the value of %s, at line %d of %s, cannot be removed with it", sel.Sel.Name, f.g.line(as.Pos()), appFileLabel(f))
		}
		assigns[f] = append(assigns[f], as)
		add(f, sel, sel.Sel.Name)
	}
	for _, f := range sortedFiles(assigns) {
		for _, stmt := range f.removedWith(assigns[f]) {
			if !appRemoveStmt(f.g, editsOf(f), stmt) && !appRemoveInline(f, editsOf(f), stmt) {
				return nil, fmt.Sprintf("the statement at line %d of %s shares its line with other code", f.g.line(stmt.Pos()), appFileLabel(f))
			}
		}
	}
	if first == nil {
		return nil, ""
	}

	// the files it changes are the project's, and nothing else changes them
	for f, es := range edits {
		if fm, ok := pl.moduleOf(f); !ok || fm.root != mod.root || !pl.editable(f) {
			return nil, fmt.Sprintf("%s is not part of the migration", appFileLabel(f))
		}
		var planned []textEdit
		if fp := pl.plan.files[f.path]; fp != nil {
			planned = fp.edits.edits
		}
		if appEditsOverlap(slices.Concat(planned, es.edits)) {
			return nil, fmt.Sprintf("its changes overlap other changes of %s", appFileLabel(f))
		}
	}

	slices.SortFunc(names, func(a, b string) int { return appListenerFieldIndex(a) - appListenerFieldIndex(b) })
	return func() {
		for f, es := range edits {
			fp := pl.plan.fileOf(f)
			fp.edits.edits = append(fp.edits.edits, es.edits...)
		}
		for sel := range fl.sels {
			pl.followed[sel] = true
		}
		pl.flows = append(pl.flows, fl)
		internal.Notef(firstFile.fi, firstFile.g.line(first.Pos()),
			"fiber.Config has no %s in v3: dropped, because no call of this module starts the app this config makes; "+
				"set %s in the fiber.ListenConfig of the call that starts the app if you start it later",
			appFieldList(names), appThem(len(names)))
	}, ""
}

// sortedFiles returns the files that key the map, in the order of their paths.
func sortedFiles[V any](m map[*appFile]V) []*appFile {
	out := slices.Collect(maps.Keys(m))
	slices.SortFunc(out, func(a, b *appFile) int { return cmp.Compare(a.path, b.path) })
	return out
}

// appRemoveInline schedules the removal of a statement that is the only one of a
// block written on one line, such as the body of the function literal of an option.
func appRemoveInline(f *appFile, edits *editSet, stmt ast.Stmt) bool {
	block, ok := f.parent[stmt].(*ast.BlockStmt)
	if !ok || len(block.List) != 1 || f.g.line(block.Lbrace) != f.g.line(block.Rbrace) {
		return false
	}
	src := f.g.src
	before := strings.TrimSpace(src[f.g.offset(block.Lbrace)+1 : f.g.offset(stmt.Pos())])
	after := strings.TrimSpace(src[f.g.offset(stmt.End()):f.g.offset(block.Rbrace)])
	if before != "" || after != "" {
		return false
	}
	edits.replaceNode(f.g, stmt, "")
	return true
}
