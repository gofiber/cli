package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/gomod"
)

// externalFiberV2Module is the module path of Fiber v2.
const externalFiberV2Module = "github.com/gofiber/fiber/v2"

// MigrateExternalModules reports the modules that the project builds from a
// directory outside of the one the command runs in, through a replace directive of
// a go.mod or a use directive of a go.work, and that still require Fiber v2. The
// command only rewrites the files below its directory, so such a module stays on
// Fiber v2, and the packages of the project that use it hand it the context and the
// app of v3. It reports and changes nothing else.
func MigrateExternalModules(_ *cobra.Command, cwd string, _, _ *semver.Version) error {
	root, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("failed to check the modules outside of the project: %w", err)
	}

	_, err = gomod.Edit(cwd, func(f *gomod.File) error {
		for _, rep := range f.Replace {
			if !modfile.IsDirectoryPath(rep.New.Path) {
				continue
			}
			noteExternalModule(cwd, root, f.Rel, gomod.Line(rep.Syntax), filepath.Dir(f.Path), rep.New.Path,
				"the replace directive for "+rep.Old.Path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to check the replace directives: %w", err)
	}

	works, err := gomod.WorkPaths(cwd)
	if err != nil {
		return fmt.Errorf("failed to check the workspaces: %w", err)
	}
	for _, path := range works {
		b, err := os.ReadFile(path) // #nosec G304 -- a workspace file of the migrated project
		if err != nil {
			continue
		}
		work, err := modfile.ParseWork(path, b, nil)
		if err != nil {
			continue // the migration of the workspace files says so
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			rel = path
		}
		for _, use := range work.Use {
			noteExternalModule(cwd, root, filepath.ToSlash(rel), gomod.Line(use.Syntax), filepath.Dir(path), use.Path, "the workspace")
		}
	}
	return nil
}

// noteExternalModule adds the note for the directive in file at line when it names a
// directory (relative to dir, or absolute) outside of root that holds a module that
// requires Fiber v2. what says what the directive is.
func noteExternalModule(cwd, root, file string, line int, dir, target, what string) {
	target = filepath.FromSlash(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	if rel, err := filepath.Rel(root, target); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return // below the directory of the run: its files are migrated with the others
	}

	b, err := os.ReadFile(filepath.Join(target, gomod.FileName)) // #nosec G304 -- the module a directive of the migrated project names
	if err != nil {
		return
	}
	mod, err := modfile.ParseLax(filepath.Join(target, gomod.FileName), b, nil)
	if err != nil {
		return
	}
	for _, req := range mod.Require {
		if req.Mod.Path != externalFiberV2Module {
			continue
		}
		internal.AddNote(cwd, internal.Note{
			File: file,
			Line: line,
			Message: fmt.Sprintf("%s uses %s, a module outside of the directory this command runs in that still requires %s %s. "+
				"The command only rewrites the files below its directory, so that module stays on Fiber v2 and the packages of this project "+
				"that use it get the context and the app of v3 where it expects those of v2: run fiber migrate in it as well.",
				what, target, externalFiberV2Module, req.Mod.Version),
		})
		return
	}
}
