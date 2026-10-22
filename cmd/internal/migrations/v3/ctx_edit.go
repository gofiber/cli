package v3

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
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

// ctxCommentsIn returns the comments of the file that lie within [from, to), as
// block comments, which are safe in any position of an expression.
func ctxCommentsIn(g *goSource, from, to token.Pos) []string {
	var out []string
	for _, group := range g.file.Comments {
		for _, c := range group.List {
			if c.Pos() < from || c.End() > to {
				continue
			}
			text := c.Text
			if line, isLine := strings.CutPrefix(text, "//"); isLine {
				text = "/* " + strings.TrimSpace(strings.ReplaceAll(line, "*/", "* /")) + " */"
			}
			out = append(out, text)
		}
	}
	return out
}

// ctxRunNotes collects what a migration has to say once for the whole project
// rather than at each place: a behavior change that applies to every use of an
// API is reported at its first use, with the number of the others.
type ctxRunNotes struct {
	kinds map[string]*ctxKindNote
	order []string
}

// ctxKindNote is the first place of a kind of note, and how many places there are.
type ctxKindNote struct {
	file    string
	message string
	line    int
	places  int
}

func newCtxRunNotes() *ctxRunNotes {
	return &ctxRunNotes{kinds: map[string]*ctxKindNote{}}
}

// report records the collected notes for the project in root.
func (r *ctxRunNotes) report(root string) {
	for _, kind := range r.order {
		k := r.kinds[kind]
		msg := k.message
		if k.places > 1 {
			msg += fmt.Sprintf(" (%d more in this project)", k.places-1)
		}
		internal.AddNote(root, internal.Note{File: k.file, Line: k.line, Message: msg})
	}
}

// ctxNoter hands the notes of one file to the internal package.
type ctxNoter struct {
	g   *goSource
	run *ctxRunNotes
	fi  internal.FileInfo
}

func newCtxNoter(fi internal.FileInfo, g *goSource, run *ctxRunNotes) *ctxNoter {
	return &ctxNoter{fi: fi, g: g, run: run}
}

// notef records a note for the construct at pos.
func (n *ctxNoter) notef(pos token.Pos, format string, args ...any) {
	internal.Notef(n.fi, n.g.line(pos), format, args...)
}

// oncef records a note of the given kind for the construct at pos. Only the first
// place of a kind in the project is reported, with the number of the others.
func (n *ctxNoter) oncef(kind string, pos token.Pos, format string, args ...any) {
	if k, seen := n.run.kinds[kind]; seen {
		k.places++
		return
	}
	n.run.kinds[kind] = &ctxKindNote{file: n.fi.Rel, line: n.g.line(pos), message: fmt.Sprintf(format, args...), places: 1}
	n.run.order = append(n.run.order, kind)
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
	if ctxImportName(src, path, def) != "" {
		return src
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil && file == nil {
		return src
	}
	spec := strconv.Quote(path)

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

// apply runs the queued rewrites and returns src with them applied.
func (p *ctxPlan) apply(src string) string {
	var edits editSet
	for _, step := range slices.Backward(p.steps) {
		step(&edits)
	}
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
