package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSendFileConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msendfile")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"

func handler(c fiber.Ctx) error {
    if err := c.SendFile("./public/index.html", true); err != nil {
        return err
    }
    return c.SendFile("./public/file.txt", compressEnabled)
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `c.SendFile("./public/index.html", fiber.SendFile{Compress: true})`)
	assert.Contains(t, content, `c.SendFile("./public/file.txt", fiber.SendFile{Compress: compressEnabled})`)
	assert.Contains(t, buf.String(), "Migrating SendFile compress flag")
}
