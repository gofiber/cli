package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ctxRender returns src[start:end] with the edits that lie inside that range
// applied. A rewrite that moves the text of a sub-expression into new text uses
// it to carry along the rewrites already scheduled for the sub-expression.
func ctxRender(e *editSet, src string, start, end int) string {
	var sub editSet
	for _, ed := range e.edits {
		if ed.start >= start && ed.end <= end {
			sub.replace(ed.start-start, ed.end-start, ed.text)
		}
	}
	return sub.apply(src[start:end])
}

// ctxRenderNode is ctxRender for the source text of n.
func ctxRenderNode(e *editSet, g *goSource, n ast.Node) string {
	return ctxRender(e, g.src, g.offset(n.Pos()), g.offset(n.End()))
}

// ctxStrayComments returns the comments between the parentheses of call that
// are not part of one of the arguments that are kept, as block comments, which
// are safe in any position of an expression. A rewrite that moves the arguments
// around, or drops some, would lose them.
func ctxStrayComments(g *goSource, call *ast.CallExpr, kept []ast.Expr) []string {
	var stray []string
	for _, group := range g.file.Comments {
		for _, c := range group.List {
			if c.Pos() < call.Lparen || c.End() > call.Rparen || ctxWithinAny(c, kept) {
				continue
			}
			text := c.Text
			if line, isLine := strings.CutPrefix(text, "//"); isLine {
				text = "/* " + strings.TrimSpace(strings.ReplaceAll(line, "*/", "* /")) + " */"
			}
			stray = append(stray, text)
		}
	}
	return stray
}

// ctxWithinAny reports whether n lies inside one of the nodes.
func ctxWithinAny(n ast.Node, nodes []ast.Expr) bool {
	for _, other := range nodes {
		if n.Pos() >= other.Pos() && n.End() <= other.End() {
			return true
		}
	}
	return false
}

// ctxImportName returns the name under which src imports path: the alias, or
// def for an import without one. It is empty when src does not import path for
// use by name (not at all, only blank, or with a dot).
func ctxImportName(src, path, def string) string {
	re := regexp.MustCompile("^" + regexp.QuoteMeta(path) + "$")
	for _, name := range importNames(src, re, def) {
		if name != "." {
			return name
		}
	}
	return ""
}

// ctxAddImport returns src with an import of path added, unless src already
// imports it by name. def is the package name an import without alias gets. The
// import is put next to the import that shares the most of its path, so that it
// lands in the right group of the import block.
func ctxAddImport(src, path, def string) string {
	return ctxAddImportAs(src, path, def, "")
}

// ctxAddImportAs is ctxAddImport for an import that gets the name alias, when that
// is not empty.
func ctxAddImportAs(src, path, def, alias string) string {
	if ctxImportName(src, path, def) != "" {
		return src
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil && file == nil {
		return src
	}
	spec := strconv.Quote(path)
	if alias != "" {
		spec = alias + " " + spec
	}

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}

		if !gd.Lparen.IsValid() {
			// import "x" becomes a block with both
			start, end := fset.Position(gd.Pos()).Offset, fset.Position(gd.End()).Offset
			existing := strings.TrimSpace(strings.TrimPrefix(src[start:end], "import"))
			return src[:start] + "import (\n\t" + existing + "\n\t" + spec + "\n)" + src[end:]
		}

		return ctxInsertImportSpec(src, fset, gd, path, spec)
	}

	at := fset.Position(file.Name.End()).Offset
	return src[:at] + "\n\nimport " + spec + src[at:]
}

// ctxAddNamedImport adds the import of path under the name alias, even when the
// source imports the package under another name. It does nothing when the source
// has that import already.
func ctxAddNamedImport(src, path, alias string) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil && file == nil {
		return src
	}
	for _, spec := range file.Imports {
		if p, err := strconv.Unquote(spec.Path.Value); err == nil && p == path && spec.Name != nil && spec.Name.Name == alias {
			return src
		}
	}
	spec := alias + " " + strconv.Quote(path)

	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}

		if !gd.Lparen.IsValid() {
			start, end := fset.Position(gd.Pos()).Offset, fset.Position(gd.End()).Offset
			existing := strings.TrimSpace(strings.TrimPrefix(src[start:end], "import"))
			return src[:start] + "import (\n\t" + existing + "\n\t" + spec + "\n)" + src[end:]
		}

		return ctxInsertImportSpec(src, fset, gd, path, spec)
	}

	at := fset.Position(file.Name.End()).Offset
	return src[:at] + "\n\nimport " + spec + src[at:]
}

// ctxUsesName reports whether the source uses name as the operand of a selector,
// which is how it refers to a package. A source that does not parse is taken to.
func ctxUsesName(src, name string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil && file == nil {
		return true
	}

	used := false
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && isIdentNamed(sel.X, name) {
			used = true
		}
		return !used
	})
	return used
}

// ctxInsertImportSpec adds the import spec to the import block gd.
func ctxInsertImportSpec(src string, fset *token.FileSet, gd *ast.GenDecl, path, spec string) string {
	best, bestShared := -1, 0
	for i, s := range gd.Specs {
		imp, ok := s.(*ast.ImportSpec)
		if !ok {
			continue
		}
		existing, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if shared := ctxSharedPrefix(existing, path); shared > 0 && shared >= bestShared {
			best, bestShared = i, shared
		}
	}

	at, text := fset.Position(gd.Rparen).Offset, "\t"+spec+"\n"
	switch {
	case best >= 0:
		// on the line after the import it goes with
		end := fset.Position(gd.Specs[best].End()).Offset
		if nl := strings.IndexByte(src[end:], '\n'); nl >= 0 {
			at = end + nl + 1
		} else {
			at, text = end, "\n\t"+spec
		}
	case !strings.Contains(strings.SplitN(path, "/", 2)[0], "."):
		// a package of the standard library goes first
		at, text = fset.Position(gd.Lparen).Offset+1, "\n\t"+spec
	case at > 0 && src[at-1] != '\n':
		text = "\n\t" + spec + "\n"
	}
	return src[:at] + text + src[at:]
}

// ctxSharedPrefix returns the length of the common prefix of a and b.
func ctxSharedPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// ctxIsIntLiteral reports whether e is an integer literal.
func ctxIsIntLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.INT
}

// ctxPlan queues the rewrites of one file. They run in reverse order of
// discovery, which is innermost first, so that a rewrite which moves the text of
// a sub-expression (see ctxRender) sees the rewrites of that sub-expression.
type ctxPlan struct {
	steps []func(*editSet)
}

// add queues a rewrite.
func (p *ctxPlan) add(step func(*editSet)) {
	p.steps = append(p.steps, step)
}

// qualify queues the detour through the embedded field via for the call of sel
// (see ctxEnv.receiverKindVia): c.Bind(...) becomes c.Ctx.Bind(...). Nothing is
// queued when via is empty.
func (p *ctxPlan) qualify(g *goSource, sel *ast.SelectorExpr, via string) {
	if via == "" {
		return
	}
	p.add(func(e *editSet) {
		at := g.offset(sel.X.End())
		e.replace(at, at, "."+via)
	})
}

// run runs the queued rewrites against edits, which may hold more edits of the
// same file already.
func (p *ctxPlan) run(edits *editSet) {
	for _, step := range slices.Backward(p.steps) {
		step(edits)
	}
}

// apply runs the queued rewrites and returns src with them applied.
func (p *ctxPlan) apply(src string) string {
	var edits editSet
	p.run(&edits)
	return edits.apply(src)
}

// ctxContainsAny reports whether src contains one of the words.
func ctxContainsAny(src string, words ...string) bool {
	return slices.ContainsFunc(words, func(word string) bool { return strings.Contains(src, word) })
}

// ctxParentCall returns the call that has sel as its function, given the
// ancestors of sel.
func ctxParentCall(sel *ast.SelectorExpr, stack []ast.Node) (*ast.CallExpr, bool) {
	if len(stack) == 0 {
		return nil, false
	}
	call, ok := stack[len(stack)-1].(*ast.CallExpr)
	return call, ok && call.Fun == sel
}
