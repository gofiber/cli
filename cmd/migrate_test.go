package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_RunGoMod(t *testing.T) {
	dir := t.TempDir()

	modContent := `module example

require github.com/gofiber/fiber/v2 v2.0.0`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	vendor := filepath.Join(dir, "vendor")
	require.NoError(t, os.Mkdir(vendor, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vendor, "go.mod"), []byte("module vendor"), 0o600))

	origExec := execCommand
	var cmds []*exec.Cmd
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		env := []string{"GO_WANT_HELPER_PROCESS=1"}
		if needError {
			env = append(env, "GO_WANT_HELPER_NEED_ERR=1")
		}
		cmd.Env = env
		cmds = append(cmds, cmd)
		return cmd
	}
	defer func() {
		execCommand = origExec
		needError = false
	}()

	require.NoError(t, runGoMod(dir))
	assert.Len(t, cmds, 3)
	for _, c := range cmds {
		assert.Equal(t, dir, c.Dir)
	}

	cmds = nil
	needError = true
	assert.Error(t, runGoMod(dir))
}
