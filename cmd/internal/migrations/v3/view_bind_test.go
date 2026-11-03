package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateViewBind(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mvbtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    return c.Bind(fiber.Map{})
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateViewBind(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".ViewBind(")
	assert.NotContains(t, content, "c.Bind(")
	assert.Contains(t, buf.String(), "Migrating view binding helpers")
}

func Test_MigrateViewBind_NoChange(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mvbtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
func handler() {
    queries.Bind(results, &resultSlice)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateViewBind(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "queries.Bind(")
	assert.NotContains(t, content, "ViewBind(")
	assert.Empty(t, buf.String())
}
