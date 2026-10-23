package v3

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// tagKeyRenames are the keys of struct tags that a parser of v2 read and the binder of v3
// reads under another name: params:"id" is uri:"id" for ParamsParser, and reqHeader:"X-Token"
// is header:"X-Token" for ReqHeaderParser, whose values stayed empty when the tag kept the
// old key. A key is a whole word of the tag, so mine:"..." is not changed.
var tagKeyRenames = []struct {
	re   *regexp.Regexp
	to   string
	skip *regexp.Regexp
}{
	{re: regexp.MustCompile(`(^|\s)params:"`), to: `${1}uri:"`},
	// a field that has a header key already keeps it
	{re: regexp.MustCompile(`(^|\s)reqHeader:"`), to: `${1}header:"`, skip: regexp.MustCompile(`(^|\s)header:"`)},
}

// MigrateParamsTagKeys renames the struct tag keys of the parsers that v3 binds under other
// names (see tagKeyRenames).
func MigrateParamsTagKeys(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, rewriteParamsTagKeys)
	if err != nil {
		return fmt.Errorf("failed to migrate params tag keys: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating params tag keys")
	return nil
}

func rewriteParamsTagKeys(content string) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil {
		return content
	}

	modified := false
	ast.Inspect(file, func(n ast.Node) bool {
		field, ok := n.(*ast.Field)
		if !ok || field.Tag == nil {
			return true
		}

		tagLiteral, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			return true
		}

		updatedTag := tagLiteral
		for _, rename := range tagKeyRenames {
			if rename.skip == nil || !rename.skip.MatchString(updatedTag) {
				updatedTag = rename.re.ReplaceAllString(updatedTag, rename.to)
			}
		}
		if updatedTag == tagLiteral {
			return true
		}

		field.Tag.Value = "`" + updatedTag + "`"
		modified = true
		return true
	})

	if !modified {
		return content
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return content
	}

	return buf.String()
}
