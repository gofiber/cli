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

func Test_MigrateFilesystemMiddleware_WithNotFoundFile(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfs-notfound")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/filesystem"
    "net/http"
)
func main() {
    app.All("/*", filesystem.New(filesystem.Config{
        Root:         http.Dir("./dist"),
        NotFoundFile: "index.html",
        Index:        "index.html",
    }))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))

	content := readFile(t, file)
	// Check that NotFoundFile is commented out
	assert.Contains(t, content, "// NotFoundFile: \"index.html\"")
	// Check that TODO comment is added
	assert.Contains(t, content, "// TODO: Migrate to NotFoundHandler (fiber.Handler) - NotFoundFile is deprecated")
	// Check that static migration happened
	assert.Contains(t, content, `static.New("", static.Config{`)
	assert.Contains(t, content, "FS:         os.DirFS(\"./dist\")")
	assert.Contains(t, content, `IndexNames: []string{"index.html"}`)
}

func Test_MigrateFilesystemMiddleware_NotFoundFileIdempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfs-idempotent")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/filesystem"
    "net/http"
)
func main() {
    app.All("/*", filesystem.New(filesystem.Config{
        Root:         http.Dir("./dist"),
        NotFoundFile: "index.html",
        Index:        "index.html",
    }))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)

	// First migration
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))
	firstContent := readFile(t, file)

	// Second migration - should be idempotent
	buf.Reset()
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))
	secondContent := readFile(t, file)

	// Content should be exactly the same after second migration
	assert.Equal(t, firstContent, secondContent, "Migration should be idempotent")

	// Verify the TODO comment is only present once
	assert.Equal(t, 1, countOccurrences(secondContent, "TODO: Migrate to NotFoundHandler"))
	// Verify the NotFoundFile comment is only present once
	assert.Equal(t, 1, countOccurrences(secondContent, "// NotFoundFile:"))
}

// Helper function to count occurrences of a substring
func countOccurrences(str, substr string) int {
	count := 0
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			count++
		}
	}
	return count
}
