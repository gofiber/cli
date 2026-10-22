package v3

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// mw2Modules answers questions about the modules of a project that need every
// file of the module to be read, such as whether any of them imports a package.
type mw2Modules struct {
	// scanned holds the import paths found, per module root, once the module was read.
	scanned map[string]map[string]bool
	root    string
}

func newMW2Modules(root string) *mw2Modules {
	return &mw2Modules{root: filepath.Clean(root), scanned: map[string]map[string]bool{}}
}

// moduleOf returns the root of the module that holds the file.
func (m *mw2Modules) moduleOf(file string) string {
	return mw2ModuleRoot(m.root, filepath.Dir(filepath.Clean(file)))
}

// imports reports whether any file of the module that holds file imports a
// package whose path matches pathRe.
func (m *mw2Modules) imports(file string, pathRe *regexp.Regexp) bool {
	for path := range m.importsOf(m.moduleOf(file)) {
		if pathRe.MatchString(path) {
			return true
		}
	}
	return false
}

// importsOf returns the import paths of every file of the module rooted at dir.
func (m *mw2Modules) importsOf(dir string) map[string]bool {
	if paths, ok := m.scanned[dir]; ok {
		return paths
	}

	paths := map[string]bool{}
	m.scanned[dir] = paths
	// A directory that cannot be read has no imports to report.
	if err := mw2Walk(dir, func(path string) {
		if m.moduleOf(path) != dir {
			return
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil && file == nil {
			return
		}
		for _, spec := range file.Imports {
			if p, err := strconv.Unquote(spec.Path.Value); err == nil {
				paths[p] = true
			}
		}
	}); err != nil {
		return paths
	}
	return paths
}

// mw2Walk calls fn for every Go file below root. Like the migrations, it does
// not enter vendor directories.
func mw2Walk(root string, fn func(path string)) error {
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is skipped
		}
		if info.IsDir() {
			if info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".go") {
			fn(path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", root, err)
	}
	return nil
}
