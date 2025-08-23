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
	assert.Contains(t, content, `timeout.New(func(c fiber.Ctx) error { return nil }, timeout.Config{Timeout: 2*time.Second})`)
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
	assert.Contains(t, content, `timeout.New(wrap("a", "b"), timeout.Config{Timeout: 2*time.Second})`)
	assert.Contains(t, buf.String(), "Migrating timeout middleware configs")
}
