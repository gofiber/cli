package v3

import (
	"fmt"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateLoggerGenerics(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reAllLogger := regexp.MustCompile(`(\w+)\.AllLogger([^\w\[]|$)`)
	reConfigurableLogger := regexp.MustCompile(`(\w+)\.ConfigurableLogger([^\w\[]|$)`)
	reDefaultLogger := regexp.MustCompile(`(\w+)\.DefaultLogger\(\)`)
	reSetLogger := regexp.MustCompile(`(\w+)\.SetLogger\(`)
	reLoggerToWriter := regexp.MustCompile(`(\w+)\.LoggerToWriter\(`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		content = reAllLogger.ReplaceAllString(content, `$1.AllLogger[any]$2`)
		content = reConfigurableLogger.ReplaceAllString(content, `$1.ConfigurableLogger[any]$2`)
		content = reDefaultLogger.ReplaceAllString(content, `$1.DefaultLogger[any]()`)
		content = reSetLogger.ReplaceAllString(content, `$1.SetLogger[any](`)
		content = reLoggerToWriter.ReplaceAllString(content, `$1.LoggerToWriter[any](`)
		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate logger generics: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating logger generics")
	return nil
}
