package migrations

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var pkgRegex = regexp.MustCompile(`(github\.com\/gofiber\/fiber\/)(v\d+)( *?)(v[\w.-]+)`)

func MigrateGoPkgs(cmd *cobra.Command, cwd string, curr *semver.Version, target *semver.Version) error {
	pkgReplacer := strings.NewReplacer(
		"github.com/gofiber/fiber/v"+strconv.FormatUint(curr.Major(), 10),
		"github.com/gofiber/fiber/v"+strconv.FormatUint(target.Major(), 10),
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return pkgReplacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Go packages: %v", err)
	}

	// get go.mod file
	modFile := "go.mod"
	fileContent, err := os.ReadFile(modFile)
	if err != nil {
		return err
	}

	// replace old version with new version in go.mod file
	fileContentStr := pkgRegex.ReplaceAllString(
		string(fileContent),
		"${1}v"+strconv.FormatUint(target.Major(), 10)+"${3}v"+target.String(),
	)

	// update go.mod file
	if err := os.WriteFile(modFile, []byte(fileContentStr), 0o644); err != nil {
		return err
	}

	cmd.Println("Migrating Go packages")

	return nil
}
