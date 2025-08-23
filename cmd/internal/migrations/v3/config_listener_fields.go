package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateConfigListenerFields(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	var disableStartup string
	var enablePrint string

	err := internal.ChangeFileContent(cwd, func(content string) string {
		rePrefork := regexp.MustCompile(`(?m)^(\s*)Prefork:`)
		content = rePrefork.ReplaceAllString(content, `${1}EnablePrefork:`)
		reNetwork := regexp.MustCompile(`(?m)^(\s*)Network:`)
		content = reNetwork.ReplaceAllString(content, `${1}ListenerNetwork:`)

		reStartup := regexp.MustCompile(`(?m)^\s*DisableStartupMessage:\s*([^,]+),?\n`)
		content = reStartup.ReplaceAllStringFunc(content, func(s string) string {
			if disableStartup == "" {
				sub := reStartup.FindStringSubmatch(s)
				if len(sub) > 1 {
					disableStartup = strings.TrimSpace(sub[1])
				}
			}
			return ""
		})

		rePrint := regexp.MustCompile(`(?m)^\s*EnablePrintRoutes:\s*([^,]+),?\n`)
		content = rePrint.ReplaceAllStringFunc(content, func(s string) string {
			if enablePrint == "" {
				sub := rePrint.FindStringSubmatch(s)
				if len(sub) > 1 {
					enablePrint = strings.TrimSpace(sub[1])
				}
			}
			return ""
		})

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener related config fields: %w", err)
	}

	err = internal.ChangeFileContent(cwd, func(content string) string {
		if disableStartup == "" && enablePrint == "" {
			return content
		}

		return replaceCall(content, ".Listen", func(call string, args []string) string {
			if len(args) == 0 {
				return call
			}
			if len(args) == 2 && strings.Contains(args[1], "fiber.ListenConfig") {
				cfg := strings.TrimPrefix(args[1], "fiber.ListenConfig{")
				cfg = strings.TrimSuffix(cfg, "}")
				if disableStartup != "" && !strings.Contains(cfg, "DisableStartupMessage:") {
					if len(strings.TrimSpace(cfg)) > 0 {
						cfg += ","
					}
					cfg += " DisableStartupMessage: " + disableStartup
				}
				if enablePrint != "" && !strings.Contains(cfg, "EnablePrintRoutes:") {
					if len(strings.TrimSpace(cfg)) > 0 {
						cfg += ","
					}
					cfg += " EnablePrintRoutes: " + enablePrint
				}
				cfg = strings.TrimSuffix(strings.TrimSpace(cfg), ",")
				return fmt.Sprintf(".Listen(%s, fiber.ListenConfig{%s})", args[0], cfg)
			}

			if len(args) == 1 {
				fields := ""
				if disableStartup != "" {
					fields += "DisableStartupMessage: " + disableStartup + ", "
				}
				if enablePrint != "" {
					fields += "EnablePrintRoutes: " + enablePrint + ", "
				}
				fields = strings.TrimSuffix(strings.TrimSpace(fields), ",")
				return fmt.Sprintf(".Listen(%s, fiber.ListenConfig{%s})", args[0], fields)
			}

			return call
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener related listen calls: %w", err)
	}

	cmd.Println("Migrating listener related config fields")
	return nil
}

// ListenerConfig. Fiber v3 replaces these with the OnPostShutdown hook.
