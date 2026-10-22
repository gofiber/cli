package v3

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
)

// use returns the name the code of the migration refers to the package path
// by, and remembers that the file may need to import it. defName is the name
// the package declares; when the file already imports the package the name it
// imports it under is used, and when defName is taken by something else in the
// file or the package an alias is chosen.
func (f *clientFile) use(path, defName string) string {
	if name, ok := f.pkgNames[path]; ok {
		return name
	}
	name := defName
	if imported, ok := f.importedAs(path, defName); ok {
		name = imported
	} else if f.nameTaken(defName) {
		name = f.pkgAlias(path, defName)
	}
	f.pkgNames[path] = name
	return name
}

// importedAs returns the name the file imports path under.
func (f *clientFile) importedAs(path, defName string) (string, bool) {
	for _, spec := range f.g.file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil || p != path {
			continue
		}
		switch {
		case spec.Name == nil:
			return defName, true
		case spec.Name.Name == "_" || spec.Name.Name == ".":
			continue
		default:
			return spec.Name.Name, true
		}
	}
	return "", false
}

// nameTaken reports whether name is declared or used in the file, or declared
// at package level in another file of the package.
func (f *clientFile) nameTaken(name string) bool {
	if f.idents == nil {
		f.idents = map[string]bool{}
		ast.Inspect(f.g.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				ast.Inspect(x.X, func(c ast.Node) bool {
					if id, ok := c.(*ast.Ident); ok {
						f.idents[id.Name] = true
					}
					return true
				})
				return false
			case *ast.Ident:
				f.idents[x.Name] = true
			}
			return true
		})
	}
	return f.idents[name] || f.pkgs.declared(filepath.Dir(f.fi.Path), f.g.file.Name.Name, name)
}

// pkgAlias picks an import alias for path that nothing else uses.
func (f *clientFile) pkgAlias(path, defName string) string {
	base := defName + "pkg"
	if path == clientPkgPath {
		base = "fiberclient"
	}
	name := base
	for i := 2; f.nameTaken(name); i++ {
		name = base + strconv.Itoa(i)
	}
	return name
}

// fixImports adds the imports the rewritten code needs and removes the import
// of the fiber package when the code no longer uses it.
func (f *clientFile) fixImports(src string) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return src
	}

	// A declaration of one import that had no parentheses keeps none.
	var bare *ast.GenDecl
	for _, d := range file.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			if !gd.Lparen.IsValid() && len(gd.Specs) == 1 {
				bare = gd
			}
			break
		}
	}

	used := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})

	have := map[string]bool{}
	var unused []*ast.ImportSpec
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		have[path] = true
		if !reFiberCorePath.MatchString(path) || (spec.Name != nil && (spec.Name.Name == "_" || spec.Name.Name == ".")) {
			continue
		}
		name := "fiber"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if !used[name] {
			unused = append(unused, spec)
		}
	}

	paths := make([]string, 0, len(f.pkgNames))
	for path := range f.pkgNames {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	changed := false
	for _, path := range paths {
		name := f.pkgNames[path]
		if have[path] || !used[name] {
			continue
		}
		if name == filepath.Base(path) {
			astutil.AddImport(fset, file, path)
		} else {
			astutil.AddNamedImport(fset, file, name, path)
		}
		changed = true
	}
	for _, spec := range unused {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		alias := ""
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		astutil.DeleteNamedImport(fset, file, alias, path)
		changed = true
	}
	if !changed {
		return src
	}
	if bare != nil && len(bare.Specs) == 1 {
		if spec, ok := bare.Specs[0].(*ast.ImportSpec); ok && spec.Doc == nil && spec.Comment == nil {
			bare.Lparen, bare.Rparen = token.NoPos, token.NoPos
		}
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return src
	}
	return buf.String()
}
