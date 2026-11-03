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

// The expression handed over is kept whole, whatever it holds: the call used to be cut at
// the first closing parenthesis, which left code that does not parse.
func Test_MigrateProxyTLSConfig_Arguments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/gofiber/fiber/v2/middleware/proxy"
)

func buildTLS() *tls.Config { return &tls.Config{MinVersion: tls.VersionTLS12} }

func setup() {
	proxy.WithTlsConfig(buildTLS())
	proxy.WithTlsConfig(&tls.Config{
		RootCAs:    x509.NewCertPool(),
		MinVersion: tls.VersionTLS12,
	})
}
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateProxyTLSConfig(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "proxy.WithClient(&fasthttp.Client{TLSConfig: buildTLS()})")
	assert.Contains(t, content, "proxy.WithClient(&fasthttp.Client{TLSConfig: &tls.Config{")
	assert.Contains(t, content, "MinVersion: tls.VersionTLS12,\n\t}})")
	assert.NotContains(t, content, "WithTlsConfig")
	assert.Contains(t, content, `"github.com/valyala/fasthttp"`, "the import of fasthttp is added")
}

func Test_MigrateProxyTLSConfig_KeepsTheNamesOfTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"crypto/tls"

	fh "github.com/valyala/fasthttp"
	fp "github.com/gofiber/fiber/v2/middleware/proxy"
)

var _ = fh.StatusOK

func setup(cfg *tls.Config) {
	fp.WithTlsConfig(cfg)
}
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateProxyTLSConfig(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "fp.WithClient(&fh.Client{TLSConfig: cfg})")
}

func Test_MigrateProxyTLSConfig_LeavesWhatIsNotFibers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import "example.com/mine/proxy"

// proxy.WithTlsConfig(cfg) stays a comment.
const doc = "proxy.WithTlsConfig(cfg)"

func setup() {
	proxy.WithTlsConfig(nil)
}
`
	file := writeTempFile(t, dir, source)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateProxyTLSConfig(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, buf.String())
}

func Test_MigrateProxyTLSConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"crypto/tls"

	"github.com/gofiber/fiber/v2/middleware/proxy"
)

func setup(cfg *tls.Config) {
	proxy.WithTlsConfig(cfg)
}
`)

	var first bytes.Buffer
	require.NoError(t, v3.MigrateProxyTLSConfig(newCmd(&first), dir, nil, nil))
	migrated := readFile(t, file)
	assert.NotEmpty(t, first.String())

	var second bytes.Buffer
	require.NoError(t, v3.MigrateProxyTLSConfig(newCmd(&second), dir, nil, nil))
	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, second.String())
}
