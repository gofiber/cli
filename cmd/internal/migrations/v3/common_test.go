package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
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

func TestFormatFieldWithComment_SkipsDuplicateMigrationComment(t *testing.T) {
	t.Parallel()

	formatted := v3.FormatFieldWithComment(
		"    ",
		"// TODO: migrate KeyLookup",
		`strings.Join([]string{"cookie", "session_id"}, ":")`,
		",",
		"// TODO: migrate KeyLookup: strings.Join([]string{\"cookie\", \"session_id\"}, \":\")",
		"\n",
	)

	require.Equal(t, "    // TODO: migrate KeyLookup: strings.Join([]string{\"cookie\", \"session_id\"}, \":\"),\n", formatted)
}
