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

func Test_MigrateHealthcheckConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhealth")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/healthcheck"
)
func main() {
    app := fiber.New()
    app.Use(healthcheck.New(healthcheck.Config{
        LivenessProbe: func(c fiber.Ctx) bool { return true },
        LivenessEndpoint: "/live",
        ReadinessProbe: func(c fiber.Ctx) bool { return true },
        ReadinessEndpoint: "/ready",
    }))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateHealthcheckConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "app.Get(\"/live\", healthcheck.New(healthcheck.Config{")
	assert.Contains(t, content, "app.Get(\"/ready\", healthcheck.New(healthcheck.Config{")
	assert.NotContains(t, content, "app.Use(")
	assert.Equal(t, 2, strings.Count(content, "Probe: func(c fiber.Ctx) bool { return true }"))
	assert.NotContains(t, content, "LivenessProbe")
	assert.NotContains(t, content, "ReadinessProbe")
	assert.NotContains(t, content, "LivenessEndpoint")
	assert.NotContains(t, content, "ReadinessEndpoint")
	assert.Contains(t, buf.String(), "Migrating healthcheck middleware configs")
}

func Test_MigrateHealthcheckConfig_Default(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhealthdef")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/healthcheck"
)
func main() {
    app := fiber.New()
    app.Use(healthcheck.New())
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateHealthcheckConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "app.Get(healthcheck.LivenessEndpoint, healthcheck.New())")
	assert.Contains(t, content, "app.Get(healthcheck.ReadinessEndpoint, healthcheck.New())")
	assert.NotContains(t, content, "app.Use(")
	assert.Contains(t, buf.String(), "Migrating healthcheck middleware configs")
}

func Test_MigrateHealthcheckConfig_LivenessOnly(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhealthlive")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/healthcheck"
)
func main() {
    app := fiber.New()
    app.Use(healthcheck.New(healthcheck.Config{
        LivenessProbe: func(c fiber.Ctx) bool { return true },
        LivenessEndpoint: "/live",
    }))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateHealthcheckConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "app.Get(\"/live\"")
	assert.Contains(t, content, "app.Get(healthcheck.ReadinessEndpoint, healthcheck.New(")
	assert.NotContains(t, content, "app.Use(")
	assert.NotContains(t, content, "LivenessProbe")
	assert.NotContains(t, content, "LivenessEndpoint")
	assert.Contains(t, buf.String(), "Migrating healthcheck middleware configs")
}

func Test_MigrateHealthcheckConfig_ReadinessOnly(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhealthready")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/healthcheck"
)
func main() {
    app := fiber.New()
    app.Use(healthcheck.New(healthcheck.Config{
        ReadinessProbe: func(c fiber.Ctx) bool { return true },
        ReadinessEndpoint: "/ready",
    }))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateHealthcheckConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "app.Get(healthcheck.LivenessEndpoint, healthcheck.New(")
	assert.Contains(t, content, "app.Get(\"/ready\"")
	assert.NotContains(t, content, "app.Use(")
	assert.NotContains(t, content, "ReadinessProbe")
	assert.NotContains(t, content, "ReadinessEndpoint")
	assert.Contains(t, buf.String(), "Migrating healthcheck middleware configs")
}

func Test_MigrateHealthcheckConfig_StandaloneConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhealthcfg")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/healthcheck"
)
func check(a, b int) bool { return a > b }
var cfg = healthcheck.Config{
    LivenessProbe: func(c fiber.Ctx) bool {
        return true
    },
    ReadinessProbe: func(c fiber.Ctx) bool {
        return check(1, 2)
    },
    LivenessEndpoint: "/live",
    ReadinessEndpoint: "/ready",
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateHealthcheckConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "healthcheck.Config{")
	assert.Contains(t, content, "Probe: func(c fiber.Ctx) bool {")
	assert.NotContains(t, content, "ReadinessProbe")
	assert.NotContains(t, content, "LivenessEndpoint")
	assert.NotContains(t, content, "ReadinessEndpoint")
	assert.Contains(t, buf.String(), "Migrating healthcheck middleware configs")
}

func Test_MigrateHealthcheckConfig_WithComments(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhealthc")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/healthcheck"
)
func main() {
    app := fiber.New()
    app.Use(healthcheck.New(healthcheck.Config{
        LivenessEndpoint: "/live", // live comment
        ReadinessEndpoint: "/ready", // ready comment
    }))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateHealthcheckConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `app.Get("/live"`)
	assert.Contains(t, content, `app.Get("/ready"`)
	assert.NotContains(t, content, "live comment")
	assert.NotContains(t, content, "ready comment")
}
