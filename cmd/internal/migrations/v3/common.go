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
	replacer := strings.NewReplacer(
		".Context()", ".RequestCtx()",
		".UserContext()", ".Context()",
		".SetUserContext(", ".SetContext(", // TODO: check if this is correct
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
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
	err := internal.ChangeFileContent(cwd, func(content string) string {
		replacer := strings.NewReplacer(
			"Prefork:", "EnablePrefork:",
			"Network:", "ListenerNetwork:",
		)
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate listener related config fields: %w", err)
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

// MigrateHealthcheckConfig updates healthcheck middleware configuration fields
func MigrateHealthcheckConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		content = strings.ReplaceAll(content, "LivenessProbe:", "Probe:")

		re := regexp.MustCompile(`\s*ReadinessProbe:\s*[^,]+,?\n`)
		content = re.ReplaceAllString(content, "")

		re = regexp.MustCompile(`\s*LivenessEndpoint:\s*[^,]+,?\n?`)
		content = re.ReplaceAllString(content, "")

		re = regexp.MustCompile(`\s*ReadinessEndpoint:\s*[^,]+,?\n?`)
		content = re.ReplaceAllString(content, "")

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
			s = strings.ReplaceAll(s, "Expiration:", "IdleTimeout:")
			return s
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate session configs: %w", err)
	}

	cmd.Println("Migrating session middleware configs")
	return nil
}

// MigrateAppTestConfig updates app.Test calls to use the new TestConfig parameter
func MigrateAppTestConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Test\(([^,\n]+),\s*([^\n)]+)\)`)
		return re.ReplaceAllString(content, `.Test($1, fiber.TestConfig{Timeout: $2})`)
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
