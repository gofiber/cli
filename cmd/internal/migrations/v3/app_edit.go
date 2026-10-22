package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// appLineStart returns the offset of the first byte of the line holding off.
func appLineStart(src string, off int) int {
	return strings.LastIndexByte(src[:off], '\n') + 1
}

// appLineEnd returns the offset just after the newline that ends the line
// holding off, or len(src) on the last line.
func appLineEnd(src string, off int) int {
	i := strings.IndexByte(src[off:], '\n')
	if i < 0 {
		return len(src)
	}
	return off + i + 1
}

// appWholeLines returns the range of the lines that only hold the code in
// [start, end): nothing but blanks before it, and after it at most a comma (or
// semicolon) and a comment. ok is false when something else shares the lines.
func appWholeLines(src string, start, end int) (from, to int, ok bool) {
	from = appLineStart(src, start)
	if strings.TrimSpace(src[from:start]) != "" {
		return 0, 0, false
	}

	to = appLineEnd(src, end)
	tail := strings.TrimSpace(src[end:to])
	tail = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(tail, ","), ";"))
	switch {
	case tail == "", strings.HasPrefix(tail, "//"):
		return from, to, true
	case strings.HasPrefix(tail, "/*"):
		closing := strings.Index(tail, "*/")
		if closing >= 0 && strings.TrimSpace(tail[closing+2:]) == "" {
			return from, to, true
		}
	}
	return 0, 0, false
}

// appRemoveStmt schedules the removal of a statement that sits on lines of its
// own.
func appRemoveStmt(g *goSource, edits *editSet, stmt ast.Stmt) bool {
	from, to, ok := appWholeLines(g.src, g.offset(stmt.Pos()), g.offset(stmt.End()))
	if !ok {
		return false
	}
	edits.replace(from, to, "")
	return true
}

// appRemoveElements schedules the removal of the elements of lit at the given
// indexes, and the replacement of the elements in repl by new text, and reports
// whether the literal has a layout it can edit: a literal on one line is rebuilt
// from the elements that stay, in a literal over several lines every removed
// element must sit on lines of its own. Nothing is scheduled when it returns
// false.
func appRemoveElements(g *goSource, edits *editSet, lit *ast.CompositeLit, drop []int, repl map[int]string) bool {
	src := g.src
	open, closing := g.offset(lit.Lbrace), g.offset(lit.Rbrace)
	dropped := func(i int) bool { return slices.Contains(drop, i) }

	if len(drop) == 0 {
		for i, text := range repl {
			edits.replaceNode(g, lit.Elts[i], text)
		}
		return true
	}

	if g.line(lit.Lbrace) == g.line(lit.Rbrace) {
		var kept []string
		prevEnd := open + 1
		for i, el := range lit.Elts {
			start, end := g.offset(el.Pos()), g.offset(el.End())
			if strings.Trim(src[prevEnd:start], " \t,") != "" {
				return false // a comment between the elements
			}
			prevEnd = end
			switch text, replaced := repl[i]; {
			case dropped(i):
			case replaced:
				kept = append(kept, text)
			default:
				kept = append(kept, src[start:end])
			}
		}
		if strings.Trim(src[prevEnd:closing], " \t,") != "" {
			return false
		}
		edits.replace(open+1, closing, strings.Join(kept, ", "))
		return true
	}

	type span struct{ from, to int }
	var spans []span
	for _, i := range drop {
		el := lit.Elts[i]
		from, to, ok := appWholeLines(src, g.offset(el.Pos()), g.offset(el.End()))
		if !ok {
			return false
		}
		spans = append(spans, span{from, to})
	}

	if len(drop) == len(lit.Elts) && appOnlyElements(g, lit) {
		edits.replace(open+1, closing, "")
		return true
	}
	for _, s := range spans {
		edits.replace(s.from, s.to, "")
	}
	for i, text := range repl {
		if !dropped(i) {
			edits.replaceNode(g, lit.Elts[i], text)
		}
	}
	return true
}

// appOnlyElements reports whether nothing but the elements, commas and blanks
// is between the braces of lit, so that emptying the literal loses no comment.
func appOnlyElements(g *goSource, lit *ast.CompositeLit) bool {
	prevEnd := g.offset(lit.Lbrace) + 1
	for _, el := range lit.Elts {
		if strings.Trim(g.src[prevEnd:g.offset(el.Pos())], " \t\r\n,") != "" {
			return false
		}
		prevEnd = g.offset(el.End())
	}
	return strings.Trim(g.src[prevEnd:g.offset(lit.Rbrace)], " \t\r\n,") == ""
}

// appKeyedElement returns the index and value of the element of lit that sets
// the field key, -1 when there is none.
func appKeyedElement(lit *ast.CompositeLit, key string) (idx int, kv *ast.KeyValueExpr) {
	for i, el := range lit.Elts {
		if pair, ok := el.(*ast.KeyValueExpr); ok && isIdentNamed(pair.Key, key) {
			return i, pair
		}
	}
	return -1, nil
}

// packageLevelNameTaken reports whether name is declared by the package of f or
// is the name of one of the imports of f.
func (f *appFile) packageLevelNameTaken(name string) bool {
	if pkg := f.ix.pkgOf(f); pkg != nil && pkg.names[name] {
		return true
	}
	for _, imp := range f.imports {
		local := imp.name
		if local == "" {
			local = defaultImportName(imp.path)
		}
		if local == name {
			return true
		}
	}
	return false
}

// nameTaken reports whether name is already in use in f: as a package-level
// name, as the name of an import or as any identifier of the file.
func (f *appFile) nameTaken(name string) bool {
	if f.packageLevelNameTaken(name) {
		return true
	}

	taken := false
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || taken || id.Name != name {
			return !taken
		}
		if sel, isSel := f.parent[id].(*ast.SelectorExpr); isSel && sel.Sel == id {
			return true
		}
		taken = true
		return false
	})
	return taken
}

// appImportReq is an import a migration adds to a file.
type appImportReq struct {
	path string
	// alias is empty when the package is imported under its own name.
	alias string
}

// appImports collects the imports the rewrite of one file needs.
type appImports struct {
	reqs []appImportReq
}

// qualifier returns the prefix that names the package at path in f ("static.",
// "staticmw.", or "" for a dot import) and records the import when f does not
// have it yet. preferred is the name for a new import; when it is taken
// another one is derived from it.
func (a *appImports) qualifier(f *appFile, path, preferred string) string {
	for _, imp := range f.imports {
		if imp.path != path || imp.name == "_" {
			continue
		}
		switch imp.name {
		case ".":
			return ""
		case "":
			return defaultImportName(path) + "."
		default:
			return imp.name + "."
		}
	}
	for _, req := range a.reqs {
		if req.path == path {
			return appRequestedName(req) + "."
		}
	}

	name := preferred
	for i := 0; f.nameTaken(name) || a.requested(name); i++ {
		switch i {
		case 0:
			name = preferred + "mw"
		case 1:
			name = preferred + "pkg"
		default:
			name = preferred + strconv.Itoa(i)
		}
	}

	req := appImportReq{path: path}
	if name != defaultImportName(path) {
		req.alias = name
	}
	a.reqs = append(a.reqs, req)
	return name + "."
}

func appRequestedName(req appImportReq) string {
	if req.alias != "" {
		return req.alias
	}
	return defaultImportName(req.path)
}

func (a *appImports) requested(name string) bool {
	for _, req := range a.reqs {
		if appRequestedName(req) == name {
			return true
		}
	}
	return false
}

// fiberQualifierFor returns the prefix that names the fiber package in f,
// importing the package when f does not. The import path is the one the rest of
// the package uses.
func (a *appImports) fiberQualifier(f *appFile) string {
	if prefix, ok := f.fiberQualifier(); ok {
		return prefix
	}
	return a.qualifier(f, f.fiberImportPath(), "fiber")
}

// fiberImportPath finds the path under which f, or failing that the rest of the
// project, imports fiber: a project is on one major version at a time.
func (f *appFile) fiberImportPath() string {
	return f.ix.fiberImportPath(f)
}

// fiberImportPath returns the fiber import path of near, or else the smallest
// one of any file read so far, or else the v3 path.
func (ix *appIndex) fiberImportPath(near *appFile) string {
	pathOf := func(file *appFile) string {
		for _, imp := range file.imports {
			if reFiberCorePath.MatchString(imp.path) && imp.name != "_" {
				return imp.path
			}
		}
		return ""
	}
	if p := pathOf(near); p != "" {
		return p
	}

	best := ""
	for _, file := range ix.files {
		if file == nil {
			continue
		}
		if p := pathOf(file); p != "" && (best == "" || p < best) {
			best = p
		}
	}
	if best != "" {
		return best
	}
	return appFiberV3Path
}

// appFiberV3Path is the import path of the fiber package a migrated project uses.
const appFiberV3Path = "github.com/gofiber/fiber/v3"

// apply adds the collected imports to content.
func (a *appImports) apply(content string) string {
	if len(a.reqs) == 0 {
		return content
	}

	g, ok := parseSource(content)
	if !ok {
		return content
	}

	var specs strings.Builder
	for _, req := range a.reqs {
		specs.WriteString("\t") //nolint:errcheck // WriteString never returns an error
		if req.alias != "" {
			specs.WriteString(req.alias + " ") //nolint:errcheck // WriteString never returns an error
		}
		specs.WriteString(strconv.Quote(req.path) + "\n") //nolint:errcheck // WriteString never returns an error
	}

	var imports *ast.GenDecl
	for _, d := range g.file.Decls {
		if gd, isGen := d.(*ast.GenDecl); isGen && gd.Tok == token.IMPORT {
			imports = gd
			if gd.Lparen.IsValid() {
				break
			}
		}
	}

	var edits editSet
	switch {
	case imports != nil && imports.Lparen.IsValid():
		at := g.offset(imports.Lparen) + 1
		edits.replace(at, at, "\n"+strings.TrimSuffix(specs.String(), "\n"))
	case imports != nil:
		edits.replaceNode(g, imports, "import (\n"+strings.TrimPrefix(g.text(imports), "import ")+"\n"+specs.String()+")")
	default:
		at := g.offset(g.file.Name.End())
		edits.replace(at, at, "\n\nimport (\n"+specs.String()+")")
	}
	return edits.apply(content)
}

// constString evaluates e when it is a constant string made of literals, string
// constants of the package and +.
func (f *appFile) constString(e ast.Expr, depth int) (string, bool) {
	if depth > appMaxDepth {
		return "", false
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.constString(x.X, depth+1)
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(x.Value)
		return v, err == nil
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := f.constString(x.X, depth+1)
		r, rok := f.constString(x.Y, depth+1)
		return l + r, lok && rok
	case *ast.Ident:
		pkg := f.ix.pkgOf(f)
		if pkg == nil {
			return "", false
		}
		c := pkg.consts[x.Name]
		if c == nil || len(c.spec.Values) != len(c.spec.Names) {
			return "", false
		}
		if _, shadowed := f.binding(x); shadowed {
			return "", false
		}
		return c.file.constString(c.spec.Values[c.index], depth+1)
	}
	return "", false
}

// provablyNotEmpty reports whether the string e is never empty: a non-empty
// constant, a concatenation with one, or a path.Join or filepath.Join that has
// one among its elements.
func (f *appFile) provablyNotEmpty(e ast.Expr, depth int) bool {
	if depth > appMaxDepth {
		return false
	}
	if v, ok := f.constString(e, 0); ok {
		return v != ""
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.provablyNotEmpty(x.X, depth+1)
	case *ast.BinaryExpr:
		return x.Op == token.ADD && (f.provablyNotEmpty(x.X, depth+1) || f.provablyNotEmpty(x.Y, depth+1))
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Join" {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return false
		}
		if p := f.importPathOf(id.Name); p != "path" && p != "path/filepath" {
			return false
		}
		return slices.ContainsFunc(x.Args, func(a ast.Expr) bool { return f.provablyNotEmpty(a, depth+1) })
	}
	return false
}
