package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// proxySource wraps the body of a Setup function in a valid v2 file.
func proxySource(body string) string {
	return `package main

import (
	"crypto/tls"
	"crypto/x509"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/proxy"
)

func buildTLS() *tls.Config { return &tls.Config{MinVersion: tls.VersionTLS12} }

var _ = x509.NewCertPool
var _ = time.Second

func Setup(app *fiber.App, tc *tls.Config) {
` + body + `
}
`
}

const proxyDefaultClient = "NoDefaultUserAgentHeader: true, DisablePathNormalizing: true"

func Test_MigrateProxyTLSConfig_WithTlsConfig(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateProxyTLSConfig, proxySource(`	proxy.WithTlsConfig(&tls.Config{InsecureSkipVerify: true})
	proxy.WithTlsConfig(tc)
	proxy.WithTlsConfig(buildTLS())
	proxy.WithTlsConfig(&tls.Config{
		RootCAs:    x509.NewCertPool(),
		MinVersion: tls.VersionTLS12,
	})
	app.Get("/gif", proxy.Forward("https://i.imgur.com/IWaBepg.gif"))`))

	assert.Contains(t, content, "proxy.WithClient(&fasthttp.Client{"+proxyDefaultClient+", TLSConfig: &tls.Config{InsecureSkipVerify: true}})")
	assert.Contains(t, content, "proxy.WithClient(&fasthttp.Client{"+proxyDefaultClient+", TLSConfig: tc})")
	assert.Contains(t, content, "proxy.WithClient(&fasthttp.Client{"+proxyDefaultClient+", TLSConfig: buildTLS()})")
	assert.Contains(t, content, "TLSConfig: &tls.Config{\n\t\tRootCAs:    x509.NewCertPool(),\n\t\tMinVersion: tls.VersionTLS12,\n\t}})")
	assert.NotContains(t, content, "WithTlsConfig")
	assert.Contains(t, content, `"github.com/valyala/fasthttp"`)
	assert.Contains(t, run.out, "Migrating proxy TLS config")
	assert.Empty(t, run.notes)
}

func Test_MigrateProxyTLSConfig_ReusesTheFasthttpImport(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateProxyTLSConfig, `package main

import (
	"crypto/tls"

	"github.com/gofiber/fiber/v2/middleware/proxy"
	fh "github.com/valyala/fasthttp"
)

var _ = fh.StatusOK

func Setup(tc *tls.Config) {
	proxy.WithTlsConfig(tc)
}
`)

	assert.Contains(t, content, "proxy.WithClient(&fh.Client{")
	assert.Equal(t, 1, countImports(content, "github.com/valyala/fasthttp"))
}

func Test_MigrateProxyTLSConfig_ConfigField(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateProxyTLSConfig, proxySource(`	app.Use(proxy.Balancer(proxy.Config{
		Servers: []string{
			"https://backend1.internal:8443",
			"https://backend2.internal:8443",
		},
		TlsConfig: &tls.Config{InsecureSkipVerify: true},
	}))
	cfg := proxy.Config{
		Servers:   []string{"https://backend1.internal:8443"},
		TlsConfig: tc,
	}
	cfg.TlsConfig = buildTLS()
	var other proxy.Config
	other.TlsConfig = tc
	app.Use("/api", proxy.Balancer(cfg, other))
	_ = proxy.ConfigDefault.TlsConfig`))

	assert.Contains(t, content, "TLSConfig: &tls.Config{InsecureSkipVerify: true},")
	assert.Contains(t, content, "TLSConfig: tc,")
	assert.Contains(t, content, "cfg.TLSConfig = buildTLS()")
	assert.Contains(t, content, "other.TLSConfig = tc")
	assert.NotContains(t, content, "TlsConfig")
	assert.Contains(t, run.out, "Migrating proxy TLS config")
}

func Test_MigrateProxyTLSConfig_New(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateProxyTLSConfig, proxySource(`	app.Use(proxy.New(proxy.Config{
		Servers: []string{"http://localhost:3001", "http://localhost:3002"},
	}))`))

	assert.Contains(t, content, "app.Use(proxy.Balancer(proxy.Config{")
	assert.NotContains(t, content, "proxy.New(")
}

func Test_MigrateProxyTLSConfig_Alias(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateProxyTLSConfig, `package main

import (
	"crypto/tls"

	"github.com/gofiber/fiber/v2"
	px "github.com/gofiber/fiber/v2/middleware/proxy"
)

func Setup(app *fiber.App) {
	px.WithTlsConfig(&tls.Config{InsecureSkipVerify: true})
	app.Use(px.Balancer(px.Config{
		Servers:   []string{"https://backend1.internal:8443"},
		TlsConfig: &tls.Config{InsecureSkipVerify: true},
	}))
	app.Use(px.New(px.Config{Servers: []string{"http://localhost:3001"}}))
}
`)

	assert.Contains(t, content, "px.WithClient(&fasthttp.Client{")
	assert.Contains(t, content, "TLSConfig: &tls.Config{InsecureSkipVerify: true},")
	assert.Contains(t, content, "px.Balancer(px.Config{Servers: []string{\"http://localhost:3001\"}})")
	assert.NotContains(t, content, "TlsConfig")
}

func Test_MigrateProxyTLSConfig_LeavesTheRestAlone(t *testing.T) {
	t.Parallel()

	src := proxySource(`	app.Get("/a", func(c *fiber.Ctx) error { return proxy.Do(c, "http://localhost:3001/a") })
	app.Get("/b", proxy.Forward("http://localhost:3001/e"))
	app.Use(proxy.Balancer(proxy.Config{
		Servers: []string{"http://localhost:3001"},
		Timeout: 5 * time.Second,
	}))`)
	content, run := mw2Single(t, v3.MigrateProxyTLSConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateProxyTLSConfig_IgnoresOtherPackages(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"crypto/tls"

	"example.com/proxy"
)

type backend struct{ TlsConfig *tls.Config }

func main() {
	proxy.WithTlsConfig(nil)
	_ = proxy.New(proxy.Config{TlsConfig: nil})
	_ = backend{TlsConfig: nil}
}
`
	content, run := mw2Single(t, v3.MigrateProxyTLSConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateProxyTLSConfig_Idempotent(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateProxyTLSConfig, map[string]string{"main.go": proxySource(`	proxy.WithTlsConfig(tc)
	app.Use(proxy.New(proxy.Config{Servers: []string{"http://localhost:3001"}, TlsConfig: tc}))`)})
	first := run.file(t, "main.go")
	assert.Contains(t, run.out, "Migrating proxy TLS config")

	second := mw2Again(t, v3.MigrateProxyTLSConfig, run.dir)

	assert.Equal(t, first, second.file(t, "main.go"))
	assert.Empty(t, second.out)
}
