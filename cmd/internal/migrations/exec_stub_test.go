package migrations_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations"
)

// stubFiberDownload replaces migrations.ExecCommand with a helper that
// returns the provided Fiber go.mod path in JSON format. It returns a
// function to restore the original ExecCommand.
func stubFiberDownload(t *testing.T, fiberGoMod string) func() {
	t.Helper()
	orig := migrations.ExecCommand
	out := fmt.Sprintf(`{"GoMod":%q}`, filepath.ToSlash(fiberGoMod))
	migrations.ExecCommand = func(string, ...string) *exec.Cmd {
		cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestHelperProcess", "--") // #nosec G204 -- test helper
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"GO_HELPER_STDOUT=" + out,
		}
		return cmd
	}
	return func() { migrations.ExecCommand = orig }
}

func TestHelperProcess(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if out := os.Getenv("GO_HELPER_STDOUT"); out != "" {
		_, _ = fmt.Fprint(os.Stdout, out)
	}
	os.Exit(0) // helper process exits intentionally
}
