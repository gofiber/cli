package v3

import (
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The conditions of the behavior rules that a regular expression over the text
// of a file cannot state: they look at the syntax tree, so a word in a comment, a
// string or an unrelated type does not switch a rule on.

// literalHasKey reports whether the file has a composite literal of the type
// pkg.typeName, pkg being a name the file binds to the package importPath, that
// sets one of the keys.
func (rf *reviewFile) literalHasKey(importPath, typeName string, keys ...string) bool {
	found := false
	rf.g.walk(func(n ast.Node, _ []ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || found {
			return !found
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != typeName {
			return true
		}
		if _, path, bound := rf.packageOf(sel.X); !bound || path != importPath {
			return true
		}
		for _, elt := range lit.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			if !isKV {
				continue
			}
			if key, isIdent := kv.Key.(*ast.Ident); isIdent && slices.Contains(keys, key.Name) {
				found = true
			}
		}
		return !found
	})
	return found
}

// callsCtxMethod reports whether the file calls one of the methods on a fiber
// context, not on any other value that has a method of that name.
func (rf *reviewFile) callsCtxMethod(names ...string) bool {
	found := false
	rf.g.walk(func(n ast.Node, stack []ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && slices.Contains(names, sel.Sel.Name) && rf.isCtxMethod(sel, stack) {
			found = true
		}
		return !found
	})
	return found
}

// reviewMaxTestFiles is how many test files a note names.
const reviewMaxTestFiles = 2

// httptestNote names the test files next to the scanned file that use
// net/http/httptest, for a rule whose failure shows in the tests of a project
// before it shows anywhere else: a test starts its upstream with httptest, and
// the servers of that package listen on loopback. It is empty for a test file,
// which is where the note stands already, and when no test file next to it
// uses httptest.
func (rf *reviewFile) httptestNote() string {
	if strings.HasSuffix(rf.fi.Rel, "_test.go") {
		return ""
	}
	dir := filepath.Dir(rf.fi.Path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, entry.Name())) // #nosec G304 -- a file of the project that is reviewed
		if err != nil || !strings.Contains(string(src), `"net/http/httptest"`) {
			continue
		}
		rel, err := filepath.Rel(rf.fi.Root, filepath.Join(dir, entry.Name()))
		if err != nil {
			rel = entry.Name()
		}
		names = append(names, filepath.ToSlash(rel))
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) > reviewMaxTestFiles {
		names = append(names[:reviewMaxTestFiles], "...")
	}
	return " Tests that proxy to a net/http/httptest server hit this as well, since such servers listen on 127.0.0.1 (" + strings.Join(names, ", ") + ")."
}
