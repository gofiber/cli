package v3

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
)

// appValueNeeds describes what an expression needs from the code around it, so
// that it can be moved elsewhere and still mean the same.
type appValueNeeds struct {
	locals []appBinding
	// imports are the local names of imported packages the expression uses.
	imports []string
	// pkgLevel is set when the expression uses a name of its own package.
	pkgLevel bool
	// hasFunc is set when the expression holds a function literal.
	hasFunc bool
	// effects is set when evaluating the expression changes something, as
	// defining a flag or receiving from a channel does, so that evaluating it
	// later or more than once is not the same.
	effects bool
	// unknown is set when an identifier could not be classified.
	unknown bool
}

// valueNeeds classifies the identifiers e refers to.
func (f *appFile) valueNeeds(e ast.Expr) appValueNeeds {
	var needs appValueNeeds
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			needs.hasFunc = true
		case *ast.UnaryExpr:
			needs.effects = needs.effects || x.Op == token.ARROW
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if id, isIdent := sel.X.(*ast.Ident); isIdent && f.importPathOf(id.Name) == "flag" {
					needs.effects = true
				}
			}
		case *ast.Ident:
			f.classifyIdent(x, e, &needs)
		}
		return true
	})
	return needs
}

// classifyIdent adds what the identifier id, part of the expression whole,
// needs to needs.
func (f *appFile) classifyIdent(id *ast.Ident, whole ast.Expr, needs *appValueNeeds) {
	switch p := f.parent[id].(type) {
	case *ast.SelectorExpr:
		if p.Sel == id {
			return // a field or method name
		}
	case *ast.KeyValueExpr:
		if p.Key == id {
			if lit, ok := f.parent[p].(*ast.CompositeLit); ok && lit.Type != nil {
				if _, isMap := lit.Type.(*ast.MapType); !isMap {
					return // a struct field name
				}
			}
		}
	}

	if types.Universe.Lookup(id.Name) != nil {
		return
	}
	if f.importPathOf(id.Name) != "" {
		if !slices.Contains(needs.imports, id.Name) {
			needs.imports = append(needs.imports, id.Name)
		}
		return
	}

	if b, ok := f.binding(id); ok {
		switch {
		case b.scope == nil:
			needs.pkgLevel = true
		case b.scope.Pos() >= whole.Pos() && b.scope.End() <= whole.End():
			// declared inside the expression, for example by a function literal
		default:
			needs.locals = append(needs.locals, b)
		}
		return
	}
	if pkg := f.ix.pkgOf(f); pkg != nil && pkg.names[id.Name] {
		needs.pkgLevel = true
		return
	}
	if f.declaredWithin(id, whole) {
		return
	}
	needs.unknown = true
}

// declaredWithin reports whether id is a name that a function literal inside
// whole declares (a parameter, a label or a result).
func (f *appFile) declaredWithin(id *ast.Ident, whole ast.Expr) bool {
	for p := f.parent[id]; p != nil && p != f.parent[whole]; p = f.parent[p] {
		if _, ok := p.(*ast.FuncLit); ok {
			return true
		}
	}
	return false
}

// importPathOf returns the path of the import f refers to as name, empty when
// name is not an import of f.
func (f *appFile) importPathOf(name string) string {
	for _, imp := range f.imports {
		switch {
		case imp.name == "_" || imp.name == ".":
		case imp.name != "":
			if imp.name == name {
				return imp.path
			}
		case defaultImportName(imp.path) == name:
			return imp.path
		default:
			if pkg := f.ix.packageAtImport(imp.path); pkg != nil && pkg.name == name {
				return imp.path
			}
		}
	}
	return ""
}

// valueProblem reports why the expression e, written in f, cannot be moved to
// the node at of the file target; it returns "" when it can. Imports the
// target needs for it are recorded in imps.
func (f *appFile) valueProblem(e ast.Expr, target *appFile, at ast.Node, imps *appImports) string {
	needs := f.valueNeeds(e)
	if needs.unknown {
		return "it uses a name that could not be resolved"
	}
	if needs.effects {
		return "evaluating it has side effects, which would happen later"
	}
	if needs.hasFunc {
		decl := f.enclosingDecl(e)
		if f != target || decl == nil || decl != target.enclosingDecl(at) {
			return "it holds a function literal"
		}
	}

	for _, b := range needs.locals {
		if b.file != target || b.scope.Pos() > at.Pos() || b.scope.End() < at.End() {
			return "it uses " + b.name + ", which is not in scope there"
		}
		if b.init != nil {
			if stmt := b.file.parent[b.init]; stmt != nil && stmt.End() > at.Pos() {
				return "it uses " + b.name + ", which is declared after that call"
			}
		}
	}

	if needs.pkgLevel && f.ix.pkgOf(f) != f.ix.pkgOf(target) {
		return "it uses a name of another package"
	}

	if f != target {
		for _, name := range needs.imports {
			if imps.qualifier(target, f.importPathOf(name), name) != name+"." {
				return "it uses the package " + name + ", which that file imports under another name"
			}
		}
		return ""
	}
	// a variable of the code around the call can have the name of a package the value uses
	for _, name := range needs.imports {
		if target.hiddenAt(name, at.Pos()) {
			return "it uses the package " + name + ", which a variable of that name hides where that call is"
		}
	}
	return ""
}
