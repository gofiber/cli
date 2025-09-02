package v3_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateListenMethods(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlisten")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "crypto/tls"
)
func main() {
    app := fiber.New()
    cert, _ := tls.LoadX509KeyPair("cert.pem", "key.pem")
    app.ListenTLS(":443", "cert.pem", "key.pem")
    app.ListenTLSWithCertificate(":443", cert)
    app.ListenMutualTLS(":443", "cert.pem", "key.pem", "ca.pem")
    app.ListenMutualTLSWithCertificate(":443", cert, "ca.pem")
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateListenMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "ListenTLS(")
	assert.NotContains(t, content, "ListenTLSWithCertificate(")
	assert.NotContains(t, content, "ListenMutualTLS(")
	assert.NotContains(t, content, "ListenMutualTLSWithCertificate(")
	assert.Equal(t, 4, strings.Count(content, ".Listen("))
	assert.Contains(t, buf.String(), "Migrating listen methods")
}

func Test_MigrateListenMethods_SkipNonFiber(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlistenskip")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
type server struct{}
func (server) ListenTLS(a, b, c string) {}
func (server) ListenTLSWithCertificate(a string, b struct{}) {}
func (server) ListenMutualTLS(a, b, c, d string) {}
func (server) ListenMutualTLSWithCertificate(a string, b struct{}, c string) {}
func main() {
    var s server
    cert := struct{}{}
    s.ListenTLS(":443", "cert.pem", "key.pem")
    s.ListenTLSWithCertificate(":443", cert)
    s.ListenMutualTLS(":443", "cert.pem", "key.pem", "ca.pem")
    s.ListenMutualTLSWithCertificate(":443", cert, "ca.pem")
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateListenMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "s.ListenTLS(")
	assert.Contains(t, content, "s.ListenTLSWithCertificate(")
	assert.Contains(t, content, "s.ListenMutualTLS(")
	assert.Contains(t, content, "s.ListenMutualTLSWithCertificate(")
	assert.NotContains(t, buf.String(), "Migrating listen methods")
}
