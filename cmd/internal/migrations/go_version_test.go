package migrations_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	migrations "github.com/gofiber/cli/cmd/internal/migrations"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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

func Test_MigrateGoVersion(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mgover")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	mod := `module example

go 1.21

require github.com/gofiber/fiber/v2 v2.0.0`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600))

	vendor := filepath.Join(dir, "vendor")
	require.NoError(t, os.Mkdir(vendor, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vendor, "go.mod"), []byte("module vendor\n\ngo 1.10"), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	fn := migrations.MigrateGoVersion("1.23")
	require.NoError(t, fn(cmd, dir, nil, nil))

	content := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, content, "go 1.23")
	assert.Contains(t, buf.String(), "1.23")

	vendorContent := readFile(t, filepath.Join(vendor, "go.mod"))
	assert.Contains(t, vendorContent, "go 1.10")
}
