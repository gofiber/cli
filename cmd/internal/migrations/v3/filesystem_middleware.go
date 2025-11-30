package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	reFilesystemNew          = regexp.MustCompile(`filesystem\.New\s*\(`)
	reFilesystemRootHTTP     = regexp.MustCompile(`Root:\s*http.Dir\(([^)]+)\)`)
	reFilesystemRoot         = regexp.MustCompile(`Root:\s*([^,\n]+)`)
	reFilesystemIndex        = regexp.MustCompile(`Index:\s*([^,\n]+)`)
	reFilesystemNotFoundFile = regexp.MustCompile(`(?m)^(\s*)(NotFoundFile:\s*[^,\n]+)(,?)`)
)

func MigrateFilesystemMiddleware(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		content = strings.ReplaceAll(content,
			"github.com/gofiber/fiber/v2/middleware/filesystem",
			"github.com/gofiber/fiber/v3/middleware/static")
		content = strings.ReplaceAll(content,
			"github.com/gofiber/fiber/v3/middleware/filesystem",
			"github.com/gofiber/fiber/v3/middleware/static")

		content = reFilesystemNew.ReplaceAllString(content, `static.New("", `)

		content = strings.ReplaceAll(content, "filesystem.Config{", "static.Config{")

		content = reFilesystemRootHTTP.ReplaceAllString(content, `FS: os.DirFS($1)`)

		content = reFilesystemRoot.ReplaceAllString(content, `FS: os.DirFS($1)`)

		content = reFilesystemIndex.ReplaceAllString(content, `IndexNames: []string{$1}`)

		// Handle NotFoundFile migration - comment it out and add TODO for NotFoundHandler
		// Only migrate if not already migrated (check for TODO comment)
		if !strings.Contains(content, "TODO: Migrate to NotFoundHandler") {
			content = reFilesystemNotFoundFile.ReplaceAllString(content,
				`$1// TODO: Migrate to NotFoundHandler (fiber.Handler) - NotFoundFile is deprecated
$1// $2$3`)
		}

		return content
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
