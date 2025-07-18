package migrations

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
)

// MigrateGoVersion ensures that all go.mod files referencing Fiber declare at
// least the provided Go version. Vendor directories are skipped.
func MigrateGoVersion(minVersion string) func(*cobra.Command, string, *semver.Version, *semver.Version) error {
	minVer := semver.MustParse(minVersion)
	return func(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
		dirs, err := fiberModuleDirs(cwd)
		if err != nil {
			return fmt.Errorf("find modules: %w", err)
		}
		for _, dir := range dirs {
			modFile := filepath.Join(dir, "go.mod")
			b, err := os.ReadFile(modFile) // #nosec G304
			if err != nil {
				return fmt.Errorf("read %s: %w", modFile, err)
			}
			lines := strings.Split(string(b), "\n")
			changed := false
			for i, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "go ") {
					currVer, err := semver.NewVersion(strings.TrimSpace(strings.TrimPrefix(line, "go")))
					if err != nil {
						return fmt.Errorf("parse go version in %s: %w", modFile, err)
					}
					if currVer.LessThan(minVer) {
						lines[i] = "go " + minVer.String()
						changed = true
					}
					break
				}
			}
			if changed {
				if err := os.WriteFile(modFile, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
					return fmt.Errorf("write %s: %w", modFile, err)
				}
			}
		}
		cmd.Printf("Ensuring go version >= %s\n", minVer.String())
		return nil
	}
}

// fiberModuleDirs returns directories under root containing a go.mod file that
// requires github.com/gofiber/fiber. vendor directories are skipped.
func fiberModuleDirs(root string) ([]string, error) {
	var dirs []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "vendor" {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" {
			b, err := os.ReadFile(path) // #nosec G304 -- reading module file
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			mf, err := modfile.Parse(path, b, nil)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			for _, r := range mf.Require {
				if strings.HasPrefix(r.Mod.Path, "github.com/gofiber/fiber") {
					dirs = append(dirs, filepath.Dir(path))
					break
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", root, walkErr)
	}
	return dirs, nil
}
