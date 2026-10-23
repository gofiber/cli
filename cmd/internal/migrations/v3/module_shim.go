package v3

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal"
)

// A migration that needs code Fiber v3 does not have adds it to the module, in a
// package of its own, <module>/internal/fibermigrate. Every package of the
// module then shares the same types (an agent that one package makes and another
// one uses, an app that one package creates and another one starts), the code is
// declared once, and the project removes it with one directory when it no longer
// needs it. Each migration adds its own files to the package; a file carries a
// sentence that tells a later run that it was added by one, and the package is
// left alone when it holds a file the migrations did not write.
const (
	// moduleShimPkg is the name of the package.
	moduleShimPkg = "fibermigrate"
	// moduleShimDir is where the package lives, relative to the root of the module.
	moduleShimDir = "internal/" + moduleShimPkg
	// moduleShimAlias is the name of the second import of the package, for a file in
	// which fibermigrate means something else.
	moduleShimAlias = "fibermigrateshim"
	// moduleShimMarker is in every file of the package.
	moduleShimMarker = `This file was added by "fiber migrate"`
)

// moduleShimFile is one file of the package.
type moduleShimFile struct {
	// name is the name of the file in the directory of the package.
	name string
	// marker is a sentence of the source that tells that an earlier run added the
	// file, so that a later run leaves it alone. It contains moduleShimMarker.
	marker string
	// source is the complete text of the file. Its package clause names
	// moduleShimPkg.
	source string
}

// goModule is a module of the project: the directory of its go.mod file and its
// path.
type goModule struct {
	root string
	path string
}

// contains reports whether the import path is the module or a package below it. A module
// that is not known contains nothing.
func (m goModule) contains(importPath string) bool {
	return m.path != "" && (importPath == m.path || strings.HasPrefix(importPath, m.path+"/"))
}

// moduleOf finds the module that the file belongs to: the nearest go.mod in its
// directory or above it, not above top (the directory the migration runs in).
func moduleOf(file, top string) (goModule, bool) {
	top, err := filepath.Abs(top)
	if err != nil {
		return goModule{}, false
	}
	dir, err := filepath.Abs(filepath.Dir(file))
	if err != nil || !isWithin(top, dir) {
		return goModule{}, false
	}

	for {
		if data, readErr := os.ReadFile(filepath.Join(dir, "go.mod")); readErr == nil { // #nosec G304 -- the go.mod of the project being migrated
			if modPath := modfile.ModulePath(data); modPath != "" {
				return goModule{root: dir, path: modPath}, true
			}
			return goModule{}, false
		}
		parent := filepath.Dir(dir)
		if dir == top || parent == dir {
			return goModule{}, false
		}
		dir = parent
	}
}

// isWithin reports whether dir is top or below it.
func isWithin(top, dir string) bool {
	rel, err := filepath.Rel(top, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// moduleShimPath is the import path of the package of the module that the file
// belongs to. It adds nothing.
func moduleShimPath(fi internal.FileInfo) (string, bool) {
	mod, ok := moduleOf(fi.Path, fi.Root)
	if !ok {
		return "", false
	}
	return path.Join(mod.path, moduleShimDir), true
}

// ensureModuleShim makes sure the package of the module that the file belongs to
// has the file f, adding it on first use, and returns the import path of the
// package. It reports false when the module cannot have it: the file is outside
// of a module, the directory of the package holds a Go file that the migrations
// did not write, or the package has a file of that name that they did not write.
func ensureModuleShim(fi internal.FileInfo, f moduleShimFile) (string, bool) {
	mod, ok := moduleOf(fi.Path, fi.Root)
	if !ok {
		return "", false
	}
	dir := filepath.Join(mod.root, filepath.FromSlash(moduleShimDir))
	importPath := path.Join(mod.path, moduleShimDir)

	if !moduleShimDirIsOurs(dir) {
		return "", false
	}

	file := filepath.Join(dir, f.name)
	data, err := os.ReadFile(file) // #nosec G304 -- next to the files of the project being migrated
	switch {
	case err == nil && bytes.Contains(data, []byte(f.marker)):
		return importPath, true // added by an earlier run
	case err == nil:
		return "", false
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", false
	}
	if err := os.WriteFile(file, []byte(f.source), 0o644); err != nil { //nolint:gosec // a source file of the project, not a secret
		return "", false
	}
	return importPath, true
}

// moduleShimDirIsOurs reports whether every Go file of the directory was added
// by a migration. A directory that does not exist yet is.
func moduleShimDirIsOurs(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return os.IsNotExist(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name())) // #nosec G304 -- a file of the project being migrated
		if err != nil || !bytes.Contains(data, []byte(moduleShimMarker)) {
			return false
		}
	}
	return true
}
