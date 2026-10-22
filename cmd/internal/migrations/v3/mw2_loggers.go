package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// mw2LoggerResult is the type a custom logger returns from Logger().
type mw2LoggerResult struct {
	// text is the source text of the type.
	text string
	// imports are the imports its qualifiers need.
	imports []mw2Import
}

// mw2LoggerType is a custom logger of the project: a type with a WithContext
// method that returns the log package's CommonLogger.
type mw2LoggerType struct {
	result *mw2LoggerResult
	name   string
	dir    string
	pkg    string
	// stub is set when the type has no Logger method yet, so that one is added.
	stub bool
}

// mw2LoggerModule is what is known about the loggers of one module.
type mw2LoggerModule struct {
	// types are the custom loggers.
	types []*mw2LoggerType
	// setsLogger is set when the module installs a logger with log.SetLogger,
	// which makes it the one log.DefaultLogger hands out.
	setsLogger bool
}

// mw2Loggers finds the custom loggers of the modules of a project.
type mw2Loggers struct {
	modules *mw2Modules
	scanned map[string]*mw2LoggerModule
}

func newMW2Loggers(root string) *mw2Loggers {
	return &mw2Loggers{modules: newMW2Modules(root), scanned: map[string]*mw2LoggerModule{}}
}

// module returns the loggers of the module that holds file.
func (l *mw2Loggers) module(file string) *mw2LoggerModule {
	root := l.modules.moduleOf(file)
	if m, ok := l.scanned[root]; ok {
		return m
	}

	scan := &mw2LoggerScan{withContext: map[mw2TypeKey]bool{}, results: map[mw2TypeKey]*mw2LoggerResult{}}
	m := &mw2LoggerModule{}
	l.scanned[root] = m
	if err := mw2Walk(root, func(path string) {
		if l.modules.moduleOf(path) == root {
			scan.add(path)
		}
	}); err != nil {
		return m
	}
	m.types = scan.types()
	m.setsLogger = scan.setsLogger
	return m
}

// find returns the custom logger called name of the package in the directory of file.
func (l *mw2Loggers) find(file, pkg, name string) *mw2LoggerType {
	key := mw2TypeKey{dir: filepath.Dir(filepath.Clean(file)), pkg: pkg, name: name}
	for _, t := range l.module(file).types {
		if t.key() == key {
			return t
		}
	}
	return nil
}

// mw2TypeKey identifies a type: its name within a package, which is a directory and a package clause.
type mw2TypeKey struct {
	dir, pkg, name string
}

func (t *mw2LoggerType) key() mw2TypeKey {
	return mw2TypeKey{dir: t.dir, pkg: t.pkg, name: t.name}
}

// mw2LoggerScan collects the methods that make a type a custom logger.
type mw2LoggerScan struct {
	withContext map[mw2TypeKey]bool
	results     map[mw2TypeKey]*mw2LoggerResult
	setsLogger  bool
}

// add reads the logger methods of one file.
func (s *mw2LoggerScan) add(path string) {
	src, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		return
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return
	}
	logNames := importNames(string(src), mw2Log, "log")
	dir := filepath.Dir(filepath.Clean(path))
	s.setsLogger = s.setsLogger || mw2CallsSetLogger(file, logNames)

	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		name := ""
		if ok {
			name = mw2ReceiverName(fn)
		}
		if name == "" {
			continue
		}

		key := mw2TypeKey{dir: dir, pkg: file.Name.Name, name: name}
		res := fn.Type.Results
		switch {
		case fn.Name.Name == "WithContext" && res != nil && len(res.List) == 1 && isFiberType(res.List[0].Type, logNames, "CommonLogger"):
			s.withContext[key] = true
		case fn.Name.Name == "Logger" && res != nil && len(res.List) == 1 && len(fn.Type.Params.List) == 0:
			s.results[key] = mw2ResultOf(res.List[0].Type, file, fset, string(src))
		}
	}
}

// mw2CallsSetLogger reports whether file calls SetLogger of the log package.
func mw2CallsSetLogger(file *ast.File, logNames []string) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && mw2IsSelector(call.Fun, logNames, "SetLogger") {
			found = true
		}
		return !found
	})
	return found
}

// types returns the custom loggers that were found, in a stable order.
func (s *mw2LoggerScan) types() []*mw2LoggerType {
	var types []*mw2LoggerType
	for key := range s.withContext {
		t := &mw2LoggerType{name: key.name, dir: key.dir, pkg: key.pkg, result: s.results[key]}
		if t.result == nil {
			t.result = &mw2LoggerResult{text: "any"}
			t.stub = true
		}
		types = append(types, t)
	}
	slices.SortFunc(types, func(a, b *mw2LoggerType) int {
		return strings.Compare(a.dir+"\x00"+a.pkg+"\x00"+a.name, b.dir+"\x00"+b.pkg+"\x00"+b.name)
	})
	return types
}

// mw2ResultOf records a Logger result type and the imports its qualifiers need.
func mw2ResultOf(typ ast.Expr, file *ast.File, fset *token.FileSet, src string) *mw2LoggerResult {
	out := &mw2LoggerResult{text: src[fset.Position(typ.Pos()).Offset:fset.Position(typ.End()).Offset]}
	ast.Inspect(typ, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			named := spec.Name != nil && spec.Name.Name == id.Name
			if named || (spec.Name == nil && defaultImportName(path) == id.Name) {
				imp := mw2Import{path: path}
				if spec.Name != nil {
					imp.alias = spec.Name.Name
				}
				out.imports = append(out.imports, imp)
			}
		}
		return true
	})
	return out
}
