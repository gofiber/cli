package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/console"
	"github.com/muesli/termenv"
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

type FileProcessor func(content string) string

func ChangeFileContent(cwd string, processorFn FileProcessor) error {
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
		fileContent, err := os.ReadFile(path) // #nosec G304

		// update go.mod file
		if err2 := os.WriteFile(path, []byte(processorFn(string(fileContent))), 0o600); err2 != nil {
			return fmt.Errorf("write file %s: %w", path, err2)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error while traversing the directory tree: %w", err)
	}

	return nil
}
