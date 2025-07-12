package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spf13/cobra"
)

var (
	needError bool
	errFlag   = struct{}{}
	testExit  = os.Exit // for testing exit
)

func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	// gosec: G204 - safe for test, args are controlled
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	if needError {
		cmd.Env = append(cmd.Env, "GO_WANT_HELPER_NEED_ERR=1")
	}
	return cmd
}

func TestHelperProcess(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "No command")
		testExit(2)
		return
	}

	if os.Getenv("GO_WANT_HELPER_NEED_ERR") == "1" {
		_, _ = fmt.Fprintf(os.Stderr, "fake error")
		testExit(1)
		return
	}

	testExit(0)
}

func setupCmd(flag ...struct{}) {
	execCommand = fakeExecCommand
	if len(flag) > 0 {
		needError = true
	}
}

func teardownCmd() {
	execCommand = exec.Command
	needError = false
}

func setupLookPath(flag ...struct{}) {
	execLookPath = func(_ string) (s string, err error) {
		if len(flag) > 0 {
			err = errors.New("fake look path error")
		}
		return "", err
	}
}

func teardownLookPath() {
	execLookPath = exec.LookPath
}

func setupOsExit(override ...func(int)) {
	fn := func(_ int) {}
	if len(override) > 0 {
		fn = override[0]
	}
	osExit = fn
	testExit = fn
}

func teardownOsExit() {
	osExit = os.Exit
	testExit = os.Exit
}

func runCobraCmd(cmd *cobra.Command, args ...string) (string, error) {
	b := new(bytes.Buffer)

	cmd.ResetCommands()
	cmd.SetErr(b)
	cmd.SetOut(b)
	cmd.SetArgs(args)
	err := cmd.Execute()

	return b.String(), err
}

func setupHomeDir(t *testing.T, pattern string) string {
	t.Helper()
	homeDir, err := os.MkdirTemp("", "test_"+pattern)
	assert.NoError(t, err)
	return homeDir
}

func teardownHomeDir(dir string) {
	err := os.RemoveAll(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to remove temp dir: %v", err)
	}
}

func setupSpinner() {
	skipSpinner = true
}

func teardownSpinner() {
	skipSpinner = false
}
