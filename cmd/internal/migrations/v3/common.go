package v3

import (
	"fmt"
	"regexp"
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
		".SetUserContext(", ".SetContext(",
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

// MigrateRouterAddSignature updates Router.Add signature to use a slice of methods
func MigrateRouterAddSignature(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	err := internal.ChangeFileContent(cwd, func(content string) string {
		re := regexp.MustCompile(`\.Add\((\s*[^,]+\s*),`)
		return re.ReplaceAllString(content, ".Add([]string{$1},")
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Router.Add signature: %w", err)
	}

	cmd.Println("Migrating Router.Add signature")
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
