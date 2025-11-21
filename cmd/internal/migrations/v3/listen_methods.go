package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateListenMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		orig := content
		re := regexp.MustCompile(`\.(ListenTLSWithCertificate|ListenTLS|ListenMutualTLSWithCertificate|ListenMutualTLS)\(`)
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
				if !isIdentifierChar(ch) {
					break
				}
				identStart--
			}
			ident := content[identStart+1 : startCall]

			if isFiberApp(orig, ident) {
				if _, err := b.WriteString(".Listen(" + inner + ")"); err != nil {
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
		return fmt.Errorf("failed to migrate listen methods: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating listen methods")
	return nil
}
