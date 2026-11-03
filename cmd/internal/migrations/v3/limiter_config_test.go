package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateLimiterConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlimiter")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/limiter"
    "time"
)
var _ = limiter.New(limiter.Config{
    Duration: time.Minute,
    Store: nil,
    Key: func(c fiber.Ctx) string { return "a" },
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateLimiterConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "Expiration:")
	assert.Contains(t, content, "Storage:")
	assert.Contains(t, content, "KeyGenerator:")
	assert.Contains(t, buf.String(), "Migrating limiter middleware configs")
}
