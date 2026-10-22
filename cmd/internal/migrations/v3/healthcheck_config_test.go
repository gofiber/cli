package v3_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const hcImports = `import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
)
`

const hcImportsLogger = `import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/logger"
)
`

func Test_MigrateHealthcheckConfig(t *testing.T) {
	t.Parallel()

	// the example of the v2 documentation, with the code that follows it
	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImportsLogger+`
func main() {
	app := fiber.New()

	app.Use(healthcheck.New(healthcheck.Config{
		LivenessProbe: func(c *fiber.Ctx) bool {
			return true
		},
		LivenessEndpoint: "/live",
		ReadinessProbe: func(c *fiber.Ctx) bool {
			return serviceA.Ready() && serviceB.Ready()
		},
		ReadinessEndpoint: "/ready",
	}))

	app.Use(logger.New())
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("hello") })
	_ = app.Listen(":3000")
}
`)

	assert.Equal(t, `package main

`+hcImportsLogger+`
func main() {
	app := fiber.New()

	app.Get("/live", healthcheck.New(healthcheck.Config{
		Probe: func(c *fiber.Ctx) bool {
			return true
		},
	}))
	app.Get("/ready", healthcheck.New(healthcheck.Config{
		Probe: func(c *fiber.Ctx) bool {
			return serviceA.Ready() && serviceB.Ready()
		},
	}))

	app.Use(logger.New())
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("hello") })
	_ = app.Listen(":3000")
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating healthcheck middleware configs")
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_DefaultKeepsTheRestOfTheFile(t *testing.T) {
	t.Parallel()

	// the statements after the Use call used to be pasted into both routes
	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	app := fiber.New()
	app.Use(healthcheck.New())
	app.Use(logger.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("hello")
	})

	log.Fatal(app.Listen(":3000"))
}
`)

	assert.Equal(t, `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	app := fiber.New()
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New())
	app.Get(healthcheck.ReadinessEndpoint, healthcheck.New())
	app.Use(logger.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("hello")
	})

	log.Fatal(app.Listen(":3000"))
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating healthcheck middleware configs")
}

func Test_MigrateHealthcheckConfig_OneProbe(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImports+`
func main() {
	app := fiber.New()
	app.Use(healthcheck.New(healthcheck.Config{
		LivenessProbe:    func(c *fiber.Ctx) bool { return true },
		LivenessEndpoint: "/live",
	}))
	app.Use(healthcheck.New(healthcheck.Config{
		ReadinessProbe:    func(c *fiber.Ctx) bool { return false },
		ReadinessEndpoint: "/ready",
	}))
}
`)

	assert.Equal(t, `package main

`+hcImports+`
func main() {
	app := fiber.New()
	app.Get("/live", healthcheck.New(healthcheck.Config{
		Probe: func(c *fiber.Ctx) bool { return true },
	}))
	app.Get(healthcheck.ReadinessEndpoint, healthcheck.New())
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New())
	app.Get("/ready", healthcheck.New(healthcheck.Config{
		Probe: func(c *fiber.Ctx) bool { return false },
	}))
}
`, res.file())
}

func Test_MigrateHealthcheckConfig_NextGoesToBothRoutes(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImports+`
func main() {
	app := fiber.New()
	app.Use(healthcheck.New(healthcheck.Config{Next: func(c *fiber.Ctx) bool { return c.Query("skip") != "" }}))
}
`)

	assert.Equal(t, `package main

`+hcImports+`
func main() {
	app := fiber.New()
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New(healthcheck.Config{Next: func(c *fiber.Ctx) bool { return c.Query("skip") != "" }}))
	app.Get(healthcheck.ReadinessEndpoint, healthcheck.New(healthcheck.Config{Next: func(c *fiber.Ctx) bool { return c.Query("skip") != "" }}))
}
`, res.file())
}

func Test_MigrateHealthcheckConfig_EmptyEndpointIsTheDefault(t *testing.T) {
	t.Parallel()

	// v2 replaced an empty endpoint with its default
	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImports+`
func main() {
	app := fiber.New()
	app.Use(healthcheck.New(healthcheck.Config{LivenessEndpoint: "", ReadinessEndpoint: "/up"}))
}
`)

	file := res.file()
	assert.Contains(t, file, `app.Get(healthcheck.LivenessEndpoint, healthcheck.New())`)
	assert.Contains(t, file, `app.Get("/up", healthcheck.New())`)
}

func Test_MigrateHealthcheckConfig_Receivers(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImportsLogger+`
type Server struct{ app *fiber.App }

func (s *Server) routes() {
	s.app.Use(healthcheck.New())
	s.app.Get("/hello", func(c *fiber.Ctx) error { return c.SendString("hello") })
}

func groups(app *fiber.App) {
	api := app.Group("/api")
	api.Use(healthcheck.New())
	app.Group("/ops").Use(healthcheck.New())
}

func prefixed(app *fiber.App) {
	app.Use("/health", healthcheck.New())
	app.Use("/status/", healthcheck.New(healthcheck.Config{LivenessEndpoint: "/alive"}))
	app.Use(prefix, healthcheck.New())
}

const prefix = "/internal"

func other(app *fiber.App) {
	app.Use(logger.New(), healthcheck.New(), logger.New())
	app.Use("/x", logger.New(), healthcheck.New())
	if app != nil {
		app.Use(healthcheck.New())
	}
	switch {
	case true:
		app.Use(healthcheck.New())
	}
}
`)

	assert.Equal(t, `package main

`+hcImportsLogger+`
type Server struct{ app *fiber.App }

func (s *Server) routes() {
	s.app.Get(healthcheck.LivenessEndpoint, healthcheck.New())
	s.app.Get(healthcheck.ReadinessEndpoint, healthcheck.New())
	s.app.Get("/hello", func(c *fiber.Ctx) error { return c.SendString("hello") })
}

func groups(app *fiber.App) {
	api := app.Group("/api")
	api.Get(healthcheck.LivenessEndpoint, healthcheck.New())
	api.Get(healthcheck.ReadinessEndpoint, healthcheck.New())
	app.Group("/ops").Get(healthcheck.LivenessEndpoint, healthcheck.New()).Get(healthcheck.ReadinessEndpoint, healthcheck.New())
}

func prefixed(app *fiber.App) {
	app.Get("/health"+healthcheck.LivenessEndpoint, healthcheck.New())
	app.Get("/health"+healthcheck.ReadinessEndpoint, healthcheck.New())
	app.Get("/status/alive", healthcheck.New())
	app.Get("/status"+healthcheck.ReadinessEndpoint, healthcheck.New())
	app.Get(prefix+healthcheck.LivenessEndpoint, healthcheck.New())
	app.Get(prefix+healthcheck.ReadinessEndpoint, healthcheck.New())
}

const prefix = "/internal"

func other(app *fiber.App) {
	app.Use(logger.New())
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New())
	app.Get(healthcheck.ReadinessEndpoint, healthcheck.New())
	app.Use(logger.New())
	app.Use("/x", logger.New())
	app.Get("/x"+healthcheck.LivenessEndpoint, healthcheck.New())
	app.Get("/x"+healthcheck.ReadinessEndpoint, healthcheck.New())
	if app != nil {
		app.Get(healthcheck.LivenessEndpoint, healthcheck.New())
		app.Get(healthcheck.ReadinessEndpoint, healthcheck.New())
	}
	switch {
	case true:
		app.Get(healthcheck.LivenessEndpoint, healthcheck.New())
		app.Get(healthcheck.ReadinessEndpoint, healthcheck.New())
	}
}
`, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ExpressionContext(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImports+`
func setup(app *fiber.App) fiber.Router {
	return app.Use(healthcheck.New())
}

func oneLine(app *fiber.App) {
	if true { app.Use(healthcheck.New()) }
}
`)

	file := res.file()
	assert.Contains(t, file, `return app.Get(healthcheck.LivenessEndpoint, healthcheck.New()).Get(healthcheck.ReadinessEndpoint, healthcheck.New())`)
	assert.Contains(t, file, `app.Get(healthcheck.LivenessEndpoint, healthcheck.New()).Get(healthcheck.ReadinessEndpoint, healthcheck.New())`)
	assert.NotContains(t, file, ".Use(")
}

func Test_MigrateHealthcheckConfig_Alias(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

import (
	f "github.com/gofiber/fiber/v2"
	hc "github.com/gofiber/fiber/v2/middleware/healthcheck"
)

var ready = func(c *f.Ctx) bool { return true }

func main() {
	app := f.New()
	app.Use(hc.New(hc.Config{ReadinessProbe: ready, ReadinessEndpoint: "/ready"}))
}
`)

	assert.Contains(t, res.file(), `app.Get(hc.LivenessEndpoint, hc.New())`)
	assert.Contains(t, res.file(), `app.Get("/ready", hc.New(hc.Config{Probe: ready}))`)
}

func Test_MigrateHealthcheckConfig_ConfigInAPackageLevelVariable(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
)

// the config is built once and reused
var hcCfg = healthcheck.Config{
	LivenessProbe:     func(c *fiber.Ctx) bool { return true },
	ReadinessProbe:    func(c *fiber.Ctx) bool { return false }, // e.g. database not ready yet
	LivenessEndpoint:  "/live",
	ReadinessEndpoint: "/ready",
}

func main() {
	app := fiber.New()
	app.Use(healthcheck.New(hcCfg))
}
`)

	assert.Equal(t, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
)

// the config is built once and reused
var hcCfg = healthcheck.Config{
	Probe: func(c *fiber.Ctx) bool { return true },
}

var hcCfgReadiness = healthcheck.Config{
	Probe: func(c *fiber.Ctx) bool { return false }, // e.g. database not ready yet
}

func main() {
	app := fiber.New()
	app.Get("/live", healthcheck.New(hcCfg))
	app.Get("/ready", healthcheck.New(hcCfgReadiness))
}
`, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ConfigInALocalVariable(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImports+`
func main() {
	app := fiber.New()
	cfg := healthcheck.Config{
		ReadinessProbe: func(c *fiber.Ctx) bool { return false },
	}
	app.Use(healthcheck.New(cfg))
}
`)

	assert.Equal(t, `package main

`+hcImports+`
func main() {
	app := fiber.New()
	cfg := healthcheck.Config{}
	cfgReadiness := healthcheck.Config{
		Probe: func(c *fiber.Ctx) bool { return false },
	}
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New(cfg))
	app.Get(healthcheck.ReadinessEndpoint, healthcheck.New(cfgReadiness))
}
`, res.file())
}

func Test_MigrateHealthcheckConfig_SharedConfigWithoutProbes(t *testing.T) {
	t.Parallel()

	// nothing in the config is specific to a probe: both routes share it
	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImports+`
var skip = healthcheck.Config{Next: func(c *fiber.Ctx) bool { return false }}

func main() {
	app := fiber.New()
	app.Use(healthcheck.New(skip))
}
`)

	assert.Contains(t, res.file(), `app.Get(healthcheck.LivenessEndpoint, healthcheck.New(skip))`)
	assert.Contains(t, res.file(), `app.Get(healthcheck.ReadinessEndpoint, healthcheck.New(skip))`)
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ConfigThatCannotBeSplit(t *testing.T) {
	t.Parallel()

	src := `package main

` + hcImports + `
var shared = healthcheck.Config{
	LivenessProbe: func(c *fiber.Ctx) bool { return true },
}

func first(app *fiber.App)  { app.Use(healthcheck.New(shared)) }
func second(app *fiber.App) { app.Use(healthcheck.New(shared)) }

func external(app *fiber.App) { app.Use(healthcheck.New(buildConfig())) }
`
	res := mw1Run(t, v3.MigrateHealthcheckConfig, src)

	// the v2 code stays, so that the compiler points at it
	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	notes := strings.Join(res.notes, "\n")
	if assert.Len(t, res.notes, 4) {
		assert.Contains(t, notes, fmt.Sprintf("main.go:%d: healthcheck.Config has no LivenessProbe", mw1LineOf(t, src, "var shared")))
		assert.Contains(t, notes, fmt.Sprintf("main.go:%d: healthcheck.New() was left as it is: its config shared is used in more places than this one", mw1LineOf(t, src, "func first")))
		assert.Contains(t, notes, fmt.Sprintf("main.go:%d: ", mw1LineOf(t, src, "func second")))
		assert.Contains(t, notes, "its config is not a literal")
	}
}

func Test_MigrateHealthcheckConfig_HandlerKeptInAVariable(t *testing.T) {
	t.Parallel()

	src := `package main

` + hcImports + `
func main() {
	app := fiber.New()
	h := healthcheck.New()
	app.Use(h)
	app.Group("/g", healthcheck.New())
}
`
	res := mw1Run(t, v3.MigrateHealthcheckConfig, src)

	assert.Equal(t, src, res.file())
	if assert.Len(t, res.notes, 2) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: healthcheck.New() was left as it is: the healthcheck handler is kept in the variable h", mw1LineOf(t, src, "app.Use(h)")))
		assert.Contains(t, res.notes[1], fmt.Sprintf("main.go:%d: ", mw1LineOf(t, src, "app.Group")))
	}
}

func Test_MigrateHealthcheckConfig_StandaloneV2Config(t *testing.T) {
	t.Parallel()

	src := `package main

` + hcImports + `
func config() healthcheck.Config {
	return healthcheck.Config{
		LivenessProbe:    func(c *fiber.Ctx) bool { return true },
		LivenessEndpoint: "/live",
	}
}
`
	res := mw1Run(t, v3.MigrateHealthcheckConfig, src)

	assert.Equal(t, src, res.file())
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: healthcheck.Config has no LivenessProbe", mw1LineOf(t, src, "return healthcheck.Config")))
	}
}

func Test_MigrateHealthcheckConfig_RenamedMembers(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
)

var probe healthcheck.HealthChecker = func(c *fiber.Ctx) bool { return true }

func paths() []string {
	return []string{healthcheck.DefaultLivenessEndpoint, healthcheck.DefaultReadinessEndpoint}
}

func custom(app *fiber.App, h healthcheck.HealthCheckerHandler) {
	app.Get("/x", h(probe))
}

func convert(f func(*fiber.Ctx) bool) healthcheck.HealthChecker {
	return healthcheck.HealthChecker(f)
}
`)

	assert.Equal(t, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
)

var probe func(fiber.Ctx) bool = func(c *fiber.Ctx) bool { return true }

func paths() []string {
	return []string{healthcheck.LivenessEndpoint, healthcheck.ReadinessEndpoint}
}

func custom(app *fiber.App, h func(func(fiber.Ctx) bool) fiber.Handler) {
	app.Get("/x", h(probe))
}

func convert(f func(*fiber.Ctx) bool) func(fiber.Ctx) bool {
	return (func(fiber.Ctx) bool)(f)
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating healthcheck middleware configs")
}

func Test_MigrateHealthcheckConfig_RenamedMembersNeedTheFiberImport(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

import "github.com/gofiber/fiber/v2/middleware/healthcheck"

var probe healthcheck.HealthChecker
`)

	assert.Contains(t, res.file(), `"github.com/gofiber/fiber/v3"`)
	assert.Contains(t, res.file(), `var probe func(fiber.Ctx) bool`)
}

func Test_MigrateHealthcheckConfig_IgnoresOtherCode(t *testing.T) {
	t.Parallel()

	// a struct of the application with the same field names, and a package of
	// the project that is called healthcheck
	src := `package main

import (
	"example.com/app/healthcheck"
	"github.com/gofiber/fiber/v2"
)

type HealthSettings struct {
	LivenessEndpoint  string
	ReadinessEndpoint string
	ReadinessProbe    func() bool
	LivenessProbe     func() bool
}

func settings(app *fiber.App) HealthSettings {
	app.Use(healthcheck.New())
	_ = healthcheck.DefaultLivenessEndpoint
	return HealthSettings{LivenessEndpoint: "/healthz", ReadinessEndpoint: "/ready", ReadinessProbe: func() bool { return true }}
}
`
	res := mw1Run(t, v3.MigrateHealthcheckConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)

	// and the same struct in a file that imports the real package
	res = mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImports+`
type HealthSettings struct {
	LivenessEndpoint string
	LivenessProbe    func() bool
}

func settings() HealthSettings {
	return HealthSettings{LivenessEndpoint: "/healthz", LivenessProbe: func() bool { return true }}
}
`)
	assert.Contains(t, res.file(), `HealthSettings{LivenessEndpoint: "/healthz", LivenessProbe: func() bool { return true }}`)
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_V3CodeIsLeftAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
)

func main() {
	app := fiber.New()
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New())
	app.Get("/ready", healthcheck.New(healthcheck.Config{
		Probe: func(c fiber.Ctx) bool { return true },
	}))
}
`
	res := mw1Run(t, v3.MigrateHealthcheckConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateHealthcheckConfig, `package main

`+hcImports+`
var hcCfg = healthcheck.Config{
	LivenessProbe:  func(c *fiber.Ctx) bool { return true },
	ReadinessProbe: func(c *fiber.Ctx) bool { return false },
}

func main() {
	app := fiber.New()
	app.Use(healthcheck.New(hcCfg))
	app.Use("/health", healthcheck.New(healthcheck.Config{LivenessEndpoint: "/up"}))
	app.Use(healthcheck.New())
}
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateHealthcheckConfig, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
	assert.Empty(t, second.notes)
}
