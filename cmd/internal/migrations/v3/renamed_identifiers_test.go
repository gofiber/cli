package v3_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// migrateRenamed runs MigrateRenamedIdentifiers on a file and returns its content and what the
// migration printed.
func migrateRenamed(t *testing.T, source string) (content, out string) {
	t.Helper()

	dir := t.TempDir()
	file := writeTempFile(t, dir, source)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateRenamedIdentifiers(newCmd(&buf), dir, nil, nil))
	return readFile(t, file), buf.String()
}

func Test_MigrateRenamedIdentifiers_FiberConstants(t *testing.T) {
	t.Parallel()

	content, out := migrateRenamed(t, `package main

import "github.com/gofiber/fiber/v3"

const guid = fiber.ConstraintGuid

func handler(c fiber.Ctx) error {
	c.Set(fiber.HeaderFeaturePolicy, "geolocation 'none'")
	return c.Next()
}
`)

	assert.Equal(t, `package main

import "github.com/gofiber/fiber/v3"

const guid = fiber.ConstraintGUID

func handler(c fiber.Ctx) error {
	c.Set("Feature-Policy", "geolocation 'none'")
	return c.Next()
}
`, content)
	assert.Contains(t, out, "Migrating identifiers that v3 renamed")
}

func Test_MigrateRenamedIdentifiers_AliasedImport(t *testing.T) {
	t.Parallel()

	content, _ := migrateRenamed(t, `package main

import fc "github.com/gofiber/fiber/v3"

const guid = fc.ConstraintGuid
`)

	assert.Contains(t, content, "fc.ConstraintGUID")
}

func Test_MigrateRenamedIdentifiers_IndexRune(t *testing.T) {
	t.Parallel()

	t.Run("adds the import of strings", func(t *testing.T) {
		t.Parallel()

		content, _ := migrateRenamed(t, `package main

import "github.com/gofiber/fiber/v3"

var _ = fiber.Version

func has(s string) bool {
	return fiber.IndexRune(s, 'a')
}
`)

		assert.Contains(t, content, "return strings.ContainsRune(s, 'a')")
		assert.Contains(t, content, `"strings"`)
		assert.NotContains(t, content, "IndexRune")
	})

	t.Run("uses the name the file gave strings", func(t *testing.T) {
		t.Parallel()

		content, _ := migrateRenamed(t, `package main

import (
	str "strings"

	"github.com/gofiber/fiber/v3"
)

var _ = fiber.Version
var _ = str.ToUpper

func has(s string) bool {
	return fiber.IndexRune(s, 'a')
}
`)

		assert.Contains(t, content, "return str.ContainsRune(s, 'a')")
	})
}

func Test_MigrateRenamedIdentifiers_GenerateKey(t *testing.T) {
	t.Parallel()

	content, _ := migrateRenamed(t, `package main

import "github.com/gofiber/fiber/v3/middleware/encryptcookie"

var key = encryptcookie.GenerateKey()
var other = encryptcookie.GenerateKey(16)
`)

	assert.Contains(t, content, "var key = encryptcookie.GenerateKey(32)")
	assert.Contains(t, content, "var other = encryptcookie.GenerateKey(16)", "a call with a length is v3 already")
}

func Test_MigrateRenamedIdentifiers_Healthcheck(t *testing.T) {
	t.Parallel()

	content, _ := migrateRenamed(t, `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
)

var probe healthcheck.HealthChecker

func paths() []string {
	return []string{healthcheck.DefaultLivenessEndpoint, healthcheck.DefaultReadinessEndpoint}
}

func custom(app *fiber.App, h healthcheck.HealthCheckerHandler) {
	app.Get("/x", h(probe))
}
`)

	assert.Contains(t, content, "var probe func(fiber.Ctx) bool")
	assert.Contains(t, content, "[]string{healthcheck.LivenessEndpoint, healthcheck.ReadinessEndpoint}")
	assert.Contains(t, content, "h func(func(fiber.Ctx) bool) fiber.Handler")
}

func Test_MigrateRenamedIdentifiers_Limiter(t *testing.T) {
	t.Parallel()

	content, _ := migrateRenamed(t, `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
)

type custom struct{ limiter.LimiterHandler }

func build(cfg limiter.Config) []fiber.Handler {
	return []fiber.Handler{
		limiter.FixedWindow{}.New(cfg),
		limiter.SlidingWindow{}.New(limiter.Config{Max: 3}),
		limiter.FixedWindow{}.New(&cfg),
	}
}
`)

	assert.Contains(t, content, "type custom struct{ limiter.Handler }")
	assert.Contains(t, content, "limiter.FixedWindow{}.New(&cfg),\n")
	assert.Contains(t, content, "limiter.SlidingWindow{}.New(&limiter.Config{Max: 3})")
	assert.NotContains(t, content, "&&", "a config that is a pointer already is left alone")
}

func Test_MigrateRenamedIdentifiers_ConfigFields(t *testing.T) {
	t.Parallel()

	t.Run("monitor", func(t *testing.T) {
		t.Parallel()

		content, _ := migrateRenamed(t, `package main

import "github.com/gofiber/fiber/v3/middleware/monitor"

var _ = monitor.Config{
	Title:      "metrics",
	ChartJsURL: "https://cdn.example.com/chart.js",
}
`)

		assert.Contains(t, content, "ChartJSURL: \"https://cdn.example.com/chart.js\"")
		assert.NotContains(t, content, "ChartJsURL")
	})

	t.Run("monitor after it moved to contrib", func(t *testing.T) {
		t.Parallel()

		content, _ := migrateRenamed(t, `package main

import "github.com/gofiber/contrib/v3/monitor"

var _ = monitor.Config{ChartJsURL: "https://cdn.example.com/chart.js"}
`)

		assert.Contains(t, content, "monitor.Config{ChartJSURL:")
	})

	t.Run("proxy", func(t *testing.T) {
		t.Parallel()

		content, _ := migrateRenamed(t, `package main

import (
	"crypto/tls"

	"github.com/gofiber/fiber/v3/middleware/proxy"
)

func cfg(tc *tls.Config) proxy.Config {
	return proxy.Config{
		Servers:   []string{"https://backend:8443"},
		TlsConfig: tc,
	}
}
`)

		assert.Contains(t, content, "TLSConfig: tc,")
		assert.NotContains(t, content, "TlsConfig")
	})
}

func Test_MigrateRenamedIdentifiers_CtxFormat(t *testing.T) {
	t.Parallel()

	content, _ := migrateRenamed(t, `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	if err := c.Format("hello"); err != nil {
		return err
	}
	return c.Status(201).Format(fiber.Map{"ok": true})
}
`)

	assert.Contains(t, content, `c.AutoFormat("hello")`)
	assert.Contains(t, content, "c.Status(201).AutoFormat(fiber.Map{")
	assert.NotContains(t, content, ".Format(")
}

// Names that merely look like the ones of Fiber stay as they are: another package with the
// same name, a method of another type, the Format of v3, comments and strings.
func Test_MigrateRenamedIdentifiers_LeavesWhatIsNotFibers(t *testing.T) {
	t.Parallel()

	source := `package main

import (
	"time"

	"example.com/mine/limiter"
	"github.com/gofiber/fiber/v3"
)

// fiber.ConstraintGuid and encryptcookie.GenerateKey() stay in a comment.
const doc = "fiber.HeaderFeaturePolicy and healthcheck.HealthChecker"

var _ limiter.LimiterHandler

func handler(c fiber.Ctx) error {
	_ = time.Now().Format(time.RFC3339)
	return c.Format(fiber.ResFmt{MediaType: "text/plain", Handler: nil})
}
`
	content, out := migrateRenamed(t, source)

	assert.Equal(t, source, content)
	assert.Empty(t, out)
}

func Test_MigrateRenamedIdentifiers_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/encryptcookie"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
)

const guid = fiber.ConstraintGuid

var key = encryptcookie.GenerateKey()

var path = healthcheck.DefaultLivenessEndpoint

func handler(c fiber.Ctx) error {
	return c.Format("hello")
}
`)

	var first bytes.Buffer
	require.NoError(t, v3.MigrateRenamedIdentifiers(newCmd(&first), dir, nil, nil))
	migrated := readFile(t, file)
	assert.NotEmpty(t, first.String())

	var second bytes.Buffer
	require.NoError(t, v3.MigrateRenamedIdentifiers(newCmd(&second), dir, nil, nil))
	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, second.String())
}

// proxy.New was the deprecated name of proxy.Balancer, which v3 dropped.
func Test_MigrateRenamedIdentifiers_ProxyNew(t *testing.T) {
	t.Parallel()

	content, _ := migrateRenamed(t, `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/proxy"
)

func setup(app *fiber.App) {
	app.Use(proxy.New(proxy.Config{Servers: []string{"http://localhost:3001"}}))
	app.Use(proxy.Balancer(proxy.Config{Servers: []string{"http://localhost:3002"}}))
}
`)

	assert.Contains(t, content, `app.Use(proxy.Balancer(proxy.Config{Servers: []string{"http://localhost:3001"}}))`)
	assert.Contains(t, content, `app.Use(proxy.Balancer(proxy.Config{Servers: []string{"http://localhost:3002"}}))`)
	assert.NotContains(t, content, "proxy.New")
}
