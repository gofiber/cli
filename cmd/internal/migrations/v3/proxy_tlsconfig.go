package v3

import (
	"fmt"
	"go/ast"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateProxyTLSConfig moves the proxy middleware to its v3 API:
//   - Config.TlsConfig is called TLSConfig,
//   - proxy.New(Config{...}), which v3 removed, is proxy.Balancer(Config{...}),
//   - proxy.WithTlsConfig(c) is proxy.WithClient with a client that has the TLS
//     config. v2 set it on its default client, which does not send a default
//     User-Agent and does not normalize paths, so the new client says the same.
//
// The package is found through its import path, whatever name the file gives it.
func MigrateProxyTLSConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateProxyContent)
	if err != nil {
		return fmt.Errorf("failed to migrate proxy TLS config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating proxy TLS config")
	return nil
}

func migrateProxyContent(content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "TlsConfig") && !strings.Contains(content, ".New(") {
		return content
	}
	names := importNames(content, mw2Proxy, "proxy")
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	fasthttp := mw2FirstName(content, mw2Fasthttp, "fasthttp")
	needsImport := fasthttp == ""
	if needsImport {
		fasthttp = "fasthttp"
	}
	usedFasthttp := false

	var edits editSet
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if lit, ok := mw2ConfigLit(x, names); ok && mw2IsKeyed(lit) {
				if _, kv := mw2Field(lit, "TlsConfig"); kv != nil {
					edits.replaceNode(g, kv.Key, "TLSConfig")
				}
			}
		case *ast.SelectorExpr:
			if x.Sel.Name == "TlsConfig" && mw2IsTyped(x.X, names, mw2ConfigType, stack, 0) {
				edits.replaceNode(g, x.Sel, "TLSConfig")
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok || !mw2IsSelector(sel, names, sel.Sel.Name) {
				return true
			}
			switch sel.Sel.Name {
			case mw2New:
				edits.replaceNode(g, sel.Sel, "Balancer")
			case "WithTlsConfig":
				if len(x.Args) == 1 && !x.Ellipsis.IsValid() {
					usedFasthttp = true
					edits.replaceNode(g, sel.Sel, "WithClient")
					edits.replaceNode(g, x.Args[0], "&"+fasthttp+".Client{NoDefaultUserAgentHeader: true, DisablePathNormalizing: true, TLSConfig: "+g.text(x.Args[0])+"}")
				}
			}
		}
		return true
	})

	out := edits.apply(content)
	if usedFasthttp && needsImport {
		out = mw2AddImports(out, mw2Import{path: "github.com/valyala/fasthttp"})
	}
	return out
}
