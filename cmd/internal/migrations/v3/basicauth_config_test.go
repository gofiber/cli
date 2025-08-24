package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateBasicauthConfig_WithComments(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mbasiccfg")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/basicauth"
var _ = basicauth.New(basicauth.Config{
    ContextUsername: "user", // username comment
    ContextPassword: "pass", // password comment
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateBasicauthConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "ContextUsername")
	assert.NotContains(t, content, "ContextPassword")
	assert.NotContains(t, content, "username comment")
	assert.NotContains(t, content, "password comment")
	assert.Contains(t, buf.String(), "Migrating basicauth configs")
}
