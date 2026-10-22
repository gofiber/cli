package v3

import (
	"fmt"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateReqHeaderParser turns c.ReqHeaderParser(&v) into c.Bind().Header(&v).
// Only methods of a fiber context are rewritten. The reqHeader struct tag it
// reads is renamed by MigrateParamsTagKeys.
func MigrateReqHeaderParser(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxMigrateParserCalls(content, fi, run, []string{nameReqHeaderParser})
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate ReqHeaderParser: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating request header parser helper")
	return nil
}
