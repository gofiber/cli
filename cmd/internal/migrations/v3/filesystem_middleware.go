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
	reFilesystemRootHTTPDir  = regexp.MustCompile(`Root:\s*http\.Dir\(([^)]+)\)`)
	reFilesystemRootHTTPFS   = regexp.MustCompile(`Root:\s*http\.FS\(([^)]+)\)`)
	reFilesystemRoot         = regexp.MustCompile(`Root:\s*([^,\n]+)`)
	reFilesystemIndex        = regexp.MustCompile(`Index:\s*([^,\n]+)`)
	reFilesystemNotFoundFile = regexp.MustCompile(`(?m)^(\s*)(NotFoundFile:\s*[^,\n]+)(,?)`)
	// Matches imports that end with /static, capturing alias (group 1) and path (group 2)
	// Also handles dot imports (import . "path/to/static")
	reStaticImport = regexp.MustCompile(`(?m)^\s*(?:(\w+|\.)\s+)?"([^"]+/static)"`)
)

// staticMiddlewarePkg returns the package name to use for the static middleware.
// If there's already an import with package name "static", returns "staticmw".
// Excludes Fiber's own middleware paths from conflict detection.
func staticMiddlewarePkg(content string) string {
	// Check if there's already an import that would conflict with "static"
	matches := reStaticImport.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		// m[1] is the alias if present
		// m[2] is the import path
		alias := m[1]
		importPath := m[2]

		// Skip Fiber's own middleware - not a conflict
		if strings.Contains(importPath, "github.com/gofiber/fiber") {
			continue
		}

		// If no alias or alias is "static" or ".", there's a conflict
		if alias == "" || alias == "static" || alias == "." {
			return "staticmw"
		}
		// If user has their own alias (e.g., mystatic "pkg/static"), no conflict
	}
	return "static"
}

// MigrateFilesystemMiddleware migrates the filesystem middleware to the static middleware.
// It handles import path changes, http.Dir/http.FS conversions, config field renames,
// and import naming conflicts when users have their own package named "static".
func MigrateFilesystemMiddleware(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		// Determine the package name to use for the static middleware
		staticPkg := staticMiddlewarePkg(content)

		// Handle import replacement based on whether we need an alias
		if staticPkg == "staticmw" {
			content = strings.ReplaceAll(content,
				`"github.com/gofiber/fiber/v2/middleware/filesystem"`,
				`staticmw "github.com/gofiber/fiber/v3/middleware/static"`)
			content = strings.ReplaceAll(content,
				`"github.com/gofiber/fiber/v3/middleware/filesystem"`,
				`staticmw "github.com/gofiber/fiber/v3/middleware/static"`)
		} else {
			content = strings.ReplaceAll(content,
				`"github.com/gofiber/fiber/v2/middleware/filesystem"`,
				`"github.com/gofiber/fiber/v3/middleware/static"`)
			content = strings.ReplaceAll(content,
				`"github.com/gofiber/fiber/v3/middleware/filesystem"`,
				`"github.com/gofiber/fiber/v3/middleware/static"`)
		}

		content = reFilesystemNew.ReplaceAllString(content, staticPkg+`.New("", `)

		content = strings.ReplaceAll(content, "filesystem.Config{", staticPkg+".Config{")

		// Handle http.Dir() - convert to os.DirFS()
		content = reFilesystemRootHTTPDir.ReplaceAllString(content, `FS: os.DirFS($1)`)

		// Handle http.FS() - extract inner value directly (embed.FS implements fs.FS)
		content = reFilesystemRootHTTPFS.ReplaceAllString(content, `FS: $1`)

		// Handle remaining Root: patterns (fallback to os.DirFS wrapping)
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
