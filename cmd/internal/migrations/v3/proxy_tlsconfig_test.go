package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateProxyTLSConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mproxy")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/proxy"
    "crypto/tls"
)
func main() {
    proxy.WithTlsConfig(&tls.Config{InsecureSkipVerify: true})
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateProxyTLSConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "proxy.WithClient(&fasthttp.Client{TLSConfig: &tls.Config{InsecureSkipVerify: true}})")
	assert.Contains(t, buf.String(), "Migrating proxy TLS config")
}
