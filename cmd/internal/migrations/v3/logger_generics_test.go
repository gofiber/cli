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
    "github.com/gofiber/fiber/v2/log"
)
var _ log.AllLogger = (*customLogger)(nil)
var _ log.ConfigurableLogger = (*customLogger)(nil)
func main() {
    logger := log.DefaultLogger()
    log.SetLogger(logger)
    _ = log.LoggerToWriter(logger, log.LevelInfo)
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
	assert.Contains(t, content, "log.DefaultLogger[any]()")
	assert.Contains(t, content, "log.SetLogger[any](")
	assert.Contains(t, content, "log.LoggerToWriter[any](")
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

func Test_MigrateLoggerGenerics_SkipNonFiber(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mloggenericstestskip")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    otherlog "example.com/other/log"
)
var _ otherlog.AllLogger = (*customLogger)(nil)
var _ otherlog.ConfigurableLogger = (*customLogger)(nil)
func main() {
    logger := otherlog.DefaultLogger()
    otherlog.SetLogger(logger)
    _ = otherlog.LoggerToWriter(logger, otherlog.LevelInfo)
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateLoggerGenerics(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "otherlog.DefaultLogger()")
	assert.NotContains(t, content, "AllLogger[")
	assert.NotContains(t, content, "ConfigurableLogger[")
	assert.NotContains(t, buf.String(), "Migrating logger generics")
}
