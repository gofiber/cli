package internal_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// TestRunGoModHelperProcess stands in for the go command in the tests of
// RunGoMod.
func TestRunGoModHelperProcess(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_WANT_RUN_GO_MOD_HELPER") != "1" {
		return
	}
	if out := os.Getenv("GO_RUN_GO_MOD_STDERR"); out != "" {
		_, _ = fmt.Fprintln(os.Stderr, out)
		os.Exit(1) // helper process exits intentionally
	}
	os.Exit(0) // helper process exits intentionally
}

type importsGoModRun struct {
	cmd  *exec.Cmd
	args []string
}

// importsStubGoMod replaces internal.ExecCommand with a command that does nothing and
// records what it was asked to run.
func importsStubGoMod(t *testing.T) *[]importsGoModRun {
	t.Helper()

	var runs []importsGoModRun
	orig := internal.ExecCommand
	t.Cleanup(func() { internal.ExecCommand = orig })
	internal.ExecCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestRunGoModHelperProcess$") // #nosec G204 -- test helper
		cmd.Env = []string{"GO_WANT_RUN_GO_MOD_HELPER=1"}
		runs = append(runs, importsGoModRun{cmd: cmd, args: append([]string{name}, args...)})
		return cmd
	}
	return &runs
}

func Test_RunGoMod_VendorsOnlyWhereAVendorDirectoryExists(t *testing.T) {
	// Not parallel: it replaces a package-level variable.
	runs := importsStubGoMod(t)

	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	write("go.mod", "module a\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n")
	write("vendored/go.mod", "module a/vendored\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n")
	write("vendored/vendor/modules.txt", "# github.com/gofiber/fiber/v3 v3.6.1\n")
	write("vendored/vendor/go.mod", "module vendor\n")
	write("other/go.mod", "module a/other\n\nrequire github.com/google/uuid v1.6.0\n")
	write("testdata/bad/go.mod", "module {{ .Name }}\n")

	require.NoError(t, internal.RunGoMod(dir))

	var got []string
	for _, run := range *runs {
		rel, err := filepath.Rel(dir, run.cmd.Dir)
		require.NoError(t, err)
		got = append(got, rel+": "+run.args[2])
		assert.Contains(t, run.cmd.Env, "GOWORK=off")
	}
	assert.Equal(t, []string{
		".: tidy",
		".: download",
		"vendored: tidy",
		"vendored: download",
		"vendored: vendor",
	}, got, "a project that never vendored must not get a vendor directory")
	assert.NoDirExists(t, filepath.Join(dir, "vendor"))
}

// importsStubFailingGoMod replaces internal.ExecCommand with a command that prints stderr and fails.
func importsStubFailingGoMod(t *testing.T, stderr string) {
	t.Helper()

	orig := internal.ExecCommand
	t.Cleanup(func() { internal.ExecCommand = orig })
	internal.ExecCommand = func(string, ...string) *exec.Cmd {
		cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestRunGoModHelperProcess$") // #nosec G204 -- test helper
		cmd.Env = []string{"GO_WANT_RUN_GO_MOD_HELPER=1", "GO_RUN_GO_MOD_STDERR=" + stderr}
		return cmd
	}
}

func Test_RunGoMod_NamesTheCauseOfAnOpenTelemetrySkew(t *testing.T) {
	// Not parallel: it replaces a package-level variable.
	importsStubFailingGoMod(t, "go: example.com/app/internal/server imports\n\tgo.opentelemetry.io/otel/exporters/otlp/otlptrace imports\n\tgo.opentelemetry.io/otel/exporters/otlp/internal: "+
		"module go.opentelemetry.io/otel/exporters/otlp@latest found (v0.20.1), but does not contain package go.opentelemetry.io/otel/exporters/otlp/internal")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module a\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n"), 0o600))

	err := internal.RunGoMod(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to run")
	assert.Contains(t, err.Error(), "exit status 1")
	assert.Contains(t, err.Error(), "the OpenTelemetry modules of go.mod are on different releases, and they have to be on one")
}

func Test_RunGoMod_OtherFailuresKeepTheirMessage(t *testing.T) {
	// Not parallel: it replaces a package-level variable.
	importsStubFailingGoMod(t, "go: some other failure")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module a\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n"), 0o600))

	err := internal.RunGoMod(dir)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "OpenTelemetry")
	assert.True(t, strings.HasSuffix(err.Error(), "exit status 1"), err.Error())
}
