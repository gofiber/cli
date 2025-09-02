package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateParserMethods(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mptest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    var v any
    c.BodyParser(&v)
    c.CookieParser(&v)
    c.ParamsParser(&v)
    c.QueryParser(&v)
    c.AllParams(&p)
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateParserMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Bind().Body(&v)")
	assert.Contains(t, content, ".Bind().Cookie(&v)")
	assert.Contains(t, content, ".Bind().URI(&v)")
	assert.Contains(t, content, ".Bind().Query(&v)")
	assert.Contains(t, buf.String(), "Migrating parser methods")
}

func Test_MigrateParserMethods_SkipNonFiber(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mptestskip")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
type ctx struct{}
func (ctx) BodyParser(any) {}
func (ctx) CookieParser(any) {}
func (ctx) ParamsParser(any) {}
func (ctx) QueryParser(any) {}
func handler(c ctx) {
    var v any
    c.BodyParser(&v)
    c.CookieParser(&v)
    c.ParamsParser(&v)
    c.QueryParser(&v)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateParserMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "c.BodyParser(&v)")
	assert.Contains(t, content, "c.CookieParser(&v)")
	assert.Contains(t, content, "c.ParamsParser(&v)")
	assert.Contains(t, content, "c.QueryParser(&v)")
	assert.NotContains(t, buf.String(), "Migrating parser methods")
}
