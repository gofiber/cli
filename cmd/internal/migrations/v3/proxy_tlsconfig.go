package v3

import (
	"fmt"
	"go/ast"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var proxyFasthttpPath = regexp.MustCompile(`^github\.com/valyala/fasthttp$`)

// MigrateProxyTLSConfig turns proxy.WithTlsConfig(cfg), which set the TLS config of the
// client the middleware uses, into the client of v3 with that config:
// proxy.WithClient(&fasthttp.Client{TLSConfig: cfg}). The argument is whatever expression
// the call had, and the import of fasthttp is added when the file lacks it.
func MigrateProxyTLSConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, migrateProxyTLSConfigContent)
	if err != nil {
		return fmt.Errorf("failed to migrate proxy TLS config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating proxy TLS config")
	return nil
}

func migrateProxyTLSConfigContent(content string) string {
	if !strings.Contains(content, "WithTlsConfig") {
		return content
	}
	src, ok := parseForEdits(content)
	if !ok {
		return content
	}
	proxy := src.importName(renameProxyPath)
	if proxy == "" {
		return content
	}

	fasthttp := src.importName(proxyFasthttpPath)
	needsImport := false
	ast.Inspect(src.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "WithTlsConfig" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != proxy {
			return true
		}

		if fasthttp == "" {
			fasthttp = "fasthttp"
			needsImport = true
		}
		src.replace(call, proxy+".WithClient(&"+fasthttp+".Client{TLSConfig: "+src.text(call.Args[0])+"})")
		return true
	})

	out := src.apply()
	if needsImport && out != content {
		out = addImport(out, "github.com/valyala/fasthttp")
	}
	return out
}
