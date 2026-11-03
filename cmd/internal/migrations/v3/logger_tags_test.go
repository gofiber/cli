package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateLoggerTags(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mloggertest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/logger"
)
var _ = logger.TagHeader
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateLoggerTags(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TagHeader")
	assert.Contains(t, content, "TagReqHeader")
	assert.Contains(t, buf.String(), "Migrating logger tag constants")
}
