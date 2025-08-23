package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateMimeConstants(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mmimetest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
const mime = fiber.MIMEApplicationJavaScript
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMimeConstants(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "MIMEApplicationJavaScript")
	assert.Contains(t, content, "MIMETextJavaScript")
	assert.Contains(t, buf.String(), "Migrating MIME constants")
}
