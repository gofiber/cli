package v3

import (
	"fmt"
	"go/ast"
	"slices"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxMimeRenames are the JavaScript media type constants of the fiber package
// that v3 replaces with the ones for text/javascript, the type that RFC 9239
// made the standard one. MIMEApplicationJavaScriptCharsetUTF8 is gone, the
// plain constant is only deprecated in spirit.
var ctxMimeRenames = map[string]string{
	"MIMEApplicationJavaScriptCharsetUTF8": "MIMETextJavaScriptCharsetUTF8",
	"MIMEApplicationJavaScript":            "MIMETextJavaScript",
}

// ctxMimeStaticHint is added to the note of the plain constant: the static handler
// and c.Type("js") send the type with a charset, which no plain constant equals.
const ctxMimeStaticHint = `; a .js file served by the static handler is "text/javascript; charset=utf-8", ` +
	"which this constant does not equal: compare it with MIMETextJavaScriptCharsetUTF8"

// MigrateMimeConstants renames the JavaScript media type constants of the fiber
// package. Only the constants of the package are renamed, not strings, comments
// or identifiers that merely contain the name.
func MigrateMimeConstants(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxMimeConstantsContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate MIME constants: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating MIME constants")
	return nil
}

func ctxMimeConstantsContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !ctxContainsAny(content, "MIMEApplicationJavaScript") {
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
	var edits editSet
	g.walk(func(n ast.Node, _ []ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || !slices.Contains(names, id.Name) {
			return true
		}

		if renamed, ok := ctxMimeRenames[sel.Sel.Name]; ok {
			edits.replaceNode(g, sel.Sel, renamed)
			hint := ""
			if sel.Sel.Name == "MIMEApplicationJavaScript" {
				hint = ctxMimeStaticHint
			}
			noter.oncef("mime-"+sel.Sel.Name, sel.Pos(),
				"%s is now %s: the media type is text/javascript, it was application/javascript%s",
				g.text(sel), renamed, hint)
		}
		return true
	})

	return edits.apply(content)
}
