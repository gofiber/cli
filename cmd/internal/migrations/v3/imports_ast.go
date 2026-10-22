package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// importsSpec is one import declaration of a file.
type importsSpec struct {
	node *ast.ImportSpec
	path string
	// alias is the explicit name of the import, empty when it has none. A blank
	// import is "_" and a dot import ".".
	alias string
}

// importsOf returns the imports of g whose path satisfies match, in file order.
func importsOf(g *goSource, match func(path string) bool) []importsSpec {
	var specs []importsSpec
	for _, node := range g.file.Imports {
		path, err := strconv.Unquote(node.Path.Value)
		if err != nil || !match(path) {
			continue
		}
		spec := importsSpec{node: node, path: path}
		if node.Name != nil {
			spec.alias = node.Name.Name
		}
		specs = append(specs, spec)
	}
	return specs
}

func (s importsSpec) blank() bool { return s.alias == "_" }

func (s importsSpec) dot() bool { return s.alias == "." }

// local returns the identifier the import is referred to by: its alias or, for
// an import without one, def, the package name.
func (s importsSpec) local(def string) string {
	if s.alias != "" {
		return s.alias
	}
	return def
}

// importsNameOf returns the identifier that the file binds to the import of
// path, "" when the file does not import it by a usable name (it does not
// import it, or only blank or dot). def is the package name of path.
func importsNameOf(g *goSource, path, def string) string {
	for _, s := range importsOf(g, func(p string) bool { return p == path }) {
		if !s.blank() && !s.dot() {
			return s.local(def)
		}
	}
	return ""
}

// importsBinds reports whether the file binds name to an import, which makes
// name unusable for another package.
func importsBinds(g *goSource, name string) bool {
	for _, s := range importsOf(g, func(string) bool { return true }) {
		if !s.blank() && !s.dot() && s.local(defaultImportName(s.path)) == name {
			return true
		}
	}
	return false
}

// importsRewritePath schedules the replacement of the import path of s, keeping
// the quotes it was written with.
func importsRewritePath(g *goSource, edits *editSet, s importsSpec, path string) {
	quoted := strconv.Quote(path)
	if strings.HasPrefix(s.node.Path.Value, "`") {
		quoted = "`" + path + "`"
	}
	edits.replaceNode(g, s.node.Path, quoted)
}

// importsAdd schedules the addition of an import to the file. name is the alias
// to write, empty for none. The caller makes sure that the file does not import
// path yet; the import block is sorted and grouped by imports.Process later.
func importsAdd(g *goSource, edits *editSet, name, path string) {
	spec := strconv.Quote(path)
	if name != "" {
		spec = name + " " + spec
	}

	for _, decl := range g.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}

		if gen.Lparen.IsValid() {
			// Insert on its own line before the closing parenthesis.
			end := g.offset(gen.Rparen)
			lineStart := strings.LastIndexByte(g.src[:end], '\n') + 1
			if strings.TrimLeft(g.src[lineStart:end], " \t") == "" {
				edits.replace(lineStart, lineStart, "\t"+spec+"\n")
			} else {
				edits.replace(end, end, "\n\t"+spec+"\n")
			}
			return
		}

		// import "a" becomes a block with both imports.
		first := g.text(gen.Specs[0])
		edits.replaceNode(g, gen, "import (\n\t"+first+"\n\t"+spec+"\n)")
		return
	}

	// No import declaration at all: put one after the package clause.
	end := g.offset(g.file.Name.End())
	edits.replace(end, end, "\n\nimport "+spec)
}

// importsShadowed reports whether the identifier name used at pos refers to a
// declaration of an enclosing scope instead of to a package-level name or an
// import. stack holds the ancestors of the use (see goSource.walk).
func importsShadowed(name string, pos token.Pos, stack []ast.Node) bool {
	for _, node := range slices.Backward(stack) {
		switch n := node.(type) {
		case *ast.FuncDecl:
			if n.Body != nil && pos > n.Body.Lbrace &&
				(importsFieldsDeclare(n.Recv, name) || importsFieldsDeclare(n.Type.TypeParams, name) ||
					importsFieldsDeclare(n.Type.Params, name) || importsFieldsDeclare(n.Type.Results, name)) {
				return true
			}
		case *ast.FuncLit:
			if pos > n.Body.Lbrace && (importsFieldsDeclare(n.Type.Params, name) || importsFieldsDeclare(n.Type.Results, name)) {
				return true
			}
		case *ast.BlockStmt:
			if importsDeclaredBefore(n.List, name, pos) {
				return true
			}
		case *ast.CaseClause:
			if importsDeclaredBefore(n.Body, name, pos) {
				return true
			}
		case *ast.CommClause:
			if importsDeclaredBefore(append([]ast.Stmt{n.Comm}, n.Body...), name, pos) {
				return true
			}
		case *ast.IfStmt:
			if importsDeclaredBefore([]ast.Stmt{n.Init}, name, pos) {
				return true
			}
		case *ast.ForStmt:
			if importsDeclaredBefore([]ast.Stmt{n.Init}, name, pos) {
				return true
			}
		case *ast.SwitchStmt:
			if importsDeclaredBefore([]ast.Stmt{n.Init}, name, pos) {
				return true
			}
		case *ast.TypeSwitchStmt:
			if importsDeclaredBefore([]ast.Stmt{n.Init, n.Assign}, name, pos) {
				return true
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE && pos > n.Body.Lbrace && (isIdentNamed(n.Key, name) || isIdentNamed(n.Value, name)) {
				return true
			}
		}
	}
	return false
}

// importsFieldsDeclare reports whether one of the fields is named name.
func importsFieldsDeclare(fields *ast.FieldList, name string) bool {
	if fields == nil {
		return false
	}
	for _, f := range fields.List {
		for _, id := range f.Names {
			if id.Name == name {
				return true
			}
		}
	}
	return false
}

// importsDeclaredBefore reports whether one of the statements that end before
// pos declares name: a short variable declaration, a var, const or type.
func importsDeclaredBefore(stmts []ast.Stmt, name string, pos token.Pos) bool {
	for _, stmt := range stmts {
		if stmt == nil || stmt.End() > pos {
			continue
		}

		switch s := stmt.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE && slices.ContainsFunc(s.Lhs, func(e ast.Expr) bool { return isIdentNamed(e, name) }) {
				return true
			}
		case *ast.DeclStmt:
			gen, ok := s.Decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				switch sp := spec.(type) {
				case *ast.ValueSpec:
					if slices.ContainsFunc(sp.Names, func(id *ast.Ident) bool { return id.Name == name }) {
						return true
					}
				case *ast.TypeSpec:
					if sp.Name.Name == name {
						return true
					}
				}
			}
		}
	}
	return false
}
