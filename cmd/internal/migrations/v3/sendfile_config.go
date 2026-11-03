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

// MigrateSendFileConfig moves the calls of Ctx.SendFile to the config of v3.
//
// v2 took the compress flag, c.SendFile(path, true), and always served the byte ranges
// of the file. v3 takes a fiber.SendFile and serves ranges only when ByteRange is set, so
// the calls get ByteRange: true to keep the Range requests working, and the call without
// the flag gets the config for that.
//
// Only calls on a fiber.Ctx are changed: the v2 client had an Agent.SendFile(path,
// field) of its own.
func MigrateSendFileConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, migrateSendFileContent)
	if err != nil {
		return fmt.Errorf("failed to migrate SendFile calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating SendFile calls")
	return nil
}

func migrateSendFileContent(content string) string {
	if !strings.Contains(content, ".SendFile(") {
		return content
	}
	src, ok := parseForEdits(content)
	if !ok {
		return content
	}

	ast.Inspect(src.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Ellipsis.IsValid() {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != methodSendFile {
			return true
		}
		if ident := GetBaseIdent(sel.X); ident == nil || !isFiberCtx(content, ident.Name) {
			return true
		}

		switch len(call.Args) {
		case 1:
			src.insertAfter(call.Args[0], ", fiber.SendFile{ByteRange: true}")
		case 2:
			if !isSendFileConfig(call.Args[1]) {
				src.replace(call.Args[1], "fiber.SendFile{Compress: "+src.text(call.Args[1])+", ByteRange: true}")
			}
		}
		return true
	})

	return src.apply()
}

// isSendFileConfig reports whether e is a config of v3 already: a fiber.SendFile{...}
// literal, or a pointer to one.
func isSendFileConfig(e ast.Expr) bool {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	switch t := lit.Type.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == methodSendFile
	case *ast.Ident:
		return t.Name == methodSendFile
	default:
		return false
	}
}
