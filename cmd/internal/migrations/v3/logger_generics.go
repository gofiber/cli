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

		type replacement struct {
			re               *regexp.Regexp
			format           string
			expectedSubmatch int
		}

		replacements := []replacement{
			{reAllLogger, "%s.AllLogger[%s]%s", 3},
			{reConfigurableLogger, "%s.ConfigurableLogger[%s]%s", 3},
			{reDefaultLogger, "%s.DefaultLogger[%s]()", 2},
			{reSetLogger, "%s.SetLogger[%s](", 2},
			{reLoggerToWriter, "%s.LoggerToWriter[%s](", 2},
		}

		for _, r := range replacements {
			content = r.re.ReplaceAllStringFunc(content, func(m string) string {
				sub := r.re.FindStringSubmatch(m)
				if len(sub) != r.expectedSubmatch {
					return m
				}

				args := []any{sub[1], loggerType}
				if len(sub) > 2 {
					args = append(args, sub[2])
				}

				return fmt.Sprintf(r.format, args...)
			})
		}
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
