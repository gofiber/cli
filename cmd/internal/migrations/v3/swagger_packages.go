package v3

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

const (
	contribSwaggerOld   = "github.com/gofiber/contrib/swagger"
	contribSwaggerNew   = "github.com/gofiber/contrib/v3/swaggerui"
	fiberSwaggerOld     = "github.com/gofiber/swagger"
	fiberSwaggerNew     = "github.com/gofiber/contrib/v3/swaggo"
	goModVersionPattern = `v[a-zA-Z0-9.+-]+`
)

func MigrateSwaggerPackages(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changedImports, err := internal.ChangeFileContent(cwd, func(content string) string {
		updated, changed := rewriteSwaggerImports(content)
		if changed {
			return updated
		}
		return content
	})
	if err != nil {
		return fmt.Errorf("failed to migrate swagger imports: %w", err)
	}

	modChanged, err := migrateSwaggerModules(cwd)
	if err != nil {
		return err
	}

	if !changedImports && !modChanged {
		return nil
	}

	cmd.Println("Migrating swagger packages")
	return nil
}

func migrateSwaggerModules(cwd string) (bool, error) {
	modChanged := false
	var swaggoVersion, swaggerUIVersion string

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

		b, err := os.ReadFile(path) // #nosec G304 -- reading module files
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		content := string(b)

		needsSwaggerUI := strings.Contains(content, contribSwaggerOld) || strings.Contains(content, contribSwaggerNew)
		needsSwaggo := strings.Contains(content, fiberSwaggerOld) || strings.Contains(content, fiberSwaggerNew)
		if !needsSwaggo && !needsSwaggerUI {
			return nil
		}

		if needsSwaggerUI && swaggerUIVersion == "" {
			swaggerUIVersion, err = contribV3Version("swaggerui")
			if err != nil {
				return fmt.Errorf("fetch swaggerui version: %w", err)
			}
		}
		if needsSwaggo && swaggoVersion == "" {
			swaggoVersion, err = contribV3Version("swaggo")
			if err != nil {
				return fmt.Errorf("fetch swaggo version: %w", err)
			}
		}

		updated := content
		if needsSwaggerUI {
			updated = updateGoModModule(updated, contribSwaggerOld, contribSwaggerNew, swaggerUIVersion)
		}
		if needsSwaggo {
			updated = updateGoModModule(updated, fiberSwaggerOld, fiberSwaggerNew, swaggoVersion)
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
		return false, fmt.Errorf("failed to migrate swagger go.mod entries: %w", walkErr)
	}

	return modChanged, nil
}

func rewriteSwaggerImports(content string) (string, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil {
		return content, false
	}

	changed := false
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, "\"`")
		var (
			newPath     string
			wasMigrated bool
		)

		switch path {
		case contribSwaggerOld:
			newPath = contribSwaggerNew
			wasMigrated = true
		case fiberSwaggerOld:
			newPath = fiberSwaggerNew
			wasMigrated = true
		case contribSwaggerNew, fiberSwaggerNew:
			newPath = path
		default:
			continue
		}

		if path != newPath {
			imp.Path.Value = fmt.Sprintf("%q", newPath)
			changed = true
		}

		if wasMigrated && (imp.Name == nil || imp.Name.Name == "") {
			imp.Name = ast.NewIdent("swagger")
			changed = true
		}
	}

	if !changed {
		return content, false
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return content, false
	}

	return buf.String(), true
}

func updateGoModModule(content, oldPath, newPath, version string) string {
	if version == "" {
		return content
	}

	reRequireOld := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*(?:require\s+)?)%s\s+%s`, regexp.QuoteMeta(oldPath), goModVersionPattern))
	content = reRequireOld.ReplaceAllString(content, fmt.Sprintf(`${1}%s %s`, newPath, version))

	reRequireNew := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*(?:require\s+)?)%s\s+%s`, regexp.QuoteMeta(newPath), goModVersionPattern))
	content = reRequireNew.ReplaceAllString(content, fmt.Sprintf(`${1}%s %s`, newPath, version))

	reReplaceOld := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*replace\s+)%s(\s+%s)?(\s+=>\s+)`, regexp.QuoteMeta(oldPath), goModVersionPattern))
	content = reReplaceOld.ReplaceAllString(content, fmt.Sprintf(`${1}%s${2}${3}`, newPath))

	reReplaceNew := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*replace\s+)%s(\s+%s)?(\s+=>\s+)`, regexp.QuoteMeta(newPath), goModVersionPattern))
	content = reReplaceNew.ReplaceAllString(content, fmt.Sprintf(`${1}%s${2}${3}`, newPath))

	return content
}
