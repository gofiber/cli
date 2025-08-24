package v3_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateContextMethods(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    ctx := c.Context()
    uc := c.UserContext()
    c.SetUserContext(ctx)
    _ = uc
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".RequestCtx()")
	assert.NotContains(t, content, ".Context()")
	assert.Contains(t, content, `// TODO: SetUserContext was removed, please migrate manually: c.SetUserContext(ctx)`)
	assert.Contains(t, content, "uc := c")
	assert.Contains(t, buf.String(), "Migrating context methods")
}

func Test_MigrateContextMethods_SetUserContextAssignment(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtest2")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(ctx fiber.Ctx) error {
    res := ctx.SetUserContext(ctx.Context())
    _ = res
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `// TODO: SetUserContext was removed, please migrate manually: res := ctx.SetUserContext(ctx.RequestCtx())`)
	assert.NotContains(t, content, `.UserContext()`)
	assert.Contains(t, buf.String(), "Migrating context methods")
}

func Test_MigrateContextMethods_Idempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtestid")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    c.SetUserContext(c.Context())
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))
	first := readFile(t, file)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))
	second := readFile(t, file)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, strings.Count(second, "TODO: SetUserContext was removed"))
}

func Test_MigrateContextMethods_SkipNonFiber(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtestskip")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
type ctx struct{}

func (ctx) UserContext() {}

func (ctx) SetUserContext(any) {}
func (ctx) Context() {}
func handler(c ctx) {
    c.UserContext()
    c.SetUserContext(nil)
    c.Context()
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "c.UserContext()")
	assert.Contains(t, content, "c.SetUserContext(nil)")
	assert.Contains(t, content, "c.Context()")
	assert.NotContains(t, buf.String(), "Migrating context methods")
}

func Test_MigrateContextMethods_RequestCtxChained(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtestchain")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    c.Status(fiber.StatusOK).Context().SetBodyStreamWriter(nil)
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".RequestCtx().SetBodyStreamWriter")
	assert.NotContains(t, content, ".Context().SetBodyStreamWriter")
	assert.Contains(t, buf.String(), "Migrating context methods")
}

func Test_MigrateContextMethods_MultipleContextCalls(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtestmulti")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    rc := c.Context()
    c.Type("json").Status(fiber.StatusOK).Context().SetBodyStreamWriter(nil)
    _ = rc
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Equal(t, 2, strings.Count(content, ".RequestCtx()"))
	assert.NotContains(t, content, ".Context()")
	assert.Contains(t, buf.String(), "Migrating context methods")
}
