package v3

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// The identifiers of the core fiber package that v3 removed or moved.
const (
	ctxIDHeaderFeaturePolicy = "HeaderFeaturePolicy"
	ctxIDOldConstraintGUID   = "ConstraintGuid"
	ctxIDIndexRune           = "IndexRune"
	ctxIDOnShutdownHandler   = "OnShutdownHandler"
	ctxIDCustomtype          = "Customtype"
	ctxIDParserType          = "ParserType"
)

// ctxBinderIdents are the identifiers that moved from the fiber package to its
// binder package, which is where v3 keeps the decoder of the parsers.
var ctxBinderIdents = []string{"SetParserDecoder", "ParserConfig", ctxIDParserType}

// MigrateCoreIdentifiers follows the identifiers of the fiber package that v3
// removed:
//
//	fiber.HeaderFeaturePolicy      "Feature-Policy"   (v3 only has Permissions-Policy, a different header)
//	fiber.ConstraintGuid           fiber.ConstraintGUID
//	fiber.IndexRune(s, r)          strings.ContainsRune(s, r)
//	fiber.OnShutdownHandler        func() error       (what the alias stood for; a note says where it was used)
//	fiber.SetParserDecoder, fiber.ParserConfig, fiber.ParserType
//	                               the same names in the binder package, with Customtype now CustomType
func MigrateCoreIdentifiers(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxCoreIdentifiersContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate core identifiers: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating removed fiber identifiers")
	return nil
}

func ctxCoreIdentifiersContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !ctxContainsAny(content, ctxIDHeaderFeaturePolicy, ctxIDOldConstraintGUID, ctxIDIndexRune, ctxIDOnShutdownHandler, "SetParserDecoder", "ParserConfig", ctxIDParserType) {
		return content
	}
	names := fiberNames(content)
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	noter := newCtxNoter(fi, g, run)
	binderName := ctxImportName(content, ctxFiberSubpath(content, "binder"), "binder")
	if binderName == "" {
		binderName = "binder"
	}
	stringsName := ctxImportName(content, "strings", "strings")
	if stringsName == "" {
		stringsName = "strings"
	}

	var edits editSet
	needBinder, needStrings := false, false
	g.walk(func(n ast.Node, _ []ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok || !slices.Contains(names, id.Name) {
				return true
			}
			switch {
			case x.Sel.Name == ctxIDHeaderFeaturePolicy:
				edits.replaceNode(g, x, `"Feature-Policy"`)
			case x.Sel.Name == ctxIDOldConstraintGUID:
				edits.replaceNode(g, x.Sel, "ConstraintGUID")
			case x.Sel.Name == ctxIDIndexRune:
				edits.replaceNode(g, x, stringsName+".ContainsRune")
				needStrings = true
			case x.Sel.Name == ctxIDOnShutdownHandler:
				edits.replaceNode(g, x, "func() error")
				noter.oncef("on-shutdown-handler", x.Pos(),
					"%s no longer exists in Fiber v3 and was replaced by func() error; Hooks().OnShutdown is OnPostShutdown(func(error) error) now, so adapt the code that registers these handlers",
					g.text(x))
			case slices.Contains(ctxBinderIdents, x.Sel.Name):
				edits.replaceNode(g, id, binderName)
				needBinder = true
			}
		case *ast.CompositeLit:
			ctxRenameCustomtypeKeys(g, &edits, x, names)
		}
		return true
	})

	out := edits.apply(content)
	if needBinder {
		out = ctxAddImport(out, ctxFiberSubpath(content, "binder"), "binder")
	}
	if needStrings {
		out = ctxAddImport(out, "strings", "strings")
	}
	return out
}

// ctxDefaultFiberPath is the import path of the fiber module that v3 migrations
// target when a file does not tell.
const ctxDefaultFiberPath = "github.com/gofiber/fiber/v3"

// ctxFiberSubpath returns the import path of a package of the fiber module, in
// the major version the file imports the module.
func ctxFiberSubpath(src, sub string) string {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil && file == nil {
		return ctxDefaultFiberPath + "/" + sub
	}

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err == nil && reFiberCorePath.MatchString(path) && (spec.Name == nil || spec.Name.Name != "_") {
			return path + "/" + sub
		}
	}
	return ctxDefaultFiberPath + "/" + sub
}

// ctxRenameCustomtypeKeys renames the Customtype key of the ParserType literals
// that lit is or holds. The literals of a []fiber.ParserType leave out the type
// of the elements, so the key is renamed through the type of the slice.
func ctxRenameCustomtypeKeys(g *goSource, edits *editSet, lit *ast.CompositeLit, names []string) {
	if lit.Type == nil {
		return
	}

	var literals []*ast.CompositeLit
	switch {
	case ctxIsFiberType(lit.Type, names, ctxIDParserType):
		literals = append(literals, lit)
	case ctxIsParserTypeSlice(lit.Type, names):
		for _, elt := range lit.Elts {
			if inner, ok := elt.(*ast.CompositeLit); ok && inner.Type == nil {
				literals = append(literals, inner)
			}
		}
	}

	for _, l := range literals {
		for _, elt := range l.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok && isIdentNamed(kv.Key, ctxIDCustomtype) {
				edits.replaceNode(g, kv.Key, "CustomType")
			}
		}
	}
}

// ctxIsParserTypeSlice reports whether t is a slice or array of fiber.ParserType.
func ctxIsParserTypeSlice(t ast.Expr, names []string) bool {
	arr, ok := t.(*ast.ArrayType)
	return ok && ctxIsFiberType(arr.Elt, names, ctxIDParserType)
}
