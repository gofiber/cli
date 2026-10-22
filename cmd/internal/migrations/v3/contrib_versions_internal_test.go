package v3

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// stubGoList replaces the command the lookup runs with this test binary, which
// plays the go command: TestContribGoListProcess prints the output given in the
// environment and refuses to run anywhere a real lookup would be disturbed. It
// returns the arguments the lookup asked for.
func stubGoList(t *testing.T, stdout, stderr string, exitCode int) *[]string {
	t.Helper()

	var asked []string
	orig := internal.ExecCommand
	internal.ExecCommand = func(name string, args ...string) *exec.Cmd {
		asked = append([]string{name}, args...)
		return exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestContribGoListProcess", "--") // #nosec G204 -- test helper
	}
	t.Cleanup(func() { internal.ExecCommand = orig })

	t.Setenv("GO_WANT_CONTRIB_GOLIST", "1")
	t.Setenv("GO_CONTRIB_GOLIST_STDOUT", stdout)
	t.Setenv("GO_CONTRIB_GOLIST_STDERR", stderr)
	t.Setenv("GO_CONTRIB_GOLIST_EXIT", strconv.Itoa(exitCode))
	t.Setenv("GOWORK", "/somewhere/go.work")
	return &asked
}

// TestContribGoListProcess is the helper process of stubGoList, not a test.
func TestContribGoListProcess(t *testing.T) {
	if os.Getenv("GO_WANT_CONTRIB_GOLIST") != "1" {
		return
	}

	// A lookup inside the project would use its go.mod: it must run in a
	// directory of its own, and not in a workspace.
	if _, err := os.Stat("go.mod"); err == nil {
		_, _ = fmt.Fprintln(os.Stderr, "helper: go.mod in the working directory")
		os.Exit(90)
	}
	if got := os.Getenv("GOWORK"); got != "off" {
		_, _ = fmt.Fprintf(os.Stderr, "helper: GOWORK=%q, want off\n", got)
		os.Exit(91)
	}

	_, _ = fmt.Fprint(os.Stdout, os.Getenv("GO_CONTRIB_GOLIST_STDOUT"))
	_, _ = fmt.Fprint(os.Stderr, os.Getenv("GO_CONTRIB_GOLIST_STDERR"))
	code, err := strconv.Atoi(os.Getenv("GO_CONTRIB_GOLIST_EXIT"))
	if err != nil {
		code = 92
	}
	os.Exit(code) // helper process exits intentionally
}

func Test_fetchContribV3Version(t *testing.T) {
	asked := stubGoList(t, `{
	"Path": "github.com/gofiber/contrib/v3/jwt",
	"Version": "v1.2.6",
	"Query": "latest",
	"GoVersion": "1.26.0"
}`, "", 0)

	version, err := fetchContribV3Version("jwt")
	require.NoError(t, err)

	assert.Equal(t, "v1.2.6", version)
	// The go command answers for GOPROXY, GOFLAGS and the module cache, and
	// -mod=mod keeps a GOFLAGS=-mod=vendor from failing the query.
	assert.Equal(t, []string{"go", "list", "-mod=mod", "-m", "-json", "github.com/gofiber/contrib/v3/jwt@latest"}, *asked)
}

func Test_fetchContribV3Version_Failure(t *testing.T) {
	stubGoList(t, "", "go: github.com/gofiber/contrib/v3/jwt@latest: module lookup disabled by GOPROXY=off\n", 1)

	_, err := fetchContribV3Version("jwt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "module lookup disabled by GOPROXY=off")
	assert.Contains(t, err.Error(), "github.com/gofiber/contrib/v3/jwt@latest")
}

func Test_fetchContribV3Version_BadAnswers(t *testing.T) {
	stubGoList(t, `{"Path": "github.com/gofiber/contrib/v3/jwt"}`, "", 0)
	_, err := fetchContribV3Version("jwt")
	require.ErrorIs(t, err, errContribNoVersion)

	stubGoList(t, `not json`, "", 0)
	_, err = fetchContribV3Version("jwt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse go list output")
}

func Test_fetchContribV3Version_NoGoCommand(t *testing.T) {
	orig := internal.ExecCommand
	internal.ExecCommand = func(string, ...string) *exec.Cmd {
		return exec.CommandContext(context.Background(), "/nonexistent/fiber-migrate-go")
	}
	t.Cleanup(func() { internal.ExecCommand = orig })

	_, err := fetchContribV3Version("jwt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "run go list")
}
