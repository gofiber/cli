package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateHealthcheckConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContent(cwd, func(content string) string {
		// Replace app.Use(healthcheck.New(...)) with app.Get registrations
		reUse := regexp.MustCompile(`(?m)^(\s*)(\w+)\.Use\(\s*healthcheck\.New\((?s:(.*))\)\s*\)`)
		content = reUse.ReplaceAllStringFunc(content, func(s string) string {
			sub := reUse.FindStringSubmatch(s)
			indent := sub[1]
			appVar := sub[2]
			args := strings.TrimSpace(sub[3])

			lEndpoint := "healthcheck.LivenessEndpoint"
			rEndpoint := "healthcheck.ReadinessEndpoint"
			lCfg := args
			rCfg := args

			if strings.HasPrefix(args, "healthcheck.Config") {
				reBody := regexp.MustCompile(`healthcheck\.Config\{(?s:(.*))\}`)
				bodyMatch := reBody.FindStringSubmatch(args)
				body := ""
				if len(bodyMatch) > 1 {
					body = bodyMatch[1]
				}

				reLE := regexp.MustCompile(`(?m)\s*LivenessEndpoint:\s*([^,\n}]+),?\s*(//[^\n]*)?\n?`)
				if m := reLE.FindStringSubmatch(body); len(m) > 1 {
					lEndpoint = strings.TrimSpace(m[1])
				}
				body = reLE.ReplaceAllString(body, "")

				reRE := regexp.MustCompile(`(?m)\s*ReadinessEndpoint:\s*([^,\n}]+),?\s*(//[^\n]*)?\n?`)
				if m := reRE.FindStringSubmatch(body); len(m) > 1 {
					rEndpoint = strings.TrimSpace(m[1])
				}
				body = reRE.ReplaceAllString(body, "")

				// Liveness config
				bodyL := removeConfigField(body, "ReadinessProbe")
				bodyL = strings.ReplaceAll(bodyL, "LivenessProbe:", "Probe:")
				bodyL = strings.TrimSpace(bodyL)
				bodyL = strings.TrimSuffix(bodyL, ",")
				lCfg = strings.TrimSpace("healthcheck.Config{" + bodyL + "}")

				// Readiness config
				bodyR := removeConfigField(body, "LivenessProbe")
				bodyR = strings.ReplaceAll(bodyR, "ReadinessProbe:", "Probe:")
				bodyR = strings.TrimSpace(bodyR)
				bodyR = strings.TrimSuffix(bodyR, ",")
				rCfg = strings.TrimSpace("healthcheck.Config{" + bodyR + "}")
			}

			makeCall := func(cfg string) string {
				cfg = strings.TrimSpace(cfg)
				cfg = strings.TrimSuffix(cfg, ",")
				cfg = strings.TrimSpace(cfg)
				if cfg == "" || cfg == "healthcheck.Config{}" {
					return "healthcheck.New()"
				}
				return fmt.Sprintf("healthcheck.New(%s)", cfg)
			}

			lLine := fmt.Sprintf("%s%s.Get(%s, %s)", indent, appVar, lEndpoint, makeCall(lCfg))
			rLine := fmt.Sprintf("%s%s.Get(%s, %s)", indent, appVar, rEndpoint, makeCall(rCfg))
			return lLine + "\n" + rLine
		})

		// Adapt remaining config structures outside of app.Use replacements
		content = strings.ReplaceAll(content, "LivenessProbe:", "Probe:")
		content = removeConfigField(content, "ReadinessProbe")
		content = removeConfigField(content, "LivenessEndpoint")
		content = removeConfigField(content, "ReadinessEndpoint")
		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate healthcheck configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating healthcheck middleware configs")
	return nil
}
