package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func writeTempFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "main.go")
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	return string(b)
}

func newCmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd
}
