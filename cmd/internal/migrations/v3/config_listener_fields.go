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
	var enablePrefork string
	var listenerNetwork string
	var disableStartup string
	var enablePrint string

	reField := regexp.MustCompile(`\s*(Prefork|Network|DisableStartupMessage|EnablePrintRoutes):\s*([^,}]+),?`)
	changed1, err := internal.ChangeFileContent(cwd, func(content string) string {
		reConfig := regexp.MustCompile(`fiber\.Config\{[^}]*\}`)
		return reConfig.ReplaceAllStringFunc(content, func(cfg string) string {
			inner := cfg[len("fiber.Config{") : len(cfg)-1]
			inner = reField.ReplaceAllStringFunc(inner, func(f string) string {
				sub := reField.FindStringSubmatch(f)
				if len(sub) == 3 {
					name := sub[1]
					val := strings.TrimSpace(sub[2])
					switch name {
					case "Prefork":
						if enablePrefork == "" {
							enablePrefork = val
						}
					case "Network":
						if listenerNetwork == "" {
							listenerNetwork = val
						}
					case "DisableStartupMessage":
						if disableStartup == "" {
							disableStartup = val
						}
					case "EnablePrintRoutes":
						if enablePrint == "" {
							enablePrint = val
						}
					}
					return ""
				}
				return f
			})
			inner = regexp.MustCompile(`,\s*,`).ReplaceAllString(inner, ",")
			inner = strings.TrimSpace(inner)
			inner = strings.TrimPrefix(inner, ",")
			inner = strings.TrimSuffix(inner, ",")
			inner = strings.TrimSpace(inner)
			if inner == "" {
				return "fiber.Config{}"
			}
			return "fiber.Config{" + inner + "}"
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener related config fields: %w", err)
	}

	changed2, err := internal.ChangeFileContent(cwd, func(content string) string {
		if enablePrefork == "" && listenerNetwork == "" && disableStartup == "" && enablePrint == "" {
			return content
		}

		return replaceCall(content, ".Listen", func(call string, args []string) string {
			if len(args) == 0 {
				return call
			}
			if len(args) == 2 && strings.Contains(args[1], "fiber.ListenConfig") {
				cfg := strings.TrimPrefix(args[1], "fiber.ListenConfig{")
				cfg = strings.TrimSuffix(cfg, "}")
				if enablePrefork != "" && !strings.Contains(cfg, "EnablePrefork:") {
					if len(strings.TrimSpace(cfg)) > 0 {
						cfg += ","
					}
					cfg += " EnablePrefork: " + enablePrefork
				}
				if listenerNetwork != "" && !strings.Contains(cfg, "ListenerNetwork:") {
					if len(strings.TrimSpace(cfg)) > 0 {
						cfg += ","
					}
					cfg += " ListenerNetwork: " + listenerNetwork
				}
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
				var fields []string
				if enablePrefork != "" {
					fields = append(fields, "EnablePrefork: "+enablePrefork)
				}
				if listenerNetwork != "" {
					fields = append(fields, "ListenerNetwork: "+listenerNetwork)
				}
				if disableStartup != "" {
					fields = append(fields, "DisableStartupMessage: "+disableStartup)
				}
				if enablePrint != "" {
					fields = append(fields, "EnablePrintRoutes: "+enablePrint)
				}
				return fmt.Sprintf(".Listen(%s, fiber.ListenConfig{%s})", args[0], strings.Join(fields, ", "))
			}

			return call
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener related listen calls: %w", err)
	}
	if !(changed1 || changed2) {
		return nil
	}

	cmd.Println("Migrating listener related config fields")
	return nil
}
