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
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "IdleTimeout:")
	assert.NotContains(t, content, "Expiration:")
	assert.NotContains(t, content, "SessionKey")
	assert.Contains(t, buf.String(), "Migrating CSRF middleware configs")
}

func Test_MigrateCSRFConfig_SessionKeyWithComment(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrfskc")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/csrf"
)
var _ = csrf.New(csrf.Config{
    SessionKey: "csrf", // session comment
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "SessionKey")
	assert.NotContains(t, content, "session comment")
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
	assert.Contains(t, content, `Extractor: extractors.FromHeader("X-CSRF-Token")`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating CSRF middleware configs")
}

func Test_MigrateCSRFConfig_KeyLookup_Form(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrfkl_form")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/csrf"
)
var _ = csrf.New(csrf.Config{
    KeyLookup: "form:csrf",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.Contains(t, content, `Extractor: extractors.FromForm("csrf")`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating CSRF middleware configs")
}

func Test_MigrateCSRFConfig_KeyLookup_Func(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrfkl_func")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/csrf"
    "strings"
)
var _ = csrf.New(csrf.Config{
    KeyLookup: strings.Join([]string{"header", "X-CSRF-Token"}, ":"),
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "// TODO: migrate KeyLookup: strings.Join([]string{\"header\", \"X-CSRF-Token\"}, \":\")")
	assert.NotContains(t, content, "Extractor")
	assert.Contains(t, buf.String(), "Migrating CSRF middleware configs")
}

func Test_MigrateCSRFConfig_IgnoresOtherConfigs(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrf_ignore")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/keyauth"
)
var _ = keyauth.New(keyauth.Config{
    KeyLookup: "header:Authorization",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `KeyLookup: "header:Authorization"`)
	assert.NotContains(t, content, "Extractor")
	assert.NotContains(t, buf.String(), "Migrating CSRF middleware configs")
}

func Test_MigrateCSRFConfig_IgnoresSessionConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrf_ignore_session")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/session"
var _ = session.New(session.Config{
    KeyLookup: "cookie:__Host-session",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `KeyLookup: "cookie:__Host-session"`)
	assert.NotContains(t, buf.String(), "Migrating CSRF middleware configs")
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
