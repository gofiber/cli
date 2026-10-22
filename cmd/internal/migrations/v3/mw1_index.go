package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// mw1Index holds what the files of a project declare, so that a migration can
// tell whether an identifier of another file is already written for v3: a
// function that takes the context, a variable that holds a slice or an
// extractors.Extractor. It is built when it is first asked something, since
// most runs never need it, and from the files as they were then.
//
// A nil index knows nothing, which leaves a migration with what its own file
// declares.
type mw1Index struct {
	// funcs are the parameter counts of every function and method by name
	funcs map[string][]int
	// fields are the types of every struct field by name
	fields map[string][]string
	// vars are the package level variables and constants by directory and name
	vars map[string]map[string]mw1VarDecl
	root string
	once sync.Once
}

// mw1VarDecl is a package level variable or constant: its declared type and
// initializer, as source text (types.ExprString, which abbreviates the
// contents of a literal).
type mw1VarDecl struct {
	typ  string
	init string
}

func newMW1Index(root string) *mw1Index {
	return &mw1Index{root: root}
}

// build reads the Go files below the root, except the vendor directories.
func (x *mw1Index) build() {
	x.funcs = map[string][]int{}
	x.fields = map[string][]string{}
	x.vars = map[string]map[string]mw1VarDecl{}

	fset := token.NewFileSet()
	_ = filepath.WalkDir(x.root, func(path string, d fs.DirEntry, err error) error { //nolint:errcheck // an unreadable file is not part of the index
		if err != nil {
			return nil //nolint:nilerr // keep walking
		}
		if d.IsDir() {
			if d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		src, readErr := os.ReadFile(path) // #nosec G304 -- a file of the project being migrated
		if readErr != nil {
			return nil //nolint:nilerr // keep walking
		}
		file, parseErr := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if file != nil && parseErr == nil {
			x.add(filepath.Dir(path), file)
		}
		return nil
	})
}

func (x *mw1Index) add(dir string, file *ast.File) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			x.funcs[d.Name.Name] = append(x.funcs[d.Name.Name], mw1ParamCount(d.Type))
		case *ast.GenDecl:
			x.addGen(dir, d)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if st, ok := n.(*ast.StructType); ok {
			for _, f := range st.Fields.List {
				for _, name := range f.Names {
					x.fields[name.Name] = append(x.fields[name.Name], types.ExprString(f.Type))
				}
			}
		}
		return true
	})
}

func (x *mw1Index) addGen(dir string, d *ast.GenDecl) {
	if d.Tok != token.VAR && d.Tok != token.CONST {
		return
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range vs.Names {
			info := mw1VarDecl{}
			if vs.Type != nil {
				info.typ = types.ExprString(vs.Type)
			}
			if len(vs.Values) == len(vs.Names) {
				info.init = types.ExprString(vs.Values[i])
			}
			if x.vars[dir] == nil {
				x.vars[dir] = map[string]mw1VarDecl{}
			}
			x.vars[dir][name.Name] = info
		}
	}
}

// funcParams returns the parameter counts of the functions and methods called
// name anywhere in the project.
func (x *mw1Index) funcParams(name string) []int {
	if x == nil {
		return nil
	}
	x.once.Do(x.build)
	return x.funcs[name]
}

// fieldTypes returns the types of the struct fields called name.
func (x *mw1Index) fieldTypes(name string) []string {
	if x == nil {
		return nil
	}
	x.once.Do(x.build)
	return x.fields[name]
}

// pkgVar returns the package level variable or constant called name in the
// package of the directory that holds path.
func (x *mw1Index) pkgVar(path, name string) (mw1VarDecl, bool) {
	if x == nil {
		return mw1VarDecl{}, false
	}
	x.once.Do(x.build)
	v, ok := x.vars[filepath.Dir(path)][name]
	return v, ok
}

// allEqual reports whether the list is not empty and holds only want.
func allEqual[T comparable](list []T, want T) bool {
	if len(list) == 0 {
		return false
	}
	for _, item := range list {
		if item != want {
			return false
		}
	}
	return true
}
