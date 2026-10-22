package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateTrustedProxyConfig(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

var trusted = []string{"10.0.0.0/8", "172.16.0.0/12"}

func guideExample() *fiber.App {
	return fiber.New(fiber.Config{
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.8.0.0", "127.0.0.0/8", "::1/128"},
	})
}

func multiLine() *fiber.App {
	return fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		TrustedProxies: []string{
			"10.0.0.0/8",
			"192.168.0.0/16",
		},
	})
}

func viaVariable() *fiber.App {
	return fiber.New(fiber.Config{EnableTrustedProxyCheck: true, TrustedProxies: trusted})
}

func fromEnv() *fiber.App {
	return fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		TrustedProxies:          strings.Split(os.Getenv("TRUSTED_PROXIES"), ","), // from the environment
	})
}

func single() *fiber.App {
	return fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.8.0.0"},
	})
}
`)

	out := appRun(t, dir, v3.MigrateTrustedProxyConfig)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "TrustProxy:       true,\n\t\tTrustProxyConfig: fiber.TrustProxyConfig{Proxies: []string{\"0.8.0.0\", \"127.0.0.0/8\", \"::1/128\"}},")
	assert.Contains(t, content, "TrustProxyConfig: fiber.TrustProxyConfig{Proxies: []string{\n\t\t\t\"10.0.0.0/8\",\n\t\t\t\"192.168.0.0/16\",\n\t\t}},")
	assert.Contains(t, content, "fiber.Config{TrustProxy: true, TrustProxyConfig: fiber.TrustProxyConfig{Proxies: trusted}}")
	assert.Contains(t, content, `TrustProxyConfig: fiber.TrustProxyConfig{Proxies: strings.Split(os.Getenv("TRUSTED_PROXIES"), ",")}, // from the environment`)
	assert.Contains(t, content, `TrustProxyConfig: fiber.TrustProxyConfig{Proxies: []string{"0.8.0.0"}},`)
	assert.NotContains(t, content, "TrustedProxies")
	assert.NotContains(t, content, "EnableTrustedProxyCheck")
	assert.Contains(t, out, "Migrating trusted proxy config")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateTrustedProxyConfig_Assignments(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func assigned() *fiber.App {
	cfg := fiber.Config{}
	cfg.EnableTrustedProxyCheck = true
	cfg.TrustedProxies = []string{"127.0.0.1"}
	cfg.TrustedProxies = append(cfg.TrustedProxies, "10.0.0.1")
	return fiber.New(cfg)
}

func inspect(app *fiber.App) (bool, []string) {
	return app.Config().EnableTrustedProxyCheck, app.Config().TrustedProxies
}

type options struct {
	TrustedProxies []string
}

func unrelated(o options) []string {
	o.TrustedProxies = append(o.TrustedProxies, "x")
	return o.TrustedProxies
}
`)

	appRun(t, dir, v3.MigrateTrustedProxyConfig)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "cfg.TrustProxy = true")
	assert.Contains(t, content, `cfg.TrustProxyConfig.Proxies = []string{"127.0.0.1"}`)
	assert.Contains(t, content, `cfg.TrustProxyConfig.Proxies = append(cfg.TrustProxyConfig.Proxies, "10.0.0.1")`)
	assert.Contains(t, content, "return app.Config().TrustProxy, app.Config().TrustProxyConfig.Proxies")
	// a struct of the project that has a field of the same name is not fiber.Config
	assert.Contains(t, content, "TrustedProxies []string")
	assert.Contains(t, content, `o.TrustedProxies = append(o.TrustedProxies, "x")`)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateTrustedProxyConfig_ProxyHeaderWithoutTheCheck(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func headerOnly() *fiber.App {
	return fiber.New(fiber.Config{ProxyHeader: fiber.HeaderXForwardedFor})
}

func listWithoutCheck() *fiber.App {
	return fiber.New(fiber.Config{
		TrustedProxies: []string{"10.0.0.1"},
	})
}

func assignedHeader() *fiber.App {
	cfg := fiber.Config{}
	cfg.ProxyHeader = "X-Real-IP"
	return fiber.New(cfg)
}

func checked() *fiber.App {
	cfg := fiber.Config{ProxyHeader: "X-Real-IP"}
	cfg.EnableTrustedProxyCheck = true
	return fiber.New(cfg)
}

func checkedLiteral() *fiber.App {
	return fiber.New(fiber.Config{ProxyHeader: "X-Real-IP", EnableTrustedProxyCheck: true})
}

func switchedOff() *fiber.App {
	return fiber.New(fiber.Config{ProxyHeader: "X-Real-IP", EnableTrustedProxyCheck: false})
}
`)

	appRun(t, dir, v3.MigrateTrustedProxyConfig)

	notes := internal.DrainNotes(dir)
	lines := make([]int, 0, len(notes))
	for _, n := range notes {
		lines = append(lines, n.Line)
	}
	assert.ElementsMatch(t, []int{6, 11, 17, 32}, lines)
	for _, n := range notes {
		switch n.Line {
		case 11:
			assert.Contains(t, n.Message, "TrustedProxies")
		default:
			assert.Contains(t, n.Message, "ProxyHeader")
		}
	}
}

func Test_MigrateTrustedProxyConfig_ExistingTrustProxyConfig(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func mixed() *fiber.App {
	return fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"10.0.0.1"},
		TrustProxyConfig:        fiber.TrustProxyConfig{Loopback: true},
	})
}
`)

	appRun(t, dir, v3.MigrateTrustedProxyConfig)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `TrustedProxies:   []string{"10.0.0.1"}`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, 8, notes[0].Line)
}

func Test_MigrateTrustedProxyConfig_OtherStructsAndComments(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

// Config of our own, with the same field names as fiber.Config.
type Config struct {
	EnableTrustedProxyCheck bool
	TrustedProxies          []string
}

func main() {
	// fiber.Config{EnableTrustedProxyCheck: true} used to be spelled like this
	_ = Config{EnableTrustedProxyCheck: true, TrustedProxies: []string{"a"}}
	_ = "TrustedProxies: []string{} EnableTrustedProxyCheck"
	_ = fiber.New()
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateTrustedProxyConfig)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateTrustedProxyConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	_ = fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.8.0.0", "127.0.0.0/8"},
	})
}
`)

	appRun(t, dir, v3.MigrateTrustedProxyConfig)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateTrustedProxyConfig)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}
