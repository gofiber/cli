package internal

import (
	"errors"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/containerd/console"
	"github.com/muesli/termenv"
)

const vendorDir = "vendor"

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

// FileProcessorInfo is a FileProcessor that also receives information about the
// file being processed, so that a migration can attach notes to it.
type FileProcessorInfo func(content string, fi FileInfo) string

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
//
// A file whose rewritten content does not parse is left untouched, a note is
// recorded for it, and the walk continues with the next file.
func ChangeFileContent(cwd string, processorFn FileProcessor) (bool, error) {
	return ChangeFileContentInfo(cwd, func(content string, _ FileInfo) string {
		return processorFn(content)
	})
}

// ChangeFileContentInfo is like ChangeFileContent, but the processor also
// receives the FileInfo of the file it is rewriting.
func ChangeFileContentInfo(cwd string, processorFn FileProcessorInfo) (bool, error) {
	changed := false
	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories named "vendor"
		if info.IsDir() && info.Name() == vendorDir {
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

		fi := FileInfo{Root: cwd, Path: path, Rel: rel}
		original := string(fileContent)
		processed := processorFn(original, fi)
		if processed != original {
			fixed, err := tidyImports(path, []byte(processed))
			if err != nil {
				noteSkippedFile(fi, original, err)
				return nil
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

// noteSkippedFile records why the file was left untouched after its rewritten
// content failed to parse: either the file was already broken, or the rewrite
// broke it.
func noteSkippedFile(fi FileInfo, original string, rewriteErr error) {
	_, err := parser.ParseFile(token.NewFileSet(), fi.Path, original, parser.SkipObjectResolution)
	if err != nil {
		NoteParseError(fi, err)
		return
	}
	Notef(fi, 0, "left unchanged because the rewritten code does not parse (please report this): %v", rewriteErr)
}

// NoteParseError records that the file was skipped because it does not parse.
func NoteParseError(fi FileInfo, err error) {
	line, msg := 0, err.Error()
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		line, msg = list[0].Pos.Line, list[0].Msg
	}
	Notef(fi, line, "skipped because the file does not parse: %s", msg)
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
