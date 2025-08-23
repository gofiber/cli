package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateAddMethod(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "maddtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New()
    app.Add(fiber.MethodGet, "/foo", func(c fiber.Ctx) error { return nil })
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAddMethod(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `Add([]string{fiber.MethodGet}, "/foo"`)
	assert.Contains(t, buf.String(), "Migrating Add method calls")
}

func Test_MigrateAddMethod_NestedCall(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "maddnested")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func merge(a, b string) string { return a + b }
func main() {
    app := fiber.New()
    app.Add(merge("GE", "T"), "/foo", func(c fiber.Ctx) error { return nil })
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAddMethod(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `Add([]string{merge("GE", "T")}, "/foo"`)
	assert.Contains(t, buf.String(), "Migrating Add method calls")
}

func Test_MigrateAddMethod_SkipUnrelated(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "maddskip")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
func main() {
    req.Header.Add("Authorization", "Bearer "+test.Token)
    c.Response().Header.Add("X-Key", "Value")
    httpServerActiveRequests.Add(1)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAddMethod(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `req.Header.Add("Authorization", "Bearer "+test.Token)`)
	assert.Contains(t, content, `c.Response().Header.Add("X-Key", "Value")`)
	assert.Contains(t, content, `httpServerActiveRequests.Add(1)`)
	assert.NotContains(t, content, "[]string{")
	assert.Contains(t, buf.String(), "Migrating Add method calls")
}

func Test_MigrateAddMethod_Idempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "maddid")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New()
    app.Add(fiber.MethodGet, "/foo", func(c fiber.Ctx) error { return nil })
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAddMethod(cmd, dir, nil, nil))
	first := readFile(t, file)
	require.NoError(t, v3.MigrateAddMethod(cmd, dir, nil, nil))
	second := readFile(t, file)
	assert.Equal(t, first, second)
}
