package internal

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/imports"
)

// importsOptions formats a rewritten file and keeps its imports in order, but
// does not let goimports add or remove any. Which imports a rewrite needs is
// the rewrite's business: goimports finds them in the module cache of the
// machine it runs on, and an unaliased import whose package name differs from
// the last element of its path (contrib/jwt is jwtware) looks unused to it
// whenever it cannot load the package, as in a fresh clone.
var importsOptions = &imports.Options{Comments: true, TabIndent: true, TabWidth: 8, FormatOnly: true}

// stdlibRoots are the first path elements of the standard library.
var stdlibRoots = []string{
	"archive", "bufio", "bytes", "cmp", "compress", "container", "context", "crypto", "database", "debug", "embed",
	"encoding", "errors", "expvar", "flag", "fmt", "go", "hash", "html", "image", "index", "io", "iter", "log", "maps",
	"math", "mime", "net", "os", "path", "plugin", "reflect", "regexp", "runtime", "slices", "sort", "strconv",
	"strings", "structs", "sync", "syscall", "testing", "text", "time", "unicode", "unique", "unsafe", "weak",
}

// reKnownModule matches the packages whose name is the last element of their
// import path: the packages of the fiber module and of gofiber/utils.
var reKnownModule = regexp.MustCompile(`^github\.com/gofiber/(fiber/v\d+|utils/v\d+)(/|$)`)

// reMajorVersion matches a major version path element such as v2.
var reMajorVersion = regexp.MustCompile(`^v\d+$`)

// tidyImports drops the imports that a rewrite left unused and formats src.
// An import whose package name is certain (one with an alias, the standard
// library, the packages of the fiber module, whose name is the last element of
// their path) is dropped when nothing refers to it. Any other import is only
// dropped when every package qualifier of the file is explained by an import
// whose name is certain: its own name cannot be read from the path
// (contrib/jwt declares jwtware), so while the file qualifies something that no
// import explains, it may be that import, and an import that is used must never
// be dropped. Adding an import that is missing is left to the rewrite that
// needs it.
func tidyImports(filename string, src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller names the file
	}

	// A package is used by a selector whose base the parser could not resolve to
	// a declaration of the file: a local variable that has the name of a package
	// does not use it. The parser's scope resolution is all that is needed, and
	// unlike a type check it works on code that refers to packages that are not
	// loaded.
	used := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
				used[id.Name] = true
			}
		}
		return true
	})

	type candidate struct{ alias, path string }
	var drop, unknown []candidate
	certain := map[string]bool{}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		alias := ""
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		switch name := importName(alias, importPath); {
		case alias == "_" || alias == "." || importPath == "C":
		case name == "":
			unknown = append(unknown, candidate{alias, importPath})
		case !used[name]:
			certain[name] = true
			drop = append(drop, candidate{alias, importPath})
		default:
			certain[name] = true
		}
	}

	// Every qualifier is explained by an import of a certain name, so the other
	// imports cannot be what the file refers to.
	if !slices.ContainsFunc(slices.Collect(maps.Keys(used)), func(name string) bool { return !certain[name] }) {
		drop = append(drop, unknown...)
	}

	changed := false
	for _, imp := range drop {
		changed = astutil.DeleteNamedImport(fset, file, imp.alias, imp.path) || changed
	}

	if changed {
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, file); err != nil {
			return nil, err //nolint:wrapcheck // the caller names the file
		}
		src = buf.Bytes()
	}

	return imports.Process(filename, src, importsOptions) //nolint:wrapcheck // the caller names the file
}

// formatOnly formats src like tidyImports does and keeps every import.
func formatOnly(filename string, src []byte) ([]byte, error) {
	return imports.Process(filename, src, importsOptions) //nolint:wrapcheck // the caller names the file
}

// importName returns the identifier the file refers to the import by, or "" when
// it cannot be known from the path or the import is not subject to tidying (a
// blank, dot or cgo import).
func importName(alias, importPath string) string {
	switch alias {
	case "_", ".":
		return ""
	case "":
	default:
		return alias
	}

	if importPath == "C" {
		return ""
	}

	elems := strings.Split(importPath, "/")
	if !slices.Contains(stdlibRoots, elems[0]) && !reKnownModule.MatchString(importPath) {
		return ""
	}

	name := path.Base(importPath)
	if reMajorVersion.MatchString(name) && len(elems) > 1 {
		name = elems[len(elems)-2]
	}
	return name
}
