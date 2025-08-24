package v3

import (
	"fmt"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateLoggerGenerics(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	fiberImport := regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/log$`)
	reLoggerType := regexp.MustCompile(`(?m)^func\s+(?:\(\w+\s+\*?\w+\)\s+)?Logger\(\)\s*(\*?\w+(?:\.\w+)*)\s*{`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "", content, parser.ImportsOnly)
		if err != nil {
			return content
		}

		aliases := make([]string, 0, len(file.Imports))
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if !fiberImport.MatchString(path) {
				continue
			}
			alias := "log"
			if imp.Name != nil {
				alias = imp.Name.Name
			} else if idx := strings.LastIndex(path, "/"); idx != -1 {
				alias = path[idx+1:]
			}
			aliases = append(aliases, regexp.QuoteMeta(alias))
		}

		if len(aliases) == 0 {
			return content
		}

		aliasPattern := strings.Join(aliases, "|")
		reAllLogger := regexp.MustCompile(fmt.Sprintf("(%s)\\.AllLogger([^\\w\\[]|$)", aliasPattern))
		reConfigurableLogger := regexp.MustCompile(fmt.Sprintf("(%s)\\.ConfigurableLogger([^\\w\\[]|$)", aliasPattern))
		reDefaultLogger := regexp.MustCompile(fmt.Sprintf("(%s)\\.DefaultLogger\\(\\)", aliasPattern))
		reSetLogger := regexp.MustCompile(fmt.Sprintf("(%s)\\.SetLogger\\(", aliasPattern))
		reLoggerToWriter := regexp.MustCompile(fmt.Sprintf("(%s)\\.LoggerToWriter\\(", aliasPattern))

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
