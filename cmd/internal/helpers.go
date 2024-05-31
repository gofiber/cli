package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/containerd/console"
	"github.com/muesli/termenv"
)

var term = termenv.ColorProfile()

type finishedMsg struct{ error }

func checkConsole() (size console.WinSize, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()

	return console.Current().Size()
}

func errCmd(err error) tea.Cmd {
	return func() tea.Msg {
		return finishedMsg{err}
	}
}

type FileProcessor func(content string) string

func ChangeFileContent(cwd string, processorFn FileProcessor) error {
	// change go files in project
	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			//fmt.Printf("Error while traversing %s: %v\n", path, err)
			return err
		}

		// Skip directories named "vendor"
		if info.IsDir() && info.Name() == "vendor" {
			//fmt.Printf("Skipping directory: %s\n", path)
			return filepath.SkipDir
		}

		// Check if the file is a Go file (ending with ".go")
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		//fmt.Printf("Processing Go file: %s\n", path)
		fileContent, err := os.ReadFile(path)

		// update go.mod file
		if err2 := os.WriteFile(path, []byte(processorFn(string(fileContent))), 0644); err != nil {
			return err2
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("Error while traversing the directory tree: %v\n", err)
	}

	return nil
}
