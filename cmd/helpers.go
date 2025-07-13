package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"time"
)

var (
	homeDir string

	execLookPath = exec.LookPath
	execCommand  = exec.Command
	osExit       = os.Exit

	skipSpinner bool
)

func init() {
	if dir, err := os.UserHomeDir(); err == nil {
		homeDir = dir
	}
}

func runCmd(cmd *exec.Cmd) (err error) {
	var (
		stderr io.ReadCloser
		stdout io.ReadCloser
	)

	if stderr, err = cmd.StderrPipe(); err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	go func() {
		if _, cErr := io.Copy(os.Stderr, stderr); cErr != nil {
			fmt.Fprintf(os.Stderr, "copy stderr: %v", cErr)
		}
	}()

	if stdout, err = cmd.StdoutPipe(); err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	go func() {
		if _, cErr := io.Copy(os.Stdout, stdout); cErr != nil {
			fmt.Fprintf(os.Stderr, "copy stdout: %v", cErr)
		}
	}()

	if err = cmd.Run(); err != nil {
		err = fmt.Errorf("failed to run %s: %w", cmd.String(), err)
	}

	return err
}

// replaces matching file patterns in a path, including subdirectories
func replace(pathname, pattern, old, replacement string) error {
	walkErr := filepath.Walk(pathname, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		return replaceWalkFn(p, info, pattern, []byte(old), []byte(replacement))
	})
	if walkErr != nil {
		return fmt.Errorf("walk %s: %w", pathname, walkErr)
	}
	return nil
}

func replaceWalkFn(pathname string, info os.FileInfo, pattern string, old, replacement []byte) (err error) {
	var matched bool
	if matched, err = filepath.Match(pattern, info.Name()); err != nil {
		return fmt.Errorf("match pattern %s: %w", pattern, err)
	}

	if matched {
		cleanedPath := filepath.Clean(pathname)

		oldContent, readErr := os.ReadFile(cleanedPath)
		if readErr != nil {
			return fmt.Errorf("read file %s: %w", cleanedPath, readErr)
		}

		if err := os.WriteFile(cleanedPath, bytes.ReplaceAll(oldContent, old, replacement), 0); err != nil {
			return fmt.Errorf("write file %s: %w", cleanedPath, err)
		}
	}

	return nil
}

func createFile(filePath, content string) error {
	f, err := os.Create(filepath.Clean(filePath))
	if err != nil {
		return fmt.Errorf("create %s: %w", filePath, err)
	}

	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "close file: %v", cerr)
		}
	}()

	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("write %s: %w", filePath, err)
	}

	return nil
}

func formatLatency(d time.Duration) time.Duration {
	switch {
	case d > time.Second:
		return d.Truncate(time.Second / 100)
	case d > time.Millisecond:
		return d.Truncate(time.Millisecond / 100)
	case d > time.Microsecond:
		return d.Truncate(time.Microsecond / 100)
	default:
		return d
	}
}

func loadConfig() (err error) {
	configFilePath := configFilePath()

	if fileExist(configFilePath) {
		if err := loadJSON(configFilePath, &rc); err != nil {
			return err
		}
	}

	return nil
}

func storeConfig() error {
	return storeJSON(configFilePath(), rc)
}

func configFilePath() string {
	if homeDir == "" {
		return configName
	}

	return fmt.Sprintf("%s%c%s", homeDir, os.PathSeparator, configName)
}

var fileExist = func(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}

func storeJSON(filename string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	if err := os.WriteFile(filename, b, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}

	return nil
}

func loadJSON(filename string, v any) error {
	b, err := os.ReadFile(path.Clean(filename))
	if err != nil {
		return fmt.Errorf("read file %s: %w", filename, err)
	}

	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("unmarshal %s: %w", filename, err)
	}
	return nil
}
