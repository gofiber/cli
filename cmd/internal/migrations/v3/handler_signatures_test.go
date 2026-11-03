package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateHandlerSignatures(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhstest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c *fiber.Ctx) error { return nil }
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateHandlerSignatures(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "*fiber.Ctx")
	assert.Contains(t, content, "fiber.Ctx")
	assert.Contains(t, buf.String(), "Migrating handler signatures")
}
