package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateEnvVarConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "menvvar")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/envvar"
var _ = envvar.New(envvar.Config{
    ExcludeVars: []string{"SECRET"},
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateEnvVarConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "ExcludeVars")
	assert.Contains(t, buf.String(), "Migrating EnvVar middleware configs")
}
