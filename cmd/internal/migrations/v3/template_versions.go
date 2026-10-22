package v3

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/gomod"
)

type templateTarget struct {
	modulePath string
	version    string
}

// templateTargets maps template packages to their minimum module path and version
// required for Fiber v3 core compatibility. Source:
// https://github.com/gofiber/template/pull/437
//
//nolint:goconst // each template's minimum version moves on its own
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

var importsTemplateFamily = newImportsFamily("github.com/gofiber/template/", importsResolveTemplate)

// importsResolveTemplate says where the engine name on the major version line moves
// to. An engine that is already on a newer line than the one Fiber v3 started
// with is left alone: the table only knows where to start.
func importsResolveTemplate(name, major string) importsDestination {
	target, ok := templateTargets[name]
	if !ok {
		return importsDestination{}
	}

	elems := strings.Split(target.modulePath, "/")
	if importsMajorRank(major) > importsMajorRank(elems[len(elems)-1]) {
		return importsDestination{known: true}
	}
	return importsDestination{path: target.modulePath, minimum: target.version, known: true}
}

// MigrateTemplateVersions updates template package imports and go.mod entries
// to the minimum versions required for Fiber v3 core compatibility. A version
// that is already higher is kept: the table only raises.
func MigrateTemplateVersions(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changedImports, err := internal.ChangeFileContentInfo(cwd, importsMigrateTemplate)
	if err != nil {
		return fmt.Errorf("failed to migrate template imports: %w", err)
	}

	modChanged, err := gomod.Edit(cwd, func(f *gomod.File) error {
		importsTemplateFamily.retarget(f, nil)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to migrate template modules: %w", err)
	}

	if !changedImports && !modChanged {
		return nil
	}

	cmd.Println("Migrated template package versions")
	return nil
}

func importsMigrateTemplate(content string, fi internal.FileInfo) string {
	if !strings.Contains(content, importsTemplateFamily.prefix) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	importsTemplateFamily.moveImports(g, &edits)
	return edits.apply(content)
}
