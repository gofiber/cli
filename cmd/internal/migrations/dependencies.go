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
)

// ExecCommand is used to run external commands. It can be replaced in tests.
var ExecCommand = exec.Command

// MigrateDependencies ensures that dependencies shared with Fiber are at least
// the versions required by the target Fiber release.
//
// It updates go.mod files that already require a dependency also required by
// Fiber, bumping the version when it is lower than Fiber's requirement. No
// changes are made if the existing version is equal or higher.
func MigrateDependencies(cmd *cobra.Command, cwd string, _, target *semver.Version) error {
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
	mf, err := modfile.Parse(info.GoMod, b, nil)
	if err != nil {
		return fmt.Errorf("parse fiber go.mod: %w", err)
	}

	deps := make(map[string]*semver.Version, len(mf.Require))
	for _, r := range mf.Require {
		v, err := semver.NewVersion(strings.TrimPrefix(r.Mod.Version, "v"))
		if err != nil {
			return fmt.Errorf("parse fiber dependency %s version %s: %w", r.Mod.Path, r.Mod.Version, err)
		}
		deps[r.Mod.Path] = v
	}

	dirs, err := fiberModuleDirs(cwd)
	if err != nil {
		return fmt.Errorf("find modules: %w", err)
	}

	anyChanged := false
	for _, dir := range dirs {
		modFile := filepath.Join(dir, "go.mod")
		b, err := os.ReadFile(modFile) // #nosec G304
		if err != nil {
			return fmt.Errorf("read %s: %w", modFile, err)
		}
		mf, err := modfile.Parse(modFile, b, nil)
		if err != nil {
			return fmt.Errorf("parse %s: %w", modFile, err)
		}

		changed := false
		for _, r := range mf.Require {
			targetVer, ok := deps[r.Mod.Path]
			if !ok {
				continue
			}
			currVer, err := semver.NewVersion(strings.TrimPrefix(r.Mod.Version, "v"))
			if err != nil {
				return fmt.Errorf("parse %s version in %s: %w", r.Mod.Path, modFile, err)
			}
			if currVer.LessThan(targetVer) {
				r.Mod.Version = "v" + targetVer.String()
				changed = true
			}
		}

		if changed {
			mf.SetRequire(mf.Require)
			formatted, err := mf.Format()
			if err != nil {
				return fmt.Errorf("format %s: %w", modFile, err)
			}
			if err := os.WriteFile(modFile, formatted, 0o600); err != nil {
				return fmt.Errorf("write %s: %w", modFile, err)
			}
			anyChanged = true
		}
	}

	if anyChanged {
		cmd.Println("Updating dependency versions")
	}
	return nil
}
