package v3

import (
	"fmt"
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
		return migrateListenMethodsContent(ix, content, fi)
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

func migrateListenMethodsContent(ix *appIndex, content string, fi internal.FileInfo) string {
	if !strings.Contains(content, "ListenTLS") && !strings.Contains(content, "ListenMutualTLS") {
		return content
	}
	f, ok := ix.openFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	var imps appImports
	for _, s := range f.startsIn() {
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
	return imps.apply(edits.apply(content))
}
