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
	assert.Contains(t, content, "ctx := c.RequestCtx()")
	assert.Contains(t, content, "uc := c.Context()")
	assert.Contains(t, content, "c.SetContext(ctx)")
	assert.NotContains(t, content, ".UserContext()")
	assert.NotContains(t, content, "SetUserContext")
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
	assert.Contains(t, content, `res := ctx.SetContext(ctx.RequestCtx())`)
	assert.NotContains(t, content, `.UserContext()`)
	assert.NotContains(t, content, "SetUserContext")
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
	assert.Equal(t, 1, strings.Count(second, "SetContext("))
}

func Test_MigrateContextMethods_MultipleRuns(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtestmulti2")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    ctx := c.Context()
    uc := c.UserContext()
    c.SetUserContext(ctx)
    c.SetUserContext(c.Context())
    _ = uc
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))
	first := readFile(t, file)

	require.Contains(t, first, "ctx := c.RequestCtx()")
	require.Contains(t, first, "uc := c.Context()")
	require.Contains(t, first, "c.SetContext(ctx)")
	require.Contains(t, first, "c.SetContext(c.RequestCtx())")
	require.NotContains(t, first, ".UserContext()")
	require.NotContains(t, first, "SetUserContext")

	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))
	second := readFile(t, file)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, strings.Count(second, "uc := c.Context()"))
	assert.Equal(t, 2, strings.Count(second, ".RequestCtx()"))
	assert.Equal(t, 2, strings.Count(second, "SetContext("))
}

func Test_MigrateContextMethods_UserContextOnlyMultipleRuns(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtestuserctx")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    ctx := c.UserContext()
    _ = ctx
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))

	first := readFile(t, file)
	require.Contains(t, first, "ctx := c.Context()")
	require.NotContains(t, first, ".RequestCtx()")
	require.Contains(t, first, "// fiber:context-methods migrated")

	require.NoError(t, v3.MigrateContextMethods(cmd, dir, nil, nil))
	second := readFile(t, file)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, strings.Count(second, "ctx := c.Context()"))
	assert.Equal(t, 0, strings.Count(second, ".RequestCtx()"))
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

func Test_MigrateContextMethods_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateContextMethods, []ctxCase{
		{
			name: "the three accessors",
			in: `package main

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	rc := c.Context()
	uc := c.UserContext()
	c.SetUserContext(context.WithValue(uc, "k", "v"))
	rc.SetUserValue("k", "v")
	return c.Next()
}
`,
			want: `package main

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	rc := c.RequestCtx()
	uc := c.Context()
	c.SetContext(context.WithValue(uc, "k", "v"))
	rc.SetUserValue("k", "v")
	return c.Next()
}

// fiber:context-methods migrated
`,
			msg: "Migrating context methods",
		},
		{
			name: "chains, fields and acquired contexts",
			in: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type req struct{ c *fiber.Ctx }

func (r *req) host() string {
	return string(r.c.Status(fiber.StatusOK).Type("json").Context().Request.Host())
}

func (r *req) uc() any { return r.c.UserContext().Value("k") }

func TestAcquire(t *testing.T) {
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	_ = ctx.Context().Request.Header.Method()
	_ = ctx.UserContext()
}
`,
			want: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type req struct{ c *fiber.Ctx }

func (r *req) host() string {
	return string(r.c.Status(fiber.StatusOK).Type("json").RequestCtx().Request.Host())
}

func (r *req) uc() any { return r.c.Context().Value("k") }

func TestAcquire(t *testing.T) {
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	_ = ctx.RequestCtx().Request.Header.Method()
	_ = ctx.Context()
}

// fiber:context-methods migrated
`,
			msg: "Migrating context methods",
		},
		{
			name: "arguments with parentheses",
			in: `package main

import "github.com/gofiber/fiber/v2"

func status(s string) int { return 200 }

func handler(c *fiber.Ctx) error {
	c.Status(status("a)b")).Type("json").Context().SetBodyStreamWriter(nil)
	c.Status(status(")")).Context().SetStatusCode(200)
	return nil
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func status(s string) int { return 200 }

func handler(c *fiber.Ctx) error {
	c.Status(status("a)b")).Type("json").RequestCtx().SetBodyStreamWriter(nil)
	c.Status(status(")")).RequestCtx().SetStatusCode(200)
	return nil
}

// fiber:context-methods migrated
`,
			msg: "Migrating context methods",
		},
		{
			name: "aliased import",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	_ = c.Context()
	return c.UserContext().Err()
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	_ = c.RequestCtx()
	return c.Context().Err()
}

// fiber:context-methods migrated
`,
			msg: "Migrating context methods",
		},
		{
			name: "other receivers stay",
			in: `package main

import "github.com/gofiber/fiber/v2"

type scope struct{}

func (scope) SetContext(k, v string) {}
func (scope) Context() string         { return "" }

func handler(c *fiber.Ctx) error {
	var sc scope
	sc.SetContext("path", c.Path())
	_ = sc.Context()
	rc := c.Context()
	_ = rc
	return nil
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type scope struct{}

func (scope) SetContext(k, v string) {}
func (scope) Context() string        { return "" }

func handler(c *fiber.Ctx) error {
	var sc scope
	sc.SetContext("path", c.Path())
	_ = sc.Context()
	rc := c.RequestCtx()
	_ = rc
	return nil
}

// fiber:context-methods migrated
`,
			msg: "Migrating context methods",
		},
		{
			name: "a context that has SetContext already is the v3 one",
			in: `package main

import (
	"context"

	"github.com/gofiber/fiber/v3"
)

func handler(c fiber.Ctx) error {
	c.SetContext(context.Background())
	_ = c.Context()
	return nil
}
`,
			notes: []string{"main.go:11: this file calls SetContext on a fiber context"},
		},
		{
			name: "unknown receiver is reported",
			in: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	ctx := web.Current(c)
	_ = ctx.Context()
	_ = ctx.UserContext()
	return nil
}
`,
			notes: []string{"main.go:12: ctx.UserContext was renamed Context in Fiber v3"},
		},
		{
			name: "no fiber import",
			in: `package main

type ctx struct{}

func (ctx) UserContext()           {}
func (ctx) SetUserContext(any)     {}
func (ctx) Context()               {}
func handler(c ctx) {
	c.UserContext()
	c.SetUserContext(nil)
	c.Context()
}
`,
		},
	})
}
