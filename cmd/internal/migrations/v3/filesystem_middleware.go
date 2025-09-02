package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateFilesystemMiddleware(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		content = strings.ReplaceAll(content,
			"github.com/gofiber/fiber/v2/middleware/filesystem",
			"github.com/gofiber/fiber/v3/middleware/static")
		content = strings.ReplaceAll(content,
			"github.com/gofiber/fiber/v3/middleware/filesystem",
			"github.com/gofiber/fiber/v3/middleware/static")

		reNew := regexp.MustCompile(`filesystem\.New\s*\(`)
		content = reNew.ReplaceAllString(content, `static.New("", `)

		content = strings.ReplaceAll(content, "filesystem.Config{", "static.Config{")

		reRootHTTP := regexp.MustCompile(`Root:\s*http.Dir\(([^)]+)\)`)
		content = reRootHTTP.ReplaceAllString(content, `FS: os.DirFS($1)`)

		reRoot := regexp.MustCompile(`Root:\s*([^,\n]+)`)
		content = reRoot.ReplaceAllString(content, `FS: os.DirFS($1)`)

		reIndex := regexp.MustCompile(`Index:\s*([^,\n]+)`)
		content = reIndex.ReplaceAllString(content, `IndexNames: []string{$1}`)

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
