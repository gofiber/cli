package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateTrustedProxyConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtpctest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New(fiber.Config{
        EnableTrustedProxyCheck: true,
        TrustedProxies: []string{"0.8.0.0"},
    })
    _ = app
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateTrustedProxyConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "TrustProxy:       true")
	assert.Contains(t, content, "TrustProxyConfig: fiber.TrustProxyConfig{Proxies: []string{\"0.8.0.0\"}},")
	assert.Contains(t, buf.String(), "Migrating trusted proxy config")
}
