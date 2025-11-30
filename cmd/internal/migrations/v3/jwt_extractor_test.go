package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateJWTExtractor(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v3"
    jwtware "github.com/gofiber/contrib/jwt"
)
var _ = jwtware.New(jwtware.Config{
    TokenLookup: "header:Authorization, cookie:jwt",
    AuthScheme: "Token",
    Filter: func(c fiber.Ctx) bool { return false },
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.NotContains(t, content, "AuthScheme")
	assert.Contains(t, content, `Extractor: extractors.Chain(extractors.FromAuthHeader("Token"), extractors.FromCookie("jwt"))`)
	assert.Contains(t, content, "Next:")
	assert.Contains(t, content, "func(c fiber.Ctx) bool { return false },")
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_TokenLookupExpr(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_expr")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    jwtware "github.com/gofiber/contrib/jwt"
    "strings"
)
var _ = jwtware.New(jwtware.Config{
    TokenLookup: strings.Join([]string{"header:Authorization"}, ","),
    AuthScheme: "Bearer",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "// TODO: migrate TokenLookup: strings.Join([]string{\"header:Authorization\"}, \",\")")
	assert.NotContains(t, content, "AuthScheme")
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}
