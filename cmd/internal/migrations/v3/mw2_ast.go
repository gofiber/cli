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
)

// The import paths of the packages the migrations of the keyauth, limiter,
// logger, log, proxy, timeout and requestid middleware (and of the readers of the
// values they keep in the request locals) rewrite. The built-in middleware live
// in the core module at any major version, so a file whose imports were not
// moved to v3 yet is recognized as well.
var (
	mw2KeyAuth    = mw2Middleware("keyauth")
	mw2Extractors = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/extractors$`)
)

// mw2Middleware returns the import path matcher of a built-in middleware.
func mw2Middleware(name string) *regexp.Regexp {
	return regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/` + regexp.QuoteMeta(name) + `$`)
}

// mw2IsSelector reports whether e is the selector name.sel where name is one of
// the identifiers a file binds to a package.
func mw2IsSelector(e ast.Expr, names []string, sel string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != sel {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && slices.Contains(names, id.Name)
}

// mw2ConfigLit returns n as a composite literal of the type typeName of a package
// bound to names, for example keyauth.Config{...}.
func mw2ConfigLit(n ast.Node, names []string, typeName string) (*ast.CompositeLit, bool) {
	lit, ok := n.(*ast.CompositeLit)
	if !ok || lit.Type == nil || !isFiberType(lit.Type, names, typeName) {
		return nil, false
	}
	return lit, true
}

// mw2Field returns the keyed element called name of lit and its index, or -1.
func mw2Field(lit *ast.CompositeLit, name string) (idx int, kv *ast.KeyValueExpr) {
	for i, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok && isIdentNamed(kv.Key, name) {
			return i, kv
		}
	}
	return -1, nil
}

// mw2IsKeyed reports whether every element of lit is a key: value pair, the only
// shape the field rewrites understand.
func mw2IsKeyed(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		if _, ok := elt.(*ast.KeyValueExpr); !ok {
			return false
		}
	}
	return true
}

// maxMW2TypeDepth bounds how far mw2IsTyped follows variables.
const maxMW2TypeDepth = 4

// mw2IsTyped reports whether e is a value of the type typeName of a package bound
// to names: a literal of the type, a pointer to one, <pkg>.ConfigDefault (for
// Config) or a variable declared as, or initialized with, one of those. stack is
// the chain of ancestors of e (see goSource.walk).
func mw2IsTyped(e ast.Expr, names []string, typeName string, stack []ast.Node, depth int) bool {
	if depth > maxMW2TypeDepth {
		return false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return mw2IsTyped(x.X, names, typeName, stack, depth+1)
	case *ast.StarExpr:
		return mw2IsTyped(x.X, names, typeName, stack, depth+1)
	case *ast.UnaryExpr:
		return x.Op == token.AND && mw2IsTyped(x.X, names, typeName, stack, depth+1)
	case *ast.CompositeLit:
		return x.Type != nil && isFiberType(x.Type, names, typeName)
	case *ast.SelectorExpr:
		return typeName == "Config" && mw2IsSelector(x, names, "ConfigDefault")
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return isFiberType(d.typ, names, typeName)
		}
		return d.init != nil && mw2IsTyped(d.init, names, typeName, stack, depth+1)
	}
	return false
}

// mw2RemoveElement schedules the removal of the element i of lit. The comma that
// separates it from its neighbors goes with it, and so does a comment at the end
// of its line when it is the only thing on that line.
func mw2RemoveElement(g *goSource, edits *editSet, lit *ast.CompositeLit, i int) {
	src := g.src
	elt := lit.Elts[i]
	start, end := g.offset(elt.Pos()), g.offset(elt.End())

	// What follows the element on its line: a comma, a comment, then the end of the line.
	next := mw2SkipBlanks(src, end)
	if next < len(src) && src[next] == ',' {
		next = mw2SkipBlanks(src, next+1)
	}
	afterComma := next
	if strings.HasPrefix(src[next:], "//") {
		next = mw2LineEnd(src, next)
	} else if strings.HasPrefix(src[next:], "/*") {
		if closing := strings.Index(src[next:], "*/"); closing >= 0 && !strings.Contains(src[next:next+closing], "\n") {
			next = mw2SkipBlanks(src, next+closing+2)
		}
	}
	endsLine := next >= len(src) || src[next] == '\n' || src[next] == '\r'

	lineStart := strings.LastIndexByte(src[:start], '\n') + 1
	if strings.TrimSpace(src[lineStart:start]) == "" && endsLine {
		cut := next
		if cut < len(src) {
			cut = mw2LineEnd(src, cut) + 1
		}
		edits.replace(mw2CommentsAbove(src, lineStart), min(cut, len(src)), "")
		return
	}

	switch {
	case i+1 < len(lit.Elts) && g.line(lit.Elts[i+1].Pos()) == g.line(elt.End()):
		edits.replace(start, g.offset(lit.Elts[i+1].Pos()), "")
	case i > 0:
		edits.replace(g.offset(lit.Elts[i-1].End()), end, "")
	default:
		edits.replace(start, afterComma, "")
	}
}

// mw2CommentsAbove returns the offset at which the comment lines that sit directly
// above the line starting at lineStart begin: the comment documents what is
// below it, so it goes when that does.
func mw2CommentsAbove(src string, lineStart int) int {
	start := lineStart
	for start > 0 {
		prev := strings.LastIndexByte(src[:start-1], '\n') + 1
		if !strings.HasPrefix(strings.TrimSpace(src[prev:start]), "//") {
			break
		}
		start = prev
	}
	return start
}

// mw2SkipBlanks returns the offset of the first byte at or after i that is not a
// space or a tab.
func mw2SkipBlanks(src string, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return i
}

// mw2LineEnd returns the offset of the line break that ends the line holding i,
// or len(src).
func mw2LineEnd(src string, i int) int {
	if nl := strings.IndexByte(src[i:], '\n'); nl >= 0 {
		return i + nl
	}
	return len(src)
}

// mw2ConstString evaluates e as a constant string: a literal, a concatenation of
// constants, or a package constant that lookup finds.
func mw2ConstString(e ast.Expr, lookup func(name string) (ast.Expr, bool), depth int) (string, bool) {
	if depth > maxMW2TypeDepth {
		return "", false
	}

	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return mw2ConstString(x.X, lookup, depth+1)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, ok := mw2ConstString(x.X, lookup, depth+1)
		if !ok {
			return "", false
		}
		r, ok := mw2ConstString(x.Y, lookup, depth+1)
		return l + r, ok
	case *ast.Ident:
		if lookup == nil {
			return "", false
		}
		init, ok := lookup(x.Name)
		if !ok {
			return "", false
		}
		return mw2ConstString(init, lookup, depth+1)
	}
	return "", false
}

// mw2Const is a package-level constant declaration.
type mw2Const struct {
	// typ is the declared type, nil for an untyped constant.
	typ ast.Expr
	// value is the initializer, nil when the constant repeats the previous one
	// of its group (iota).
	value ast.Expr
}

// mw2Package is what the migrations need to know about the other files of the
// package a file belongs to.
type mw2Package struct {
	consts map[string]mw2Const
	// decls holds every name declared at package level.
	decls map[string]bool
}

// mw2Packages reads the packages of a project on demand and remembers them.
type mw2Packages struct {
	cache map[string]*mw2Package
}

func newMW2Packages() *mw2Packages {
	return &mw2Packages{cache: map[string]*mw2Package{}}
}

// of returns the package that the files of dir with the package clause name form.
// Test files of an external test package are not part of it.
func (p *mw2Packages) of(dir, name string) *mw2Package {
	key := dir + "\x00" + name
	if pkg, ok := p.cache[key]; ok {
		return pkg
	}

	pkg := &mw2Package{consts: map[string]mw2Const{}, decls: map[string]bool{}}
	p.cache[key] = pkg

	entries, err := os.ReadDir(dir)
	if err != nil {
		return pkg
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.SkipObjectResolution)
		if err != nil || file.Name.Name != name {
			continue
		}
		pkg.add(file)
	}
	return pkg
}

// add records the package-level declarations of file.
func (p *mw2Package) add(file *ast.File) {
	for _, d := range file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil {
				p.decls[decl.Name.Name] = true
			}
		case *ast.GenDecl:
			p.addGen(decl)
		}
	}
}

func (p *mw2Package) addGen(decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			p.decls[s.Name.Name] = true
		case *ast.ValueSpec:
			for i, name := range s.Names {
				p.decls[name.Name] = true
				if decl.Tok != token.CONST {
					continue
				}
				c := mw2Const{typ: s.Type}
				if len(s.Values) == len(s.Names) {
					c.value = s.Values[i]
				}
				p.consts[name.Name] = c
			}
		}
	}
}

// constValue is the lookup function of mw2ConstString for this package.
func (p *mw2Package) constValue(name string) (ast.Expr, bool) {
	c, ok := p.consts[name]
	return c.value, ok && c.value != nil
}

// mw2Import is an import declaration to add to a file.
type mw2Import struct {
	// alias is empty when the package is imported under its own name.
	alias string
	path  string
}

// mw2AddImports returns src with the imports added to its import declaration. An
// import of a path the file already imports is not repeated.
func mw2AddImports(src string, imports ...mw2Import) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil && file == nil {
		return src
	}

	var lines []string
	for _, imp := range imports {
		if mw2HasImport(file, imp.path) {
			continue
		}
		line := strconv.Quote(imp.path)
		if imp.alias != "" {
			line = imp.alias + " " + line
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return src
	}

	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		if gd.Lparen.IsValid() {
			at := fset.Position(gd.Rparen).Offset
			text := "\t" + strings.Join(lines, "\n\t") + "\n"
			if at > 0 && src[at-1] != '\n' {
				text = "\n" + text
			}
			return src[:at] + text + src[at:]
		}

		// import "x" becomes a block so that it can hold the new imports.
		start, end := fset.Position(gd.Pos()).Offset, fset.Position(gd.End()).Offset
		spec := strings.TrimSpace(strings.TrimPrefix(src[start:end], "import"))
		block := "import (\n\t" + spec + "\n\t" + strings.Join(lines, "\n\t") + "\n)"
		return src[:start] + block + src[end:]
	}

	at := fset.Position(file.Name.End()).Offset
	return src[:at] + "\n\nimport (\n\t" + strings.Join(lines, "\n\t") + "\n)" + src[at:]
}

// mw2HasImport reports whether file imports path under a usable name.
func mw2HasImport(file *ast.File, path string) bool {
	for _, spec := range file.Imports {
		if p, err := strconv.Unquote(spec.Path.Value); err == nil && p == path && (spec.Name == nil || spec.Name.Name != "_") {
			return true
		}
	}
	return false
}

// mw2NameTaken reports whether name already means something in the file or in its
// package, so that importing a package under that name would shadow it or clash
// with it.
func mw2NameTaken(g *goSource, pkg *mw2Package, name string) bool {
	if pkg != nil && pkg.decls[name] {
		return true
	}
	taken := false
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != name || taken {
			return !taken
		}
		// An identifier that only qualifies a selector is a use of an import.
		if len(stack) > 0 {
			if sel, ok := stack[len(stack)-1].(*ast.SelectorExpr); ok && sel.X == n {
				return true
			}
		}
		taken = true
		return false
	})
	return taken
}

// mw2FirstName returns the identifier through which src can reach the package
// whose import path matches pathRe, or "" when it does not import it by a name
// (not imported, imported only for its side effects, or dot imported).
func mw2FirstName(src string, pathRe *regexp.Regexp, defaults ...string) string {
	for _, name := range importNames(src, pathRe, defaults...) {
		if name != "." {
			return name
		}
	}
	return ""
}
