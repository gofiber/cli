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
	err := internal.ChangeFileContent(cwd, func(content string) string {
		reParamsInt := regexp.MustCompile(`(\w+)\.ParamsInt\(`)
		content = reParamsInt.ReplaceAllString(content, "fiber.Params[int]($1, ")

		reQueryInt := regexp.MustCompile(`(\w+)\.QueryInt\(`)
		content = reQueryInt.ReplaceAllString(content, "fiber.Query[int]($1, ")

		reQueryFloat := regexp.MustCompile(`(\w+)\.QueryFloat\(`)
		content = reQueryFloat.ReplaceAllString(content, "fiber.Query[float64]($1, ")

		reQueryBool := regexp.MustCompile(`(\w+)\.QueryBool\(`)
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

// MigrateAllParams replaces deprecated AllParams helper with the new binding API
func MigrateAllParams(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	replacer := strings.NewReplacer(
		".AllParams(", ".Bind().URI(",
	)

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return replacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate AllParams: %w", err)
	}

	cmd.Println("Migrating AllParams")
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
	err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Add\(\s*([^,\n]+)\s*,`)
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

		reOrigins := regexp.MustCompile(`AllowOrigins:\s*"([^"]*)"`)
		content = conv(content, reOrigins, "AllowOrigins")

		reMethods := regexp.MustCompile(`AllowMethods:\s*"([^"]*)"`)
		content = conv(content, reMethods, "AllowMethods")

		reHeaders := regexp.MustCompile(`AllowHeaders:\s*"([^"]*)"`)
		content = conv(content, reHeaders, "AllowHeaders")

		reExpose := regexp.MustCompile(`ExposeHeaders:\s*"([^"]*)"`)
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

	err := internal.ChangeFileContent(cwd, func(content string) string {
		content = replacer.Replace(content)
		re := regexp.MustCompile(`\s*SessionKey:\s*[^,]+,?\n`)
		return re.ReplaceAllString(content, "")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate CSRF configs: %w", err)
	}

	cmd.Println("Migrating CSRF middleware configs")
	return nil
}

// MigrateMonitorImport updates monitor middleware import path
func MigrateMonitorImport(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		return strings.ReplaceAll(content,
			"github.com/gofiber/fiber/v2/middleware/monitor",
			"github.com/gofiber/contrib/monitor")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate monitor import: %w", err)
	}

	cmd.Println("Migrating monitor middleware import")
	return nil
}

// MigrateProxyTLSConfig updates proxy TLS helper to new client configuration
func MigrateProxyTLSConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`proxy\.WithTlsConfig\(([^)]+)\)`)
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
	err := internal.ChangeFileContent(cwd, func(content string) string {
		reEnable := regexp.MustCompile(`EnableTrustedProxyCheck`)
		content = reEnable.ReplaceAllString(content, "TrustProxy")

		reProxies := regexp.MustCompile(`TrustedProxies:\s*([^,\n]+),`)
		content = reProxies.ReplaceAllString(content, "TrustProxyConfig: fiber.TrustProxyConfig{Proxies: $1},")

		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate trusted proxy config: %w", err)
	}

	cmd.Println("Migrating trusted proxy config")
	return nil
}
