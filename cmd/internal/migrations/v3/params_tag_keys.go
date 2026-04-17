package v3

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

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
		if err != nil || !strings.Contains(tagLiteral, `params:"`) {
			return true
		}

		updatedTag := strings.ReplaceAll(tagLiteral, `params:"`, `uri:"`)
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
