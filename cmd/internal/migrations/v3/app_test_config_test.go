package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateAppTestConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtestcfg")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "net/http/httptest"
)
func main() {
    app := fiber.New()
    req := httptest.NewRequest(fiber.MethodGet, "/", nil)
    _ = app.Test(req, 2000)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAppTestConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "app.Test(req, fiber.TestConfig{Timeout: time.Duration(2000) * time.Millisecond})")
	assert.Contains(t, content, "\"time\"")
	assert.Contains(t, buf.String(), "Migrating app.Test usages")
}

func Test_MigrateAppTestConfig_DisableTimeout(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtestcfg")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "net/http/httptest"
)
func main() {
    app := fiber.New()
    req := httptest.NewRequest(fiber.MethodGet, "/", nil)
    _ = app.Test(req, -1)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAppTestConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})`)
	assert.Contains(t, buf.String(), "Migrating app.Test usages")
}

func Test_MigrateAppTestConfig_NoSecondArg(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtestcfgsingle")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "net/http/httptest"
)
func main() {
    app := fiber.New()
    _, _ = app.Test(httptest.NewRequest("GET", "/", nil))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAppTestConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `app.Test(httptest.NewRequest("GET", "/", nil))`)
	assert.NotContains(t, content, "fiber.TestConfig")
	assert.NotContains(t, buf.String(), "Migrating app.Test usages")
}

func Test_MigrateAppTestConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtestcfgid")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "net/http/httptest"
)
func main() {
    app := fiber.New()
    req := httptest.NewRequest(fiber.MethodGet, "/", nil)
    timeoutMS := 2000
    _ = app.Test(req, timeoutMS)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAppTestConfig(cmd, dir, nil, nil))
	first := readFile(t, file)
	require.NoError(t, v3.MigrateAppTestConfig(cmd, dir, nil, nil))
	second := readFile(t, file)
	assert.Equal(t, first, second)
}

func Test_MigrateAppTestConfig_VariableTimeout(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtestcfgvar")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "net/http/httptest"
)
func main() {
    app := fiber.New()
    req := httptest.NewRequest(fiber.MethodGet, "/", nil)
    timeoutMS := 250
    _ = app.Test(req, timeoutMS)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateAppTestConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "app.Test(req, fiber.TestConfig{Timeout: time.Duration(timeoutMS) * time.Millisecond})")
}
