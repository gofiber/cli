package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateCORSConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcors")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/cors"
var _ = cors.New(cors.Config{
    AllowOrigins: "https://a.com,https://b.com",
    AllowMethods: "GET,POST",
    AllowHeaders: "Content-Type",
    ExposeHeaders: "Content-Length",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCORSConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `AllowOrigins: []string{"https://a.com", "https://b.com"}`)
	assert.Contains(t, content, `AllowMethods: []string{"GET", "POST"}`)
	assert.Contains(t, content, `AllowHeaders: []string{"Content-Type"}`)
	assert.Contains(t, content, `ExposeHeaders: []string{"Content-Length"}`)
	assert.Contains(t, buf.String(), "Migrating CORS middleware configs")
}
