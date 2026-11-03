package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateTimeoutConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtimeout")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/timeout"
    "time"
)
var _ = timeout.New(func(c fiber.Ctx) error { return nil }, 2*time.Second)`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateTimeoutConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "timeout.New(func(c fiber.Ctx) error { return nil }, timeout.Config{Timeout: 2 * time.Second})")
	assert.Contains(t, buf.String(), "Migrating timeout middleware configs")
}

func Test_MigrateTimeoutConfig_NestedHandler(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtimeoutnested")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/timeout"
    "time"
)
func wrap(a, b string) fiber.Handler { return func(c fiber.Ctx) error { return nil } }
var _ = timeout.New(wrap("a", "b"), 2*time.Second)`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateTimeoutConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "timeout.New(wrap(\"a\", \"b\"), timeout.Config{Timeout: 2 * time.Second})")
	assert.Contains(t, buf.String(), "Migrating timeout middleware configs")
}

func Test_MigrateTimeoutConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtimeoutidem")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/timeout"
    "time"
)
var _ = timeout.New(func(c fiber.Ctx) error { return nil }, 2*time.Second)`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateTimeoutConfig(cmd, dir, nil, nil))
	first := readFile(t, file)
	require.NoError(t, v3.MigrateTimeoutConfig(cmd, dir, nil, nil))
	second := readFile(t, file)
	assert.Equal(t, first, second)
}

// NewWithContext was the constructor v2 recommended: it works with the context, as the New of
// v3 does, and takes the errors that count as a timeout.
func Test_MigrateTimeoutConfig_NewWithContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

var errSlow = errors.New("slow")

func handler(c fiber.Ctx) error { return nil }

func routes(app *fiber.App, errs []error) {
	app.Get("/a", timeout.NewWithContext(handler, 5*time.Second))
	app.Get("/b", timeout.NewWithContext(handler, time.Second, context.DeadlineExceeded))
	app.Get("/c", timeout.NewWithContext(handler, time.Second, errSlow, context.Canceled))
	app.Get("/d", timeout.NewWithContext(handler, time.Second, errs...))
	app.Get("/e", timeout.NewWithContext(
		func(c fiber.Ctx) error { return nil },
		time.Second,
	))
}
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateTimeoutConfig(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `timeout.New(handler, timeout.Config{Timeout: 5 * time.Second})`)
	assert.Contains(t, content, `timeout.New(handler, timeout.Config{Timeout: time.Second, Errors: []error{context.DeadlineExceeded}})`)
	assert.Contains(t, content, `timeout.New(handler, timeout.Config{Timeout: time.Second, Errors: []error{errSlow, context.Canceled}})`)
	assert.Contains(t, content, `timeout.New(handler, timeout.Config{Timeout: time.Second, Errors: errs})`)
	assert.Contains(t, content, "timeout.New(func(c fiber.Ctx) error { return nil }, timeout.Config{Timeout: time.Second})")
	assert.NotContains(t, content, "NewWithContext")
	assert.Contains(t, buf.String(), "Migrating timeout middleware configs")
}

func Test_MigrateTimeoutConfig_AliasedImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	tm "github.com/gofiber/fiber/v2/middleware/timeout"
)

func handler(c fiber.Ctx) error { return nil }

var _ = tm.New(handler, time.Second)
var _ = tm.NewWithContext(handler, time.Second)
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateTimeoutConfig(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "var _ = tm.New(handler, tm.Config{Timeout: time.Second})")
	assert.NotContains(t, content, "NewWithContext")
}

func Test_MigrateTimeoutConfig_LeavesWhatIsNotFibers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import (
	"time"

	"example.com/mine/timeout"
)

// timeout.NewWithContext(h, time.Second) stays a comment.
const doc = "timeout.New(h, time.Second)"

var _ = timeout.New(nil, time.Second)
var _ = timeout.NewWithContext(nil, time.Second)
`
	file := writeTempFile(t, dir, source)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateTimeoutConfig(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, buf.String())
}

func Test_MigrateTimeoutConfig_NewWithContextIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

func handler(c fiber.Ctx) error { return nil }

var _ = timeout.New(handler, time.Second)
var _ = timeout.NewWithContext(handler, time.Second, context.Canceled)
`)

	var first bytes.Buffer
	require.NoError(t, v3.MigrateTimeoutConfig(newCmd(&first), dir, nil, nil))
	migrated := readFile(t, file)
	assert.NotEmpty(t, first.String())

	var second bytes.Buffer
	require.NoError(t, v3.MigrateTimeoutConfig(newCmd(&second), dir, nil, nil))
	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, second.String())
}
