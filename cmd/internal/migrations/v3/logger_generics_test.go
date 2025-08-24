package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateLoggerGenerics(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mloggenericstest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    fiberlog "github.com/gofiber/fiber/v2/log"
)
var _ fiberlog.AllLogger = (*customLogger)(nil)
var _ fiberlog.ConfigurableLogger = (*customLogger)(nil)
func main() {
    logger := fiberlog.DefaultLogger()
    fiberlog.SetLogger(logger)
    _ = fiberlog.LoggerToWriter(logger, fiberlog.LevelInfo)
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateLoggerGenerics(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "AllLogger =")
	assert.NotContains(t, content, "ConfigurableLogger =")
	assert.Contains(t, content, "AllLogger[any]")
	assert.Contains(t, content, "ConfigurableLogger[any]")
	assert.Contains(t, content, "DefaultLogger[any]()")
	assert.Contains(t, content, "SetLogger[any](")
	assert.Contains(t, content, "LoggerToWriter[any](")
	assert.Contains(t, buf.String(), "Migrating logger generics")
}

func Test_MigrateLoggerGenerics_Zap(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mloggenericstestzap")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    fiberlog "github.com/gofiber/fiber/v2/log"
    "go.uber.org/zap"
)
type customLogger struct{}
func (c *customLogger) Logger() *zap.Logger { return nil }
var _ fiberlog.AllLogger = (*customLogger)(nil)
var _ fiberlog.ConfigurableLogger = (*customLogger)(nil)
func main() {
    logger := &customLogger{}
    fiberlog.SetLogger(logger)
    _ = fiberlog.LoggerToWriter(logger, fiberlog.LevelInfo)
    _ = fiberlog.DefaultLogger().Logger()
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateLoggerGenerics(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "AllLogger =")
	assert.NotContains(t, content, "ConfigurableLogger =")
	assert.Contains(t, content, "AllLogger[*zap.Logger]")
	assert.Contains(t, content, "ConfigurableLogger[*zap.Logger]")
	assert.Contains(t, content, "SetLogger[*zap.Logger](")
	assert.Contains(t, content, "LoggerToWriter[*zap.Logger](")
	assert.Contains(t, content, "DefaultLogger[*zap.Logger]()")
	assert.Contains(t, buf.String(), "Migrating logger generics")
}
