package v3

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// reFiberCorePath matches the import path of the core fiber module at any
// major version.
var reFiberCorePath = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+$`)

// fiberNames returns the identifiers src binds to the core fiber package.
func fiberNames(src string) []string {
	return importNames(src, reFiberCorePath, "fiber")
}

// importNames returns the identifiers src binds to imports whose path matches
// pathRe, sorted and without duplicates. An import with an explicit alias is
// reported under that alias. An import without one is reported under each of
// defaults, because the package name cannot be derived from the path alone
// (jwtware lives at .../contrib/jwt); when defaults is empty the last path
// element is used instead. Blank imports are ignored and a dot import is
// reported as ".".
//
// Only the import block is parsed, so it works on files that have syntax errors
// further down.
func importNames(src string, pathRe *regexp.Regexp, defaults ...string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil && file == nil {
		return nil
	}

	var names []string
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !pathRe.MatchString(path) {
			continue
		}
		switch {
		case spec.Name != nil && spec.Name.Name == "_":
		case spec.Name != nil:
			names = append(names, spec.Name.Name)
		case len(defaults) > 0:
			names = append(names, defaults...)
		default:
			names = append(names, defaultImportName(path))
		}
	}

	slices.Sort(names)
	return slices.Compact(names)
}

// isImported reports whether src imports a package whose path matches pathRe.
func isImported(src string, pathRe *regexp.Regexp) bool {
	return len(importNames(src, pathRe)) > 0
}

// reMajorVersion matches a major version path element such as v2.
var reMajorVersion = regexp.MustCompile(`^v\d+$`)

// defaultImportName guesses the package name of an import path: the last
// element, or the one before it for a major version suffix.
func defaultImportName(path string) string {
	elems := strings.Split(path, "/")
	name := elems[len(elems)-1]
	if reMajorVersion.MatchString(name) && len(elems) > 1 {
		name = elems[len(elems)-2]
	}
	if idx := strings.Index(name, "."); idx > 0 { // gopkg.in/yaml.v3
		name = name[:idx]
	}
	return name
}

// goSource is a parsed Go file together with the text it was parsed from.
type goSource struct {
	fset *token.FileSet
	file *ast.File
	src  string
}

// parseSource parses src. ok is false when src is not valid Go.
func parseSource(src string) (g *goSource, ok bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	return &goSource{fset: fset, file: file, src: src}, true
}

// parseFile is parseSource for a file being migrated: when the file does not
// parse, the user is told that it was skipped.
func parseFile(fi internal.FileInfo, src string) (g *goSource, ok bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fi.Path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		internal.NoteParseError(fi, err)
		return nil, false
	}
	return &goSource{fset: fset, file: file, src: src}, true
}

// offset returns the byte offset of pos within the source.
func (g *goSource) offset(pos token.Pos) int {
	return g.fset.Position(pos).Offset
}

// line returns the 1-based line of pos within the source.
func (g *goSource) line(pos token.Pos) int {
	return g.fset.Position(pos).Line
}

// text returns the source text of n.
func (g *goSource) text(n ast.Node) string {
	return g.src[g.offset(n.Pos()):g.offset(n.End())]
}

// walk calls fn for every node of the file together with the chain of its
// ancestors (outermost first, n itself excluded). Returning false skips the
// children of n.
func (g *goSource) walk(fn func(n ast.Node, stack []ast.Node) bool) {
	var stack []ast.Node
	ast.Inspect(g.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if !fn(n, stack) {
			return false
		}
		stack = append(stack, n)
		return true
	})
}

// textEdit replaces the bytes [start, end) of a source text.
type textEdit struct {
	text  string
	start int
	end   int
}

// editSet collects replacements for a source text so that they can be applied
// in one pass, which keeps the offsets of the AST valid while it is walked.
type editSet struct {
	edits []textEdit
}

// replace schedules the replacement of src[start:end] with text.
func (e *editSet) replace(start, end int, text string) {
	e.edits = append(e.edits, textEdit{start: start, end: end, text: text})
}

// replaceNode schedules the replacement of n with text.
func (e *editSet) replaceNode(g *goSource, n ast.Node, text string) {
	e.replace(g.offset(n.Pos()), g.offset(n.End()), text)
}

// apply returns src with the edits applied. An edit that overlaps an earlier
// one (the outer edit wins) is dropped.
func (e *editSet) apply(src string) string {
	if len(e.edits) == 0 {
		return src
	}

	slices.SortStableFunc(e.edits, func(a, b textEdit) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end))
	})

	out := make([]byte, 0, len(src))
	pos := 0
	for _, ed := range e.edits {
		if ed.start < pos || ed.end < ed.start || ed.end > len(src) {
			continue
		}
		out = append(out, src[pos:ed.start]...)
		out = append(out, ed.text...)
		pos = ed.end
	}
	out = append(out, src[pos:]...)
	return string(out)
}

// decl describes the declaration an identifier refers to.
type decl struct {
	// typ is the declared type, nil when the declaration has none.
	typ ast.Expr
	// init is the initializing expression, nil when there is none. For a name
	// that is one of several results of a single call, init is that call.
	init ast.Expr
}

// resolveIdent finds the declaration that name refers to at pos, given the
// ancestors of the use (see goSource.walk). It understands parameters,
// receivers, results, short variable declarations, var declarations and
// package-level variables; ok is false when none of them declares name or when
// name is declared in a way that says nothing about its type (range variables,
// type switch bindings).
func resolveIdent(name string, pos token.Pos, stack []ast.Node) (d decl, ok bool) {
	for _, node := range slices.Backward(stack) {
		switch n := node.(type) {
		case *ast.BlockStmt:
			if d, found, ok := declInStmts(name, pos, n.List); found {
				return d, ok
			}
		case *ast.CaseClause:
			if d, found, ok := declInStmts(name, pos, n.Body); found {
				return d, ok
			}
		case *ast.CommClause:
			if d, found, ok := declInStmts(name, pos, n.Body); found {
				return d, ok
			}
		case *ast.IfStmt:
			if d, found, ok := declInStmts(name, pos, []ast.Stmt{n.Init}); found {
				return d, ok
			}
		case *ast.ForStmt:
			if d, found, ok := declInStmts(name, pos, []ast.Stmt{n.Init}); found {
				return d, ok
			}
		case *ast.SwitchStmt:
			if d, found, ok := declInStmts(name, pos, []ast.Stmt{n.Init}); found {
				return d, ok
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE && (isIdentNamed(n.Key, name) || isIdentNamed(n.Value, name)) {
				return decl{}, false
			}
		case *ast.TypeSwitchStmt:
			if as, isAssign := n.Assign.(*ast.AssignStmt); isAssign && len(as.Lhs) == 1 && isIdentNamed(as.Lhs[0], name) {
				return decl{}, false
			}
			if d, found, ok := declInStmts(name, pos, []ast.Stmt{n.Init}); found {
				return d, ok
			}
		case *ast.FuncLit:
			if d, found := declInFields(name, n.Type); found {
				return d, true
			}
		case *ast.FuncDecl:
			if n.Recv != nil {
				if d, found := declInFieldList(name, n.Recv); found {
					return d, true
				}
			}
			if d, found := declInFields(name, n.Type); found {
				return d, true
			}
		case *ast.File:
			for _, gd := range n.Decls {
				if d, found, ok := declInStmts(name, token.NoPos, []ast.Stmt{&ast.DeclStmt{Decl: gd}}); found {
					return d, ok
				}
			}
		}
	}
	return decl{}, false
}

func isIdentNamed(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// declInFields looks name up in the parameters and results of ft.
func declInFields(name string, ft *ast.FuncType) (d decl, found bool) {
	if d, found = declInFieldList(name, ft.Params); found {
		return d, true
	}
	return declInFieldList(name, ft.Results)
}

func declInFieldList(name string, fl *ast.FieldList) (d decl, found bool) {
	if fl == nil {
		return decl{}, false
	}
	for _, f := range fl.List {
		for _, id := range f.Names {
			if id.Name == name {
				return decl{typ: f.Type}, true
			}
		}
	}
	return decl{}, false
}

// declInStmts looks for the declaration of name among the statements that
// precede pos (all of them when pos is token.NoPos), closest first. found is
// whether a statement declares name, ok whether the declaration says anything
// about its type.
func declInStmts(name string, pos token.Pos, stmts []ast.Stmt) (d decl, found, ok bool) {
	for _, stmt := range slices.Backward(stmts) {
		if stmt == nil || (pos != token.NoPos && stmt.End() > pos) {
			continue
		}

		switch s := stmt.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				continue
			}
			for j, lhs := range s.Lhs {
				if !isIdentNamed(lhs, name) {
					continue
				}
				if len(s.Rhs) == len(s.Lhs) {
					return decl{init: s.Rhs[j]}, true, true
				}
				if len(s.Rhs) == 1 {
					return decl{init: s.Rhs[0]}, true, true
				}
				return decl{}, true, false
			}
		case *ast.DeclStmt:
			gd, isGen := s.Decl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, isValue := spec.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				for j, id := range vs.Names {
					if id.Name != name {
						continue
					}
					d = decl{typ: vs.Type}
					switch {
					case len(vs.Values) == len(vs.Names):
						d.init = vs.Values[j]
					case len(vs.Values) == 1:
						d.init = vs.Values[0]
					}
					return d, true, true
				}
			}
		}
	}
	return decl{}, false, false
}

// isFiberType reports whether e names the type typeName of the core fiber
// package, possibly behind pointers: fiber.Ctx, *fiber.Ctx, f.Ctx for an alias.
// names are the identifiers the file binds to the fiber package.
func isFiberType(e ast.Expr, names []string, typeName string) bool {
	for {
		star, ok := e.(*ast.StarExpr)
		if !ok {
			break
		}
		e = star.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != typeName {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains(names, id.Name)
}

// ctxChainMethods are the fiber.Ctx methods that return the context again, so
// that c.Status(404).SendFile(...) is still a call on a context.
var ctxChainMethods = []string{"Status", "Type"}

// isFiberCtxExpr reports whether e is a fiber request context: an identifier
// declared as one, possibly followed by chaining methods such as Status.
// names are the identifiers the file binds to the fiber package and stack the
// ancestors of e (see goSource.walk).
func isFiberCtxExpr(e ast.Expr, names []string, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return isFiberCtxExpr(x.X, names, stack)
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		return ok && d.typ != nil && isFiberType(d.typ, names, "Ctx")
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		return ok && slices.Contains(ctxChainMethods, sel.Sel.Name) && isFiberCtxExpr(sel.X, names, stack)
	}
	return false
}
