package v3

import (
	"go/ast"
	"strings"
)

// mw3Field returns the element of the composite literal that is keyed by name,
// or nil.
func mw3Field(lit *ast.CompositeLit, name string) *ast.KeyValueExpr {
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok && isIdentNamed(kv.Key, name) {
			return kv
		}
	}
	return nil
}

// mw3LineStart returns the offset of the start of the line that holds off.
func mw3LineStart(src string, off int) int {
	return strings.LastIndexByte(src[:off], '\n') + 1
}

// mw3LineEnd returns the offset of the newline that ends the line holding off,
// or the length of src when it is the last line.
func mw3LineEnd(src string, off int) int {
	if i := strings.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i
	}
	return len(src)
}

// mw3Indent returns the leading whitespace of the line that holds off.
func mw3Indent(src string, off int) string {
	line := src[mw3LineStart(src, off):off]
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// mw3AddElement schedules text as the last element of the composite literal. A
// literal that spans lines gets the element on a line of its own.
func mw3AddElement(g *goSource, edits *editSet, lit *ast.CompositeLit, text string) {
	if len(lit.Elts) == 0 {
		// The literal may hold nothing but a comment on a line of its own.
		if g.line(lit.Rbrace) > g.line(lit.Lbrace) {
			text += ","
		}
		open := g.offset(lit.Lbrace) + 1
		edits.replace(open, open, text)
		return
	}

	last := lit.Elts[len(lit.Elts)-1]
	end := g.offset(last.End())
	if g.line(lit.Rbrace) > g.line(last.End()) {
		lineEnd := mw3LineEnd(g.src, end)
		edits.replace(lineEnd, lineEnd, "\n"+mw3Indent(g.src, g.offset(last.Pos()))+text+",")
		return
	}
	edits.replace(end, end, ", "+text)
}

// mw3RemoveElement schedules the removal of the element of a composite literal,
// together with its comma, and with its line when it has the line to itself.
func mw3RemoveElement(g *goSource, edits *editSet, elt ast.Expr) {
	src := g.src
	start, end := g.offset(elt.Pos()), g.offset(elt.End())

	// The comma that follows the element, if there is one.
	afterComma := end
	for afterComma < len(src) && (src[afterComma] == ' ' || src[afterComma] == '\t') {
		afterComma++
	}
	if afterComma < len(src) && src[afterComma] == ',' {
		afterComma++
	} else {
		afterComma = end
	}

	lineStart := mw3LineStart(src, start)
	ownLine := strings.TrimLeft(src[lineStart:start], " \t") == ""
	if ownLine {
		rest := strings.TrimLeft(src[afterComma:mw3LineEnd(src, afterComma)], " \t")
		ownLine = rest == "" || strings.HasPrefix(rest, "//")
	}

	if !ownLine {
		// What is left, a trailing comma included, is valid in a literal.
		edits.replace(start, afterComma, "")
		return
	}
	lineEnd := mw3LineEnd(src, afterComma)
	if lineEnd < len(src) {
		lineEnd++
	}
	edits.replace(lineStart, lineEnd, "")
}
