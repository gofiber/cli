package v3

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
)

// parseGoFile parses Go source content into an AST. It returns the parsed file
// and token.FileSet or an error if the content cannot be parsed.
func parseGoFile(content string) (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse Go file: %w", err)
	}
	return file, fset, nil
}

// collectImportAliases finds all import aliases for the given import path within
// the provided file. The default alias derived from the path basename is also
// included when the import does not specify an explicit name.
func collectImportAliases(file *ast.File, importPath string) map[string]struct{} {
	aliases := make(map[string]struct{})

	for _, imp := range file.Imports {
		if imp.Path == nil || imp.Path.Value == "" {
			continue
		}

		if imp.Path.Value != "\""+importPath+"\"" {
			continue
		}

		if imp.Name != nil {
			aliases[imp.Name.Name] = struct{}{}
			continue
		}

		aliases[path.Base(importPath)] = struct{}{}
	}

	return aliases
}

// collectAssignedCallIdents walks assignment statements and collects identifier
// names that are assigned the result of a call expression matching the provided
// predicate.
func collectAssignedCallIdents(file *ast.File, predicate func(*ast.CallExpr) bool) map[string]struct{} {
	matches := make(map[string]struct{})

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}

		for idx, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok || !predicate(call) {
				continue
			}

			ident, ok := assign.Lhs[idx].(*ast.Ident)
			if !ok {
				continue
			}

			matches[ident.Name] = struct{}{}
		}

		return true
	})

	return matches
}
