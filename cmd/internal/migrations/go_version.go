package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal/gomod"
)

// importsFiberGoVersions lists the go directive that Fiber v3 releases declare in
// their go.mod, as the first release of each value. A release needs the Go
// version of the newest row it is at or after; a release newer than the table
// is assumed to need the last one (MigrateDependencies reads the real value
// from the release once it is downloaded).
var importsFiberGoVersions = []struct{ since, goVersion string }{
	{since: "3.0.0-0", goVersion: "1.21"},
	{since: "3.0.0-beta.4", goVersion: "1.23"},
	{since: "3.0.0-beta.5", goVersion: "1.24.0"},
	{since: "3.0.0-rc.1", goVersion: "1.25.0"},
	{since: "3.6.0", goVersion: "1.26.0"},
}

// importsWorkVersionMessage is printed when the go line of a workspace was raised.
const importsWorkVersionMessage = "Ensuring the go version of go.work is not behind its modules"

// importsReFiberCore matches the module path of the core fiber module at any major.
var importsReFiberCore = regexp.MustCompile(`^github\.com/gofiber/fiber(/v\d+)?$`)

// importsMinimumGoVersion returns the Go version that the Fiber release target needs,
// "" when the target is not a Fiber v3 release.
func importsMinimumGoVersion(target *semver.Version) string {
	minimum := ""
	for _, row := range importsFiberGoVersions {
		since, err := semver.NewVersion(row.since)
		if err != nil || target.Compare(since) < 0 {
			break
		}
		minimum = row.goVersion
	}
	return minimum
}

// MigrateGoVersion raises the go directive of every module that requires Fiber
// to the Go version the target release needs. A newer directive is kept. The go
// line of a workspace that uses such a module follows, the go command refuses a
// workspace that is behind one of its modules.
func MigrateGoVersion(cmd *cobra.Command, cwd string, _, target *semver.Version) error {
	minimum := importsMinimumGoVersion(target)
	if minimum == "" {
		return nil
	}

	changed, err := gomod.Edit(cwd, func(f *gomod.File) error {
		if importsRequiresFiber(f.File) {
			f.RaiseGo(minimum)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("raise go version: %w", err)
	}
	workChanged, err := gomod.SyncWork(cwd)
	if err != nil {
		return fmt.Errorf("raise go version of the workspace: %w", err)
	}
	if changed {
		cmd.Printf("Ensuring go version >= %s\n", minimum)
	}
	if workChanged {
		cmd.Println(importsWorkVersionMessage)
	}
	return nil
}

// importsRequiresFiber reports whether the module requires the core fiber module.
func importsRequiresFiber(f *modfile.File) bool {
	for _, r := range f.Require {
		if importsReFiberCore.MatchString(r.Mod.Path) {
			return true
		}
	}
	return false
}

// fiberModuleDirs returns directories under root containing a go.mod file that
// requires github.com/gofiber/fiber. vendor directories are skipped, and so is
// a go.mod that does not parse (the migration that edits it reports it).
func fiberModuleDirs(root string) ([]string, error) {
	paths, err := gomod.Paths(root)
	if err != nil {
		return nil, fmt.Errorf("find go.mod files: %w", err)
	}

	var dirs []string
	for _, path := range paths {
		b, err := os.ReadFile(path) // #nosec G304 -- reading module file
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		mf, err := modfile.ParseLax(path, b, nil)
		if err != nil {
			continue
		}
		if importsRequiresFiber(mf) {
			dirs = append(dirs, filepath.Dir(path))
		}
	}
	return dirs, nil
}
