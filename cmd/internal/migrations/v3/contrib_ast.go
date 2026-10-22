package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// The helpers in this file are the text level half of the contrib migrations.
// They edit the source around AST nodes (removing a field of a struct literal,
// adding an import) so that comments, line breaks and the rest of the
// formatting survive; the final gofmt is left to ChangeFileContent.

// contribLineIndent returns the blanks the line holding off starts with.
func contribLineIndent(src string, off int) string {
	ls := contribLineStart(src, off)
	return src[ls:contribSkipBlanks(src, ls)]
}

// contribSkipBlanks returns the offset of the first byte at or after off that is
// not a space or a tab.
func contribSkipBlanks(src string, off int) int {
	for off < len(src) && (src[off] == ' ' || src[off] == '\t') {
		off++
	}
	return off
}

// contribRestIsEmpty reports whether nothing but blanks and a line comment is
// left on the line from off on.
func contribRestIsEmpty(src string, off int) bool {
	off = contribSkipBlanks(src, off)
	return off >= len(src) || src[off] == '\n' || src[off] == '\r' || strings.HasPrefix(src[off:], "//")
}

// contribOwnsLine reports whether the node spanning [start, end) is the only
// thing on its first and last line (a line comment after it does not count).
func contribOwnsLine(src string, start, end int) bool {
	return strings.TrimSpace(src[contribLineStart(src, start):start]) == "" && contribRestIsEmpty(src, end)
}

// contribRemoveStmt schedules the removal of stmt. A statement that owns its
// lines takes them along, line comment included.
func contribRemoveStmt(g *goSource, edits *editSet, stmt ast.Stmt) {
	start, end := g.offset(stmt.Pos()), g.offset(stmt.End())
	if contribOwnsLine(g.src, start, end) {
		edits.replace(contribLineStart(g.src, start), contribLineEnd(g.src, end), "")
		return
	}
	edits.replace(start, end, "")
}

// contribRemoveElements schedules the removal of the elements idxs of the
// composite literal lit, together with the commas that separate them from their
// neighbors. Elements that sit next to each other go as one.
func contribRemoveElements(g *goSource, edits *editSet, lit *ast.CompositeLit, idxs ...int) {
	sorted := slices.Sorted(slices.Values(idxs))
	for len(sorted) > 0 {
		first, last := sorted[0], sorted[0]
		sorted = sorted[1:]
		for len(sorted) > 0 && sorted[0] == last+1 {
			last = sorted[0]
			sorted = sorted[1:]
		}
		contribRemoveRun(g, edits, lit, first, last)
	}
}

// contribRemoveRun removes the elements first through last of lit.
func contribRemoveRun(g *goSource, edits *editSet, lit *ast.CompositeLit, first, last int) {
	src := g.src
	start, end := g.offset(lit.Elts[first].Pos()), g.offset(lit.Elts[last].End())

	afterComma := end
	hasComma := false
	if j := contribSkipBlanks(src, end); j < len(src) && src[j] == ',' {
		afterComma, hasComma = j+1, true
	}

	// Elements that fill their lines leave nothing behind.
	if contribOwnsLine(src, start, afterComma) {
		edits.replace(contribLineStart(src, start), contribLineEnd(src, afterComma), "")
		return
	}

	// More elements follow on the same line: drop the run and its comma.
	if hasComma && last+1 < len(lit.Elts) && g.line(lit.Elts[last+1].Pos()) == g.line(lit.Elts[last].End()) {
		edits.replace(start, g.offset(lit.Elts[last+1].Pos()), "")
		return
	}

	// The end of the line: the comma in front of the run has to go too, unless
	// the run brings a comma of its own that closes the line.
	if first > 0 && g.line(lit.Elts[first-1].End()) == g.line(lit.Elts[first].Pos()) && !hasComma {
		edits.replace(g.offset(lit.Elts[first-1].End()), end, "")
		return
	}

	edits.replace(start, afterComma, "")
}

// contribAppendElement schedules the addition of text as the last element of
// the composite literal lit.
func contribAppendElement(g *goSource, edits *editSet, lit *ast.CompositeLit, text string) {
	src := g.src
	if len(lit.Elts) == 0 {
		at := g.offset(lit.Lbrace) + 1
		if g.line(lit.Lbrace) == g.line(lit.Rbrace) {
			edits.replace(at, at, text)
			return
		}
		edits.replace(at, at, "\n"+contribLineIndent(src, at-1)+"\t"+text+",")
		return
	}

	last := lit.Elts[len(lit.Elts)-1]
	end := g.offset(last.End())
	if g.line(last.End()) == g.line(lit.Rbrace) {
		// The closing brace shares the line: Config{A: 1} -> Config{A: 1, text}.
		if j := contribSkipBlanks(src, end); j < len(src) && src[j] == ',' {
			edits.replace(j+1, j+1, " "+text+",")
			return
		}
		edits.replace(end, end, ", "+text)
		return
	}

	// One element per line: the new one goes on a line of its own, below the
	// last element and the comment that may follow it.
	insertAt, eol := end+strings.IndexByte(src[end:], '\n'), "\n"
	if insertAt > 0 && src[insertAt-1] == '\r' {
		insertAt, eol = insertAt-1, "\r\n"
	}
	edits.replace(insertAt, insertAt, eol+contribLineIndent(src, g.offset(last.Pos()))+text+",")
}

// contribAddImport makes sure the file imports path and returns the name the
// code has to use for it. An import of path that exists already is reused under
// whatever name it has; blank and dot imports do not count. name is the
// explicit name to give a new import, "" for none.
func contribAddImport(g *goSource, edits *editSet, path, name string) string {
	want := strconv.Quote(path)
	for _, spec := range g.file.Imports {
		if spec.Path.Value != want {
			continue
		}
		switch {
		case spec.Name == nil:
			return defaultImportName(path)
		case spec.Name.Name != "_" && spec.Name.Name != ".":
			return spec.Name.Name
		}
	}

	line := want
	if name != "" {
		line = name + " " + want
	}

	var decls []*ast.GenDecl
	for _, d := range g.file.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			decls = append(decls, gd)
		}
	}

	switch {
	case len(decls) == 0:
		at := g.offset(g.file.Name.End())
		edits.replace(at, at, "\n\nimport "+line)
	case slices.ContainsFunc(decls, func(d *ast.GenDecl) bool { return d.Lparen.IsValid() }):
		for _, d := range slices.Backward(decls) {
			if d.Lparen.IsValid() {
				contribInsertIntoBlock(g, edits, d, path, line)
				break
			}
		}
	default:
		at := g.offset(decls[len(decls)-1].End())
		edits.replace(at, at, "\nimport "+line)
	}

	if name != "" {
		return name
	}
	return defaultImportName(path)
}

// contribInsertIntoBlock adds line to a parenthesized import declaration. A
// standard library import goes with the other ones at the top, anything else to
// the end of the last group.
func contribInsertIntoBlock(g *goSource, edits *editSet, decl *ast.GenDecl, path, line string) {
	src := g.src
	if len(decl.Specs) == 0 {
		at := g.offset(decl.Lparen) + 1
		edits.replace(at, at, "\n\t"+line)
		return
	}

	if contribIsStdPath(path) {
		firstSpec := decl.Specs[0]
		ls := contribLineStart(src, g.offset(firstSpec.Pos()))
		if spec, ok := firstSpec.(*ast.ImportSpec); ok && contribIsStdPath(contribUnquote(spec.Path.Value)) {
			edits.replace(ls, ls, "\t"+line+"\n")
			return
		}
		edits.replace(ls, ls, "\t"+line+"\n\n")
		return
	}

	lastSpec := decl.Specs[len(decl.Specs)-1]
	at := contribLineEnd(src, g.offset(lastSpec.End()))
	if at > 0 && src[at-1] != '\n' {
		edits.replace(at, at, "\n\t"+line)
		return
	}
	edits.replace(at, at, "\t"+line+"\n")
}

// contribIsStdPath reports whether path is in the standard library: the first
// element of its path has no dot.
func contribIsStdPath(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// contribStringLit returns the value of e when it is a string literal.
func contribStringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// contribKeyName returns the field name of a key: value element, "" for any
// other element.
func contribKeyName(e ast.Expr) string {
	kv, ok := e.(*ast.KeyValueExpr)
	if !ok {
		return ""
	}
	id, ok := kv.Key.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}

// contribIsPkgType reports whether e names the type typeName of one of the
// packages bound to names, behind any number of pointers: jwtware.Config,
// *jwtware.Config.
func contribIsPkgType(e ast.Expr, names []string, typeName string) bool {
	return isFiberType(e, names, typeName)
}

// contribIdentNames returns every identifier used anywhere in the file, which
// is what a new name has to stay clear of.
func contribIdentNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			names[id.Name] = true
		}
		return true
	})
	return names
}
