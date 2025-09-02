package v3

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateStaticRoutes(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Static\(\s*("[^"]*")\s*,\s*("[^"]*")(?:,\s*([^)]*))?\)`)
		content = re.ReplaceAllStringFunc(content, func(m string) string {
			sub := re.FindStringSubmatch(m)
			pathLit := sub[1]
			root := sub[2]
			cfg := sub[3]

			path, err := strconv.Unquote(pathLit)
			if err != nil {
				path = strings.Trim(pathLit, "\"")
			}

			switch path {
			case "/":
				path = "/*"
			case "*":
				// keep as is
			default:
				path += "*"
			}

			quoted := strconv.Quote(path)

			if cfg != "" {
				cfg = strings.TrimSpace(cfg)
				cfg = strings.Replace(cfg, "Static{", "static.Config{", 1)
				reIndex := regexp.MustCompile(`Index:\s*([^,}\n]+)`)
				cfg = reIndex.ReplaceAllString(cfg, "IndexNames: []string{$1}")
				return fmt.Sprintf(".Get(%s, static.New(%s, %s))", quoted, root, cfg)
			}

			return fmt.Sprintf(".Get(%s, static.New(%s))", quoted, root)
		})

		if strings.Contains(content, "static.New(") {
			content = addImport(content, "github.com/gofiber/fiber/v3/middleware/static")
		}

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate static usages: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating app.Static usage")
	return nil
}
