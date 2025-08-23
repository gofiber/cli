package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateFilesystemMiddleware(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfs")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/filesystem"
    "net/http"
)
func main() {
    _ = filesystem.New(filesystem.Config{
        Root: http.Dir("./assets"),
        Index: "index.html",
    })
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `static.New("", static.Config{`)
	assert.Contains(t, content, "FS:         os.DirFS(\"./assets\")")
	assert.Contains(t, content, `IndexNames: []string{"index.html"}`)
	assert.Contains(t, buf.String(), "Migrating filesystem middleware")
}
