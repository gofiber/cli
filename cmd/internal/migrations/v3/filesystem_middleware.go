package v3

import (
	"fmt"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateFilesystemMiddleware migrates the filesystem middleware to the static
// middleware, in the files that import it, whatever name they give it:
//
//	filesystem.New(filesystem.Config{Root: http.Dir("./public")})
//	static.New("", static.Config{FS: os.DirFS("./public")})
//
// Root becomes FS (http.Dir to os.DirFS, http.FS to the file system itself),
// Index becomes IndexNames, PathPrefix the first argument of New, and
// NotFoundFile a NotFoundHandler that sends the file with a 200, as v2 did.
// filesystem.SendFile(c, fsys, path) becomes c.SendFile(path, fiber.SendFile{FS: fsys}).
//
// Configs used as a type or built from ConfigDefault, and assignments to their
// fields, are migrated as well. What static cannot do is left as it was and
// reported. The new import takes the name static, or another when the file has
// a name of that kind already.
func MigrateFilesystemMiddleware(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs := newMw3Pkgs()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return mw3FilesystemContent(content, fi, pkgs)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate filesystem middleware: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating filesystem middleware")
	return nil
}
