package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateMiddlewareLocals(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlocals")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    id := c.Locals("requestid").(string)
    csrfToken, _ := c.Locals("csrf").(string)
    token := c.Locals("token").(string)
    _ = id
    _ = csrfToken
    _ = token
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `id := requestid.FromContext(c)`)
	assert.Contains(t, content, `csrfToken := csrf.TokenFromContext(c)`)
	assert.Contains(t, content, `token := keyauth.TokenFromContext(c)`)
	assert.Contains(t, buf.String(), "Migrating middleware locals")
}
