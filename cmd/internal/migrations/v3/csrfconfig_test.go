package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateCSRFConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrf")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/csrf"
    "time"
)
var _ = csrf.New(csrf.Config{
    Expiration: 10 * time.Minute,
    SessionKey: "csrf",
    ContextKey: "csrf",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "IdleTimeout:")
	assert.NotContains(t, content, "Expiration:")
	assert.NotContains(t, content, "SessionKey")
	assert.NotContains(t, content, "ContextKey")
	assert.Contains(t, buf.String(), "Migrating CSRF middleware configs")
}

func Test_MigrateCSRFConfig_KeyLookup(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrfkl")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/csrf"
)
var _ = csrf.New(csrf.Config{
    KeyLookup: "header:X-CSRF-Token",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.Contains(t, content, `Extractor: csrf.FromHeader("X-CSRF-Token")`)
	assert.Contains(t, buf.String(), "Migrating CSRF middleware configs")
}

func Test_MigrateCSRFConfig_IgnoresPaseto(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrfpaseto")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/o1egl/paseto"
func main() {
    payload := &paseto.JSONToken{
        Audience: pasetoTokenAudience,
        Jti: tokenID.String(),
        Subject: pasetoTokenSubject,
        IssuedAt: timeNow,
        Expiration: timeNow.Add(duration),
        NotBefore: timeNow,
    }
    _ = payload
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "Expiration:")
	assert.NotContains(t, content, "IdleTimeout:")
}
