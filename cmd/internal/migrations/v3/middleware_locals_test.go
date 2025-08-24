package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
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
    csrfToken, ok := c.Locals("csrf").(string)
    token := c.Locals("token").(string)
    _ = id
    _ = csrfToken
    _ = token
    _ = ok
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `id := requestid.FromContext(c)`)
	assert.Contains(t, content, `csrfToken, ok := csrf.TokenFromContext(c), true`)
	assert.Contains(t, content, `token := keyauth.TokenFromContext(c)`)
	assert.Contains(t, buf.String(), "Migrating middleware locals")
}

func Test_MigrateMiddlewareLocals_ContextKey(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mctxkey")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/keyauth"
    "github.com/gofiber/fiber/v2/middleware/csrf"
    "github.com/gofiber/fiber/v2/middleware/session"
)

func main() {
    _ = keyauth.New(keyauth.Config{ContextKey: "token"})
    _ = csrf.New(csrf.Config{ContextKey: "csrf"})
    _ = session.New(session.Config{ContextKey: "session"})
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "ContextKey")
}

func Test_MigrateMiddlewareLocals_ContextKeyWithComment(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mctxkeyc")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/keyauth"
)

func main() {
    _ = keyauth.New(keyauth.Config{ContextKey: "token", // key comment
    })
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "ContextKey")
	assert.NotContains(t, content, "key comment")
}

func Test_MigrateMiddlewareLocals_CustomContextKey(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcustomctx")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.New(csrf.Config{ContextKey: "token"})

func handler(c fiber.Ctx) error {
    token := c.Locals("token").(string)
    _ = token
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `token := csrf.TokenFromContext(c)`)
	assert.NotContains(t, content, "ContextKey")
}

func Test_MigrateMiddlewareLocals_ContextKeyFormatting(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mctxfmt")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import ka "github.com/gofiber/fiber/v2/middleware/keyauth"
func main() {
    _ = ka.New(ka.Config{
        Next:        nil,
        ContextKey:  "token",
        SuccessHandler: nil,
    })
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "ContextKey")
	assert.Contains(t, content, "Next:           nil,\n\t\tSuccessHandler: nil")
}

func Test_MigrateMiddlewareLocals_NonFiberConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mnonfiber")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"

var ConfigDefault = Config{
    Next:           nil,
    SuccessHandler: nil,
    ErrorHandler:   nil,
    Validate:       nil,
    SymmetricKey:   nil,
    ContextKey:     DefaultContextKey,
    TokenLookup:    [2]string{LookupHeader, fiber.HeaderAuthorization},
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "ContextKey:     DefaultContextKey")
}

func Test_MigrateMiddlewareLocals_CustomContextKeyAcrossFiles(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcustomctxfiles")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// config in separate file
	cfg := `package main
import "github.com/gofiber/fiber/v2/middleware/csrf"

var _ = csrf.New(csrf.Config{ContextKey: "token"})`
	cfgPath := filepath.Join(dir, "config.go")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	handler := `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/csrf"
)

func handler(c fiber.Ctx) error {
    token := c.Locals("token").(string)
    _ = token
    return nil
}`
	file := writeTempFile(t, dir, handler)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `token := csrf.TokenFromContext(c)`)
	cfgContent := readFile(t, cfgPath)
	assert.NotContains(t, cfgContent, "ContextKey")
}

func Test_MigrateMiddlewareLocals_CustomContextKeyWithFunc(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcustomctxfunc")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	cfg := `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/csrf"
)

var _ = csrf.New(csrf.Config{
    Next: func(c *fiber.Ctx) bool { return csrfActivated },
    ContextKey: "token",
})`
	cfgPath := filepath.Join(dir, "config.go")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	handler := `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/keyauth"
)

func handler(c fiber.Ctx) error {
    _ = keyauth.New(keyauth.Config{})
    token := c.Locals("token").(string)
    _ = token
    return nil
}`
	handlerPath := filepath.Join(dir, "handler.go")
	require.NoError(t, os.WriteFile(handlerPath, []byte(handler), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, handlerPath)
	assert.Contains(t, content, `token := csrf.TokenFromContext(c)`)
	assert.NotContains(t, content, `keyauth.TokenFromContext`)
	cfgContent := readFile(t, cfgPath)
	assert.NotContains(t, cfgContent, "ContextKey")
}

func Test_MigrateMiddlewareLocals_SameContextKeyDifferentPackages(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mctxdup")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	kaCfg := `package main
import "github.com/gofiber/fiber/v2/middleware/keyauth"

var _ = keyauth.New(keyauth.Config{ContextKey: "token"})`
	kaCfgPath := filepath.Join(dir, "keyauth.go")
	require.NoError(t, os.WriteFile(kaCfgPath, []byte(kaCfg), 0o600))

	csrfCfg := `package main
import "github.com/gofiber/fiber/v2/middleware/csrf"

var _ = csrf.New(csrf.Config{ContextKey: "token"})`
	csrfCfgPath := filepath.Join(dir, "csrf.go")
	require.NoError(t, os.WriteFile(csrfCfgPath, []byte(csrfCfg), 0o600))

	kaHandler := `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/keyauth"
)

func handlerKA(c fiber.Ctx) error {
    token := c.Locals("token").(string)
    _ = token
    return nil
}`
	kaHandlerPath := filepath.Join(dir, "ka_handler.go")
	require.NoError(t, os.WriteFile(kaHandlerPath, []byte(kaHandler), 0o600))

	csrfHandler := `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/csrf"
)

func handlerCSRF(c fiber.Ctx) error {
    token := c.Locals("token").(string)
    _ = token
    return nil
}`
	csrfHandlerPath := filepath.Join(dir, "csrf_handler.go")
	require.NoError(t, os.WriteFile(csrfHandlerPath, []byte(csrfHandler), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMiddlewareLocals(cmd, dir, nil, nil))

	kaContent := readFile(t, kaHandlerPath)
	assert.Contains(t, kaContent, `token := keyauth.TokenFromContext(c)`)

	csrfContent := readFile(t, csrfHandlerPath)
	assert.Contains(t, csrfContent, `token := csrf.TokenFromContext(c)`)

	assert.NotContains(t, readFile(t, kaCfgPath), "ContextKey")
	assert.NotContains(t, readFile(t, csrfCfgPath), "ContextKey")
}
