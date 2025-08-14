package v3

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	hexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	b64Re = regexp.MustCompile(`^[A-Za-z0-9+/]{43}=?$`)
)

// skipCommaSuffix advances the index past a comma and any trailing
// whitespace or newline characters.
func skipCommaSuffix(src string, i int) int {
	i++
	for i < len(src) {
		switch src[i] {
		case ' ', '\t':
			i++
		case '\n':
			return i + 1
		default:
			return i
		}
	}
	return i
}

// removeConfigField deletes a field assignment from a struct literal. It
// handles nested parentheses and braces so it can process complex values like
// multi-line function literals or function calls with arguments that contain
// commas.
func removeConfigField(src, field string) string {
	re := regexp.MustCompile(`(?m)^\s*` + field + `:\s*`)
	for {
		loc := re.FindStringIndex(src)
		if loc == nil {
			break
		}
		start := loc[0]
		i := loc[1]
		depth := 0
		inString := false
		for i < len(src) {
			ch := src[i]
			if inString {
				if ch == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				if ch == '"' {
					inString = false
				}
			} else {
				switch ch {
				case '"':
					inString = true
				case '(', '{', '[':
					depth++
				case ')', '}', ']':
					if depth > 0 {
						depth--
					}
				case ',':
					if depth == 0 {
						i = skipCommaSuffix(src, i)
						src = src[:start] + src[i:]
						goto nextField
					}
				case '\n':
					if depth == 0 {
						i++
						src = src[:start] + src[i:]
						goto nextField
					}
				}
			}
			i++
		}
		src = src[:start]
	nextField:
	}
	return src
}

func MigrateHandlerSignatures(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	sigReplacer := strings.NewReplacer("*fiber.Ctx", "fiber.Ctx")

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return sigReplacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate handler signatures: %w", err)
	}

	cmd.Println("Migrating handler signatures")

	return nil
}

// MigrateParserMethods replaces deprecated parser helper methods with the new binding API
func MigrateParserMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(
		".BodyParser(", ".Bind().Body(",
		".CookieParser(", ".Bind().Cookie(",
		".ParamsParser(", ".Bind().URI(",
		".QueryParser(", ".Bind().Query(",
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate parser methods: %w", err)
	}

	cmd.Println("Migrating parser methods")
	return nil
}

// MigrateRedirectMethods updates redirect helper methods to the new API
func MigrateRedirectMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(
		".RedirectBack(", ".Redirect().Back(",
		".RedirectToRoute(", ".Redirect().Route(",
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Redirect\(`)
		content = re.ReplaceAllString(content, ".Redirect().To(")
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate redirect methods: %w", err)
	}

	cmd.Println("Migrating redirect methods")
	return nil
}

// MigrateGenericHelpers migrates helper functions that now use generics
func MigrateGenericHelpers(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reParamsInt := regexp.MustCompile(`(\w+)\.ParamsInt\(`)
	reQueryInt := regexp.MustCompile(`(\w+)\.QueryInt\(`)
	reQueryFloat := regexp.MustCompile(`(\w+)\.QueryFloat\(`)
	reQueryBool := regexp.MustCompile(`(\w+)\.QueryBool\(`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		content = reParamsInt.ReplaceAllString(content, "fiber.Params[int]($1, ")
		content = reQueryInt.ReplaceAllString(content, "fiber.Query[int]($1, ")
		content = reQueryFloat.ReplaceAllString(content, "fiber.Query[float64]($1, ")
		content = reQueryBool.ReplaceAllString(content, "fiber.Query[bool]($1, ")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate generic helpers: %w", err)
	}

	cmd.Println("Migrating generic helpers")
	return nil
}

// MigrateContextMethods updates context related methods to the new names
func MigrateContextMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		// UserContext() removed - Ctx implements context.Context
		reUserCtx := regexp.MustCompile(`(\w+)\.UserContext\(\)`)
		content = reUserCtx.ReplaceAllString(content, `$1`)

		// SetUserContext removed - comment out the call
		reSetUserCtx := regexp.MustCompile(`(?m)^(\s*)(.*\.SetUserContext\([^\n]*\).*)$`)
		content = reSetUserCtx.ReplaceAllString(content, `$1// TODO: SetUserContext was removed, please migrate manually: $2`)

		// old Context() returned fasthttp.RequestCtx
		reReqCtx := regexp.MustCompile(`(\w+)\.Context\(\)`)
		content = reReqCtx.ReplaceAllString(content, `$1.RequestCtx()`)

		// remaining Context() usages return context.Context -> remove call
		content = strings.ReplaceAll(content, ".Context()", "")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate context methods: %w", err)
	}

	cmd.Println("Migrating context methods")
	return nil
}

// MigrateViewBind replaces the old Ctx.Bind view binding helper with ViewBind
func MigrateViewBind(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	// Replace .Bind() with arguments, not the Bind() from the binding package
	reViewBind := regexp.MustCompile(`\.Bind\(([^)]+)\)`)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return reViewBind.ReplaceAllString(content, ".ViewBind($1)")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate ViewBind calls: %w", err)
	}

	cmd.Println("Migrating view binding helpers")
	return nil
}

// MigrateMount replaces app.Mount with app.Use
func MigrateMount(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(".Mount(", ".Use(")

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Mount usages: %w", err)
	}

	cmd.Println("Migrating Mount usages")
	return nil
}

// MigrateAddMethod adapts the Add method signature
func MigrateAddMethod(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`\.Add\(\s*([^,\n]+)\s*,`)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, ".Add([]string{$1},")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Add method calls: %w", err)
	}

	cmd.Println("Migrating Add method calls")
	return nil
}

// MigrateCORSConfig updates cors middleware configuration fields
func MigrateCORSConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reOrigins := regexp.MustCompile(`AllowOrigins:\s*"([^"]*)"`)
	reMethods := regexp.MustCompile(`AllowMethods:\s*"([^"]*)"`)
	reHeaders := regexp.MustCompile(`AllowHeaders:\s*"([^"]*)"`)
	reExpose := regexp.MustCompile(`ExposeHeaders:\s*"([^"]*)"`)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		conv := func(src string, re *regexp.Regexp, field string) string {
			return re.ReplaceAllStringFunc(src, func(s string) string {
				matches := re.FindStringSubmatch(s)
				if len(matches) < 2 {
					return s
				}
				parts := strings.Split(matches[1], ",")
				for i, p := range parts {
					parts[i] = fmt.Sprintf("%q", strings.TrimSpace(p))
				}
				return fmt.Sprintf("%s: []string{%s}", field, strings.Join(parts, ", "))
			})
		}

		content = conv(content, reOrigins, "AllowOrigins")
		content = conv(content, reMethods, "AllowMethods")
		content = conv(content, reHeaders, "AllowHeaders")
		content = conv(content, reExpose, "ExposeHeaders")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate CORS configs: %w", err)
	}

	cmd.Println("Migrating CORS middleware configs")
	return nil
}

// MigrateCSRFConfig updates csrf middleware configuration fields
func MigrateCSRFConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer("Expiration:", "IdleTimeout:")
	reSession := regexp.MustCompile(`\s*SessionKey:\s*[^,]+,?\n`)
	reKeyLookup := regexp.MustCompile(`(\s*)KeyLookup:\s*([^,\n]+)(,?)(\n?)`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		content = replacer.Replace(content)
		content = reSession.ReplaceAllString(content, "")

		content = reKeyLookup.ReplaceAllStringFunc(content, func(s string) string {
			sub := reKeyLookup.FindStringSubmatch(s)
			indent := sub[1]
			val := strings.TrimSpace(sub[2])
			comma := sub[3]
			newline := sub[4]

			if uq, err := strconv.Unquote(val); err == nil {
				val = uq
			}

			var extractor string
			switch {
			case strings.HasPrefix(val, "header:"):
				extractor = fmt.Sprintf("Extractor: csrf.FromHeader(%q)", strings.TrimPrefix(val, "header:"))
			case strings.HasPrefix(val, "form:"):
				extractor = fmt.Sprintf("Extractor: csrf.FromForm(%q)", strings.TrimPrefix(val, "form:"))
			case strings.HasPrefix(val, "query:"):
				extractor = fmt.Sprintf("Extractor: csrf.FromQuery(%q)", strings.TrimPrefix(val, "query:"))
			default:
				// Unsupported or insecure value (e.g. cookie) - remove
				return ""
			}

			return fmt.Sprintf("%s%s%s%s", indent, extractor, comma, newline)
		})

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate CSRF configs: %w", err)
	}

	cmd.Println("Migrating CSRF middleware configs")
	return nil
}

// MigrateMonitorImport updates monitor middleware import path
func MigrateMonitorImport(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`github\.com/gofiber/fiber/([^/]+)/middleware/monitor`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, "github.com/gofiber/contrib/monitor")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate monitor import: %w", err)
	}

	cmd.Println("Migrating monitor middleware import")
	return nil
}

// MigrateProxyTLSConfig updates proxy TLS helper to new client configuration
func MigrateProxyTLSConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`proxy\.WithTlsConfig\(([^)]+)\)`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content,
			"proxy.WithClient(&fasthttp.Client{TLSConfig: $1})")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate proxy TLS config: %w", err)
	}

	cmd.Println("Migrating proxy TLS config")
	return nil
}

// MigrateMimeConstants updates deprecated MIME constants
func MigrateMimeConstants(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(
		"MIMEApplicationJavaScriptCharsetUTF8", "MIMETextJavaScriptCharsetUTF8",
		"MIMEApplicationJavaScript", "MIMETextJavaScript",
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate MIME constants: %w", err)
	}

	cmd.Println("Migrating MIME constants")
	return nil
}

// MigrateLoggerTags updates deprecated logger tag constants
func MigrateLoggerTags(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return strings.ReplaceAll(content, "logger.TagHeader", "logger.TagReqHeader")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate logger tags: %w", err)
	}

	cmd.Println("Migrating logger tag constants")
	return nil
}

// MigrateStaticRoutes replaces app.Static calls with static middleware
func MigrateStaticRoutes(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Static\(\s*("[^"]*")\s*,\s*("[^"]*")(?:,\s*([^)]*))?\)`)
		return re.ReplaceAllStringFunc(content, func(m string) string {
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
	})
	if err != nil {
		return fmt.Errorf("failed to migrate static usages: %w", err)
	}

	cmd.Println("Migrating app.Static usage")
	return nil
}

// MigrateTrustedProxyConfig updates trusted proxy configuration options
func MigrateTrustedProxyConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reEnable := regexp.MustCompile(`EnableTrustedProxyCheck`)
	reProxies := regexp.MustCompile(`TrustedProxies:\s*([^,\n]+),`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		content = reEnable.ReplaceAllString(content, "TrustProxy")
		content = reProxies.ReplaceAllString(content, "TrustProxyConfig: fiber.TrustProxyConfig{Proxies: $1},")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate trusted proxy config: %w", err)
	}

	cmd.Println("Migrating trusted proxy config")
	return nil
}

// MigrateConfigListenerFields updates config fields that have been moved or renamed
// in Fiber v3. It renames Prefork and Network fields and adapts them to the new
// listener configuration fields.
func MigrateConfigListenerFields(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	var disableStartup string
	var enablePrint string

	err := internal.ChangeFileContent(cwd, func(content string) string {
		replacer := strings.NewReplacer(
			"Prefork:", "EnablePrefork:",
			"Network:", "ListenerNetwork:",
		)
		content = replacer.Replace(content)

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

		reWithCfg := regexp.MustCompile(`\.Listen\(([^,]+),\s*fiber.ListenConfig\{([^}]*)\}\)`)
		content = reWithCfg.ReplaceAllStringFunc(content, func(s string) string {
			sub := reWithCfg.FindStringSubmatch(s)
			addr := sub[1]
			cfg := strings.TrimSpace(sub[2])

			if disableStartup != "" && !strings.Contains(cfg, "DisableStartupMessage:") {
				if len(cfg) > 0 && !strings.HasSuffix(cfg, ",") {
					cfg += ","
				}
				cfg += " DisableStartupMessage: " + disableStartup + ","
			}
			if enablePrint != "" && !strings.Contains(cfg, "EnablePrintRoutes:") {
				if len(cfg) > 0 && !strings.HasSuffix(cfg, ",") {
					cfg += ","
				}
				cfg += " EnablePrintRoutes: " + enablePrint + ","
			}

			cfg = strings.TrimSuffix(cfg, ",")
			return fmt.Sprintf(".Listen(%s, fiber.ListenConfig{%s})", addr, cfg)
		})

		rePlain := regexp.MustCompile(`\.Listen\(([^,\n)]+)\)`)
		content = rePlain.ReplaceAllStringFunc(content, func(s string) string {
			if strings.Contains(s, "fiber.ListenConfig") {
				return s
			}

			addr := rePlain.FindStringSubmatch(s)[1]
			fields := ""
			if disableStartup != "" {
				fields += "DisableStartupMessage: " + disableStartup + ", "
			}
			if enablePrint != "" {
				fields += "EnablePrintRoutes: " + enablePrint + ", "
			}
			fields = strings.TrimSpace(fields)
			fields = strings.TrimSuffix(fields, ",")

			return fmt.Sprintf(".Listen(%s, fiber.ListenConfig{%s})", addr, fields)
		})

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener related listen calls: %w", err)
	}

	cmd.Println("Migrating listener related config fields")
	return nil
}

// MigrateListenerCallbacks removes deprecated OnShutdown callbacks from
// ListenerConfig. Fiber v3 replaces these with the OnPostShutdown hook.
func MigrateListenerCallbacks(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reErr := regexp.MustCompile(`\s*OnShutdownError:\s*[^,]+,?\n`)
	reSuccess := regexp.MustCompile(`\s*OnShutdownSuccess:\s*[^,]+,?\n`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		content = reErr.ReplaceAllString(content, "")
		content = reSuccess.ReplaceAllString(content, "")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener callbacks: %w", err)
	}

	cmd.Println("Migrating listener callbacks")
	return nil
}

// MigrateShutdownHook updates the deprecated OnShutdown hook to OnPostShutdown
// and adapts inline hook functions to accept the error parameter.
func MigrateShutdownHook(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		reName := regexp.MustCompile(`\.Hooks\(\)\.OnShutdown\(`)
		content = reName.ReplaceAllString(content, ".Hooks().OnPostShutdown(")

		reInline := regexp.MustCompile(`\.OnPostShutdown\(func\(\s*\)`)
		content = reInline.ReplaceAllString(content, ".OnPostShutdown(func(err error)")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate shutdown hooks: %w", err)
	}

	cmd.Println("Migrating shutdown hooks")
	return nil
}

// MigrateFilesystemMiddleware replaces deprecated filesystem middleware with static middleware
func MigrateFilesystemMiddleware(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		content = strings.ReplaceAll(content,
			"github.com/gofiber/fiber/v2/middleware/filesystem",
			"github.com/gofiber/fiber/v3/middleware/static")
		content = strings.ReplaceAll(content,
			"github.com/gofiber/fiber/v3/middleware/filesystem",
			"github.com/gofiber/fiber/v3/middleware/static")

		reNew := regexp.MustCompile(`filesystem\.New\s*\(`)
		content = reNew.ReplaceAllString(content, `static.New("", `)

		content = strings.ReplaceAll(content, "filesystem.Config{", "static.Config{")

		reRootHTTP := regexp.MustCompile(`Root:\s*http.Dir\(([^)]+)\)`)
		content = reRootHTTP.ReplaceAllString(content, `FS: os.DirFS($1)`)

		reRoot := regexp.MustCompile(`Root:\s*([^,\n]+)`)
		content = reRoot.ReplaceAllString(content, `FS: os.DirFS($1)`)

		reIndex := regexp.MustCompile(`Index:\s*([^,\n]+)`)
		content = reIndex.ReplaceAllString(content, `IndexNames: []string{$1}`)

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate filesystem middleware: %w", err)
	}

	cmd.Println("Migrating filesystem middleware")
	return nil
}

// MigrateEnvVarConfig removes deprecated ExcludeVars field from envvar middleware configuration
func MigrateEnvVarConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`\s*ExcludeVars:\s*[^,]+,?\n`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, "")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate EnvVar configs: %w", err)
	}

	cmd.Println("Migrating EnvVar middleware configs")
	return nil
}

// MigrateHealthcheckConfig updates healthcheck middleware configuration and usage
// to the new Fiber v3 handler based API. It replaces the old middleware usage
//
//	app.Use(healthcheck.New(...))
//
// with explicit registrations using `app.Get` on the liveness and readiness
// endpoints and adapts the configuration structure.
func MigrateHealthcheckConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
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

				reLE := regexp.MustCompile(`(?m)\s*LivenessEndpoint:\s*([^,\n}]+),?`)
				if m := reLE.FindStringSubmatch(body); len(m) > 1 {
					lEndpoint = strings.TrimSpace(m[1])
				}
				body = reLE.ReplaceAllString(body, "")

				reRE := regexp.MustCompile(`(?m)\s*ReadinessEndpoint:\s*([^,\n}]+),?`)
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

	cmd.Println("Migrating healthcheck middleware configs")
	return nil
}

// MigrateLimiterConfig updates limiter middleware configuration fields
func MigrateLimiterConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		reConfig := regexp.MustCompile(`limiter\.Config{[^}]*}`)
		return reConfig.ReplaceAllStringFunc(content, func(s string) string {
			s = strings.ReplaceAll(s, "Duration:", "Expiration:")
			s = strings.ReplaceAll(s, "Store:", "Storage:")
			s = strings.ReplaceAll(s, "Key:", "KeyGenerator:")
			return s
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate limiter configs: %w", err)
	}

	cmd.Println("Migrating limiter middleware configs")
	return nil
}

// MigrateSessionConfig updates session middleware configuration fields
func MigrateSessionConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		reConfig := regexp.MustCompile(`session\.Config{[^}]*}`)
		return reConfig.ReplaceAllStringFunc(content, func(s string) string {
			return strings.ReplaceAll(s, "Expiration:", "IdleTimeout:")
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session configs: %w", err)
	}

	cmd.Println("Migrating session middleware configs")
	return nil
}

// MigrateTimeoutConfig updates timeout middleware usage to the new Config parameter
func MigrateTimeoutConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`timeout\.New\(\s*([^,\n]+)\s*,\s*([^\n)]+)\)`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, `timeout.New($1, timeout.Config{Timeout: $2})`)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate timeout middleware configs: %w", err)
	}

	cmd.Println("Migrating timeout middleware configs")
	return nil
}

// MigrateAppTestConfig updates app.Test calls to use the new TestConfig parameter
func MigrateAppTestConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Test\(([^,\n]+),\s*([^\n)]+)\)`)
		return re.ReplaceAllStringFunc(content, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if len(sub) < 3 {
				return m
			}
			arg := strings.TrimSpace(sub[2])
			if arg == "-1" {
				return fmt.Sprintf(".Test(%s, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})", sub[1])
			}
			return fmt.Sprintf(".Test(%s, fiber.TestConfig{Timeout: %s})", sub[1], arg)
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate app.Test calls: %w", err)
	}

	cmd.Println("Migrating app.Test usages")
	return nil
}

// MigrateMiddlewareLocals replaces Locals lookups for middleware data with helper functions
func MigrateMiddlewareLocals(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		replacements := []struct {
			re   *regexp.Regexp
			repl string
		}{
			{regexp.MustCompile(`(\w+)\.Locals\("requestid"\)`), `requestid.FromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("csrf"\)`), `csrf.TokenFromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("csrf_handler"\)`), `csrf.HandlerFromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("session"\)`), `session.FromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("username"\)`), `basicauth.UsernameFromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("password"\)`), `basicauth.PasswordFromContext($1)`},
			{regexp.MustCompile(`(\w+)\.Locals\("token"\)`), `keyauth.TokenFromContext($1)`},
		}
		for _, r := range replacements {
			content = r.re.ReplaceAllString(content, r.repl)
		}
		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate middleware locals: %w", err)
	}

	cmd.Println("Migrating middleware locals")
	return nil
}

// MigrateListenMethods replaces removed Listen helpers with Listen
func MigrateListenMethods(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(
		".ListenTLSWithCertificate(", ".Listen(",
		".ListenTLS(", ".Listen(",
		".ListenMutualTLSWithCertificate(", ".Listen(",
		".ListenMutualTLS(", ".Listen(",
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listen methods: %w", err)
	}

	cmd.Println("Migrating listen methods")
	return nil
}

// MigrateReqHeaderParser replaces the deprecated ReqHeaderParser helper with the new binding API
func MigrateReqHeaderParser(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(
		".ReqHeaderParser(", ".Bind().Header(",
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate ReqHeaderParser: %w", err)
	}

	cmd.Println("Migrating request header parser helper")
	return nil
}

// MigrateBasicauthAuthorizer updates inline basicauth authorizer functions to include the context parameter
func MigrateBasicauthAuthorizer(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`Authorizer:\s*func\(([^)]*)\)`)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, `Authorizer: func($1, _ fiber.Ctx)`)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate basicauth authorizer: %w", err)
	}

	cmd.Println("Migrating basicauth authorizer")
	return nil
}

// MigrateBasicauthConfig adapts basicauth configuration to the new API
// * removes ContextUsername and ContextPassword fields
// * hashes plaintext Users entries using SHA-256
func MigrateBasicauthConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reCtxUser := regexp.MustCompile(`\s*ContextUsername:\s*[^,]+,?\n`)
	reCtxPass := regexp.MustCompile(`\s*ContextPassword:\s*[^,]+,?\n`)
	reUsers := regexp.MustCompile(`Users:\s*map\[string\]string{([^}]*)}`)
	reEntry := regexp.MustCompile(`("[^"]+")\s*:\s*"((?:[^"\\]|\\.)*)"`)

	err := internal.ChangeFileContent(cwd, func(content string) string {
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

	cmd.Println("Migrating basicauth configs")
	return nil
}

// MigrateBasicauthStorePassword comments usages of the removed StorePassword option
// in basicauth middleware configuration.
func MigrateBasicauthStorePassword(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	re := regexp.MustCompile(`(\s*)StorePassword:\s*([^,\n]+)(,?)`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return re.ReplaceAllString(content, `$1// TODO: StorePassword removed ($2)$3`)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate basicauth StorePassword: %w", err)
	}

	cmd.Println("Migrating basicauth StorePassword option")
	return nil
}

// MigrateCacheConfig updates cache middleware configuration fields
func MigrateCacheConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		reConfig := regexp.MustCompile(`cache\.Config{[^}]*}`)
		return reConfig.ReplaceAllStringFunc(content, func(s string) string {
			s = strings.ReplaceAll(s, "Store:", "Storage:")
			s = strings.ReplaceAll(s, "Key:", "KeyGenerator:")
			return s
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate cache configs: %w", err)
	}

	cmd.Println("Migrating cache middleware configs")
	return nil
}

// MigrateSessionExtractor updates session KeyLookup to the new Extractor pattern
func MigrateSessionExtractor(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reKeyLookup := regexp.MustCompile(`(\s*)KeyLookup:\s*([^,\n]+)(,?)(\n?)`)
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return reKeyLookup.ReplaceAllStringFunc(content, func(s string) string {
			sub := reKeyLookup.FindStringSubmatch(s)
			indent := sub[1]
			val := strings.TrimSpace(sub[2])
			comma := sub[3]
			newline := sub[4]

			if uq, err := strconv.Unquote(val); err == nil {
				val = uq
			}

			parts := strings.Split(val, ",")
			var extractors []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				switch {
				case strings.HasPrefix(p, "cookie:"):
					extractors = append(extractors, fmt.Sprintf("session.FromCookie(%q)", strings.TrimPrefix(p, "cookie:")))
				case strings.HasPrefix(p, "header:"):
					extractors = append(extractors, fmt.Sprintf("session.FromHeader(%q)", strings.TrimPrefix(p, "header:")))
				case strings.HasPrefix(p, "query:"):
					extractors = append(extractors, fmt.Sprintf("session.FromQuery(%q)", strings.TrimPrefix(p, "query:")))
				default:
					return ""
				}
			}

			if len(extractors) == 0 {
				return ""
			}

			extractor := extractors[0]
			if len(extractors) > 1 {
				extractor = fmt.Sprintf("session.Chain(%s)", strings.Join(extractors, ", "))
			}

			return fmt.Sprintf("%sExtractor: %s%s%s", indent, extractor, comma, newline)
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session extractor config: %w", err)
	}

	cmd.Println("Migrating session KeyLookup config")
	return nil
}

// MigrateKeyAuthConfig updates keyauth middleware configuration to use Extractor
// instead of KeyLookup/AuthScheme and removes the deprecated fields.
func MigrateKeyAuthConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	reConfig := regexp.MustCompile(`keyauth\.Config{[^}]*}`)
	reKeyLookup := regexp.MustCompile(`(?m)(\s*)KeyLookup:\s*("[^"]+")(,?)(\n?)`)
	reAuthScheme := regexp.MustCompile(`(?m)\s*AuthScheme:\s*([^,\n]+)`)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return reConfig.ReplaceAllStringFunc(content, func(cfg string) string {
			keyMatch := reKeyLookup.FindStringSubmatch(cfg)
			if len(keyMatch) < 5 {
				// remove AuthScheme if present
				return removeConfigField(cfg, "AuthScheme")
			}

			indent := keyMatch[1]
			val := strings.TrimSpace(keyMatch[2])
			comma := keyMatch[3]
			newline := keyMatch[4]

			if uq, err := strconv.Unquote(val); err == nil {
				val = uq
			}

			scheme := "Bearer"
			if am := reAuthScheme.FindStringSubmatch(cfg); len(am) > 1 {
				scheme = strings.TrimSpace(am[1])
				if uq, err := strconv.Unquote(scheme); err == nil {
					scheme = uq
				}
			}

			parts := strings.Split(val, ",")
			var extractors []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				switch {
				case strings.HasPrefix(p, "header:"):
					header := strings.TrimPrefix(p, "header:")
					if strings.EqualFold(header, "Authorization") {
						extractors = append(extractors, fmt.Sprintf("keyauth.FromAuthHeader(%q, %q)", header, scheme))
					} else {
						extractors = append(extractors, fmt.Sprintf("keyauth.FromHeader(%q)", header))
					}
				case strings.HasPrefix(p, "query:"):
					extractors = append(extractors, fmt.Sprintf("keyauth.FromQuery(%q)", strings.TrimPrefix(p, "query:")))
				case strings.HasPrefix(p, "param:"):
					extractors = append(extractors, fmt.Sprintf("keyauth.FromParam(%q)", strings.TrimPrefix(p, "param:")))
				case strings.HasPrefix(p, "form:"):
					extractors = append(extractors, fmt.Sprintf("keyauth.FromForm(%q)", strings.TrimPrefix(p, "form:")))
				case strings.HasPrefix(p, "cookie:"):
					extractors = append(extractors, fmt.Sprintf("keyauth.FromCookie(%q)", strings.TrimPrefix(p, "cookie:")))
				default:
					// unrecognized source; remove field and return
					cfg = removeConfigField(cfg, "AuthScheme")
					return removeConfigField(cfg, "KeyLookup")
				}
			}

			extractor := ""
			if len(extractors) == 1 {
				extractor = extractors[0]
			} else if len(extractors) > 1 {
				extractor = fmt.Sprintf("keyauth.Chain(%s)", strings.Join(extractors, ", "))
			}

			cfg = removeConfigField(cfg, "AuthScheme")
			if extractor == "" {
				return removeConfigField(cfg, "KeyLookup")
			}

			newField := fmt.Sprintf("%sExtractor: %s%s%s", indent, extractor, comma, newline)
			cfg = reKeyLookup.ReplaceAllString(cfg, newField)
			return cfg
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate keyauth configs: %w", err)
	}

	cmd.Println("Migrating keyauth middleware configs")
	return nil
}
