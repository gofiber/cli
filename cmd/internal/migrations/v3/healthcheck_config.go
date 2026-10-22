package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var mw1Healthcheck = newMW1Package("healthcheck")

// MigrateHealthcheckConfig moves the healthcheck middleware to v3, where every
// probe is its own handler registered on its own route. v2 answered both
// probes from one `Use`:
//
//	app.Use(healthcheck.New(healthcheck.Config{LivenessProbe: ..., ReadinessEndpoint: "/ready"}))
//
// which becomes one `Get` per probe, each with a config of its own. The rewrite
// is limited to what the file imports from the healthcheck package; a config
// that cannot be split safely is left as it is and reported.
//
// A config that is declared in another file of the package than the Use call
// that registers it is split where it is declared (see hcProject).
func MigrateHealthcheckConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	proj, err := planHealthcheck(cwd)
	if err != nil {
		return fmt.Errorf("failed to plan the migration of healthcheck configs: %w", err)
	}

	changed, err := internal.ChangeFileContentInfo(cwd, proj.migrate)
	proj.reportShims()
	if err != nil {
		return fmt.Errorf("failed to migrate healthcheck configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating healthcheck middleware configs")
	return nil
}

// The v2 only fields of healthcheck.Config.
const (
	hcLivenessProbe     = "LivenessProbe"
	hcLivenessEndpoint  = "LivenessEndpoint"
	hcReadinessProbe    = "ReadinessProbe"
	hcReadinessEndpoint = "ReadinessEndpoint"
	hcNext              = "Next"
)

var hcV2OnlyFields = []string{hcLivenessProbe, hcLivenessEndpoint, hcReadinessProbe, hcReadinessEndpoint}

// hcRenamedMembers are the members of the package that v3 renamed.
var hcRenamedMembers = map[string]string{
	nameDefaultLivenessEndpoint:  "LivenessEndpoint",
	nameDefaultReadinessEndpoint: "ReadinessEndpoint",
}

// hcMigration is the state of the rewrite of one file.
type hcMigration struct {
	g     *goSource
	split map[*ast.CompositeLit]bool
	rep   *mw1Reporter
	// proj knows the other files of the package.
	proj  *hcProject
	fiber string
	// renames holds the identifier edits. They are applied inside the text that
	// is copied into a rewritten statement, and as edits of their own elsewhere.
	renames editSet
	edits   editSet
	fi      internal.FileInfo
	names   []string
	// v2Configs are the healthcheck.Config literals that still have fields of v2,
	// split holds the ones that were taken apart.
	v2Configs  []*ast.CompositeLit
	needsFiber bool
	// fiberUsed is set when a rename writes a type of the core package.
	fiberUsed bool
	// shim is set when the module package holds the middleware of v2 for this file.
	shim *hcShimFile
}

// newHCMigration prepares the rewrite of a file that imports the healthcheck
// package under names.
func newHCMigration(proj *hcProject, g *goSource, fi internal.FileInfo, names []string) *hcMigration {
	h := &hcMigration{g: g, rep: newMW1Reporter(g, fi), proj: proj, fi: fi, names: names, split: map[*ast.CompositeLit]bool{}}
	h.fiber, h.needsFiber = mw1FiberName(g.src)
	h.collectRenames()
	h.openShim()
	return h
}

// migrate rewrites a file. While the project is being planned nothing is
// reported: the pass only finds the configs that have to be split in another file.
func (p *hcProject) migrate(content string, fi internal.FileInfo) string {
	names := mw1Qualifiers(mw1Healthcheck.names(content))
	if len(names) == 0 {
		return content
	}
	var g *goSource
	var ok bool
	if p.planning {
		g, ok = parseSource(content)
	} else {
		g, ok = parseFile(fi, content)
	}
	if !ok {
		return content
	}

	h := newHCMigration(p, g, fi, names)
	h.splitPackageConfigs()

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if h.rewriteCall(x, stack) {
				return false
			}
			h.strayNew(x, stack)
		case *ast.CompositeLit:
			h.collectV2Config(x)
		case *ast.SelectorExpr:
			if mw1IsMember(x, h.names, "New") && !hcIsCallee(x, stack) {
				h.newAsValue(x, stack)
			}
		}
		return true
	})

	h.reportLeftovers()
	all := editSet{edits: append(slices.Clone(h.edits.edits), h.renames.edits...)}
	applied := all.apply(content)
	out := applied
	if h.needsFiber && h.fiberUsed {
		out = mw1AddImport(out, mw1FiberCoreImport)
	}
	if h.shim != nil && h.shim.used && h.shim.add && hcUsesQualifier(applied, h.shim.name) {
		out = mw1AddImport(out, h.shim.path)
	}
	out = h.dropOrphans(out)
	if !p.planning {
		h.rep.flush(&all, applied, out)
	}
	return out
}

// collectRenames schedules the edits for the members of the package that were
// renamed or removed in v3.
func (h *hcMigration) collectRenames() {
	h.g.walk(func(n ast.Node, stack []ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, member, isSel := mw1Selector(sel)
		if !isSel || !slices.Contains(h.names, x) {
			return true
		}

		switch member {
		case "DefaultLivenessEndpoint", "DefaultReadinessEndpoint":
			h.renames.replaceNode(h.g, sel.Sel, hcRenamedMembers[member])
		case "HealthChecker":
			h.renames.replaceNode(h.g, sel, h.probeType(stack, sel))
		case "HealthCheckerHandler":
			h.renames.replaceNode(h.g, sel, "func("+h.probeTypeText()+") "+h.fiber+".Handler")
			h.fiberUsed = true
		}
		return true
	})
}

func (h *hcMigration) probeTypeText() string {
	h.fiberUsed = true
	return "func(" + h.fiber + ".Ctx) bool"
}

// probeType renders the replacement of healthcheck.HealthChecker; a conversion
// needs the function type in parentheses.
func (h *hcMigration) probeType(stack []ast.Node, sel *ast.SelectorExpr) string {
	text := h.probeTypeText()
	if len(stack) > 0 {
		if call, ok := stack[len(stack)-1].(*ast.CallExpr); ok && call.Fun == ast.Expr(sel) {
			return "(" + text + ")"
		}
	}
	return text
}

// dropOrphans removes the import of the healthcheck package that the rewrite left
// unused, as the code of the file only mentions the module package now. goimports
// would do it after the migrations, but then the lines the notes carry would be
// the lines of the text before the import went.
func (h *hcMigration) dropOrphans(out string) string {
	after, ok := parseSource(out)
	if !ok {
		return out
	}
	usedBefore, usedAfter := contribSelectorBases(h.g), contribSelectorBases(after)

	var edits editSet
	for _, spec := range after.file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !mw1Healthcheck.path.MatchString(importPath) {
			continue
		}
		name := mw1Healthcheck.name
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name != "_" && name != "." && usedBefore[name] && !usedAfter[name] {
			contribRemoveImport(after, &edits, spec)
		}
	}
	return edits.apply(out)
}

// textOf returns the source text of n with the renames applied.
func (h *hcMigration) textOf(n ast.Node) string {
	start, end := h.g.offset(n.Pos()), h.g.offset(n.End())
	var inner editSet
	for _, e := range h.renames.edits {
		if e.start >= start && e.end <= end {
			inner.replace(e.start-start, e.end-start, e.text)
		}
	}
	return inner.apply(h.g.src[start:end])
}

// isNewCall reports whether e is a call of healthcheck.New.
func (h *hcMigration) isNewCall(e ast.Expr) (*ast.CallExpr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || !mw1IsMemberOf(call.Fun, h.names, "New") {
		return nil, false
	}
	return call, true
}

// rewriteCall handles a call that registers the middleware. It reports whether
// the call, and everything inside it, has been dealt with.
func (h *hcMigration) rewriteCall(call *ast.CallExpr, stack []ast.Node) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != nameUse && sel.Sel.Name != appMethodGroup) {
		return false
	}

	idx := -1
	var news []*ast.CallExpr
	for i, arg := range call.Args {
		if newCall, isNew := h.isNewCall(arg); isNew {
			news = append(news, newCall)
			if idx < 0 {
				idx = i
			}
		}
	}
	if idx < 0 {
		h.reportHandlerVariable(call, sel)
		return false
	}

	switch {
	case len(news) > 1:
		h.refuseAll(call, news, stack, "it registers several healthcheck handlers at once")
		return true
	case sel.Sel.Name == appMethodGroup:
		h.refuse(news[0], stack, "a group cannot hold the probes of v3")
		return true
	}
	return h.rewriteUse(call, sel, idx, stack)
}

// routeMethods are the methods that register a handler on a path.
var routeMethods = strings.Fields("Get Head Post Put Delete Patch Options Connect Trace All Add Query")

// strayNew deals with a healthcheck.New() that is neither registered on a route nor
// handed to Use or Group, which those deal with where they register it: the handler
// is kept in a variable, returned or passed on, and v3 would answer every GET it
// receives. It moves to the middleware of v2 in the module package, or is reported.
func (h *hcMigration) strayNew(call *ast.CallExpr, stack []ast.Node) {
	if _, isNew := h.isNewCall(call); !isNew || len(stack) == 0 {
		return
	}

	switch stack[len(stack)-1].(type) {
	case *ast.AssignStmt, *ast.ValueSpec:
		// a Use that registers the variable reports it; a module with the
		// middleware of v2 has the handler of v2 in the variable
		if !h.hasV3Config(call) {
			h.useShim([]*ast.CallExpr{call}, stack, "the handler is kept in a variable")
		}
		return
	}

	if h.inRoute(call, stack) {
		// app.Get(path, healthcheck.New()), the form of v3, unless it is given the
		// config of v2
		if h.hasV2Config(call) {
			h.useShim([]*ast.CallExpr{call}, stack, "its config has fields of v2")
		}
		return
	}
	if h.hasV3Config(call) {
		h.reportUnmigrated(call, "it is not registered on a route here")
		return
	}
	h.refuse(call, stack, "it is not registered on a route here")
}

// reportHandlerVariable reports a Use that registers a variable holding the
// result of healthcheck.New.
func (h *hcMigration) reportHandlerVariable(call *ast.CallExpr, sel *ast.SelectorExpr) {
	if sel.Sel.Name != nameUse || h.shim != nil || h.proj.planning {
		return
	}
	for _, arg := range call.Args {
		id, ok := arg.(*ast.Ident)
		if !ok {
			continue
		}
		if h.variableHoldsNew(id) {
			h.reportUnmigrated(call, "the healthcheck handler is kept in the variable "+id.Name)
			return
		}
	}
}

// variableHoldsNew reports whether the package level or local variable id is
// initialized with a call of healthcheck.New.
func (h *hcMigration) variableHoldsNew(id *ast.Ident) bool {
	found := false
	h.g.walk(func(n ast.Node, _ []ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE && len(x.Lhs) == len(x.Rhs) {
				for i, lhs := range x.Lhs {
					if isIdentNamed(lhs, id.Name) {
						if _, isNew := h.isNewCall(x.Rhs[i]); isNew {
							found = true
						}
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range x.Names {
				if name.Name == id.Name && i < len(x.Values) {
					if _, isNew := h.isNewCall(x.Values[i]); isNew {
						found = true
					}
				}
			}
		}
		return !found
	})
	return found
}

// newAsValue deals with a reference to healthcheck.New that is not a call, such as
// a function value: the calls that go through it cannot be found, so they cannot be
// taken apart. A module with the middleware of v2 has already moved the reference.
func (h *hcMigration) newAsValue(sel *ast.SelectorExpr, stack []ast.Node) {
	if !h.useShim(nil, stack, "") {
		h.reportUnmigrated(sel, "it is used as a value, so its calls cannot be found")
	}
}

// refuseAll is refuse for the several calls of a statement; the note of a call that
// stays belongs to the statement.
func (h *hcMigration) refuseAll(at ast.Node, calls []*ast.CallExpr, stack []ast.Node, why string) {
	if !h.useShim(calls, stack, why) {
		h.reportUnmigrated(at, why)
	}
}

func (h *hcMigration) reportUnmigrated(at ast.Node, why string) {
	pkg := h.names[0]
	h.rep.notef(at,
		"%s.New() was left as it is: %s. In v3 it answers every GET and HEAD request that reaches it, not only /livez and /readyz, "+
			"so register one route per probe, app.Get(%s.LivenessEndpoint, %s.New(...)) and app.Get(%s.ReadinessEndpoint, %s.New(...))",
		pkg, why, pkg, pkg, pkg, pkg)
}

// hcProbe is one of the two probes of a v2 config.
type hcProbe struct {
	// endpoint is the path of the probe, the zero value for the default one.
	endpoint hcEndpoint
	elems    []hcElement
}

// hcEndpoint is the path a probe is registered on.
type hcEndpoint struct {
	// expr is the expression of the config it was read from.
	expr ast.Expr
	// text is the Go expression of the path, empty for the default path.
	text string
	// literal is the value of the path when it is a string literal.
	literal   string
	isLiteral bool
}

// hcElement is a field of a healthcheck.Config literal and the comment that
// trails it.
type hcElement struct {
	text    string
	comment string
}

// hcConfig is the v2 config split into the two v3 configs.
type hcConfig struct {
	liveness  hcProbe
	readiness hcProbe
	// multiline is whether the literal spans several lines.
	multiline bool
	// nextCode is set when Next is code (a function literal, a call) rather than
	// the name of a function: the config of a variable that is taken apart does
	// not repeat it, the config for the readiness probe refers to the Next of the
	// other one.
	nextCode bool
}

// rewriteUse replaces `X.Use(..., healthcheck.New(cfg), ...)` with one route per
// probe.
func (h *hcMigration) rewriteUse(call *ast.CallExpr, sel *ast.SelectorExpr, idx int, stack []ast.Node) bool {
	newCall, _ := h.isNewCall(call.Args[idx])

	prefix, handlersFrom, ok := h.prefixOf(call, idx, stack)
	if !ok {
		h.refuseAll(call, []*ast.CallExpr{newCall}, stack, "its first argument may be a path or a handler")
		return true
	}

	pkg := h.packageName(newCall.Fun)
	cfg, split, ok := h.configOf(newCall, stack)
	if !ok {
		return true
	}

	live := h.route(prefix, cfg.liveness.endpoint, pkg+".LivenessEndpoint")
	ready := h.route(prefix, cfg.readiness.endpoint, pkg+".ReadinessEndpoint")

	var calls []string
	if before := call.Args[handlersFrom:idx]; len(before) > 0 {
		calls = append(calls, "Use"+h.args(prefix, before))
	}
	calls = append(calls,
		"Get("+live+", "+pkg+".New("+hcConfigLiteral(pkg, cfg, cfg.liveness, split.liveness)+"))",
		"Get("+ready+", "+pkg+".New("+hcConfigLiteral(pkg, cfg, cfg.readiness, split.readiness)+"))",
	)
	if after := call.Args[idx+1:]; len(after) > 0 {
		calls = append(calls, "Use"+h.args(prefix, after))
	}

	h.edits.replaceNode(h.g, h.replacementTarget(call, sel, stack), h.joinCalls(call, sel, stack, calls))
	if split.apply != nil {
		split.apply()
	}
	return true
}

// packageName returns the identifier the qualified expression uses for the
// package.
func (h *hcMigration) packageName(qualified ast.Expr) string {
	x, _, _ := mw1Selector(qualified)
	if x == "" {
		return h.names[0]
	}
	return x
}

// args renders the arguments of a Use call: the path, when there is one, and
// the handlers.
func (h *hcMigration) args(prefix ast.Expr, handlers []ast.Expr) string {
	var parts []string
	if prefix != nil {
		parts = append(parts, h.textOf(prefix))
	}
	for _, handler := range handlers {
		parts = append(parts, h.textOf(handler))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// prefixOf returns the path a Use call mounts its handlers on, and the index of
// the first handler. Whether the first argument is the path or a handler is
// told by what it is; ok is false when that cannot be told.
func (h *hcMigration) prefixOf(call *ast.CallExpr, idx int, stack []ast.Node) (prefix ast.Expr, first int, ok bool) {
	if idx == 0 {
		return nil, 0, true
	}
	switch arg := call.Args[0].(type) {
	case *ast.BasicLit:
		if arg.Kind == token.STRING {
			return arg, 1, true
		}
	case *ast.CallExpr, *ast.FuncLit:
		return nil, 0, true
	case *ast.Ident:
		if _, isConst := mw1StringConsts(h.g.file)[arg.Name]; isConst {
			return arg, 1, true
		}
		if len(mw1FuncParamCounts(h.g.file, arg.Name)) > 0 {
			return nil, 0, true // a function used as a handler
		}
		if d, found := resolveIdent(arg.Name, arg.Pos(), stack); found {
			if isStringDecl(d) {
				return arg, 1, true
			}
			if d.typ != nil || d.init != nil {
				return nil, 0, true
			}
		}
	}
	return nil, 0, false
}

// isStringDecl reports whether the declaration holds a string: it has the type,
// or is initialized with a string literal.
func isStringDecl(d decl) bool {
	if d.typ != nil {
		return isIdentNamed(d.typ, "string")
	}
	lit, ok := d.init.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// route renders the path a probe is registered on: the path the Use call was
// mounted on followed by the endpoint of the probe.
func (h *hcMigration) route(prefix ast.Expr, endpoint hcEndpoint, fallback string) string {
	if prefix == nil {
		if endpoint.text == "" {
			return fallback
		}
		return endpoint.text
	}

	if p, ok := mw1StringLiteral(prefix); ok {
		p = strings.TrimRight(p, "/")
		if endpoint.text == "" {
			return strconv.Quote(p) + " + " + fallback
		}
		if endpoint.isLiteral {
			return strconv.Quote(p + endpoint.literal)
		}
		return strconv.Quote(p) + " + " + endpoint.text
	}

	if endpoint.text == "" {
		return h.textOf(prefix) + " + " + fallback
	}
	return h.textOf(prefix) + " + " + endpoint.text
}

// replacementTarget is the node a rewritten Use call replaces: the whole
// statement when the call is one, so that the probes become statements of
// their own.
func (h *hcMigration) replacementTarget(call *ast.CallExpr, sel *ast.SelectorExpr, stack []ast.Node) ast.Node {
	if h.isStatementCall(call, sel, stack) {
		return stack[len(stack)-1]
	}
	return call
}

// isStatementCall reports whether call is a statement that starts a line of a
// block and whose receiver can be written more than once, which means the call
// can be replaced with several statements.
func (h *hcMigration) isStatementCall(call *ast.CallExpr, sel *ast.SelectorExpr, stack []ast.Node) bool {
	if len(stack) < 2 || !isPlainPath(sel.X) {
		return false
	}
	stmt, ok := stack[len(stack)-1].(*ast.ExprStmt)
	if !ok || stmt.X != ast.Expr(call) {
		return false
	}
	switch stack[len(stack)-2].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		return mw1StartsLine(h.g.src, h.g.offset(stmt.Pos()))
	}
	return false
}

// isPlainPath reports whether e is an identifier or a selector chain of
// identifiers: an expression without a call, whose evaluation can be repeated.
func isPlainPath(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return isPlainPath(x.X)
	case *ast.ParenExpr:
		return isPlainPath(x.X)
	}
	return false
}

// joinCalls renders the calls on the receiver: one statement each, or a chain
// when the Use call is part of an expression.
func (h *hcMigration) joinCalls(call *ast.CallExpr, sel *ast.SelectorExpr, stack []ast.Node, calls []string) string {
	recv := h.textOf(sel.X)
	if h.isStatementCall(call, sel, stack) {
		indent := mw1Indent(h.g.src, h.g.offset(call.Pos()))
		stmts := make([]string, len(calls))
		for i, c := range calls {
			stmts[i] = recv + "." + c
		}
		return strings.Join(stmts, "\n"+indent)
	}
	return recv + "." + strings.Join(calls, ".")
}

// hcSplit says how the config of a variable is split: the literals that replace
// the declaration, and the edits that do it.
type hcSplit struct {
	apply     func()
	liveness  string
	readiness string
}

// configOf splits the argument of healthcheck.New into the two probes.
func (h *hcMigration) configOf(newCall *ast.CallExpr, stack []ast.Node) (cfg hcConfig, split hcSplit, ok bool) {
	switch len(newCall.Args) {
	case 0:
		return cfg, split, true
	case 1:
	default:
		h.refuse(newCall, stack, "it is called with more than one config")
		return cfg, split, false
	}

	switch arg := newCall.Args[0].(type) {
	case *ast.CompositeLit:
		if _, isConfig := mw1ConfigLit(arg, h.names); !isConfig {
			h.refuse(newCall, stack, "its config is not a healthcheck.Config literal")
			return cfg, split, false
		}
		cfg, ok = h.parseConfig(arg)
		if !ok {
			h.reportUnmigrated(newCall, "its config has fields it does not know")
		}
		return cfg, split, ok
	case *ast.Ident:
		return h.variableConfig(newCall, arg, stack)
	}

	h.refuse(newCall, stack, "its config is not a literal")
	return cfg, split, false
}

// parseConfig reads a healthcheck.Config literal of v2.
func (h *hcMigration) parseConfig(lit *ast.CompositeLit) (cfg hcConfig, ok bool) {
	cfg.multiline = mw1MultiLine(h.g, lit)
	var next *hcElement
	for _, elt := range lit.Elts {
		kv, isKV := elt.(*ast.KeyValueExpr)
		if !isKV {
			return cfg, false
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent {
			return cfg, false
		}

		value := h.textOf(kv.Value)
		comment := h.trailingComment(kv)
		switch key.Name {
		case hcNext:
			next = &hcElement{text: "Next: " + value, comment: comment}
			cfg.nextCode = !isPlainPath(kv.Value)
		case hcLivenessProbe:
			cfg.liveness.elems = append(cfg.liveness.elems, hcElement{text: "Probe: " + value, comment: comment})
		case hcReadinessProbe:
			cfg.readiness.elems = append(cfg.readiness.elems, hcElement{text: "Probe: " + value, comment: comment})
		case hcLivenessEndpoint:
			cfg.liveness.endpoint = h.endpointOf(kv.Value)
		case hcReadinessEndpoint:
			cfg.readiness.endpoint = h.endpointOf(kv.Value)
		default:
			return cfg, false
		}
	}

	if next != nil {
		cfg.liveness.elems = append([]hcElement{*next}, cfg.liveness.elems...)
		cfg.readiness.elems = append([]hcElement{*next}, cfg.readiness.elems...)
	}
	return cfg, true
}

// endpointOf describes the value of an endpoint field: the empty string that v2
// replaced with the default is no path at all.
func (h *hcMigration) endpointOf(e ast.Expr) hcEndpoint {
	s, isLiteral := mw1StringLiteral(e)
	if isLiteral && s == "" {
		return hcEndpoint{}
	}
	return hcEndpoint{expr: e, text: h.textOf(e), literal: s, isLiteral: isLiteral}
}

// trailingComment returns the line comment that follows the field.
func (h *hcMigration) trailingComment(kv *ast.KeyValueExpr) string {
	src := h.g.src
	end := h.g.offset(kv.End())
	j := mw1SkipSpaces(src, end)
	if j < len(src) && src[j] == ',' {
		j++
	}
	if commentEnd := mw1TrailingComment(src, j); commentEnd > j {
		return strings.TrimSpace(src[mw1SkipSpaces(src, j):commentEnd])
	}
	return ""
}

// configLiteral renders the config of one probe, empty for the defaults.
func hcConfigLiteral(pkg string, cfg hcConfig, probe hcProbe, override string) string {
	if override != "" {
		return override
	}
	if len(probe.elems) == 0 {
		return ""
	}

	parts := make([]string, len(probe.elems))
	for i, e := range probe.elems {
		parts[i] = e.text
		if cfg.multiline {
			parts[i] += ","
			if e.comment != "" {
				parts[i] += " " + e.comment
			}
		}
	}
	if cfg.multiline {
		return pkg + ".Config{\n" + strings.Join(parts, "\n") + "\n}"
	}
	return pkg + ".Config{" + strings.Join(parts, ", ") + "}"
}

// variableConfig handles `healthcheck.New(cfg)` where cfg is a variable: a
// config without a probe or an endpoint is used for both routes as it is, any
// other is split at its declaration when nothing else uses it. The declaration
// is in this file or, for a variable of the package, in another one.
func (h *hcMigration) variableConfig(newCall *ast.CallExpr, id *ast.Ident, stack []ast.Node) (cfg hcConfig, split hcSplit, ok bool) {
	d, found := resolveIdent(id.Name, id.Pos(), stack)
	if !found {
		dh, lit, why, unreadable := h.packageDeclaration(id.Name)
		if lit == nil {
			if unreadable {
				h.reportUnmigrated(newCall, "its config "+id.Name+" "+why)
			} else {
				h.refuse(newCall, stack, "its config "+id.Name+" "+why)
			}
			return cfg, split, false
		}
		return h.splitVariable(newCall, id, dh, lit, stack)
	}

	lit, isLit := d.init.(*ast.CompositeLit)
	if !isLit {
		h.refuse(newCall, stack, "its config "+id.Name+" is not declared next to it")
		return cfg, split, false
	}
	return h.splitVariable(newCall, id, h, lit, stack)
}

// splitVariable plans the routes of `healthcheck.New(id)` for the config that the
// literal lit declares. dh is the rewrite of the file that holds the literal: the
// one of this file, or of another file of the package, whose declaration is then
// split when the project is written.
func (h *hcMigration) splitVariable(newCall *ast.CallExpr, id *ast.Ident, dh *hcMigration, lit *ast.CompositeLit, stack []ast.Node) (cfg hcConfig, split hcSplit, ok bool) {
	name := id.Name
	subject := "its config " + name
	if dh != h {
		subject += " (declared in " + h.relPath(dh.fi.Path) + ")"
	}
	// refuse moves the call to the middleware of v2 when the module has it; keep
	// leaves the call, because the config is not one that the module package could
	// take
	refuse := func(why string) (hcConfig, hcSplit, bool) {
		h.refuse(newCall, stack, subject+" "+why)
		return hcConfig{}, hcSplit{}, false
	}
	keep := func(why string) (hcConfig, hcSplit, bool) {
		h.reportUnmigrated(newCall, subject+" "+why)
		return hcConfig{}, hcSplit{}, false
	}

	if _, isConfig := mw1ConfigLit(lit, dh.names); !isConfig {
		return refuse("is not a healthcheck.Config literal")
	}
	parsed, valid := dh.parseConfig(lit)
	if !valid {
		return keep("has fields it does not know")
	}

	// whoever else can mention the variable: a variable of the package is visible
	// in all of its files
	sources := []*goSource{h.g}
	packageLevel := hcPackageVar(dh.g.file, name) != nil
	if pkg := h.pkg(); packageLevel && pkg.complete {
		sources = pkg.sources()
	}
	for _, src := range sources {
		if field := hcAssignedV2Field(src, name); field != "" {
			return refuse("gets " + field + " assigned after its declaration" + hcByHand)
		}
	}
	if !hasV2OnlyField(lit) {
		// nothing to split: both routes can share it
		return hcConfig{}, hcSplit{liveness: name, readiness: name}, true
	}

	count := 0
	for _, src := range sources {
		count += countIdent(src.file, name)
	}
	switch {
	case count != 2:
		return refuse("is used in more places than this one" + hcByHand)
	case packageLevel && ast.IsExported(name) && h.g.file.Name.Name != "main":
		return refuse("is exported and may be used by other packages" + hcByHand)
	}
	if dh != h {
		if _, visited := h.proj.files[dh.fi.Path]; !visited {
			return keep("is in a file that is left out of the migration")
		}
	}

	readiness := h.readinessName(name)
	apply, ok := dh.splitDeclaration(lit, parsed, readiness)
	if !ok {
		return refuse("is declared in a way that cannot be split")
	}
	if dh != h {
		pkgName := h.packageName(newCall.Fun)
		if parsed.liveness.endpoint, ok = portableEndpoint(parsed.liveness.endpoint, dh, pkgName, stack); !ok {
			return refuse("has an endpoint that cannot be written in this file")
		}
		if parsed.readiness.endpoint, ok = portableEndpoint(parsed.readiness.endpoint, dh, pkgName, stack); !ok {
			return refuse("has an endpoint that cannot be written in this file")
		}
		apply = func() { h.proj.addSplit(dh.fi.Path, name, readiness) }
	}

	split = hcSplit{liveness: name, readiness: readiness, apply: apply}
	// the endpoints are still needed to register the routes
	parsed.liveness.elems = nil
	parsed.readiness.elems = nil
	return parsed, split, true
}

// readinessName chooses the name of the config of the readiness probe that
// replaces the config called name: no identifier of the package may have it.
func (h *hcMigration) readinessName(name string) string {
	readiness := name + "Readiness"
	for i := 2; h.nameTaken(readiness); i++ {
		readiness = name + "Readiness" + strconv.Itoa(i)
	}
	return readiness
}

// nameTaken reports whether the file, or another file of its package, mentions
// an identifier called name.
func (h *hcMigration) nameTaken(name string) bool {
	return mw1UsesName(h.g.file, name) || h.proj.packageOf(h.fi.Path, h.g.file.Name.Name).mentions(name)
}

// hcAssignedV2Field returns the first field of v2 that the file assigns to a
// variable of that name, as in `cfg.LivenessProbe = probe`, or "".
func hcAssignedV2Field(g *goSource, name string) string {
	field := ""
	g.walk(func(n ast.Node, _ []ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || field != "" {
			return field == ""
		}
		for _, lhs := range as.Lhs {
			sel, isSel := lhs.(*ast.SelectorExpr)
			if isSel && isIdentNamed(sel.X, name) && slices.Contains(hcV2OnlyFields, sel.Sel.Name) {
				field = sel.Sel.Name
			}
		}
		return true
	})
	return field
}

func hasV2OnlyField(lit *ast.CompositeLit) bool {
	for _, name := range hcV2OnlyFields {
		if mw1KeyValue(lit, name) != nil {
			return true
		}
	}
	return false
}

// countIdent counts the identifiers called name that stand for a variable or
// a declaration, not for a field or a method.
func countIdent(file *ast.File, name string) int {
	count := 0
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(x.X, visit)
			return false
		case *ast.KeyValueExpr:
			if !isIdentNamed(x.Key, name) {
				ast.Inspect(x.Key, visit)
			}
			ast.Inspect(x.Value, visit)
			return false
		case *ast.Ident:
			if x.Name == name {
				count++
			}
		}
		return true
	}
	ast.Inspect(file, visit)
	return count
}

// splitDeclaration schedules the edits that turn the declaration of a config
// into the declarations of the two configs. The declaration keeps its name for
// the liveness config.
func (h *hcMigration) splitDeclaration(lit *ast.CompositeLit, cfg hcConfig, readiness string) (apply func(), ok bool) {
	var parent, grand ast.Node
	h.g.walk(func(n ast.Node, stack []ast.Node) bool {
		if n == ast.Node(lit) && len(stack) >= 2 {
			parent, grand = stack[len(stack)-1], stack[len(stack)-2]
		}
		return parent == nil
	})

	pkg := h.packageName(lit.Type)
	empty := pkg + ".Config{}"
	liveText := hcConfigLiteral(pkg, cfg, cfg.liveness, "")
	if liveText == "" {
		liveText = empty
	}
	// readyText writes the config of the readiness probe; name is the variable that
	// keeps the one of the liveness probe
	readyText := func(name string) string {
		ready := cfg.readiness
		if cfg.nextCode && len(ready.elems) > 0 {
			ready.elems = slices.Clone(ready.elems)
			ready.elems[0] = hcElement{text: "Next: " + name + ".Next"}
		}
		if text := hcConfigLiteral(pkg, cfg, ready, ""); text != "" {
			return text
		}
		return empty
	}

	switch p := parent.(type) {
	case *ast.AssignStmt:
		if p.Tok != token.DEFINE || len(p.Lhs) != 1 || len(p.Rhs) != 1 {
			return nil, false
		}
		id, isIdent := p.Lhs[0].(*ast.Ident)
		if _, inBlock := grand.(*ast.BlockStmt); !inBlock || !isIdent {
			return nil, false
		}
		indent := mw1Indent(h.g.src, h.g.offset(p.Pos()))
		return func() {
			h.split[lit] = true
			h.edits.replaceNode(h.g, lit, liveText)
			end := h.g.offset(p.End())
			h.edits.replace(end, end, "\n"+indent+readiness+" := "+readyText(id.Name))
		}, true
	case *ast.ValueSpec:
		gd, isDecl := grand.(*ast.GenDecl)
		if !isDecl || len(p.Names) != 1 || len(p.Values) != 1 {
			return nil, false
		}
		typ := ""
		if p.Type != nil {
			typ = " " + h.g.text(p.Type)
		}
		return func() {
			h.split[lit] = true
			h.edits.replaceNode(h.g, lit, liveText)
			if p.Type != nil {
				h.dropRename(p.Type)
			}
			if gd.Lparen.IsValid() {
				end := h.g.offset(p.End())
				h.edits.replace(end, end, "\n"+readiness+typ+" = "+readyText(p.Names[0].Name))
				return
			}
			end := h.g.offset(gd.End())
			h.edits.replace(end, end, "\n\nvar "+readiness+typ+" = "+readyText(p.Names[0].Name))
		}, true
	}
	return nil, false
}

// collectV2Config remembers a healthcheck.Config literal that still has fields
// of v2.
func (h *hcMigration) collectV2Config(lit *ast.CompositeLit) {
	if _, isConfig := mw1ConfigLit(lit, h.names); isConfig && hasV2OnlyField(lit) {
		h.v2Configs = append(h.v2Configs, lit)
	}
}

// reportLeftovers reports the literals with fields of v2 that no rewrite took
// apart. A literal that belongs to a rewritten Use call is not visited, its
// children are skipped.
func (h *hcMigration) reportLeftovers() {
	if h.shim != nil {
		return // the literals name the config of the module package
	}
	pkg := h.names[0]
	for _, lit := range h.v2Configs {
		if h.split[lit] {
			continue
		}
		h.rep.notef(lit,
			"%s.Config has no LivenessProbe, ReadinessProbe, LivenessEndpoint or ReadinessEndpoint in v3, where each probe is a handler of its own with a single Probe: "+
				"register one route per probe, app.Get(<its endpoint, /livez or /readyz by default>, %s.New(%s.Config{Probe: ...})) (the literal was left as it is)",
			pkg, pkg, pkg)
	}
}
