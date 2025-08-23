package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateKeyAuthConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mkeyauth")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/keyauth"
var _ = keyauth.New(keyauth.Config{
    KeyLookup: "header:Authorization, query:api",
    AuthScheme: "Bearer",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateKeyAuthConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.NotContains(t, content, "AuthScheme")
	assert.Contains(t, content, `Extractor: keyauth.Chain(keyauth.FromAuthHeader("Authorization", "Bearer"), keyauth.FromQuery("api"))`)
	assert.Contains(t, buf.String(), "Migrating keyauth middleware configs")
}
