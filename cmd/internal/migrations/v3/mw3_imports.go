package v3

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// mw3PathRegexp returns an expression matching exactly the import path.
func mw3PathRegexp(path string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(path) + `$`)
}

// mw3ImportName returns the identifier src uses for the package at path: the
// alias of its import or the name its path ends in. It is empty when src does
// not import path, or only blank or dot imports it, which name nothing.
func mw3ImportName(src, path string) string {
	for _, name := range importNames(src, mw3PathRegexp(path)) {
		if name != "." {
			return name
		}
	}
	return ""
}

// mw3ImportEdits collects the changes a rewrite makes to the import block, so
// that they are applied to the rewritten source in one pass. The migrations
// cannot leave this to goimports: it only finds a package that is in the module
// cache, and it silently does nothing when the module cannot be loaded.
type mw3ImportEdits struct {
	add  []mw3ImportAdd
	drop []string
}

type mw3ImportAdd struct {
	path string
	name string
}

// require schedules the import of path under name. A name that is the package's
// own name, or empty, imports it without an alias. A package the file already
// imports, under whatever name, is left as it is.
func (e *mw3ImportEdits) require(path, name string) {
	if !slices.ContainsFunc(e.add, func(a mw3ImportAdd) bool { return a.path == path }) {
		e.add = append(e.add, mw3ImportAdd{path: path, name: name})
	}
}

// dropIfUnused schedules the removal of the import of path, which only happens
// when nothing in the rewritten source refers to it any more.
func (e *mw3ImportEdits) dropIfUnused(path string) {
	if !slices.Contains(e.drop, path) {
		e.drop = append(e.drop, path)
	}
}

// releaseIn schedules the removal of the imports the node refers to, for a
// rewrite that takes the node out of the source. Each stays when something else
// still uses it.
func (e *mw3ImportEdits) releaseIn(g *goSource, node ast.Node) {
	paths := map[string]string{}
	for _, spec := range g.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := defaultImportName(path)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		paths[name] = path
	}

	ast.Inspect(node, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				if path, imported := paths[id.Name]; imported {
					e.dropIfUnused(path)
				}
			}
		}
		return true
	})
}

// apply returns src with the scheduled changes made. The source is returned as
// it is when there is nothing to change or when it does not parse.
func (e *mw3ImportEdits) apply(src string) string {
	if len(e.add) == 0 && len(e.drop) == 0 {
		return src
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return src
	}

	changed := false
	for _, imp := range e.add {
		if mw3Imports(file, imp.path) {
			continue
		}
		alias := ""
		if imp.name != "" && imp.name != defaultImportName(imp.path) {
			alias = imp.name
		}
		changed = astutil.AddNamedImport(fset, file, alias, imp.path) || changed
	}
	for _, path := range e.drop {
		changed = mw3DeleteUnusedImport(fset, file, path) || changed
	}
	if !changed {
		return src
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return src
	}
	return buf.String()
}

// mw3Imports reports whether file has a usable import of path.
func mw3Imports(file *ast.File, path string) bool {
	for _, spec := range file.Imports {
		if p, err := strconv.Unquote(spec.Path.Value); err == nil && p == path && (spec.Name == nil || spec.Name.Name != "_") {
			return true
		}
	}
	return false
}

// mw3DeleteUnusedImport removes the import of path from file when the file does
// not use it. Blank and dot imports are kept: they are there for their effect.
// So is an import whose name cannot be told from its path, which only the
// package itself declares: the standard library and the fiber modules are
// known, anything else must carry an alias to be removed.
func mw3DeleteUnusedImport(fset *token.FileSet, file *ast.File, path string) bool {
	changed := false
	for _, spec := range slices.Clone(file.Imports) {
		if p, err := strconv.Unquote(spec.Path.Value); err != nil || p != path {
			continue
		}
		alias, name := "", mw3KnownPackageName(path)
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				continue
			}
			alias, name = spec.Name.Name, spec.Name.Name
		}
		if name == "" || mw3UsesName(file, name) {
			continue
		}
		changed = astutil.DeleteNamedImport(fset, file, alias, path) || changed
	}
	return changed
}

// mw3KnownPackageName returns the name of the package at path when the path
// says it: the standard library and the modules of Fiber name their packages
// after the last element of the path. It is empty for any other package, whose
// name is its own business.
func mw3KnownPackageName(path string) string {
	first, _, _ := strings.Cut(path, "/")
	if !strings.Contains(first, ".") || strings.HasPrefix(path, "github.com/gofiber/") {
		return defaultImportName(path)
	}
	return ""
}

// mw3UsesName reports whether the file refers to a package by name: a selector
// on an identifier that no declaration in the file binds. The file must have
// been parsed with its objects resolved.
func mw3UsesName(file *ast.File, name string) bool {
	used := false
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == name && id.Obj == nil {
				used = true
			}
		}
		return !used
	})
	return used
}

// mw3DeclaredNames returns every name src declares: the imports, package level
// declarations and everything declared inside functions. A new import has to
// pick a name outside of it, or it would shadow or collide with one of them.
func mw3DeclaredNames(file *ast.File) map[string]struct{} {
	names := map[string]struct{}{}
	add := func(id *ast.Ident) {
		if id != nil {
			names[id.Name] = struct{}{}
		}
	}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, id := range f.Names {
				add(id)
			}
		}
	}

	for _, spec := range file.Imports {
		if spec.Name != nil {
			add(spec.Name)
			continue
		}
		if p, err := strconv.Unquote(spec.Path.Value); err == nil {
			names[defaultImportName(p)] = struct{}{}
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ValueSpec:
			for _, id := range x.Names {
				add(id)
			}
		case *ast.TypeSpec:
			add(x.Name)
		case *ast.FuncDecl:
			if x.Recv == nil {
				add(x.Name)
			}
			addFields(x.Recv)
		case *ast.FuncType:
			addFields(x.Params)
			addFields(x.Results)
			addFields(x.TypeParams)
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						add(id)
					}
				}
			}
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE {
				if id, ok := x.Key.(*ast.Ident); ok {
					add(id)
				}
				if id, ok := x.Value.(*ast.Ident); ok {
					add(id)
				}
			}
		}
		return true
	})

	return names
}

// mw3FreeName returns the first candidate that is not in taken, or the last
// candidate with digits appended when all of them are.
func mw3FreeName(taken func(string) bool, candidates ...string) string {
	for _, c := range candidates {
		if !taken(c) {
			return c
		}
	}
	base := candidates[len(candidates)-1]
	for i := 2; ; i++ {
		if name := base + strconv.Itoa(i); !taken(name) {
			return name
		}
	}
}
