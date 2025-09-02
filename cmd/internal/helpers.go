package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/containerd/console"
	"github.com/muesli/termenv"
	"golang.org/x/tools/imports"
)

var term = termenv.ColorProfile()

type finishedError struct{ error }

func checkConsole() (size console.WinSize, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()

	size, err = console.Current().Size()
	if err != nil {
		return size, fmt.Errorf("get console size: %w", err)
	}
	return size, nil
}

// FileProcessor processes the file content and returns the modified content.
type FileProcessor func(content string) string

var (
	fileIncludePatterns []string
	fileExcludePatterns []string
	fileFilterMu        sync.RWMutex
)

// SetFileFilters sets the include and exclude patterns used by ChangeFileContent.
func SetFileFilters(include, exclude []string) {
	fileFilterMu.Lock()
	fileIncludePatterns = include
	fileExcludePatterns = exclude
	fileFilterMu.Unlock()
}

// ChangeFileContent walks through cwd and applies the processorFn to every Go
// file found. Files in a vendor directory are skipped. It returns true if any
// file content was modified.
func ChangeFileContent(cwd string, processorFn FileProcessor) (bool, error) {
	changed := false
	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories named "vendor"
		if info.IsDir() && info.Name() == "vendor" {
			return filepath.SkipDir
		}

		// Check if the file is a Go file (ending with ".go")
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}

		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			rel = path
		}
		fileFilterMu.RLock()
		include := fileIncludePatterns
		exclude := fileExcludePatterns
		fileFilterMu.RUnlock()
		if len(include) > 0 && !matchPatternList(rel, include) {
			return nil
		}
		if len(exclude) > 0 && matchPatternList(rel, exclude) {
			return nil
		}
		fileContent, err := os.ReadFile(path) // #nosec G304
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		processed := processorFn(string(fileContent))
		if processed != string(fileContent) {
			fixed, err := imports.Process(path, []byte(processed), nil)
			if err != nil {
				return fmt.Errorf("fix imports for %s: %w", path, err)
			}
			processed = string(fixed)
			if err2 := os.WriteFile(path, []byte(processed), 0o600); err2 != nil {
				return fmt.Errorf("write file %s: %w", path, err2)
			}
			changed = true
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("error while traversing the directory tree: %w", err)
	}

	return changed, nil
}

func matchPatternList(name string, patterns []string) bool {
	for _, p := range patterns {
		if matchPattern(name, p) {
			return true
		}
	}
	return false
}

func matchPattern(name, pattern string) bool {
	if ok, err := filepath.Match(pattern, name); err == nil {
		if ok {
			return true
		}
		if !isRegexPattern(pattern) {
			return false
		}
	}
	if re, err := regexp.Compile(pattern); err == nil {
		return re.MatchString(name)
	}
	return name == pattern
}

func isRegexPattern(p string) bool {
	return strings.ContainsAny(p, "^$[]()|+?\\")
}
