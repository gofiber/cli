package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateMonitorImport(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mmonitor")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/monitor"
var _ = monitor.New()`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMonitorImport(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/contrib/v3/monitor")
	assert.NotContains(t, content, "fiber/v2/middleware/monitor")
	assert.Contains(t, buf.String(), "Migrating monitor middleware import")
}
