package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateAddMethod(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Add\(`)
		matches := re.FindAllStringIndex(content, -1)
		if len(matches) == 0 {
			return content
		}

		var b strings.Builder
		last := 0
		for _, m := range matches {
			if m[0] < last {
				continue
			}

			startCall := m[0]
			if startCall > 0 {
				if _, err := b.WriteString(content[last:startCall]); err != nil {
					return content
				}
			}

			end, inner := extractCall(content, m[1])
			identStart := startCall - 1
			for identStart >= 0 {
				ch := content[identStart]
				if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
					break
				}
				identStart--
			}
			ident := content[identStart+1 : startCall]

			if isFiberRouter(content, ident) {
				args := splitArgs(inner)
				if len(args) >= 2 {
					first := strings.TrimSpace(args[0])
					if !strings.HasPrefix(first, "[]string{") {
						args[0] = fmt.Sprintf("[]string{%s}", args[0])
					}
				}
				if _, err := b.WriteString(".Add(" + strings.Join(args, ", ") + ")"); err != nil {
					return content
				}
			} else {
				if _, err := b.WriteString(content[startCall:end]); err != nil {
					return content
				}
			}

			last = end
		}

		if _, err := b.WriteString(content[last:]); err != nil {
			return content
		}

		return b.String()
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Add method calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating Add method calls")
	return nil
}
