package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// The logger tag v2 called header and v3 calls reqHeader.
const (
	mw2LoggerHeaderTag    = "${header:"
	mw2LoggerReqHeaderTag = "${reqHeader:"
)

// MigrateLoggerTags renames what v3 renamed in the logger's template language: the
// TagHeader constant is TagReqHeader, and the ${header:X} tag of a format string
// is ${reqHeader:X}. v3 does not know the old tag and logger.New panics at
// startup on it ("unknown template tag").
//
// The constant is found through the logger's import name. Format strings are
// rewritten in the files of every module that imports the logger, because a
// format kept in a constant often lives in a file of its own.
func MigrateLoggerTags(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	modules := newMW2Modules(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateLoggerTagsContent(content, fi, modules)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate logger tags: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating logger tag constants")
	return nil
}

func migrateLoggerTagsContent(content string, fi internal.FileInfo, modules *mw2Modules) string {
	hasConstant := strings.Contains(content, "TagHeader")
	hasFormat := strings.Contains(content, mw2LoggerHeaderTag)
	if !hasConstant && !hasFormat {
		return content
	}

	names := importNames(content, mw2Logger, "logger")
	if len(names) == 0 && (!hasFormat || !modules.imports(fi.Path, mw2Logger)) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	g.walk(func(n ast.Node, _ []ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if mw2IsSelector(x, names, "TagHeader") {
				edits.replaceNode(g, x.Sel, "TagReqHeader")
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				renameHeaderTag(g, &edits, x)
			}
		}
		return true
	})
	return edits.apply(content)
}

// renameHeaderTag rewrites the ${header:X} tags of a string literal.
func renameHeaderTag(g *goSource, edits *editSet, lit *ast.BasicLit) {
	start := g.offset(lit.Pos())
	for i := 0; ; {
		idx := strings.Index(lit.Value[i:], mw2LoggerHeaderTag)
		if idx < 0 {
			return
		}
		at := start + i + idx
		edits.replace(at, at+len(mw2LoggerHeaderTag), mw2LoggerReqHeaderTag)
		i += idx + len(mw2LoggerHeaderTag)
	}
}
