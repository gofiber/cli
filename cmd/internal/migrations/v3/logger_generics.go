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
	reLoggerType := regexp.MustCompile(`(?m)^func\s+(?:\(\w+\s+\*?\w+\)\s+)?Logger\(\)\s*(\*?\w+(?:\.\w+)*)\s*{`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		loggerType := "any"
		if m := reLoggerType.FindStringSubmatch(content); len(m) == 2 {
			loggerType = m[1]
		}

		content = reAllLogger.ReplaceAllStringFunc(content, func(m string) string {
			sub := reAllLogger.FindStringSubmatch(m)
			if len(sub) != 3 {
				return m
			}
			return fmt.Sprintf("%s.AllLogger[%s]%s", sub[1], loggerType, sub[2])
		})
		content = reConfigurableLogger.ReplaceAllStringFunc(content, func(m string) string {
			sub := reConfigurableLogger.FindStringSubmatch(m)
			if len(sub) != 3 {
				return m
			}
			return fmt.Sprintf("%s.ConfigurableLogger[%s]%s", sub[1], loggerType, sub[2])
		})
		content = reDefaultLogger.ReplaceAllStringFunc(content, func(m string) string {
			sub := reDefaultLogger.FindStringSubmatch(m)
			if len(sub) != 2 {
				return m
			}
			return fmt.Sprintf("%s.DefaultLogger[%s]()", sub[1], loggerType)
		})
		content = reSetLogger.ReplaceAllStringFunc(content, func(m string) string {
			sub := reSetLogger.FindStringSubmatch(m)
			if len(sub) != 2 {
				return m
			}
			return fmt.Sprintf("%s.SetLogger[%s](", sub[1], loggerType)
		})
		content = reLoggerToWriter.ReplaceAllStringFunc(content, func(m string) string {
			sub := reLoggerToWriter.FindStringSubmatch(m)
			if len(sub) != 2 {
				return m
			}
			return fmt.Sprintf("%s.LoggerToWriter[%s](", sub[1], loggerType)
		})
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
