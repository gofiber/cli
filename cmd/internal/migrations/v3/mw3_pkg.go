package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3Pkgs caches what the files of a Go package say about the names the
// migrations of this area care about. A migration makes one for a run: the
// facts are read from the files as they are when they are first needed, and a
// run does not change a declaration that a fact is about.
type mw3Pkgs struct {
	pkgs map[string]*mw3Pkg
	// sources is what each module (by the directory of its go.mod) does with the
	// Source type of the session middleware, and stores the types that stand in
	// for the session store.
	sources map[string]*mw3SourceMode
	stores  map[string]*mw3StoreTypes
	// indexes give the view of the whole module, for the facts that reach into
	// other packages of it.
	indexes moduleIndexes
}

func newMw3Pkgs() *mw3Pkgs {
	return &mw3Pkgs{pkgs: map[string]*mw3Pkg{}}
}

// mw3Pkg holds the facts about the files of one package, which a single file
// cannot know: a constant declared in a sibling file, a session store kept in a
// package level variable. A kind of fact is read when it is first asked for.
type mw3Pkg struct {
	// strs are the package level string constants, and the string variables
	// that are initialized with a constant expression and never assigned.
	strs map[string]string
	// names are all the names declared at package level.
	names map[string]struct{}
	owner *mw3Pkgs
	// session is what the package says about the fiber session middleware.
	session mw3SessionFacts
	// fi is a file of the directory: it tells which module the package is in.
	fi   internal.FileInfo
	dir  string
	name string
	// files are the files of the package, read when first needed.
	files []mw3PkgFile
	// declsRead and sessionRead tell which kinds of fact were read.
	filesRead   bool
	declsRead   bool
	sessionRead bool
	// relevance is whether the package can have anything to do with session
	// stores: 0 not known yet, 1 it has not, 2 it has.
	relevance uint8
	// httpFS is whether the package has the adapter of an http.FileSystem.
	httpFS bool
}

// of returns the facts about the package the file fi belongs to. The facts
// come from the files that sit in the directory of fi and declare pkgName.
func (p *mw3Pkgs) of(fi internal.FileInfo, pkgName string) *mw3Pkg {
	dir := filepath.Dir(fi.Path)
	key := dir + "\x00" + pkgName
	if pkg, ok := p.pkgs[key]; ok {
		return pkg
	}
	pkg := &mw3Pkg{
		strs:  map[string]string{},
		names: map[string]struct{}{},
		owner: p,
		fi:    fi,
		dir:   dir,
		name:  pkgName,
	}
	pkg.session = newMw3SessionFacts(pkg)
	p.pkgs[key] = pkg
	return pkg
}

// index returns the view of the module the package is in, nil outside of one.
func (p *mw3Pkg) index() *moduleIndex {
	return p.owner.indexes.of(p.fi)
}

// peer returns the package of the module that the import path names, nil for any
// other package.
func (p *mw3Pkg) peer(importPath string) *mw3Pkg {
	pkg := p.index().packageAt(importPath)
	if pkg == nil {
		return nil
	}
	return p.owner.of(internal.FileInfo{Root: p.fi.Root, Path: filepath.Join(pkg.dir, "_.go")}, pkg.name)
}

// mw3StrDecl is a package level declaration that may hold a string constant.
type mw3StrDecl struct {
	expr    ast.Expr
	isConst bool
}

// mw3PkgFile is a file of a package, parsed.
type mw3PkgFile struct {
	file *ast.File
	src  string
}

// goFiles calls read with the content and the syntax of every file of the
// package. A file that does not parse, or declares another package, is not read.
// The files are read once, the first time any of the facts is asked for.
func (p *mw3Pkg) goFiles(read func(file *ast.File, src string)) {
	if !p.filesRead {
		p.filesRead = true
		p.files = p.readFiles()
	}
	for _, f := range p.files {
		read(f.file, f.src)
	}
}

// readFiles parses the files of the package.
func (p *mw3Pkg) readFiles() []mw3PkgFile {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil
	}
	var files []mw3PkgFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.dir, entry.Name())) // #nosec G304 -- a file of the project being migrated
		if err != nil {
			continue
		}
		src := string(data)
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), src, parser.SkipObjectResolution)
		if err != nil || file.Name.Name != p.name {
			continue
		}
		files = append(files, mw3PkgFile{file: file, src: src})
	}
	return files
}

// loadDecls reads what the files declare at package level.
func (p *mw3Pkg) loadDecls() {
	if p.declsRead {
		return
	}
	p.declsRead = true

	decls := map[string]mw3StrDecl{}
	duplicated := map[string]struct{}{}
	assigned := map[string]struct{}{}
	p.goFiles(func(file *ast.File, _ string) {
		p.scanDecls(file, decls, duplicated)
		mw3CollectAssigned(file, assigned)
	})

	for name := range duplicated {
		delete(decls, name)
	}
	for name, decl := range decls {
		if _, reassigned := assigned[name]; reassigned && !decl.isConst {
			continue
		}
		if value, ok := mw3EvalPackageString(decl.expr, decls, assigned, 0); ok {
			p.strs[name] = value
		}
	}
}

// constants returns the string constants of the package.
func (p *mw3Pkg) constants() map[string]string {
	p.loadDecls()
	return p.strs
}

// declared returns the names the package declares.
func (p *mw3Pkg) declared() map[string]struct{} {
	p.loadDecls()
	return p.names
}

// sessions returns what the package says about what holds sessions. Nothing is
// read from a package that has nothing to do with them: none of its files
// mentions the session middleware, and none imports a package of the module that
// does (see relevant). When there is something, all of the files are read,
// because a struct in one may have a field of a type that another declares, and
// that one is the file that imports the session middleware.
func (p *mw3Pkg) sessions() *mw3SessionFacts {
	if !p.sessionRead {
		p.sessionRead = true
		if p.relevant() {
			p.goFiles(func(file *ast.File, src string) {
				p.session.scan(file, p.owner.envOf(p.fi, file, src))
			})
		}
	}
	return &p.session
}

// relevant reports whether the package can have anything to do with session
// stores: one of its files mentions the session middleware, or imports a package
// of the module that is relevant.
func (p *mw3Pkg) relevant() bool {
	if p.relevance != 0 {
		return p.relevance == mw3Relevant
	}
	p.relevance = mw3Irrelevant // a package that is asked about itself does not count

	var imported []string
	mentioned := false
	p.goFiles(func(file *ast.File, src string) {
		if strings.Contains(src, "middleware/session") {
			mentioned = true
		}
		imported = append(imported, mw3ImportPaths(file)...)
	})

	relevant := mentioned
	for _, path := range imported {
		if relevant {
			break
		}
		if peer := p.peer(path); peer != nil && peer != p {
			relevant = peer.relevant()
		}
	}
	if relevant {
		p.relevance = mw3Relevant
	}
	return relevant
}

const (
	mw3Irrelevant uint8 = iota + 1
	mw3Relevant
)

// importsSessions reports whether a file of the package, whose head is given,
// imports a package of the module that declares something that holds a store, a
// session or a config.
func (p *mw3Pkg) importsSessions(head *ast.File) bool {
	for _, path := range mw3ImportPaths(head) {
		if peer := p.peer(path); peer != nil && peer != p && peer.sessions().holdsSessions() {
			return true
		}
	}
	return false
}

// envOf returns what a file of the module says about the names it uses.
func (p *mw3Pkgs) envOf(fi internal.FileInfo, file *ast.File, src string) *mw3Env {
	env := &mw3Env{ix: p.indexes.of(fi)}
	if strings.Contains(src, "middleware/session") {
		env.session = importNames(src, mw3SessionPath, "session")
	}
	if env.ix == nil {
		return env
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || mw3SessionPath.MatchString(path) {
			continue
		}
		if _, inModule := env.ix.dirOf(path); !inModule {
			continue
		}
		imp := mw3ModuleImport{path: path}
		if spec.Name != nil {
			imp.alias = spec.Name.Name
		}
		env.modules = append(env.modules, imp)
	}
	return env
}

// scanDecls records the names the file declares at package level, and the ones
// that might hold a string.
func (p *mw3Pkg) scanDecls(file *ast.File, decls map[string]mw3StrDecl, duplicated map[string]struct{}) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				p.names[d.Name.Name] = struct{}{}
			}
		case *ast.GenDecl:
			var inherited []ast.Expr
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					p.names[s.Name.Name] = struct{}{}
				case *ast.ValueSpec:
					values := s.Values
					if d.Tok == token.CONST && len(values) == 0 {
						values = inherited // const ( a = "x"; b ) repeats the expression
					} else if d.Tok == token.CONST {
						inherited = values
					}
					for i, id := range s.Names {
						p.names[id.Name] = struct{}{}
						if len(values) != len(s.Names) || id.Name == "_" {
							continue
						}
						if _, seen := decls[id.Name]; seen {
							duplicated[id.Name] = struct{}{}
						}
						decls[id.Name] = mw3StrDecl{expr: values[i], isConst: d.Tok == token.CONST}
					}
				}
			}
		}
	}
}

// mw3CollectAssigned records the names that are assigned to, or whose address
// is taken, anywhere below root: a variable among them is not a constant.
func mw3CollectAssigned(root ast.Node, assigned map[string]struct{}) {
	mark := func(e ast.Expr) {
		if id, ok := e.(*ast.Ident); ok {
			assigned[id.Name] = struct{}{}
		}
	}
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE {
				for _, lhs := range x.Lhs {
					mark(lhs)
				}
			}
		case *ast.IncDecStmt:
			mark(x.X)
		case *ast.UnaryExpr:
			if x.Op == token.AND {
				mark(x.X)
			}
		case *ast.RangeStmt:
			if x.Tok == token.ASSIGN {
				mark(x.Key)
				mark(x.Value)
			}
		}
		return true
	})
}

// mw3MaxStringDepth bounds how many constants a string is followed through.
const mw3MaxStringDepth = 8

// mw3EvalPackageString evaluates a package level string expression: literals,
// concatenations and the names of other such declarations.
func mw3EvalPackageString(e ast.Expr, decls map[string]mw3StrDecl, assigned map[string]struct{}, depth int) (string, bool) {
	if depth > mw3MaxStringDepth {
		return "", false
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return mw3EvalPackageString(x.X, decls, assigned, depth+1)
	case *ast.BasicLit:
		return mw3UnquoteString(x)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		left, ok := mw3EvalPackageString(x.X, decls, assigned, depth+1)
		if !ok {
			return "", false
		}
		right, ok := mw3EvalPackageString(x.Y, decls, assigned, depth+1)
		return left + right, ok
	case *ast.Ident:
		decl, ok := decls[x.Name]
		if !ok {
			return "", false
		}
		if _, reassigned := assigned[x.Name]; reassigned && !decl.isConst {
			return "", false
		}
		return mw3EvalPackageString(decl.expr, decls, assigned, depth+1)
	}
	return "", false
}

// mw3UnquoteString returns the value of a string literal.
func mw3UnquoteString(lit *ast.BasicLit) (string, bool) {
	if lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

// mw3Strings reads the string value of an expression of one file, following
// the names it uses to their declarations.
type mw3Strings struct {
	pkg *mw3Pkg
	g   *goSource
	// fmtNames are the identifiers the file binds to the fmt package.
	fmtNames []string
}

func newMw3Strings(g *goSource, pkg *mw3Pkg) mw3Strings {
	return mw3Strings{
		g:        g,
		pkg:      pkg,
		fmtNames: importNames(g.src, mw3PathRegexp("fmt")),
	}
}

// value returns the string the expression evaluates to, when that can be told
// from the source: a literal, a concatenation, a constant or a variable that is
// never reassigned, or a fmt.Sprintf of such values. stack holds the ancestors
// of e (see goSource.walk).
func (s mw3Strings) value(e ast.Expr, stack []ast.Node) (string, bool) {
	return s.eval(e, stack, 0)
}

func (s mw3Strings) eval(e ast.Expr, stack []ast.Node, depth int) (string, bool) {
	if depth > mw3MaxStringDepth {
		return "", false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return s.eval(x.X, stack, depth+1)
	case *ast.BasicLit:
		return mw3UnquoteString(x)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		left, ok := s.eval(x.X, stack, depth+1)
		if !ok {
			return "", false
		}
		right, ok := s.eval(x.Y, stack, depth+1)
		return left + right, ok
	case *ast.Ident:
		return s.evalIdent(x, stack, depth)
	case *ast.SelectorExpr:
		return s.evalSourceConstant(x)
	case *ast.CallExpr:
		if isIdentNamed(x.Fun, "string") && len(x.Args) == 1 && !x.Ellipsis.IsValid() {
			// A conversion of a string, or of a Source, to a string.
			if _, hidden := resolveIdent("string", x.Pos(), stack); !hidden {
				return s.eval(x.Args[0], stack, depth+1)
			}
		}
		return s.evalSprintf(x, stack, depth)
	}
	return "", false
}

// mw3ShimPath matches the import path of the package the migrations add to a
// module.
var mw3ShimPath = regexp.MustCompile(`/` + regexp.QuoteMeta(moduleShimDir) + `$`)

// evalSourceConstant evaluates a constant of the Source type that the module
// package has, SessionSourceCookie for instance.
func (s mw3Strings) evalSourceConstant(sel *ast.SelectorExpr) (string, bool) {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	for _, c := range mw3SourceConstants {
		if c.shim == sel.Sel.Name && slices.Contains(importNames(s.g.src, mw3ShimPath, moduleShimPkg), id.Name) {
			return c.value, true
		}
	}
	return "", false
}

func (s mw3Strings) evalIdent(id *ast.Ident, stack []ast.Node, depth int) (string, bool) {
	local := mw3FunctionScope(stack)
	if init, found := mw3LocalStringInit(id.Name, id.Pos(), local); found {
		if init == nil {
			return "", false
		}
		return s.eval(init, stack, depth+1)
	}
	if d, ok := resolveIdent(id.Name, id.Pos(), local); ok {
		// A variable of the function: its value at the declaration holds when
		// nothing assigns to it.
		if d.init == nil || mw3AssignedIn(id.Name, local) {
			return "", false
		}
		return s.eval(d.init, stack, depth+1)
	}
	value, ok := s.pkg.constants()[id.Name]
	return value, ok
}

// mw3FunctionScope returns the part of the ancestors that sits inside the
// outermost function: everything a name could be declared in other than the
// file or the package.
func mw3FunctionScope(stack []ast.Node) []ast.Node {
	for i, node := range stack {
		switch node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return stack[i:]
		}
	}
	return nil
}

// evalSprintf evaluates fmt.Sprintf calls whose verbs are all %s, %v or %%.
func (s mw3Strings) evalSprintf(call *ast.CallExpr, stack []ast.Node, depth int) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" || len(call.Args) == 0 || call.Ellipsis.IsValid() {
		return "", false
	}
	pkgID, ok := sel.X.(*ast.Ident)
	if !ok || !slices.Contains(s.fmtNames, pkgID.Name) {
		return "", false
	}
	format, ok := s.eval(call.Args[0], stack, depth+1)
	if !ok {
		return "", false
	}

	var out []byte
	next := 1
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			out = append(out, format[i])
			continue
		}
		i++
		if i >= len(format) {
			return "", false
		}
		switch format[i] {
		case '%':
			out = append(out, '%')
		case 's', 'v':
			if next >= len(call.Args) {
				return "", false
			}
			arg, ok := s.eval(call.Args[next], stack, depth+1)
			if !ok {
				return "", false
			}
			out = append(out, arg...)
			next++
		default:
			return "", false
		}
	}
	return string(out), next == len(call.Args)
}

// mw3LocalStringInit looks for a constant declared inside a function: found is
// whether the innermost declaration of name is one, init the expression it is
// initialized with (nil when it has none that can be followed).
func mw3LocalStringInit(name string, pos token.Pos, stack []ast.Node) (init ast.Expr, found bool) {
	for _, node := range slices.Backward(stack) {
		var stmts []ast.Stmt
		switch n := node.(type) {
		case *ast.BlockStmt:
			stmts = n.List
		case *ast.CaseClause:
			stmts = n.Body
		case *ast.CommClause:
			stmts = n.Body
		default:
			continue
		}
		for _, stmt := range slices.Backward(stmts) {
			if stmt.End() > pos {
				continue
			}
			ds, ok := stmt.(*ast.DeclStmt)
			if !ok {
				continue
			}
			gd, ok := ds.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, id := range vs.Names {
					if id.Name != name {
						continue
					}
					if len(vs.Values) == len(vs.Names) {
						return vs.Values[i], true
					}
					return nil, true
				}
			}
		}
	}
	return nil, false
}

// mw3AssignedIn reports whether the function around the use assigns to name,
// which makes the value the variable was declared with unreliable.
func mw3AssignedIn(name string, stack []ast.Node) bool {
	for _, node := range stack {
		var body ast.Node
		switch n := node.(type) {
		case *ast.FuncDecl:
			body = n.Body
		case *ast.FuncLit:
			body = n.Body
		default:
			continue
		}
		assigned := map[string]struct{}{}
		mw3CollectAssigned(body, assigned)
		if _, reassigned := assigned[name]; reassigned {
			return true
		}
	}
	return false
}
