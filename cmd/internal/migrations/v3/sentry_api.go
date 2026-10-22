package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateSentryAPI moves the code that uses sentry-go to the version that the
// middleware for Fiber v3 needs (0.50), where the vendor removed some of the API
// that Fiber v2 projects used with contrib/fibersentry:
//
//   - Scope.SetExtra, SetExtras and RemoveExtra, and Event.Extra (0.46). The vendor
//     recommends contexts: an extra becomes a context named after its key that
//     holds the value under "value", so that every extra keeps a name of its own
//     (two SetContext calls with the same name replace each other).
//   - sentry.Logger, which is sentry.DebugLogger since 0.33.
//   - the nil check of Hub.Client(), which returns a no-op client instead of nil
//     since 0.50: it asks IsEnabled now.
//   - the ClientOptions EnableLogs, DisableLogs and DisableMetrics, which logs and
//     metrics replaced by being on whenever the code uses them (0.47 and 0.49), and
//     ProfilesSampleRate and User.Segment, which went with the profiling and the
//     segments of the Sentry product (0.31).
//
// A value is taken for a scope, event, hub or client by what its declaration says
// (see sentryKind); a call on a value that cannot be told is reported. The old
// metrics API (0.31) has no replacement that can be written for the user and is
// reported where it is used.
func MigrateSentryAPI(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := &sentryRun{}
	changed, err := internal.ChangeFileContentInfo(cwd, run.migrate)
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate the sentry-go API: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating the sentry-go API")
	return nil
}

// logImportPath is the import path of the log package of the standard library.
var logImportPath = regexp.MustCompile(`^log$`)

// sentryMentions are the texts that a file has to hold for the migration to look
// at it: anything it rewrites or reports.
var sentryMentions = []string{
	"SetExtra", "RemoveExtra", ".Extra", "Extra:", "Logger", ".Client()", "EnableLogs", "DisableLogs",
	"DisableMetrics", "ProfilesSampleRate", "Segment", "Metric",
}

// sentryCalls are the texts of the files that do not import sentry: they can call
// the scope that a function of another package returns.
var sentryCalls = []string{"SetExtra", "RemoveExtra", ".Extra"}

func (r *sentryRun) migrate(content string, fi internal.FileInfo) string {
	// the indexes read what the files were when they were first asked about; the
	// files written since are not what they know
	if len(r.changed) > 0 {
		for _, ix := range r.indexes.byRoot {
			ix.invalidate(r.changed...)
		}
		r.changed = nil
	}

	if !slices.ContainsFunc(sentryMentions, func(s string) bool { return strings.Contains(content, s) }) {
		return content
	}
	// the import path of the package says sentry, and a file that does not import
	// it can only call the scope that a function of another package returns
	if !strings.Contains(content, "sentry") && !slices.ContainsFunc(sentryCalls, func(s string) bool { return strings.Contains(content, s) }) {
		return content
	}
	g, ok := parseSource(content)
	if !ok {
		return content
	}
	names := sentryNamesOf(g.file)

	ix := r.indexes.of(fi)
	ix.packageIn(filepath.Dir(fi.Path)) // the file knows its package once the directory is read
	f := &sentryFile{run: r, g: g, mf: ix.fileAt(fi.Path), names: names, fi: fi, rep: newMW1Reporter(g, fi), done: map[ast.Node]bool{}}
	if !names.any() && f.mf == nil {
		return content // nothing tells what the values of the file are
	}

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if f.done[n] {
			return true
		}
		switch x := n.(type) {
		case *ast.CallExpr:
			f.call(x, stack)
		case *ast.AssignStmt:
			f.assign(x, stack)
		case *ast.SelectorExpr:
			f.selector(x, stack)
		case *ast.CompositeLit:
			f.literal(x, stack)
		case *ast.BinaryExpr:
			f.compare(x, stack)
		}
		return true
	})

	if len(f.edits.edits) == 0 {
		f.rep.flush(&f.edits, content, content)
		return content
	}
	out := f.edits.apply(content)
	f.rep.flush(&f.edits, out, out)
	for _, p := range f.places {
		p.tally.add(fi, internal.LineOf(out, mw1MappedOffset(f.edits.edits, p.offset)))
	}
	r.changed = append(r.changed, fi.Path)
	return out
}

// sentryPlace is a place in the file that a tally counts, found before the edits
// moved the code around it.
type sentryPlace struct {
	tally  *sentryTally
	offset int
}

// tally counts a rewrite at the node, for the note the project gets once.
func (f *sentryFile) tally(t *sentryTally, at token.Pos) {
	f.places = append(f.places, sentryPlace{tally: t, offset: f.g.offset(at)})
}

// report tells the project what the rewrites changed in what Sentry shows.
func (r *sentryRun) report(root string) {
	r.extras.report(root, "sentry-go 0.46 removed Scope.SetExtra, SetExtras, RemoveExtra and Event.Extra: each extra is now a context named after its key "+
		`that holds the value under "value". Sentry shows it in the Contexts of an event instead of Additional Data, and it is not searchable as extra.<key> `+
		"(use SetTag for a value you search by)")
	r.dropped.report(root, "sentry-go 0.31 dropped profiling and the segment of a user from the SDK and the Sentry product, so the ClientOptions.ProfilesSampleRate "+
		"and sentry.User.Segment settings were removed")
}

// context returns the type of a context at pos: sentry.Context, or the map type it
// stands for when the file cannot name the package.
func (f *sentryFile) context(pos token.Pos, stack []ast.Node) string {
	for _, name := range f.names.root {
		if !importsShadowed(name, pos, stack) {
			return name + ".Context"
		}
	}
	return "map[string]any"
}

// contextOf writes the context that holds an extra.
func (f *sentryFile) contextOf(value ast.Expr, pos token.Pos, stack []ast.Node) string {
	return f.context(pos, stack) + `{"value": ` + f.g.text(value) + `}`
}

// contextsOf writes the contexts of a map of extras that is not a literal.
func (f *sentryFile) contextsOf(extras ast.Expr, pos token.Pos, stack []ast.Node) string {
	ctx := f.context(pos, stack)
	return "func(extras map[string]any) map[string]" + ctx + " {\n" +
		"contexts := make(map[string]" + ctx + ", len(extras))\n" +
		"for key, value := range extras {\n" +
		"contexts[key] = " + ctx + `{"value": value}` + "\n" +
		"}\n" +
		"return contexts\n" +
		"}(" + f.g.text(extras) + ")"
}

// sentryReservedContexts are the contexts that the SDK sets on every event, which
// an extra of the same name would collide with.
var sentryReservedContexts = []string{"trace", "device", "os", "runtime"}

// call migrates the calls on a scope and delete on an event.
func (f *sentryFile) call(call *ast.CallExpr, stack []ast.Node) {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		switch fun.Sel.Name {
		case "SetExtra", "SetExtras", "RemoveExtra":
			f.extraCall(call, fun, stack)
		}
	case *ast.Ident:
		if fun.Name == "delete" && len(call.Args) == 2 {
			if sel := f.eventExtra(call.Args[0], stack); sel != nil {
				f.done[sel] = true
				f.edits.replaceNode(f.g, sel.Sel, "Contexts")
				f.tally(&f.run.extras, call.Pos())
			}
		}
	}
}

// extraCall migrates SetExtra, SetExtras and RemoveExtra of a scope.
func (f *sentryFile) extraCall(call *ast.CallExpr, fun *ast.SelectorExpr, stack []ast.Node) {
	name := fun.Sel.Name
	if kind := f.kindOf(fun.X, stack); kind != sentryScope {
		if kind == sentryUnknown && f.names.any() {
			f.rep.notef(fun.Sel, "%s was removed from sentry.Scope in sentry-go 0.46 and this file does not say what %s is: if it is a *sentry.Scope, "+
				`use SetContext(key, sentry.Context{"value": value}), SetContexts or RemoveContext (left as it is)`, name, f.g.text(fun.X))
		}
		return
	}

	args := 2
	if name != "SetExtra" {
		args = 1
	}
	if len(call.Args) != args || call.Ellipsis.IsValid() {
		f.rep.notef(fun.Sel, "%s was removed from sentry.Scope in sentry-go 0.46: use SetContext, SetContexts or RemoveContext (left as it is)", name)
		return
	}
	if key, isLiteral := mw1StringLiteral(call.Args[0]); isLiteral && name != "SetExtras" && slices.Contains(sentryReservedContexts, key) {
		f.rep.notef(fun.Sel, "%s was removed from sentry.Scope in sentry-go 0.46, and the key %q is the name of a context that the SDK sets on every event: "+
			"give the context another name with SetContext (left as it is)", name, key)
		return
	}

	switch name {
	case "RemoveExtra":
		f.edits.replaceNode(f.g, fun.Sel, "RemoveContext")
	case "SetExtra":
		f.edits.replaceNode(f.g, fun.Sel, "SetContext")
		f.edits.replaceNode(f.g, call.Args[1], f.contextOf(call.Args[1], call.Pos(), stack))
	case "SetExtras":
		f.edits.replaceNode(f.g, fun.Sel, "SetContexts")
		f.contextMap(call.Args[0], call.Pos(), stack)
	}
	f.tally(&f.run.extras, call.Pos())
}

// contextMap turns the map of extras e into the map of contexts: a map literal is
// changed where it stands, any other map goes through a function that converts it.
func (f *sentryFile) contextMap(e ast.Expr, pos token.Pos, stack []ast.Node) {
	lit, isLit := e.(*ast.CompositeLit)
	if isLit {
		if mt, isMap := lit.Type.(*ast.MapType); isMap && f.isStringKey(mt.Key) && f.literalValuesAreKeyed(lit) {
			f.edits.replaceNode(f.g, mt.Value, f.context(pos, stack))
			for _, elt := range lit.Elts {
				kv := elt.(*ast.KeyValueExpr)
				f.edits.replaceNode(f.g, kv.Value, `{"value": `+f.g.text(kv.Value)+`}`)
			}
			return
		}
	}
	f.edits.replaceNode(f.g, e, f.contextsOf(e, pos, stack))
}

func (*sentryFile) isStringKey(e ast.Expr) bool { return isIdentNamed(e, "string") }

// literalValuesAreKeyed reports whether every element of the literal has a key.
func (*sentryFile) literalValuesAreKeyed(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		if _, ok := elt.(*ast.KeyValueExpr); !ok {
			return false
		}
	}
	return true
}

// eventExtra returns the selector event.Extra when e is one on an event.
func (f *sentryFile) eventExtra(e ast.Expr, stack []ast.Node) *ast.SelectorExpr {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Extra" || f.kindOf(sel.X, stack) != sentryEvent {
		return nil
	}
	return sel
}

// assign migrates the assignments to the extras of an event, to sentry.Logger and
// to the options that are gone.
func (f *sentryFile) assign(as *ast.AssignStmt, stack []ast.Node) {
	if as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return
	}

	switch lhs := ast.Unparen(as.Lhs[0]).(type) {
	case *ast.IndexExpr:
		// event.Extra[key] = value
		if sel := f.eventExtra(lhs.X, stack); sel != nil {
			f.done[sel] = true
			f.edits.replaceNode(f.g, sel.Sel, "Contexts")
			f.edits.replaceNode(f.g, as.Rhs[0], f.contextOf(as.Rhs[0], as.Pos(), stack))
			f.tally(&f.run.extras, as.Pos())
		}
	case *ast.SelectorExpr:
		switch {
		case f.isLogger(lhs, stack):
			f.done[lhs] = true
			f.assignLogger(as, lhs, stack)
		case f.kindOf(lhs.X, stack) == sentryOptions:
			f.optionAssignment(as, lhs, stack)
		}
	}
}

// selector migrates the reads of event.Extra, the uses of sentry.Logger, and
// reports the identifiers of the metrics API.
func (f *sentryFile) selector(sel *ast.SelectorExpr, stack []ast.Node) {
	if id, ok := sel.X.(*ast.Ident); ok && f.isRoot(id, stack) {
		switch {
		case sel.Sel.Name == "Logger":
			if f.loggerValue(sel, stack) {
				f.edits.replaceNode(f.g, sel.Sel, "DebugLogger")
			}
		case sentryMetricsAPI[sel.Sel.Name]:
			f.rep.notef(sel, "sentry.%s was removed in sentry-go 0.31 with the rest of the metrics API of the beta that ended in October 2024: "+
				"sentry.NewMeter(ctx) replaces it, with Count, Gauge and Distribution (left as it is)", sel.Sel.Name)
		}
		return
	}

	if sel.Sel.Name == "Extra" && f.kindOf(sel.X, stack) == sentryEvent {
		f.eventExtraUse(sel, stack)
	}
}

// eventExtraUse migrates the use of event.Extra: reading the extra of a key is
// reading the value of the context with its name; any other use is reported.
func (f *sentryFile) eventExtraUse(sel *ast.SelectorExpr, stack []ast.Node) {
	if len(stack) >= 1 {
		if ix, ok := stack[len(stack)-1].(*ast.IndexExpr); ok && ix.X == ast.Expr(sel) && !f.writtenTo(ix, stack[:len(stack)-1]) {
			f.edits.replaceNode(f.g, sel.Sel, "Contexts")
			f.edits.replace(f.g.offset(ix.End()), f.g.offset(ix.End()), `["value"]`)
			f.tally(&f.run.extras, sel.Pos())
			return
		}
	}
	f.rep.notef(sel.Sel, "Event.Extra was removed in sentry-go 0.46: the extras are contexts now (Event.Contexts, one context per key, the value under \"value\"), "+
		"and this use of the whole map cannot be rewritten (left as it is)")
}

// writtenTo reports whether the index expression is assigned to, or incremented,
// by the statement above it.
func (*sentryFile) writtenTo(ix *ast.IndexExpr, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}
	switch s := stack[len(stack)-1].(type) {
	case *ast.AssignStmt:
		return slices.ContainsFunc(s.Lhs, func(e ast.Expr) bool { return e == ast.Expr(ix) })
	case *ast.IncDecStmt:
		return s.X == ast.Expr(ix)
	case *ast.RangeStmt:
		return s.Key == ast.Expr(ix) || s.Value == ast.Expr(ix)
	}
	return false
}

// literal migrates the fields that are gone from the literals of the options, of
// a user and of an event.
func (f *sentryFile) literal(lit *ast.CompositeLit, stack []ast.Node) {
	typ := lit.Type
	if typ == nil {
		typ = f.elidedType(stack)
	}
	if typ == nil {
		return
	}
	switch f.typeKind(typ, stack) {
	case sentryOptions:
		f.optionsLiteral(lit)
	case sentryUser:
		f.drop(lit, "Segment")
	case sentryEvent:
		f.eventLiteral(lit, stack)
	}
}

// elidedType returns the type of the elements of the literal whose element type is
// left out: {...} in []sentry.User{{...}} or map[string]sentry.User{"a": {...}}.
// stack holds the ancestors of the inner literal.
func (*sentryFile) elidedType(stack []ast.Node) ast.Expr {
	if len(stack) == 0 {
		return nil
	}

	parent := stack[len(stack)-1]
	isMapValue := false
	if _, ok := parent.(*ast.KeyValueExpr); ok && len(stack) >= 2 {
		parent, isMapValue = stack[len(stack)-2], true
	}
	outer, ok := parent.(*ast.CompositeLit)
	if !ok {
		return nil
	}

	switch t := outer.Type.(type) {
	case *ast.ArrayType:
		if !isMapValue {
			return t.Elt
		}
	case *ast.MapType:
		if isMapValue {
			return t.Value
		}
	}
	return nil
}

// drop removes the fields called name from a literal.
func (f *sentryFile) drop(lit *ast.CompositeLit, name string) {
	for i, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok && isIdentNamed(kv.Key, name) {
			start, end := mw1ElementSpan(f.g, lit, i)
			f.edits.replace(start, end, "")
			f.tally(&f.run.dropped, kv.Pos())
		}
	}
}

// optionsLiteral migrates the options of ClientOptions that sentry-go removed.
func (f *sentryFile) optionsLiteral(lit *ast.CompositeLit) {
	for i, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent {
			continue
		}

		switch key.Name {
		case "ProfilesSampleRate":
			start, end := mw1ElementSpan(f.g, lit, i)
			f.edits.replace(start, end, "")
			f.tally(&f.run.dropped, kv.Pos())
		case "EnableLogs", "DisableLogs", "DisableMetrics":
			if f.optionIsDefault(key.Name, kv.Value) {
				start, end := mw1ElementSpan(f.g, lit, i)
				f.edits.replace(start, end, "")
			} else {
				f.reportOption(kv.Key, key.Name)
			}
		}
	}
}

// optionIsDefault reports whether the value of the option says what sentry-go does
// without it: logs are sent when the code uses them, and so are metrics.
func (*sentryFile) optionIsDefault(name string, value ast.Expr) bool {
	id, ok := ast.Unparen(value).(*ast.Ident)
	if !ok {
		return false
	}
	if name == "EnableLogs" {
		return id.Name == "true"
	}
	return id.Name == "false"
}

// reportOption tells that a removed option cannot be dropped.
func (f *sentryFile) reportOption(at ast.Node, name string) {
	if name == "EnableLogs" {
		f.rep.notef(at, "ClientOptions.EnableLogs was removed in sentry-go 0.47: logs are sent whenever the code uses sentry.NewLogger or a logging integration, "+
			"so leave out the code that you did not want to send (left as it is)")
		return
	}
	f.rep.notef(at, "ClientOptions.%s was removed in sentry-go 0.49: logs and metrics are sent whenever the code uses their API or integration, "+
		"so leave out the code that you did not want to send (left as it is)", name)
}

// optionAssignment migrates `opts.EnableLogs = true` and its kind on a value of
// ClientOptions.
func (f *sentryFile) optionAssignment(as *ast.AssignStmt, lhs *ast.SelectorExpr, stack []ast.Node) {
	switch lhs.Sel.Name {
	case "ProfilesSampleRate", "EnableLogs", "DisableLogs", "DisableMetrics":
	default:
		return
	}

	if lhs.Sel.Name == "ProfilesSampleRate" || f.optionIsDefault(lhs.Sel.Name, as.Rhs[0]) {
		if start, end, ok := mw1StatementSpan(f.g, as, stack); ok {
			f.edits.replace(start, end, "")
			if lhs.Sel.Name == "ProfilesSampleRate" {
				f.tally(&f.run.dropped, as.Pos())
			}
			return
		}
	}
	f.reportOption(lhs.Sel, lhs.Sel.Name)
}

// eventLiteral migrates the Extra field of a literal of sentry.Event.
func (f *sentryFile) eventLiteral(lit *ast.CompositeLit, stack []ast.Node) {
	var extra *ast.KeyValueExpr
	hasContexts := false
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			switch {
			case isIdentNamed(kv.Key, "Extra"):
				extra = kv
			case isIdentNamed(kv.Key, "Contexts"):
				hasContexts = true
			}
		}
	}
	if extra == nil {
		return
	}
	if hasContexts {
		f.rep.notef(extra.Key, "Event.Extra was removed in sentry-go 0.46: move the extras into the Contexts of this literal, one context per key with the value under \"value\" (left as it is)")
		return
	}

	f.edits.replaceNode(f.g, extra.Key, "Contexts")
	f.contextMap(extra.Value, lit.Pos(), stack)
	f.tally(&f.run.extras, extra.Pos())
}

// compare migrates the nil checks of a client: Hub.Client() returns the no-op
// client of sentry-go instead of nil since 0.50, and IsEnabled is false for it, as
// it is for nil.
func (f *sentryFile) compare(b *ast.BinaryExpr, stack []ast.Node) {
	if b.Op != token.EQL && b.Op != token.NEQ {
		return
	}

	var client ast.Expr
	switch {
	case isNilIdent(b.Y):
		client = b.X
	case isNilIdent(b.X):
		client = b.Y
	default:
		return
	}
	switch ast.Unparen(client).(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr:
	default:
		return
	}
	if f.kindOf(client, stack) != sentryClient {
		return
	}

	text := f.g.text(client) + ".IsEnabled()"
	if b.Op == token.EQL {
		text = "!" + text
	}
	f.edits.replaceNode(f.g, b, text)
}

func isNilIdent(e ast.Expr) bool { return isIdentNamed(ast.Unparen(e), mw1NilIdent) }

// isLogger reports whether sel is sentry.Logger.
func (f *sentryFile) isLogger(sel *ast.SelectorExpr, stack []ast.Node) bool {
	id, ok := sel.X.(*ast.Ident)
	return ok && sel.Sel.Name == "Logger" && f.isRoot(id, stack)
}

// loggerValue reports whether sentry.Logger is used as the variable of v2 and
// not as the type that sentry-go has had since 0.35.
func (f *sentryFile) loggerValue(sel *ast.SelectorExpr, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}

	switch p := stack[len(stack)-1].(type) {
	case *ast.SelectorExpr:
		return p.X == ast.Expr(sel)
	case *ast.CallExpr:
		// an argument; new(sentry.Logger) and make name a type
		if id, isIdent := p.Fun.(*ast.Ident); isIdent && (id.Name == modBuiltinNew || id.Name == "make") {
			return false
		}
		return slices.Contains(p.Args, ast.Expr(sel))
	case *ast.AssignStmt, *ast.ReturnStmt, *ast.KeyValueExpr, *ast.BinaryExpr:
		return true
	case *ast.ValueSpec:
		return slices.Contains(p.Values, ast.Expr(sel))
	}
	return false
}

// assignLogger migrates `sentry.Logger = logger`. The SDK writes to its own logger
// since 0.33 and DebugLogger names it, so that assigning another one has no
// effect any more: the output, prefix and flags of the new one are given to it.
func (f *sentryFile) assignLogger(as *ast.AssignStmt, lhs *ast.SelectorExpr, stack []ast.Node) {
	if len(stack) == 0 {
		return
	}
	switch stack[len(stack)-1].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
	default:
		f.rep.notef(as, "sentry.Logger is sentry.DebugLogger since sentry-go 0.33, and the SDK does not read it back after an assignment: "+
			"give the logger it writes to the output, prefix and flags you want (left as it is)")
		return
	}

	pkg := f.g.text(lhs.X)
	rhs := ast.Unparen(as.Rhs[0])
	var output, prefix, flags string
	switch x := rhs.(type) {
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "New" || len(x.Args) != 3 || !f.isLogPackage(sel.X, stack) {
			f.reportLoggerAssignment(as)
			return
		}
		output, prefix, flags = f.g.text(x.Args[0]), f.g.text(x.Args[1]), f.g.text(x.Args[2])
	case *ast.Ident, *ast.SelectorExpr:
		text := f.g.text(x)
		output, prefix, flags = text+".Writer()", text+".Prefix()", text+".Flags()"
	default:
		f.reportLoggerAssignment(as)
		return
	}

	indent := mw1Indent(f.g.src, f.g.offset(as.Pos()))
	f.edits.replaceNode(f.g, as,
		pkg+".DebugLogger.SetOutput("+output+")\n"+indent+
			pkg+".DebugLogger.SetPrefix("+prefix+")\n"+indent+
			pkg+".DebugLogger.SetFlags("+flags+")")
}

func (f *sentryFile) reportLoggerAssignment(at ast.Node) {
	f.rep.notef(at, "sentry.Logger is sentry.DebugLogger since sentry-go 0.33, and the SDK does not read it back after an assignment: "+
		"call SetOutput, SetPrefix and SetFlags on sentry.DebugLogger instead (left as it is)")
}

// isLogPackage reports whether e names the log package of the standard library.
func (f *sentryFile) isLogPackage(e ast.Expr, stack []ast.Node) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	_, hidden := resolveIdent(id.Name, id.Pos(), stack)
	if hidden {
		return false
	}
	for _, name := range importNames(f.g.src, logImportPath, "log") {
		if name == id.Name {
			return true
		}
	}
	return false
}

// sentryMetricsAPI are the names that the metrics API of sentry-go had until 0.31.
var sentryMetricsAPI = func() map[string]bool {
	names := map[string]bool{}
	for name := range strings.FieldsSeq(`NewCounterMetric NewDistributionMetric NewGaugeMetric NewSetMetric CounterMetric DistributionMetric
		GaugeMetric SetMetric MetricUnit NumberOrString Ordered CustomUnit Bit Byte KibiByte KiloByte MebiByte MegaByte GibiByte GigaByte
		TebiByte TeraByte PebiByte PetaByte ExbiByte ExaByte Nanosecond NanoSecond MicroSecond MilliSecond Second Minute Hour Day Week
		Percent Ratio`) {
		names[name] = true
	}
	return names
}()
