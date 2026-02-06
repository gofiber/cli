package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateLoggerConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlogger")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "os"
    "github.com/gofiber/fiber/v2/middleware/logger"
)
var _ = logger.New(logger.Config{
    Output: os.Stdout,
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateLoggerConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "Stream:")
	assert.NotContains(t, content, "Output:")
	assert.Contains(t, buf.String(), "Migrating logger middleware configs")
}

func Test_MigrateLoggerConfig_NoChange(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlogger_noop")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	writeTempFile(t, dir, `package main
import "fmt"
func main() { fmt.Println("hello") }
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateLoggerConfig(cmd, dir, nil, nil))

	assert.Empty(t, buf.String())
}

func Test_MigrateLoggerConfig_MultipleFields(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlogger_multi")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "os"
    "github.com/gofiber/fiber/v2/middleware/logger"
)
var _ = logger.New(logger.Config{
    Format:     "[${time}] ${status}\n",
    Output:     os.Stderr,
    TimeFormat: "2006-01-02",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateLoggerConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "Stream:")
	assert.NotContains(t, content, "Output:")
	assert.Contains(t, content, "Format:")
	assert.Contains(t, content, "TimeFormat:")
}
