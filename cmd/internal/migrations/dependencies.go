package migrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal/gomod"
)

// ExecCommand is used to run external commands. It can be replaced in tests.
var ExecCommand = exec.Command

// MigrateDependencies ensures that dependencies shared with Fiber are at least
// the versions required by the target Fiber release, and preserves higher
// versions already declared by the project. The go directive is raised to the
// one the release declares; MigrateGoVersion has set it from a table by then, so
// this only changes it for a release the table does not know.
//
// The current map contains the dependency versions present before any
// migrations ran, keyed by module directory.
func MigrateDependencies(cmd *cobra.Command, cwd string, current map[string]map[string]*semver.Version, target *semver.Version) error {
	fiberModule := fmt.Sprintf("github.com/gofiber/fiber/v%d@v%s", target.Major(), target.String())

	c := ExecCommand("go", "mod", "download", "-json", fiberModule)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("download fiber module: %s: %w", msg, err)
		}
		return fmt.Errorf("download fiber module: %w", err)
	}
	var info struct {
		GoMod string `json:"GoMod"` //nolint:tagliatelle // field name defined by go tool output
	}
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return fmt.Errorf("parse download info: %w", err)
	}

	b, err := os.ReadFile(info.GoMod) // #nosec G304
	if err != nil {
		return fmt.Errorf("read fiber go.mod: %w", err)
	}
	fiberMod, err := modfile.Parse(info.GoMod, b, nil)
	if err != nil {
		return fmt.Errorf("parse fiber go.mod: %w", err)
	}
	fiberGo := ""
	if fiberMod.Go != nil {
		fiberGo = fiberMod.Go.Version
	}

	deps := make(map[string]*semver.Version, len(fiberMod.Require))
	for _, r := range fiberMod.Require {
		v, err := semver.NewVersion(strings.TrimPrefix(r.Mod.Version, "v"))
		if err != nil {
			return fmt.Errorf("parse fiber dependency %s version %s: %w", r.Mod.Path, r.Mod.Version, err)
		}
		deps[r.Mod.Path] = v
	}

	depsChanged, goRaised := false, false
	_, err = gomod.Edit(cwd, func(f *gomod.File) error {
		if !importsRequiresFiber(f.File) {
			return nil
		}

		orig := current[filepath.Dir(f.Path)]
		for _, r := range f.Requirements(func(string) bool { return true }) {
			targetVer, ok := deps[r.Mod.Path]
			if !ok {
				continue
			}
			maxVer := targetVer
			if v, ok := orig[r.Mod.Path]; ok && v.GreaterThan(maxVer) {
				maxVer = v
			}
			currVer, err := semver.NewVersion(strings.TrimPrefix(r.Mod.Version, "v"))
			if err != nil {
				return fmt.Errorf("parse %s version in %s: %w", r.Mod.Path, f.Path, err)
			}
			if currVer.LessThan(maxVer) {
				f.SetRequire(r, r.Mod.Path, "v"+maxVer.String())
				depsChanged = true
			}
		}

		if fiberGo != "" && f.RaiseGo(fiberGo) {
			goRaised = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update go.mod files: %w", err)
	}
	workChanged, err := gomod.SyncWork(cwd)
	if err != nil {
		return fmt.Errorf("update go.work files: %w", err)
	}

	if depsChanged {
		cmd.Println("Updating dependency versions")
	}
	if goRaised {
		cmd.Printf("Ensuring go version >= %s\n", fiberGo)
	}
	if workChanged {
		cmd.Println(importsWorkVersionMessage)
	}
	return nil
}

func dependencyVersions(root string) (map[string]map[string]*semver.Version, error) {
	dirs, err := fiberModuleDirs(root)
	if err != nil {
		return nil, fmt.Errorf("find modules: %w", err)
	}
	deps := make(map[string]map[string]*semver.Version, len(dirs))
	for _, dir := range dirs {
		modFile := filepath.Join(dir, "go.mod")
		b, err := os.ReadFile(modFile) // #nosec G304
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", modFile, err)
		}
		mf, err := modfile.ParseLax(modFile, b, nil)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", modFile, err)
		}
		m := make(map[string]*semver.Version, len(mf.Require))
		for _, r := range mf.Require {
			v, err := semver.NewVersion(strings.TrimPrefix(r.Mod.Version, "v"))
			if err != nil {
				return nil, fmt.Errorf("parse %s version in %s: %w", r.Mod.Path, modFile, err)
			}
			m[r.Mod.Path] = v
		}
		deps[dir] = m
	}
	return deps, nil
}
