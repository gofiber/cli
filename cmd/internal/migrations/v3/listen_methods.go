package v3

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateListenMethods turns the TLS variants of Listen, which v3 removed, into
// Listen with a fiber.ListenConfig:
//
//	app.ListenTLS(addr, cert, key)                       -> app.Listen(addr, fiber.ListenConfig{CertFile: cert, CertKeyFile: key})
//	app.ListenMutualTLS(addr, cert, key, ca)             -> ... CertClientFile: ca
//	app.ListenTLSWithCertificate(addr, c)                -> ... TLSConfig: &tls.Config{Certificates: []tls.Certificate{c}}
//	app.ListenMutualTLSWithCertificate(addr, c, pool)    -> ... ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool
//
// Only calls on a fiber app are touched.
func MigrateListenMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	ix := newAppIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return appMigrateListenMethodsContent(ix, content, fi)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listen methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating listen methods")
	return nil
}

func appMigrateListenMethodsContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "ListenTLS") && !strings.Contains(content, "ListenMutualTLS") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	var imps appImports
	known := map[*ast.CallExpr]bool{}
	for _, s := range f.startsIn() {
		known[s.call] = true
		if s.method == appNameListen || s.method == appNameListener {
			continue
		}
		rw := s.rewrite(nil, appImports{reqs: slices.Clone(imps.reqs)})
		line := f.g.line(s.call.Pos())
		if rw.problem != "" {
			internal.Notef(fi, line,
				"%s was removed in v3 and this call could not be converted to Listen with a fiber.ListenConfig (%s)", s.method, rw.problem)
			continue
		}
		imps = rw.imports
		edits.edits = append(edits.edits, rw.edits...)
		for _, note := range rw.notes {
			internal.Notef(fi, line, "%s", note)
		}
	}
	appNoteUnknownReceivers(f, known)
	return imps.apply(edits.apply(content))
}

// appNoteUnknownReceivers points at the calls of the removed methods whose
// receiver has no type that can be told, in a file that imports fiber: only
// fiber has these methods, so the call will not compile.
func appNoteUnknownReceivers(f *appFile, known map[*ast.CallExpr]bool) {
	if len(f.names) == 0 {
		return
	}
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || known[call] {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || !slices.Contains(appTLSMethods, sel.Sel.Name) {
			return true
		}
		if id, isIdent := sel.X.(*ast.Ident); isIdent && f.importPathOf(id.Name) != "" {
			return true // a function of some other package
		}
		if t, _ := f.valueType(sel.X, 0); t != nil {
			return true // a value of a type that is known, and is not an app
		}
		internal.Notef(f.fi, f.g.line(call.Pos()),
			"%s was removed in v3 and the type of its receiver could not be determined: call Listen with a fiber.ListenConfig (CertFile, CertKeyFile, CertClientFile, TLSConfig)",
			sel.Sel.Name)
		return true
	})
}
