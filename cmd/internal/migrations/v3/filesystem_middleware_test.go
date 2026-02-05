package v3_test

import (
	"bytes"
	"os"
	"strings"
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
	assert.Contains(t, content, `FS: os.DirFS("./dist")`)
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
	assert.Equal(t, 1, strings.Count(secondContent, "TODO: Migrate to NotFoundHandler"))
	// Verify the NotFoundFile comment is only present once
	assert.Equal(t, 1, strings.Count(secondContent, "// NotFoundFile:"))
}

// Test_MigrateFilesystemMiddleware_WithHTTPFS tests migration of http.FS() usage.
// Bug fix: http.FS(embedFS) should produce FS: embedFS, not FS: os.DirFS(http.FS(embedFS))
// See: https://github.com/gofiber/cli/issues/267
func Test_MigrateFilesystemMiddleware_WithHTTPFS(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfs-httpfs")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
	"embed"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

//go:embed static/*
var embedFS embed.FS

func main() {
	app := fiber.New()
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:   http.FS(embedFS),
		MaxAge: 24 * 60 * 60,
	}))
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))

	content := readFile(t, file)
	// Should use embedFS directly, not wrap with os.DirFS
	assert.Contains(t, content, "FS:     embedFS")
	assert.NotContains(t, content, "os.DirFS(http.FS(embedFS))")
	assert.NotContains(t, content, "os.DirFS(embedFS)")
	// Check static middleware migration happened
	assert.Contains(t, content, `static.New("", static.Config{`)
	assert.Contains(t, buf.String(), "Migrating filesystem middleware")
}

// Test_MigrateFilesystemMiddleware_WithStaticPackageConflict tests migration when
// the user has their own package named "static".
// Bug fix: Should alias the middleware import to avoid conflict.
// See: https://github.com/gofiber/cli/issues/267
func Test_MigrateFilesystemMiddleware_WithStaticPackageConflict(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfs-conflict")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"myproject/internal/web/static"
)

func main() {
	app := fiber.New()
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:   http.FS(static.Static),
		MaxAge: 24 * 60 * 60,
	}))
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))

	content := readFile(t, file)
	// Should use staticmw alias for the middleware to avoid conflict
	assert.Contains(t, content, `staticmw "github.com/gofiber/fiber/v3/middleware/static"`)
	assert.Contains(t, content, `staticmw.New("", staticmw.Config{`)
	// User's static import should remain unchanged
	assert.Contains(t, content, `"myproject/internal/web/static"`)
	// Should use static.Static directly (from user's package)
	assert.Contains(t, content, "FS:     static.Static")
	assert.Contains(t, buf.String(), "Migrating filesystem middleware")
}

// Test_MigrateFilesystemMiddleware_WithAliasedStaticImport tests migration when
// the user has already aliased their static package import.
func Test_MigrateFilesystemMiddleware_WithAliasedStaticImport(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfs-aliased")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	webstatic "myproject/internal/web/static"
)

func main() {
	app := fiber.New()
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:   http.FS(webstatic.Assets),
		MaxAge: 24 * 60 * 60,
	}))
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))

	content := readFile(t, file)
	// When user's import is already aliased, we can use "static" for middleware
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/middleware/static"`)
	assert.Contains(t, content, `static.New("", static.Config{`)
	// User's aliased import should remain
	assert.Contains(t, content, `webstatic "myproject/internal/web/static"`)
	// Should use webstatic.Assets directly
	assert.Contains(t, content, "FS:     webstatic.Assets")
}

// Test_MigrateFilesystemMiddleware_FiberStaticNotConflict ensures that Fiber's own
// static middleware is not incorrectly flagged as a conflict.
func Test_MigrateFilesystemMiddleware_FiberStaticNotConflict(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfs-fiber-static")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Code that already has Fiber's static middleware (v3) imported
	// Should not incorrectly alias the import
	file := writeTempFile(t, dir, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

func main() {
	app := fiber.New()
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:   http.Dir("./public"),
		MaxAge: 24 * 60 * 60,
	}))
	app.Listen(":3000")
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))

	content := readFile(t, file)

	// Should NOT have aliased import - no conflict with user packages
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/middleware/static"`)
	assert.NotContains(t, content, `staticmw "github.com/gofiber/fiber/v3/middleware/static"`)

	// Should use unaliased static.New and static.Config
	assert.Contains(t, content, `static.New("", static.Config{`)
}

// Test_MigrateFilesystemMiddleware_IssueExample tests the exact scenario from issue #267.
// This is the complete example combining both bugs: http.FS() with a package named static.
// See: https://github.com/gofiber/cli/issues/267
func Test_MigrateFilesystemMiddleware_IssueExample(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfs-issue267")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// This is the exact v2 code from issue #267
	file := writeTempFile(t, dir, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"myproject/internal/web/static"
)

func main() {
	app := fiber.New()
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:   http.FS(static.Static),
		MaxAge: 24 * 60 * 60,
	}))
	app.Listen(":3000")
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateFilesystemMiddleware(cmd, dir, nil, nil))

	content := readFile(t, file)

	// Bug #1 fixed: http.FS(static.Static) should become FS: static.Static (not os.DirFS(...))
	assert.Contains(t, content, "FS:     static.Static")
	assert.NotContains(t, content, "os.DirFS(http.FS(static.Static))")
	assert.NotContains(t, content, "os.DirFS(static.Static)")

	// Bug #2 fixed: middleware import should be aliased to avoid conflict
	assert.Contains(t, content, `staticmw "github.com/gofiber/fiber/v3/middleware/static"`)
	assert.Contains(t, content, `staticmw.New("", staticmw.Config{`)

	// User's import should remain unchanged
	assert.Contains(t, content, `"myproject/internal/web/static"`)
}
