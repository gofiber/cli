package migrations

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/gomod"
)

// importsGofiberModules is the path prefix of the modules of the Fiber family.
const importsGofiberModules = "github.com/gofiber/"

// MigrateReplaceDirectives reports the replace directives for Fiber modules that
// no longer have a requirement to apply to. The migrations move modules to new
// paths (contrib, storage and template modules got a /v3 path), and a replace
// directive names a module by its path, so one that pointed at a local copy of
// the old module does not apply to the new one and the project silently builds
// from the published release again. The directive is not rewritten: only the
// migration that moved the module knows where it went. The replace directive of
// the core fiber module is reported by MigrateGoPkgs.
func MigrateReplaceDirectives(_ *cobra.Command, cwd string, _, _ *semver.Version) error {
	_, err := gomod.Edit(cwd, func(f *gomod.File) error {
		required := make(map[string]bool, len(f.Require))
		for _, r := range f.Require {
			required[r.Mod.Path] = true
		}

		for _, rep := range f.Replacements(importsIsFamilyModule) {
			if required[rep.Old.Path] || reFiberModulePath.MatchString(rep.Old.Path) {
				continue
			}
			internal.AddNote(cwd, internal.Note{
				File: f.Rel,
				Line: gomod.Line(rep.Syntax),
				Message: fmt.Sprintf("the replace directive for %s applies to no requirement of this go.mod, the module may have moved to another path in Fiber v3; "+
					"add a replace directive for the new path if you still want to use %s.", rep.Old.Path, importsReplacement(rep.New.Path, rep.New.Version)),
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to check replace directives: %w", err)
	}
	return nil
}

// importsIsFamilyModule reports whether path is a module of the Fiber family.
func importsIsFamilyModule(path string) bool {
	return strings.HasPrefix(path, importsGofiberModules)
}

// importsReplacement spells what a replace directive points at.
func importsReplacement(path, version string) string {
	if version == "" {
		return path
	}
	return path + " " + version
}
