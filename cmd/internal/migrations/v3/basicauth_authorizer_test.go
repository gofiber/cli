package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateBasicauthAuthorizer(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mbauthorizer")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/basicauth"
)
var _ = basicauth.New(basicauth.Config{
    Authorizer: func(u, p string) bool { return true },
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateBasicauthAuthorizer(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `Authorizer: func(u, p string, _ fiber.Ctx) bool`)
	assert.Contains(t, buf.String(), "Migrating basicauth authorizer")
}
