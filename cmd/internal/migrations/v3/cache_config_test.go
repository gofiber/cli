package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateCacheConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcacheconfig")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/cache"
)

var cfg struct {
    EnableCache         bool
    DisableCacheHeaders bool
}

var _ = cache.New(cache.Config{
    CacheControl: true,
})

var _ = cache.New(cache.Config{
    CacheControl: false,
})

var _ = cache.New(cache.Config{
    CacheControl: cfg.EnableCache,
})

var _ = cache.New(cache.Config{
    CacheControl: !cfg.DisableCacheHeaders,
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCacheConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "DisableCacheControl: false")
	assert.Contains(t, content, "DisableCacheControl: true")
	assert.Contains(t, content, "DisableCacheControl: !(cfg.EnableCache)")
	assert.Contains(t, content, "DisableCacheControl: cfg.DisableCacheHeaders")
	assert.Contains(t, buf.String(), "Migrating cache middleware configs")
}

func Test_MigrateCacheConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcacheconfigidem")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/cache"
)

var _ = cache.New(cache.Config{
    CacheControl: true,
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCacheConfig(cmd, dir, nil, nil))
	first := readFile(t, file)

	require.NoError(t, v3.MigrateCacheConfig(cmd, dir, nil, nil))
	second := readFile(t, file)

	assert.Equal(t, first, second)
}
