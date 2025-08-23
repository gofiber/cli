package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	pkgRegex         = regexp.MustCompile(`(github\.com/gofiber/fiber/)(v\d+)(\s+)(v[\w.-]+)`)
	fiberImportRegex = regexp.MustCompile(`(^|")github\.com/gofiber/fiber/v\d+`)
)

func MigrateGoPkgs(cmd *cobra.Command, cwd string, _, target *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		replacement := fmt.Sprintf("${1}github.com/gofiber/fiber/v%d", target.Major())
		return fiberImportRegex.ReplaceAllString(content, replacement)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Go packages: %w", err)
	}

	// get go.mod file
	modFile := filepath.Join(cwd, "go.mod")
	fileContent, err := os.ReadFile(modFile) // #nosec G304 -- reading module file
	if err != nil {
		return fmt.Errorf("read %s: %w", modFile, err)
	}

	// replace old version with new version in go.mod file
	fileContentStr := pkgRegex.ReplaceAllString(
		string(fileContent),
		"${1}v"+strconv.FormatUint(target.Major(), 10)+"${3}v"+target.String(),
	)

	if fileContentStr != string(fileContent) {
		// update go.mod file
		if err := os.WriteFile(modFile, []byte(fileContentStr), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", modFile, err)
		}
		changed = true
	}

	if !changed {
		return nil
	}

	cmd.Println("Migrating Go packages")

	return nil
}
