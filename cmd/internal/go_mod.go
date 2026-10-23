package internal

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/mod/modfile"
)

// ExecCommand is used to run external commands. It can be replaced in tests.
var ExecCommand = exec.Command

// importsReFiberModule matches the module path of the core fiber module at any major.
var importsReFiberModule = regexp.MustCompile(`^github\.com/gofiber/fiber(/v\d+)?$`)

// RunGoMod executes `go mod tidy` and `go mod download` inside every directory
// under root that contains a go.mod file referencing github.com/gofiber/fiber,
// and `go mod vendor` where the module already has a vendor directory: a
// project that never vendored must not get a copy of its dependencies.
// Directories named `vendor` are skipped.
func RunGoMod(root string) error {
	dirs, err := fiberModuleDirs(root)
	if err != nil {
		return fmt.Errorf("find modules: %w", err)
	}
	for _, dir := range dirs {
		subs := []string{"tidy", "download"}
		if info, err := os.Stat(filepath.Join(dir, vendorDir)); err == nil && info.IsDir() {
			subs = append(subs, vendorDir)
		}
		for _, sub := range subs {
			cmd := ExecCommand("go", "mod", sub) // #nosec G204 -- commands are controlled
			setGoWorkOffEnv(cmd)
			cmd.Dir = dir
			if err := runCmd(cmd); err != nil {
				return fmt.Errorf("in %s: %w", dir, err)
			}
		}
	}
	return nil
}

func setGoWorkOffEnv(cmd *exec.Cmd) {
	env := append([]string{}, os.Environ()...)
	if len(cmd.Env) > 0 {
		env = append(env, cmd.Env...)
	}
	filtered := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, "GOWORK=") {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, "GOWORK=off")
	cmd.Env = filtered
}

// fiberModuleDirs returns directories under root containing a go.mod file that
// requires github.com/gofiber/fiber. vendor directories are skipped, and so is
// a go.mod that does not parse: no go command can run in it.
func fiberModuleDirs(root string) ([]string, error) {
	var dirs []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == vendorDir {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" {
			b, err := os.ReadFile(path) // #nosec G304 -- reading module file
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			mf, err := modfile.ParseLax(path, b, nil)
			if err != nil {
				return nil //nolint:nilerr // reported by the migration that edits go.mod files
			}
			for _, r := range mf.Require {
				if importsReFiberModule.MatchString(r.Mod.Path) {
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

// maxCommandOutput is how much of the output of a go command is kept to explain a failure.
const maxCommandOutput = 1 << 16

// tail keeps the last maxCommandOutput bytes written to it.
type tail struct{ data []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.data = append(t.data, p...)
	if len(t.data) > maxCommandOutput {
		t.data = t.data[len(t.data)-maxCommandOutput:]
	}
	return len(p), nil
}

// runCmd runs the command with its output on the terminal. When it fails, the
// error says what the output explains (see goFailureHint).
func runCmd(cmd *exec.Cmd) error {
	var out tail
	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	cmd.Stderr = io.MultiWriter(os.Stderr, &out)
	if err := cmd.Run(); err != nil {
		if hint := goFailureHint(string(out.data)); hint != "" {
			return fmt.Errorf("failed to run %s: %w (%s)", cmd.String(), err, hint)
		}
		return fmt.Errorf("failed to run %s: %w", cmd.String(), err)
	}
	return nil
}

// reOtelSkew matches the message of the go command for a package that the
// OpenTelemetry module of go.mod no longer has: the modules of the family were
// raised one by one, and one of them still wants a package an older release of
// another had.
var reOtelSkew = regexp.MustCompile(`does not contain package go\.opentelemetry\.io/otel`)

// goFailureHint explains the failure of a go command from its output, "" when
// the output says nothing the migration knows.
func goFailureHint(output string) string {
	if reOtelSkew.MatchString(output) {
		return "the OpenTelemetry modules of go.mod are on different releases, and they have to be on one: " +
			"raise them together, for example go get go.opentelemetry.io/otel@latest go.opentelemetry.io/otel/sdk@latest and the exporters you use at their latest release, then run go mod tidy"
	}
	return ""
}
