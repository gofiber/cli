package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// runGoMod executes `go mod tidy`, `go mod download` and `go mod vendor`
// inside every directory under root that contains a go.mod file referencing
// github.com/gofiber/fiber. Directories named `vendor` are skipped.
func runGoMod(root string) error {
	dirs, err := fiberModuleDirs(root)
	if err != nil {
		return fmt.Errorf("find modules: %w", err)
	}
	commands := [][]string{
		{"go", "mod", "tidy"},
		{"go", "mod", "download"},
		{"go", "mod", "vendor"},
	}
	for _, dir := range dirs {
		for _, args := range commands {
			cmd := execCommand(args[0], args[1:]...) // #nosec G204 -- commands are controlled
			cmd.Dir = dir
			if err := runCmd(cmd); err != nil {
				return fmt.Errorf("in %s: %w", dir, err)
			}
		}
	}
	return nil
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
