package v3

import (
	"go/ast"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// appShimWork is what carrying the settings of one source takes: the edits and
// imports of every file it touches, kept apart from the plan until the work is
// known to be possible.
type appShimWork struct {
	pl      *appPlanner
	edits   map[*appFile]*editSet
	imports map[*appFile]*appImports
	// path is the import path of the package of the registry.
	path string
	// notes are what the user is told about the work once it is done.
	notes []appShimNoteAt
	// wraps are the literals the work wraps in the config of the registry.
	wraps []*ast.CompositeLit
}

// appShimNoteAt is a note about a place of a file.
type appShimNoteAt struct {
	file *appFile
	text string
	line int
}

// note schedules a note for the line of the file.
func (w *appShimWork) note(f *appFile, line int, text string) {
	w.notes = append(w.notes, appShimNoteAt{file: f, line: line, text: text})
}

func newShimWork(pl *appPlanner, path string) *appShimWork {
	return &appShimWork{pl: pl, edits: map[*appFile]*editSet{}, imports: map[*appFile]*appImports{}, path: path}
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

// qualifier is the prefix that names the package of the registry in f, importing
// it when f does not.
func (w *appShimWork) qualifier(f *appFile) string {
	return w.importsOf(f).qualifier(f, w.path, moduleShimPkg)
}

func (w *appShimWork) commit() {
	for f, es := range w.edits {
		fp := w.pl.plan.fileOf(f)
		fp.edits.edits = append(fp.edits.edits, es.edits...)
	}
	for f, imps := range w.imports {
		w.pl.plan.fileOf(f).imps = *imps
	}
	for _, n := range w.notes {
		internal.Notef(n.file.fi, n.line, "%s", n.text)
	}
	for _, lit := range w.wraps {
		w.pl.wrapped[lit] = true
	}
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
	src.noteOrder(w, drop)
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
		shimCreation(w, c, name)
	}
	return ""
}

// shimLiteral plans the settings of a config that is a literal handed to
// fiber.New: they become the fiber.ListenConfig that fibermigrate.NewApp takes
// next to it.
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
	src.noteOrder(w, drop)
	shimCreation(w, c, w.importsOf(f).fiberQualifier(f)+"ListenConfig{"+strings.Join(held, ", ")+"}")
	return ""
}

// noteOrder tells when the settings that leave the literal of the source, whose
// elements are at the indexes in drop, are evaluated in another order than v2
// evaluated them: they are evaluated after the rest of the config now.
func (src *appListenSource) noteOrder(w *appShimWork, drop []int) {
	if src.lit == nil || len(drop) == 0 {
		return
	}
	if note := src.file.orderNote(src.lit, drop, false); note != "" {
		w.note(src.file, src.file.g.line(src.lit.Pos()), note)
	}
}

// shimCreation turns the fiber.New call c into the call that records the
// settings, the expression listenCfg, for the app.
func shimCreation(w *appShimWork, c appNew, listenCfg string) {
	g := c.file.g
	edits := w.editsOf(c.file)
	edits.replaceNode(g, c.call.Fun, w.qualifier(c.file)+appShimNew)
	at := g.offset(c.call.Args[0].End())
	edits.replace(at, at, ", "+listenCfg)
}

// rewriteShim turns the call that starts an app into the form that asks the
// registry for the settings of the app. A receiver that is a plain variable or
// field is named again in the call; any other receiver goes to a function of the
// registry, so that the expression is evaluated once. receiver is the text of that
// receiver when other edits changed it, empty to take it from the source.
func (s appStart) rewriteShim(imports appImports, receiver, shimPath string) appStartRewrite {
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
	recv, ok := s.file.appText(recv, s.sel.X)
	if !ok {
		out.problem = "its receiver is not a *fiber.App but a type that holds one in a way the registry cannot name"
		return out
	}

	args := s.call.Args
	var cfg string // the config the call already has, empty when it has none
	switch s.method {
	case appNameListen, appNameListener:
		switch len(args) {
		case 1:
		case 2:
			if s.file.asksRegistry(args[1], shimPath) {
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

	shim := out.imports.qualifier(s.file, shimPath, moduleShimPkg)
	addr := g.text(args[0])
	if !appPureReceiver(s.sel.X) {
		helper := shim + appShimListen
		if s.method == appNameListener {
			helper = shim + appShimListener
		}
		call := helper + "(" + recv + ", " + addr
		if cfg != "" {
			call += ", " + cfg
		}
		out.edits = append(out.edits, textEdit{start: g.offset(s.call.Pos()), end: g.offset(s.call.End()), text: call + ")"})
		return out
	}

	ask := shim + appShimConfig + "(" + recv
	if cfg != "" {
		ask += ", " + cfg
	}
	ask += ")"

	end := g.offset(args[len(args)-1].End())
	switch tls := slices.Contains(appTLSMethods, s.method); {
	case !tls && len(args) == 1:
		out.edits = append(out.edits, textEdit{start: end, end: end, text: ", " + ask})
	case !tls:
		out.edits = append(out.edits, textEdit{start: g.offset(args[1].Pos()), end: end, text: ask})
	default:
		out.edits = append(out.edits, textEdit{start: g.offset(s.sel.Sel.Pos()), end: end, text: appNameListen + "(" + addr + ", " + ask})
	}
	return out
}

// asksRegistry reports whether e is a call of ListenConfig of the package of the
// registry, which a start call already holds when an earlier run migrated it.
func (f *appFile) asksRegistry(e ast.Expr, shimPath string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != appShimConfig {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && f.importPathOf(id.Name) == shimPath
}

// appValueInfo is a method that starts an app, used as a value (start := app.Listen),
// and where the app comes from.
type appValueInfo struct {
	file    *appFile
	sel     *ast.SelectorExpr
	method  string
	origins appOrigins
}

// rewrite turns the method value into the function of the registry that holds the
// settings of the app: app.Listen is fibermigrate.ListenFunc(app), which has the
// type the method value had in v2.
func (v *appValueInfo) rewrite(imps *appImports, receiver, shimPath string) []textEdit {
	g := v.file.g
	recv, ok := v.file.appText(receiver, v.sel.X)
	if !ok {
		return nil
	}

	name := appShimListenFunc
	if v.method == appNameListener {
		name = appShimListenerFunc
	}
	text := imps.qualifier(v.file, shimPath, moduleShimPkg) + name + "(" + recv + ")"
	return []textEdit{{start: g.offset(v.sel.Pos()), end: g.offset(v.sel.End()), text: text}}
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

// appText returns the expression that is the *fiber.App held by the value e, whose
// source text is text: the text itself for an app, the embedded field for a type of
// the project that embeds one.
func (f *appFile) appText(text string, e ast.Expr) (string, bool) {
	if f.isPlainApp(e) {
		return text, true
	}
	field := f.embeddedAppField(e)
	if field == "" {
		return "", false
	}
	return text + "." + field, true
}

// isPlainApp reports whether e is a *fiber.App itself rather than a type of the
// project that embeds one, whose value the registry cannot take.
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
		name = preferred + strconv.Itoa(i)
	}
	return name
}
