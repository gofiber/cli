package migrations

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	pkgRegex       = regexp.MustCompile(`(github\.com/gofiber/fiber/)(v\d+)( *?)(v[\w.-]+)`)
	pkgImportRegex = regexp.MustCompile(`(?m)^(\s*(?:[\w.]+\s+)?")github\.com/gofiber/fiber/v\d+("$)`)
)

func MigrateGoPkgs(cmd *cobra.Command, cwd string, _ *semver.Version, target *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		replacement := fmt.Sprintf("${1}github.com/gofiber/fiber/v%d${2}", target.Major())
		return pkgImportRegex.ReplaceAllString(content, replacement)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Go packages: %w", err)
	}

	// get go.mod file
	modFile := "go.mod"
	fileContent, err := os.ReadFile(modFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", modFile, err)
	}

	// replace old version with new version in go.mod file
	fileContentStr := pkgRegex.ReplaceAllString(
		string(fileContent),
		"${1}v"+strconv.FormatUint(target.Major(), 10)+"${3}v"+target.String(),
	)

	// update go.mod file
	if err := os.WriteFile(modFile, []byte(fileContentStr), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", modFile, err)
	}

	cmd.Println("Migrating Go packages")

	return nil
}
