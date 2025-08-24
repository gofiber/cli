package v3

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateBasicauthConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reCtxUser := regexp.MustCompile(`(?m)\s*ContextUsername:\s*[^,\n]+,?\s*(//[^\n]*)?\n`)
	reCtxPass := regexp.MustCompile(`(?m)\s*ContextPassword:\s*[^,\n]+,?\s*(//[^\n]*)?\n`)
	reUsers := regexp.MustCompile(`Users:\s*map\[string\]string{([^}]*)}`)
	reEntry := regexp.MustCompile(`("[^"]+")\s*:\s*"((?:[^"\\]|\\.)*)"`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		content = reCtxUser.ReplaceAllString(content, "")
		content = reCtxPass.ReplaceAllString(content, "")

		content = reUsers.ReplaceAllStringFunc(content, func(m string) string {
			sub := reUsers.FindStringSubmatch(m)
			if len(sub) < 2 {
				return m
			}
			body := reEntry.ReplaceAllStringFunc(sub[1], func(s string) string {
				es := reEntry.FindStringSubmatch(s)
				if len(es) < 3 {
					return s
				}
				pwd := es[2]
				if strings.HasPrefix(pwd, "{") || strings.HasPrefix(pwd, "$2") || hexRe.MatchString(pwd) || b64Re.MatchString(pwd) {
					return s
				}
				sum := sha256.Sum256([]byte(pwd))
				hashed := base64.StdEncoding.EncodeToString(sum[:])
				return fmt.Sprintf("%s: \"{SHA256}%s\"", es[1], hashed)
			})
			return "Users: map[string]string{" + body + "}"
		})

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate basicauth config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating basicauth configs")
	return nil
}

// in basicauth middleware configuration.
