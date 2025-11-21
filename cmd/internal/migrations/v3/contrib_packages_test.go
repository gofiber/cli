package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateMonitorImport_NoChanges(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "contrib")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/contrib/monitor"
var _ = monitor.New()`)
	initial := readFile(t, file)

	var buf bytes.Buffer
	cmd := newCmd(&buf)

	require.NoError(t, v3.MigrateMonitorImport(cmd, dir, nil, nil))

	assert.Equal(t, initial, readFile(t, file))
	assert.Empty(t, buf.String())
}
