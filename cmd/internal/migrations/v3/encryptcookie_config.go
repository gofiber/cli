package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateEncryptcookieConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reConfig := regexp.MustCompile(`encryptcookie\.Config{`)

	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		return IterateConfigBlocks(content, reConfig, func(cfg string) string {
			cfg = addEncryptcookieParam(cfg, "Encryptor")
			cfg = addEncryptcookieParam(cfg, "Decryptor")
			return cfg
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate encryptcookie configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating encryptcookie middleware configs")
	return nil
}

func addEncryptcookieParam(cfg, field string) string {
	re := regexp.MustCompile(field + `:\s*func\s*\(([^)]*)\)`)
	return re.ReplaceAllStringFunc(cfg, func(match string) string {
		sub := re.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}

		params := sub[1]
		trimmed := strings.TrimSpace(params)
		if trimmed == "" {
			return match
		}

		args := splitArgs(params)
		var nonEmpty []string
		for _, arg := range args {
			if strings.TrimSpace(arg) != "" {
				nonEmpty = append(nonEmpty, arg)
			}
		}

		if len(nonEmpty) != 2 {
			return match
		}

		insertPos := 0
		for insertPos < len(params) && (params[insertPos] == ' ' || params[insertPos] == '\t' || params[insertPos] == '\n') {
			insertPos++
		}

		insertion := "_ string, "
		if idx := strings.LastIndex(params[:insertPos], "\n"); idx >= 0 {
			indent := params[idx+1 : insertPos]
			insertion = "_ string,\n" + indent
		}

		newParams := params[:insertPos] + insertion + params[insertPos:]
		return strings.Replace(match, "("+params+")", "("+newParams+")", 1)
	})
}
