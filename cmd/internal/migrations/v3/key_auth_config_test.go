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
	assert.Contains(t, content, `Extractor: extractors.Chain(extractors.FromAuthHeader("Bearer"), extractors.FromQuery("api"))`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating keyauth middleware configs")
}

func Test_MigrateKeyAuthConfig_KeyLookupCookieForm(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mkeyauth_cf")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/keyauth"
var _ = keyauth.New(keyauth.Config{
    KeyLookup: "cookie:__Host-session, form:csrf",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateKeyAuthConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.Contains(t, content, `Extractor: extractors.Chain(extractors.FromCookie("__Host-session"), extractors.FromForm("csrf"))`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating keyauth middleware configs")
}

func Test_MigrateKeyAuthConfig_KeyLookupFunc(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mkeyauth_func")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/keyauth"
    "strings"
)
var _ = keyauth.New(keyauth.Config{
    ErrorHandler: errHandler,
    KeyLookup: strings.Join([]string{"header:Authorization", "query:api"}, ":"),
    Validator: validator,
    ContextKey: authKey,
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateKeyAuthConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "// TODO: migrate KeyLookup: strings.Join([]string{\"header:Authorization\", \"query:api\"}, \":\")")
	assert.NotContains(t, content, "AuthScheme")
	assert.Contains(t, content, "ErrorHandler: errHandler,\n")
	assert.NotContains(t, content, "ErrorHandler: errHandler, Validator")
	assert.Contains(t, content, "Validator:  validator,")
	assert.Contains(t, content, "ContextKey: authKey,")
	assert.Contains(t, buf.String(), "Migrating keyauth middleware configs")
}
