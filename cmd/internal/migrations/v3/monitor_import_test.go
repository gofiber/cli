package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateMonitorImport(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mmonitor")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/monitor"
var _ = monitor.New()`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMonitorImport(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/contrib/v3/monitor")
	assert.NotContains(t, content, "fiber/v2/middleware/monitor")
	assert.Contains(t, buf.String(), "Migrating monitor middleware import")
}

func Test_MigrateMonitorImport_Config(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/monitor"
)

func main() {
	app := fiber.New()
	app.Get("/metrics", monitor.New())
	app.Get("/metrics2", monitor.New(monitor.Config{
		Title:      "My Service Metrics Page",
		Refresh:    2 * time.Second,
		CustomHead: `+"`<style>body{background:#fff}</style>`"+`,
		FontURL:    "https://fonts.googleapis.com/css2?family=Roboto",
		ChartJsURL: "https://cdn.jsdelivr.net/npm/chart.js@2.9/dist/Chart.bundle.min.js",
	}))

	cfg := monitor.ConfigDefault
	cfg.APIOnly = true
	cfg.ChartJsURL = "https://example.com/chart.js"
	app.Get("/api/metrics", monitor.New(cfg))
}
`, v3.MigrateMonitorImport)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/contrib/v3/monitor"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Get("/metrics", monitor.New())
	app.Get("/metrics2", monitor.New(monitor.Config{
		Title:      "My Service Metrics Page",
		Refresh:    2 * time.Second,
		CustomHead: `+"`<style>body{background:#fff}</style>`"+`,
		FontURL:    "https://fonts.googleapis.com/css2?family=Roboto",
		ChartJSURL: "https://cdn.jsdelivr.net/npm/chart.js@2.9/dist/Chart.bundle.min.js",
	}))

	cfg := monitor.ConfigDefault
	cfg.APIOnly = true
	cfg.ChartJSURL = "https://example.com/chart.js"
	app.Get("/api/metrics", monitor.New(cfg))
}
`, got)

	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 2)
	assert.Equal(t, 13, res.notes[0].Line)
	assert.Contains(t, msgs[0], "monitor.Config ChartJSURL, CustomHead, FontURL")
	assert.Contains(t, msgs[0], "has no effect")
	assert.Equal(t, 23, res.notes[1].Line)
	assert.Contains(t, msgs[1], "monitor.Config ChartJSURL")
}

func Test_MigrateMonitorImport_KeepsTheAlias(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import mon "github.com/gofiber/fiber/v2/middleware/monitor"

var _ = mon.New(mon.Config{Title: "metrics", APIOnly: true})
`, v3.MigrateMonitorImport)

	assert.Equal(t, `package main

import mon "github.com/gofiber/contrib/v3/monitor"

var _ = mon.New(mon.Config{Title: "metrics", APIOnly: true})
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateMonitorImport_OnlyTheMonitorMiddleware(t *testing.T) {
	t.Parallel()

	src := `package main

import "example.com/project/metrics"

// "github.com/gofiber/fiber/v2/middleware/monitor" is the old home of the monitor.
var doc = "github.com/gofiber/fiber/v2/middleware/monitor"

var _ = metrics.Config{ChartJsURL: "https://example.com/chart.js"}
`
	got, res := mw3RunOne(t, src, v3.MigrateMonitorImport)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)
	assert.Empty(t, res.notes)
}

func Test_MigrateMonitorImport_Idempotent(t *testing.T) {
	t.Parallel()

	got, first := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/monitor"

var _ = monitor.New(monitor.Config{ChartJsURL: "https://example.com/chart.js"})
`, v3.MigrateMonitorImport)
	require.NotEmpty(t, first.out)
	assert.Contains(t, got, "ChartJSURL:")

	again, second := mw3RunOne(t, got, v3.MigrateMonitorImport)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
}
