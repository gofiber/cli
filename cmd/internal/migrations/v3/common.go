package v3

import (
	"fmt"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

func MigrateHandlerSignatures(cmd *cobra.Command, cwd string, _ *semver.Version, _ *semver.Version) error {
	sigReplacer := strings.NewReplacer("*fiber.Ctx", "fiber.Ctx")

	err := internal.ChangeFileContent(cwd, func(content string) string {
		return sigReplacer.Replace(content)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate handler signatures: %v", err)
	}

	cmd.Println("Migrating handler signatures")

	return nil
}

// MigrateParserMethods replaces deprecated parser helper methods with the new binding API
func MigrateParserMethods(cmd *cobra.Command, cwd string, _ *semver.Version, _ *semver.Version) error {
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
		return fmt.Errorf("failed to migrate parser methods: %v", err)
	}

	cmd.Println("Migrating parser methods")
	return nil
}

// MigrateRedirectMethods updates redirect helper methods to the new API
func MigrateRedirectMethods(cmd *cobra.Command, cwd string, _ *semver.Version, _ *semver.Version) error {
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
		return fmt.Errorf("failed to migrate redirect methods: %v", err)
	}

	cmd.Println("Migrating redirect methods")
	return nil
}

// MigrateGenericHelpers migrates helper functions that now use generics
func MigrateGenericHelpers(cmd *cobra.Command, cwd string, _ *semver.Version, _ *semver.Version) error {
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
		return fmt.Errorf("failed to migrate generic helpers: %v", err)
	}

	cmd.Println("Migrating generic helpers")
	return nil
}
