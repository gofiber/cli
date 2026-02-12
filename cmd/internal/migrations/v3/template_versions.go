package v3

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var reTemplateImport = regexp.MustCompile(`"github\.com/gofiber/template/([a-zA-Z0-9_-]+)(?:/v(\d+))?([^\"]*)"`)

type templateTarget struct {
	modulePath string
	version    string
}

// templateTargets maps template packages to their minimum module path and version
// required for Fiber v3 core compatibility. Source:
// https://github.com/gofiber/template/pull/437
var templateTargets = map[string]templateTarget{
	"ace":        {modulePath: "github.com/gofiber/template/ace/v3", version: "v3.0.0"},
	"amber":      {modulePath: "github.com/gofiber/template/amber/v3", version: "v3.0.0"},
	"django":     {modulePath: "github.com/gofiber/template/django/v4", version: "v4.0.0"},
	"handlebars": {modulePath: "github.com/gofiber/template/handlebars/v3", version: "v3.0.0"},
	"html":       {modulePath: "github.com/gofiber/template/html/v3", version: "v3.0.0"},
	"jet":        {modulePath: "github.com/gofiber/template/jet/v3", version: "v3.0.0"},
	"mustache":   {modulePath: "github.com/gofiber/template/mustache/v3", version: "v3.0.0"},
	"pug":        {modulePath: "github.com/gofiber/template/pug/v3", version: "v3.0.0"},
	"slim":       {modulePath: "github.com/gofiber/template/slim/v3", version: "v3.0.0"},
}

// MigrateTemplateVersions updates template package imports and go.mod entries
// to the minimum versions required for Fiber v3 core compatibility.
func MigrateTemplateVersions(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changedImports, err := internal.ChangeFileContent(cwd, func(content string) string {
		return reTemplateImport.ReplaceAllStringFunc(content, func(match string) string {
			sub := reTemplateImport.FindStringSubmatch(match)
			if len(sub) != 4 {
				return match
			}

			target, ok := templateTargets[sub[1]]
			if !ok {
				return match
			}

			return fmt.Sprintf(`"%s%s"`, target.modulePath, sub[3])
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate template imports: %w", err)
	}

	modChanged, err := migrateTemplateModules(cwd)
	if err != nil {
		return err
	}

	if !changedImports && !modChanged {
		return nil
	}

	cmd.Println("Migrated template package versions")
	return nil
}

func migrateTemplateModules(cwd string) (bool, error) {
	modChanged := false

	walkErr := filepath.WalkDir(cwd, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		b, err := os.ReadFile(path) // #nosec G304 -- reading module file
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		content := string(b)
		updated := content

		for pkg, target := range templateTargets {
			updated = updateTemplateGoModModule(updated, pkg, target.modulePath, target.version)
		}

		if updated == content {
			return nil
		}

		if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		modChanged = true
		return nil
	})
	if walkErr != nil {
		return false, fmt.Errorf("failed to migrate template modules: %w", walkErr)
	}

	return modChanged, nil
}

func updateTemplateGoModModule(content, pkg, newPath, version string) string {
	rePath := fmt.Sprintf(`github\.com/gofiber/template/%s(?:/v\d+)?`, regexp.QuoteMeta(pkg))

	reRequire := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*(?:require\s+)?)%s\s+%s`, rePath, goModVersionPattern))
	content = reRequire.ReplaceAllString(content, fmt.Sprintf(`${1}%s %s`, newPath, version))

	reReplace := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*replace\s+)%s(\s+%s)?(\s+=>\s+)`, rePath, goModVersionPattern))
	content = reReplace.ReplaceAllString(content, fmt.Sprintf(`${1}%s${2}${3}`, newPath))

	return content
}
