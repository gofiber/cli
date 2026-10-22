package v3

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// reFilesystemPath matches the import path of the filesystem middleware at any
// major version, and reStaticPath that of the static middleware.
var (
	reFilesystemPath = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/filesystem$`)
	reStaticPath     = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/static$`)
)

// What the filesystem package exports, and the middleware packages name a
// config type alike.
const (
	mw3New           = "New"
	mw3Config        = "Config"
	mw3ConfigDefault = "ConfigDefault"
	mw3SendFile      = "SendFile"
)

// The packages the rewrite writes in the files it migrates.
const (
	mw3StaticPath = "github.com/gofiber/fiber/v3/middleware/static"
	mw3FiberPath  = "github.com/gofiber/fiber/v3"
)

// mw3FSRef is a use of the filesystem package: filesystem.member, or the bare
// member where the package is dot imported.
type mw3FSRef struct {
	// expr is the whole reference, qual the identifier of the package in it (nil
	// for a dot import) and stack the ancestors of expr.
	expr   ast.Expr
	qual   *ast.Ident
	member string
	stack  []ast.Node
}

// parent returns the node that holds the reference.
func (r mw3FSRef) parent() ast.Node {
	if len(r.stack) == 0 {
		return nil
	}
	return r.stack[len(r.stack)-1]
}

// mw3FSAssign is an assignment, kept for the second look at the config
// variables it sets.
type mw3FSAssign struct {
	as    *ast.AssignStmt
	stack []ast.Node
}

// mw3FSPass is the rewrite of one file by MigrateFilesystemMiddleware.
type mw3FSPass struct {
	g    *goSource
	pkgs *mw3Pkgs
	// pkg is what the package of the file declares, read when first needed.
	pkg *mw3Pkg
	// declared is every name the file declares.
	declared map[string]struct{}
	// litCalls are the filesystem.New calls that take a Config literal, found
	// through the variable that holds it.
	litCalls map[*ast.CompositeLit][]*ast.CallExpr
	// prefix is the PathPrefix of the config a call takes, the first argument
	// of its static.New.
	prefix  map[*ast.CallExpr]string
	edits   editSet
	imports mw3ImportEdits
	fi      internal.FileInfo
	// static is the identifier the file uses for the static package afterwards,
	// osName, stringsName and fiberName those of the packages the output needs.
	static      string
	osName      string
	fsName      string
	stringsName string
	fiberName   string
	refs        []mw3FSRef
	assigns     []mw3FSAssign
	// names are the identifiers the file binds to the filesystem package, http
	// and fiber the ones for net/http and the fiber package.
	names []string
	http  []string
	fiber []string
	dot   bool
}

func mw3FilesystemContent(src string, fi internal.FileInfo, pkgs *mw3Pkgs) string {
	if !strings.Contains(src, "middleware/filesystem") {
		return src
	}
	names := importNames(src, reFilesystemPath, "filesystem")
	if len(names) == 0 {
		return src
	}
	g, ok := parseFile(fi, src)
	if !ok {
		return src
	}

	p := &mw3FSPass{
		g:        g,
		fi:       fi,
		pkgs:     pkgs,
		names:    slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == "." }),
		dot:      slices.Contains(names, "."),
		http:     importNames(src, mw3PathRegexp("net/http")),
		fiber:    fiberNames(src),
		declared: mw3DeclaredNames(g.file),
		litCalls: map[*ast.CompositeLit][]*ast.CallExpr{},
		prefix:   map[*ast.CallExpr]string{},
	}
	p.collect()
	p.chooseStaticName()
	p.rewriteImport()

	// The literals first: the PathPrefix of a config becomes the argument of
	// the New call that takes it.
	for _, ref := range p.refs {
		if lit, ok := ref.parent().(*ast.CompositeLit); ok && ref.member == mw3Config && lit.Type == ref.expr {
			p.configLit(lit, ref.stack)
		}
	}
	for _, ref := range p.refs {
		p.reference(ref)
	}
	for _, a := range p.assigns {
		p.assign(a)
	}

	out := p.edits.apply(src)
	if out == src {
		return src
	}
	return p.imports.apply(out)
}

// collect finds every use of the filesystem package in the file, and the
// New calls that take a config held in a variable.
func (p *mw3FSPass) collect() {
	p.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && p.isPackage(id, stack) {
				p.refs = append(p.refs, mw3FSRef{expr: x, qual: id, member: x.Sel.Name, stack: slices.Clone(stack)})
			}
		case *ast.Ident:
			if p.dot && mw3IsDotMember(x, stack) {
				p.refs = append(p.refs, mw3FSRef{expr: x, member: x.Name, stack: slices.Clone(stack)})
			}
		case *ast.AssignStmt:
			p.assigns = append(p.assigns, mw3FSAssign{as: x, stack: slices.Clone(stack)})
		}
		return true
	})

	for _, ref := range p.refs {
		call, ok := ref.parent().(*ast.CallExpr)
		if !ok || ref.member != mw3New || call.Fun != ref.expr || len(call.Args) == 0 {
			continue
		}
		if lit := p.configLiteralOf(call.Args[0], ref.stack); lit != nil {
			p.litCalls[lit] = append(p.litCalls[lit], call)
		}
	}
}

// isPackage reports whether the identifier names the filesystem package here
// and not a variable that has the same name.
func (p *mw3FSPass) isPackage(id *ast.Ident, stack []ast.Node) bool {
	if !slices.Contains(p.names, id.Name) {
		return false
	}
	_, shadowed := resolveIdent(id.Name, id.Pos(), stack)
	return !shadowed
}

// mw3FSMembers are what the filesystem package exports.
var mw3FSMembers = []string{mw3New, mw3Config, mw3ConfigDefault, mw3SendFile}

// isDotMember reports whether the identifier is a name that a dot import of the
// filesystem package brings in, used as such.
func mw3IsDotMember(id *ast.Ident, stack []ast.Node) bool {
	if !slices.Contains(mw3FSMembers, id.Name) || len(stack) == 0 {
		return false
	}
	switch parent := stack[len(stack)-1].(type) {
	case *ast.SelectorExpr:
		if parent.Sel == id {
			return false
		}
	case *ast.KeyValueExpr:
		if parent.Key == ast.Expr(id) {
			return false
		}
	case *ast.Field:
		if slices.Contains(parent.Names, id) {
			return false
		}
	case *ast.ValueSpec:
		if slices.Contains(parent.Names, id) {
			return false
		}
	case *ast.FuncDecl:
		if parent.Name == id {
			return false
		}
	case *ast.TypeSpec:
		if parent.Name == id {
			return false
		}
	}
	_, declared := resolveIdent(id.Name, id.Pos(), stack)
	return !declared
}

// configLiteralOf returns the filesystem Config literal that e is, or that the
// variable e names was declared with.
func (p *mw3FSPass) configLiteralOf(e ast.Expr, stack []ast.Node) *ast.CompositeLit {
	if id, ok := e.(*ast.Ident); ok {
		d, found := resolveIdent(id.Name, id.Pos(), stack)
		if !found || d.init == nil {
			return nil
		}
		e = d.init
	}
	if lit, ok := e.(*ast.CompositeLit); ok && lit.Type != nil && p.isMember(lit.Type, mw3Config, stack) {
		return lit
	}
	return nil
}

// isMember reports whether e is filesystem.member.
func (p *mw3FSPass) isMember(e ast.Expr, member string, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && x.Sel.Name == member && p.isPackage(id, stack)
	case *ast.Ident:
		if !p.dot || x.Name != member {
			return false
		}
		_, declared := resolveIdent(x.Name, x.Pos(), stack)
		return !declared
	}
	return false
}

// isHTTP reports whether e is net/http.member.
func (p *mw3FSPass) isHTTP(e ast.Expr, member string, stack []ast.Node) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != member {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || !slices.Contains(p.http, id.Name) {
		return false
	}
	_, shadowed := resolveIdent(id.Name, id.Pos(), stack)
	return !shadowed
}

// chooseStaticName decides which identifier the static package goes by.
func (p *mw3FSPass) chooseStaticName() {
	src := p.g.src

	// A file that has the static middleware already keeps that name.
	if names := importNames(src, reStaticPath, "static"); len(names) > 0 {
		p.static = names[0]
		return
	}

	// An import with a name of its own keeps it: the code says fs.New already.
	for _, spec := range p.g.file.Imports {
		if path, err := strconv.Unquote(spec.Path.Value); err == nil && reFilesystemPath.MatchString(path) && spec.Name != nil && spec.Name.Name != "_" {
			p.static = spec.Name.Name
			return
		}
	}

	if p.pkg == nil {
		p.pkg = p.pkgs.of(p.fi, p.g.file.Name.Name)
	}
	p.static = mw3FreeName(func(name string) bool {
		if _, taken := p.declared[name]; taken {
			return true
		}
		_, taken := p.pkg.names[name]
		return taken
	}, "static", "staticmw", "fiberstatic")
}

// rewriteImport points the filesystem import at the static package. A file
// that imports static already just loses the filesystem import.
func (p *mw3FSPass) rewriteImport() {
	staticImported := len(importNames(p.g.src, reStaticPath, "static")) > 0
	for _, spec := range p.g.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !reFilesystemPath.MatchString(path) || (spec.Name != nil && spec.Name.Name == "_") {
			continue
		}
		if staticImported {
			p.imports.dropIfUnused(path)
			continue
		}

		text := strconv.Quote(mw3StaticPath)
		switch {
		case spec.Name != nil:
			text = spec.Name.Name + " " + text
		case p.static != "static":
			text = p.static + " " + text
		}
		p.edits.replaceNode(p.g, spec, text)
		p.imports.dropIfUnused(mw3StaticPath)
	}
}

// qualifier renames the package a reference names to the static one.
func (p *mw3FSPass) qualifier(ref mw3FSRef) {
	if ref.qual != nil && ref.qual.Name != p.static {
		p.edits.replaceNode(p.g, ref.qual, p.static)
	}
}

// reference migrates one use of the filesystem package.
func (p *mw3FSPass) reference(ref mw3FSRef) {
	g := p.g
	switch call, isCall := ref.parent().(*ast.CallExpr); {
	case ref.member == mw3New && isCall && call.Fun == ref.expr:
		p.newCall(call, ref)
	case ref.member == mw3SendFile && isCall && call.Fun == ref.expr:
		p.sendFile(call, ref)
	case ref.member == mw3New || ref.member == mw3SendFile:
		p.qualifier(ref)
		internal.Notef(p.fi, g.line(ref.expr.Pos()),
			"%s is not called here, so it could not be migrated: static.New takes the directory to serve first and Ctx.SendFile replaces filesystem.SendFile.", g.text(ref.expr))
	default:
		p.qualifier(ref)
	}
}

// newCall migrates filesystem.New(cfg) to static.New("", cfg).
func (p *mw3FSPass) newCall(call *ast.CallExpr, ref mw3FSRef) {
	p.qualifier(ref)

	prefix := `""`
	if text, ok := p.prefix[call]; ok {
		prefix = text
	}
	if len(call.Args) == 0 {
		p.edits.replace(p.g.offset(call.Lparen)+1, p.g.offset(call.Rparen), prefix)
		return
	}
	// The parenthesis is replaced rather than an insertion made before the first
	// argument: the qualifier of that argument may be renamed at the same offset.
	open := p.g.offset(call.Lparen)
	p.edits.replace(open, open+1, "("+prefix+", ")
}

// sendFile migrates filesystem.SendFile(c, fsys, path) to
// c.SendFile(path, fiber.SendFile{FS: ...}).
func (p *mw3FSPass) sendFile(call *ast.CallExpr, ref mw3FSRef) {
	g := p.g
	if len(call.Args) != 3 || call.Ellipsis.IsValid() {
		p.qualifier(ref)
		internal.Notef(p.fi, g.line(call.Pos()),
			"filesystem.SendFile(c, fsys, path) is c.SendFile(path, fiber.SendFile{FS: fsys}) in v3, with an fs.FS for fsys: %s could not be migrated.", g.text(call))
		return
	}

	fsys := p.rootFS(call.Args[1], ref.stack, call.Args[1], "the file system")
	recv := g.text(call.Args[0])
	switch call.Args[0].(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr, *ast.ParenExpr, *ast.IndexExpr:
	default:
		recv = "(" + recv + ")"
	}
	p.edits.replaceNode(g, call, recv+".SendFile("+g.text(call.Args[2])+", "+p.fiberQualifier()+"SendFile{FS: "+fsys+"})")
	p.imports.releaseIn(g, call)
}

// fiberQualifier returns what is written before a name of the fiber package.
func (p *mw3FSPass) fiberQualifier() string {
	if p.fiberName == "" {
		for _, name := range p.fiber {
			if name != "_" {
				p.fiberName = name
				break
			}
		}
		if p.fiberName == "" {
			p.fiberName = mw3FreeName(func(name string) bool {
				_, taken := p.declared[name]
				return taken
			}, "fiber", "fiberv3")
			p.imports.require(mw3FiberPath, p.fiberName)
		}
	}
	if p.fiberName == "." {
		return ""
	}
	return p.fiberName + "."
}

// osQualifier returns the identifier of the os package, importing it if needed.
func (p *mw3FSPass) osQualifier() string {
	if p.osName == "" {
		p.osName = mw3ImportName(p.g.src, "os")
		if p.osName == "" {
			p.osName = mw3FreeName(func(name string) bool {
				_, taken := p.declared[name]
				return taken
			}, "os", "stdos")
			p.imports.require("os", p.osName)
		}
	}
	return p.osName
}

// stringsQualifier returns the identifier of the strings package.
func (p *mw3FSPass) stringsQualifier() string {
	if p.stringsName == "" {
		p.stringsName = mw3ImportName(p.g.src, "strings")
		if p.stringsName == "" {
			p.stringsName = mw3FreeName(func(name string) bool {
				_, taken := p.declared[name]
				return taken
			}, "strings", "stdstrings")
			p.imports.require("strings", p.stringsName)
		}
	}
	return p.stringsName
}

// rootFS returns the fs.FS expression for a v2 Root: os.DirFS for http.Dir, the
// file system itself for http.FS. Any other http.FileSystem has no fs.FS in
// the standard library: it is wrapped in an adapter the package gets, and when
// the package cannot have one the expression is kept for the build to refuse.
// at is where the note about it goes and what says what the expression was.
func (p *mw3FSPass) rootFS(e ast.Expr, stack []ast.Node, at ast.Node, what string) string {
	g := p.g
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 && !call.Ellipsis.IsValid() {
		switch {
		case p.isHTTP(call.Fun, "Dir", stack):
			return p.osQualifier() + ".DirFS(" + g.text(call.Args[0]) + ")"
		case p.isHTTP(call.Fun, "FS", stack):
			return g.text(call.Args[0])
		}
	}

	if p.pkgs.ensureHTTPFSAdapter(p.fi, p.g.file.Name.Name) {
		internal.Notef(p.fi, g.line(at.Pos()),
			"%s %s is an http.FileSystem, and v3 takes an fs.FS: it is wrapped in %s, which %s adds to this package. "+
				"Serving an embed.FS or os.DirFS(dir) instead would make the adapter unnecessary.",
			what, g.text(e), mw3HTTPFSType, mw3HTTPFSFile)
		return mw3HTTPFSType + "{" + g.text(e) + "}"
	}

	internal.Notef(p.fi, g.line(at.Pos()),
		"%s %s is an http.FileSystem, and v3 takes an fs.FS: only http.Dir and http.FS(fsys) convert (to os.DirFS and to fsys). "+
			"It was kept as the file system, which does not build: serve an embed.FS or os.DirFS(dir), or wrap the value in a type with Open(name string) (fs.File, error).",
		what, g.text(e))
	return g.text(e)
}

// configLit migrates a filesystem.Config literal to a static.Config.
func (p *mw3FSPass) configLit(lit *ast.CompositeLit, stack []ast.Node) {
	g := p.g
	if slices.ContainsFunc(lit.Elts, func(e ast.Expr) bool { _, keyed := e.(*ast.KeyValueExpr); return !keyed }) {
		return
	}

	// fsys is the file system of the config, which NotFoundFile opens its file
	// from, and served the one static serves, which PathPrefix may narrow.
	root := mw3Field(lit, "Root")
	fsys := ""
	if root != nil {
		fsys = p.rootFS(root.Value, stack, root, "Root")
	}
	served := fsys
	if kv := mw3Field(lit, "PathPrefix"); kv != nil {
		served = p.pathPrefix(lit, kv, stack, fsys)
	}
	if root != nil {
		p.edits.replaceNode(g, root, "FS: "+served)
		p.imports.releaseIn(g, root.Value)
	}

	if kv := mw3Field(lit, "Index"); kv != nil {
		p.index(kv, stack)
	}
	if kv := mw3Field(lit, "NotFoundFile"); kv != nil {
		p.notFoundFile(kv, mw3Field(lit, "MaxAge"), fsys, stack)
	}
	if kv := mw3Field(lit, "ContentTypeCharset"); kv != nil {
		p.contentTypeCharset(kv, stack)
	}
}

// pathPrefix handles the PathPrefix of a config and returns the file system
// the config serves. The directory of the file system that static.New serves is
// its first argument, so the prefix goes to the New call that takes the config.
// A config that no New call takes in sight narrows its file system with
// fs.Sub instead, and one with no file system keeps the field.
func (p *mw3FSPass) pathPrefix(lit *ast.CompositeLit, kv *ast.KeyValueExpr, stack []ast.Node, fsys string) string {
	g := p.g
	if value, ok := mw3StringLiteral(kv.Value); ok && value == "" {
		mw3RemoveElement(g, &p.edits, kv)
		return fsys
	}

	calls := p.litCalls[lit]
	if len(calls) == 0 {
		if call, ok := mw3Parent(stack).(*ast.CallExpr); ok && slices.Contains(call.Args, ast.Expr(lit)) && p.isMember(call.Fun, mw3New, stack[:len(stack)-1]) {
			calls = []*ast.CallExpr{call}
		}
	}

	switch {
	case len(calls) > 0:
		for _, call := range calls {
			p.prefix[call] = g.text(kv.Value)
		}
	case fsys != "":
		mw3RemoveElement(g, &p.edits, kv)
		return p.subFS(kv, fsys, stack)
	default:
		internal.Notef(p.fi, g.line(kv.Pos()),
			"PathPrefix is the first argument of static.New in v3 (static.New(%s, static.Config{FS: ...})), but this config has no Root and does not reach a filesystem.New call here, so the field was left as it is.",
			g.text(kv.Value))
		return fsys
	}
	mw3RemoveElement(g, &p.edits, kv)
	return fsys
}

// subFS narrows the file system to the directory that PathPrefix names, with
// fs.Sub, for a config whose New call is out of sight.
func (p *mw3FSPass) subFS(kv *ast.KeyValueExpr, fsys string, stack []ast.Node) string {
	g := p.g
	dir := g.text(kv.Value)
	if value, ok := newMw3Strings(g, p.packageFacts()).value(kv.Value, stack); ok {
		dir = strconv.Quote(strings.Trim(value, "/"))
	} else {
		dir = p.stringsQualifier() + ".Trim(" + dir + ", \"/\")"
	}

	sub, err := "sub", "err"
	if regexp.MustCompile(`\bsub\b`).MatchString(fsys) {
		sub = "subFS"
	}
	if regexp.MustCompile(`\berr\b`).MatchString(fsys) {
		err = "subErr"
	}
	return "func() " + p.fsQualifier() + ".FS {\n" +
		sub + ", " + err + " := " + p.fsQualifier() + ".Sub(" + fsys + ", " + dir + ")\n" +
		"if " + err + " != nil {\npanic(" + err + ")\n}\n" +
		"return " + sub + "\n}()"
}

// fsQualifier returns the identifier of the io/fs package, importing it if needed.
func (p *mw3FSPass) fsQualifier() string {
	if p.fsName == "" {
		p.fsName = mw3ImportName(p.g.src, "io/fs")
		if p.fsName == "" {
			p.fsName = mw3FreeName(func(name string) bool {
				_, taken := p.declared[name]
				return taken
			}, "fs", "iofs")
			p.imports.require("io/fs", p.fsName)
		}
	}
	return p.fsName
}

// index turns the single index file of v2 into the list of v3. v2 put a "/" in
// front of the name when it had none and v3 refuses a name with one.
func (p *mw3FSPass) index(kv *ast.KeyValueExpr, stack []ast.Node) {
	g := p.g
	value, ok := mw3StringLiteral(kv.Value)
	if !ok {
		strs := newMw3Strings(g, p.packageFacts())
		value, ok = strs.value(kv.Value, stack)
	}

	switch {
	case ok && value == "":
		mw3RemoveElement(g, &p.edits, kv) // v2 took the empty name for index.html
	case ok:
		p.edits.replaceNode(g, kv, "IndexNames: []string{"+strconv.Quote(strings.TrimPrefix(value, "/"))+"}")
		p.imports.releaseIn(g, kv.Value)
	default:
		p.edits.replaceNode(g, kv, "IndexNames: []string{"+p.stringsQualifier()+".TrimPrefix("+g.text(kv.Value)+", \"/\")}")
	}
}

// notFoundFile replaces NotFoundFile, the file served for a path that is not
// found, with the handler that does the same: the file is sent with a 200, as
// v2 did, because the file server has set the 404 by then. v2 gave that file the
// MaxAge of the config as well, which the file server of v3 does not for a file
// its handler sends.
func (p *mw3FSPass) notFoundFile(kv, maxAge *ast.KeyValueExpr, fsys string, stack []ast.Node) {
	g := p.g
	if fsys == "" {
		internal.Notef(p.fi, g.line(kv.Pos()),
			"NotFoundFile became a NotFoundHandler that sends the file from the config's FS, and this config has no Root, so the field was left as it is.")
		return
	}

	file := g.text(kv.Value)
	if value, ok := newMw3Strings(g, p.packageFacts()).value(kv.Value, stack); ok {
		file = strconv.Quote(strings.TrimPrefix(value, "/"))
	}

	ctx := "c"
	if regexp.MustCompile(`\bc\b`).MatchString(fsys + " " + file) {
		ctx = "ctx"
	}
	sendFile := "FS: " + fsys
	if maxAge != nil {
		sendFile += ", MaxAge: " + g.text(maxAge.Value)
	}
	fiber := p.fiberQualifier()
	handler := "NotFoundHandler: func(" + ctx + " " + fiber + "Ctx) error {\n" +
		ctx + ".Status(" + fiber + "StatusOK)\n" +
		"return " + ctx + ".SendFile(" + file + ", " + fiber + "SendFile{" + sendFile + "})\n" +
		"}"
	p.edits.replaceNode(g, kv, handler)
}

// contentTypeCharset removes the charset v2 added to the content type of the
// files. static serves each file with the type of its extension, and Go's table
// has charset=utf-8 on the text types, so utf-8 is what it does by default;
// another charset has no field.
func (p *mw3FSPass) contentTypeCharset(kv *ast.KeyValueExpr, stack []ast.Node) {
	g := p.g
	strs := newMw3Strings(g, p.packageFacts())
	if value, ok := strs.value(kv.Value, stack); ok && (value == "" || strings.EqualFold(value, "utf-8")) {
		mw3RemoveElement(g, &p.edits, kv)
		p.imports.releaseIn(g, kv.Value)
		return
	}
	internal.Notef(p.fi, g.line(kv.Pos()),
		"static.Config has no ContentTypeCharset: it serves each file with the content type of its extension (charset=utf-8 for text files). "+
			"The field was left as it is: set the Content-Type in ModifyResponse if you need %s.", g.text(kv.Value))
}

// packageFacts returns the facts about the package of the file.
func (p *mw3FSPass) packageFacts() *mw3Pkg {
	if p.pkg == nil {
		p.pkg = p.pkgs.of(p.fi, p.g.file.Name.Name)
	}
	return p.pkg
}

// assign migrates cfg.Root = ... and the other fields of a config variable.
func (p *mw3FSPass) assign(a mw3FSAssign) {
	g := p.g
	as := a.as
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 || as.Tok != token.ASSIGN {
		return
	}
	sel, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok || !p.isConfigVar(sel.X, a.stack) {
		return
	}

	switch sel.Sel.Name {
	case "Root":
		p.edits.replaceNode(g, sel.Sel, "FS")
		p.edits.replaceNode(g, as.Rhs[0], p.rootFS(as.Rhs[0], a.stack, as, "Root"))
		p.imports.releaseIn(g, as.Rhs[0])
	case "Index":
		value, known := mw3StringLiteral(as.Rhs[0])
		text := ""
		switch {
		case known:
			text = "[]string{" + strconv.Quote(strings.TrimPrefix(value, "/")) + "}"
		default:
			text = "[]string{" + p.stringsQualifier() + ".TrimPrefix(" + g.text(as.Rhs[0]) + ", \"/\")}"
		}
		p.edits.replaceNode(g, sel.Sel, "IndexNames")
		p.edits.replaceNode(g, as.Rhs[0], text)
	case "PathPrefix", "NotFoundFile", "ContentTypeCharset":
		internal.Notef(p.fi, g.line(as.Pos()),
			"%s has no field in static.Config: PathPrefix is the first argument of static.New, NotFoundFile a NotFoundHandler that sends the file, "+
				"and ContentTypeCharset has no counterpart. Left as it is.", g.text(sel))
	}
}

// isConfigVar reports whether e is a variable that holds a filesystem Config.
func (p *mw3FSPass) isConfigVar(e ast.Expr, stack []ast.Node) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	d, ok := resolveIdent(id.Name, id.Pos(), stack)
	if !ok {
		return false
	}
	if d.typ != nil {
		return p.isConfigType(d.typ, stack)
	}
	init := d.init
	if un, isUnary := init.(*ast.UnaryExpr); isUnary {
		init = un.X
	}
	switch x := init.(type) {
	case *ast.CompositeLit:
		return x.Type != nil && p.isMember(x.Type, mw3Config, stack)
	case *ast.SelectorExpr, *ast.Ident:
		return p.isMember(x, mw3ConfigDefault, stack)
	case *ast.CallExpr:
		return p.returnsConfig(x, stack)
	}
	return false
}

// isConfigType reports whether the type is filesystem.Config or a pointer to it.
func (p *mw3FSPass) isConfigType(typ ast.Expr, stack []ast.Node) bool {
	for {
		star, ok := typ.(*ast.StarExpr)
		if !ok {
			break
		}
		typ = star.X
	}
	return p.isMember(typ, mw3Config, stack)
}

// returnsConfig reports whether the call is of a function of this file whose
// result is a filesystem Config.
func (p *mw3FSPass) returnsConfig(call *ast.CallExpr, stack []ast.Node) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	for _, decl := range p.g.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != id.Name || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
			continue
		}
		return p.isConfigType(fn.Type.Results.List[0].Type, stack)
	}
	return false
}

// mw3StringLiteral returns the value of a string literal.
func mw3StringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok {
		return "", false
	}
	return mw3UnquoteString(lit)
}

// mw3Parent returns the innermost ancestor.
func mw3Parent(stack []ast.Node) ast.Node {
	if len(stack) == 0 {
		return nil
	}
	return stack[len(stack)-1]
}
