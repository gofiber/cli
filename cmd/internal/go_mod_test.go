package internal_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	os.Exit(0) // helper process exits intentionally
}

type goModRun struct {
	cmd  *exec.Cmd
	args []string
}

// stubGoMod replaces internal.ExecCommand with a command that does nothing and
// records what it was asked to run.
func stubGoMod(t *testing.T) *[]goModRun {
	t.Helper()

	var runs []goModRun
	orig := internal.ExecCommand
	t.Cleanup(func() { internal.ExecCommand = orig })
	internal.ExecCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestRunGoModHelperProcess$") // #nosec G204 -- test helper
		cmd.Env = []string{"GO_WANT_RUN_GO_MOD_HELPER=1"}
		runs = append(runs, goModRun{cmd: cmd, args: append([]string{name}, args...)})
		return cmd
	}
	return &runs
}

func Test_RunGoMod_VendorsOnlyWhereAVendorDirectoryExists(t *testing.T) {
	// Not parallel: it replaces a package-level variable.
	runs := stubGoMod(t)

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
