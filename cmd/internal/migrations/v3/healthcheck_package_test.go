package v3_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The config of the middleware is a variable of the package, declared in another
// file than the Use call that registers it.

const hcPkgConfig = `package main

` + hcImports + `
// healthConfig answers the probes of the orchestrator.
var healthConfig = healthcheck.Config{
	LivenessProbe:     func(c *fiber.Ctx) bool { return true },
	ReadinessProbe:    func(c *fiber.Ctx) bool { return ready() },
	ReadinessEndpoint: "/ready",
}

func ready() bool { return true }
`

const hcPkgMain = `package main

` + hcImports + `
func main() {
	app := fiber.New()
	app.Use(healthcheck.New(healthConfig))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("hello") })
}
`

func Test_MigrateHealthcheckConfig_ConfigDeclaredInAnotherFile(t *testing.T) {
	t.Parallel()

	// whichever of the two files is visited first
	for _, declFile := range []string{"a_config.go", "z_config.go"} {
		t.Run(declFile, func(t *testing.T) {
			t.Parallel()

			res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, map[string]string{"main.go": hcPkgMain, declFile: hcPkgConfig})

			assert.Equal(t, `package main

`+hcImports+`
func main() {
	app := fiber.New()
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New(healthConfig))
	app.Get("/ready", healthcheck.New(healthConfigReadiness))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("hello") })
}
`, res.files["main.go"])
			assert.Equal(t, `package main

`+hcImports+`
// healthConfig answers the probes of the orchestrator.
var healthConfig = healthcheck.Config{
	Probe: func(c *fiber.Ctx) bool { return true },
}

var healthConfigReadiness = healthcheck.Config{
	Probe: func(c *fiber.Ctx) bool { return ready() },
}

func ready() bool { return true }
`, res.files[declFile])
			assert.Contains(t, res.stdout, "Migrating healthcheck middleware configs")
			assert.Empty(t, res.notes)

			// the second run has nothing left to do
			again := mw1RunFiles(t, v3.MigrateHealthcheckConfig, res.files)
			assert.Equal(t, res.files, again.files)
			assert.Empty(t, again.stdout)
			assert.Empty(t, again.notes)
		})
	}
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileWithAliases(t *testing.T) {
	t.Parallel()

	// each file calls the packages by names of its own
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, map[string]string{
		"main.go": `package main

import (
	f "github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
)

func main() {
	app := f.New()
	api := app.Group("/api")
	api.Use(healthcheck.New(probes))
}
`,
		"probes.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	hc "github.com/gofiber/fiber/v2/middleware/healthcheck"
)

var probes = hc.Config{
	LivenessProbe:    func(c *fiber.Ctx) bool { return true },
	LivenessEndpoint: "/live",
	ReadinessProbe:   func(c *fiber.Ctx) bool { return false },
}
`,
	})

	assert.Contains(t, res.files["main.go"], `api.Get("/live", healthcheck.New(probes))`)
	assert.Contains(t, res.files["main.go"], `api.Get(healthcheck.ReadinessEndpoint, healthcheck.New(probesReadiness))`)
	assert.NotContains(t, res.files["main.go"], ".Use(")
	assert.Equal(t, `package main

import (
	"github.com/gofiber/fiber/v2"
	hc "github.com/gofiber/fiber/v2/middleware/healthcheck"
)

var probes = hc.Config{
	Probe: func(c *fiber.Ctx) bool { return true },
}

var probesReadiness = hc.Config{
	Probe: func(c *fiber.Ctx) bool { return false },
}
`, res.files["probes.go"])
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileInAGroupOfVariables(t *testing.T) {
	t.Parallel()

	// a prefix, a path that is a constant of the package, and a declaration
	// among others
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, map[string]string{
		"main.go": `package main

` + hcImports + `
func main() {
	app := fiber.New()
	app.Use("/health/", healthcheck.New(settings))
}
`,
		"settings.go": `package main

` + hcImports + `
const livePath = "/up"

var (
	name     = "service"
	settings = healthcheck.Config{
		Next:             func(c *fiber.Ctx) bool { return c.Query("skip") != "" },
		LivenessEndpoint: livePath,
		ReadinessProbe:   func(c *fiber.Ctx) bool { return false },
	}
	other = 1
)
`,
	})

	assert.Contains(t, res.files["main.go"], `app.Get("/health"+livePath, healthcheck.New(settings))`)
	assert.Contains(t, res.files["main.go"], `app.Get("/health"+healthcheck.ReadinessEndpoint, healthcheck.New(settingsReadiness))`)
	assert.Equal(t, `package main

`+hcImports+`
const livePath = "/up"

var (
	name     = "service"
	settings = healthcheck.Config{
		Next: func(c *fiber.Ctx) bool { return c.Query("skip") != "" },
	}
	settingsReadiness = healthcheck.Config{
		Next:  settings.Next,
		Probe: func(c *fiber.Ctx) bool { return false },
	}
	other = 1
)
`, res.files["settings.go"])
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileKeepsTheDefaultEndpoints(t *testing.T) {
	t.Parallel()

	// the constants of the package are written with the name each file gives it
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	probe "github.com/gofiber/fiber/v2/middleware/healthcheck"
)

func main() {
	app := fiber.New()
	app.Use(probe.New(cfg))
}
`,
		"cfg.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
)

var cfg = healthcheck.Config{
	LivenessProbe:     func(c *fiber.Ctx) bool { return true },
	LivenessEndpoint:  healthcheck.DefaultReadinessEndpoint,
	ReadinessEndpoint: healthcheck.DefaultLivenessEndpoint + "/ready",
}
`,
	})

	assert.Contains(t, res.files["main.go"], `app.Get(probe.ReadinessEndpoint, probe.New(cfg))`)
	assert.Contains(t, res.files["main.go"], `app.Get(probe.LivenessEndpoint+"/ready", probe.New(cfgReadiness))`)
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileWithoutProbes(t *testing.T) {
	t.Parallel()

	// nothing in the config is specific to a probe: the declaration stays, and the
	// routes share it, however many Use calls there are
	files := map[string]string{
		"main.go": `package main

` + hcImports + `
func main() {
	app := fiber.New()
	app.Use(healthcheck.New(skip))
	app.Use("/other", healthcheck.New(skip))
}
`,
		"skip.go": `package main

` + hcImports + `
var skip = healthcheck.Config{Next: func(c *fiber.Ctx) bool { return false }}
`,
	}
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, files)

	assert.Equal(t, files["skip.go"], res.files["skip.go"])
	assert.Contains(t, res.files["main.go"], `app.Get(healthcheck.LivenessEndpoint, healthcheck.New(skip))`)
	assert.Contains(t, res.files["main.go"], `app.Get(healthcheck.ReadinessEndpoint, healthcheck.New(skip))`)
	assert.Contains(t, res.files["main.go"], `app.Get("/other"+healthcheck.ReadinessEndpoint, healthcheck.New(skip))`)
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileUsedElsewhere(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"main.go":   hcPkgMain,
		"config.go": hcPkgConfig,
		// a second use of the variable, in a file that does not even import the package
		"report.go": `package main

import "fmt"

func report() { fmt.Println(healthConfig.ReadinessEndpoint) }
`,
	}
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, files)

	// the v2 code stays, so that the compiler points at it
	assert.Equal(t, files, res.files)
	assert.Empty(t, res.stdout)
	notes := strings.Join(res.notes, "\n")
	if assert.Len(t, res.notes, 2) {
		assert.Contains(t, notes, fmt.Sprintf("main.go:%d: healthcheck.New() was left as it is: its config healthConfig (declared in config.go) is used in more places than this one", mw1LineOf(t, hcPkgMain, "app.Use")))
		assert.Contains(t, notes, fmt.Sprintf("config.go:%d: healthcheck.Config has no LivenessProbe", mw1LineOf(t, hcPkgConfig, "var healthConfig")))
	}
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileAssignedFieldByField(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"main.go":   hcPkgMain,
		"config.go": hcPkgConfig,
		"init.go": `package main

func init() { healthConfig.LivenessEndpoint = "/alive" }
`,
	}
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, files)

	assert.Equal(t, files, res.files)
	assert.Contains(t, strings.Join(res.notes, "\n"), "its config healthConfig (declared in config.go) gets LivenessEndpoint assigned after its declaration")
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileThatCannotBeFollowed(t *testing.T) {
	t.Parallel()

	// what each case says about why the call stays as it is
	reasons := map[string]string{
		"exported variable of a library":                "its config HealthConfig (declared in config.go) is exported and may be used by other packages",
		"function that returns the config":              "its config is not a literal",
		"variable of another package":                   "its config is not a literal",
		"endpoint of another package":                   "its config healthConfig (declared in config.go) has an endpoint that cannot be written in this file",
		"endpoint that a local name hides":              "its config healthConfig (declared in config.go) has an endpoint that cannot be written in this file",
		"variable that build constraints declare twice": "its config healthConfig is declared more than once in its package",
		"variable that a call initializes":              "its config healthConfig is declared in config.go, but not with a literal",
		"file that does not parse":                      "its config healthConfig is not declared next to it and a file of its package cannot be read",
	}

	for name, files := range map[string]map[string]string{
		"exported variable of a library": {
			"main.go": `package server

` + hcImports + `
func routes(app *fiber.App) { app.Use(healthcheck.New(HealthConfig)) }
`,
			"config.go": `package server

` + hcImports + `
var HealthConfig = healthcheck.Config{LivenessProbe: func(c *fiber.Ctx) bool { return true }}
`,
		},
		"function that returns the config": {
			"main.go": `package main

` + hcImports + `
func routes(app *fiber.App) { app.Use(healthcheck.New(healthConfig())) }
`,
			"config.go": `package main

` + hcImports + `
func healthConfig() healthcheck.Config {
	return healthcheck.Config{LivenessProbe: func(c *fiber.Ctx) bool { return true }}
}
`,
		},
		"variable of another package": {
			"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"

	"example.com/app/other"
)

func routes(app *fiber.App) { app.Use(healthcheck.New(other.HealthConfig)) }
`,
			"other/config.go": `package other

` + hcImports + `
var HealthConfig = healthcheck.Config{LivenessProbe: func(c *fiber.Ctx) bool { return true }}
`,
		},
		"endpoint of another package": {
			"main.go": `package main

` + hcImports + `
func routes(app *fiber.App) { app.Use(healthcheck.New(healthConfig)) }
`,
			"config.go": `package main

import (
	"github.com/gofiber/fiber/v2/middleware/healthcheck"

	"example.com/app/paths"
)

var healthConfig = healthcheck.Config{ReadinessEndpoint: paths.Ready}
`,
		},
		"endpoint that a local name hides": {
			"main.go": `package main

` + hcImports + `
func routes(app *fiber.App) {
	livePath := "/other"
	_ = livePath
	app.Use(healthcheck.New(healthConfig))
}
`,
			"config.go": `package main

` + hcImports + `
const livePath = "/up"

var healthConfig = healthcheck.Config{LivenessEndpoint: livePath}
`,
		},
		"variable that build constraints declare twice": {
			"main.go": `package main

` + hcImports + `
func routes(app *fiber.App) { app.Use(healthcheck.New(healthConfig)) }
`,
			"config_linux.go": `//go:build linux

package main

` + hcImports + `
var healthConfig = healthcheck.Config{LivenessProbe: func(c *fiber.Ctx) bool { return true }}
`,
			"config_other.go": `//go:build !linux

package main

` + hcImports + `
var healthConfig = healthcheck.Config{LivenessProbe: func(c *fiber.Ctx) bool { return false }}
`,
		},
		"variable that a call initializes": {
			"main.go": `package main

` + hcImports + `
func routes(app *fiber.App) { app.Use(healthcheck.New(healthConfig)) }
`,
			"config.go": `package main

` + hcImports + `
var healthConfig = buildConfig()

func buildConfig() healthcheck.Config {
	return healthcheck.Config{LivenessProbe: func(c *fiber.Ctx) bool { return true }}
}
`,
		},
		"file that does not parse": {
			"main.go": `package main

` + hcImports + `
func routes(app *fiber.App) { app.Use(healthcheck.New(healthConfig)) }
`,
			"config.go": `package main

` + hcImports + `
var healthConfig = healthcheck.Config{LivenessProbe: func(c *fiber.Ctx) bool { return true }}
`,
			"broken.go": `package main

func broken( {
`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, files)

			for file, src := range files {
				assert.Equal(t, src, res.files[file], file)
			}
			notes := strings.Join(res.notes, "\n")
			assert.Contains(t, notes, "healthcheck.New() was left as it is: "+reasons[name])
			assert.Empty(t, res.stdout)
		})
	}
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileThatTheFiltersLeaveOut(t *testing.T) {
	// Not parallel: the filters are process-wide.
	internal.SetFileFilters(nil, []string{"config.go"})
	defer internal.SetFileFilters(nil, nil)

	files := map[string]string{"main.go": hcPkgMain, "config.go": hcPkgConfig}
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, files)

	assert.Equal(t, files, res.files)
	assert.Contains(t, strings.Join(res.notes, "\n"), "its config healthConfig (declared in config.go) is in a file that is left out of the migration")
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileOfAnotherPackage(t *testing.T) {
	t.Parallel()

	// the same name in two packages of the project is two variables
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, map[string]string{
		"main.go":   hcPkgMain,
		"config.go": hcPkgConfig,
		"sub/config.go": `package sub

` + hcImports + `
var healthConfig = healthcheck.Config{LivenessProbe: func(c *fiber.Ctx) bool { return true }}

func routes(app *fiber.App) { app.Use(healthcheck.New(healthConfig)) }
`,
	})

	assert.Contains(t, res.files["main.go"], `app.Get("/ready", healthcheck.New(healthConfigReadiness))`)
	assert.Contains(t, res.files["sub/config.go"], `var healthConfigReadiness = healthcheck.Config{}`)
}

func Test_MigrateHealthcheckConfig_ReadinessConfigNameIsFreeInThePackage(t *testing.T) {
	t.Parallel()

	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, map[string]string{
		"main.go":   hcPkgMain,
		"config.go": hcPkgConfig,
		"names.go": `package main

var healthConfigReadiness = "taken by another file"
`,
	})

	assert.Contains(t, res.files["main.go"], `healthcheck.New(healthConfigReadiness2)`)
	assert.Contains(t, res.files["config.go"], `var healthConfigReadiness2 = healthcheck.Config{`)
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ConfigOfAnotherFileIsLeftAloneOnV3(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
)

func main() {
	app := fiber.New()
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New(live))
	app.Get("/ready", healthcheck.New(ready))
}
`,
		"config.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
)

var live = healthcheck.Config{Probe: func(c fiber.Ctx) bool { return true }}

var ready = healthcheck.Config{Probe: func(c fiber.Ctx) bool { return false }}
`,
	}
	res := mw1RunFiles(t, v3.MigrateHealthcheckConfig, files)

	assert.Equal(t, files, res.files)
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)
}

func Test_MigrateHealthcheckConfig_ConfigInAnotherFileWithWindowsLineEndings(t *testing.T) {
	t.Parallel()

	unix := mw1RunFiles(t, v3.MigrateHealthcheckConfig, map[string]string{"main.go": hcPkgMain, "config.go": hcPkgConfig})
	windows := mw1RunFiles(t, v3.MigrateHealthcheckConfig, map[string]string{
		"main.go":   strings.ReplaceAll(hcPkgMain, "\n", "\r\n"),
		"config.go": strings.ReplaceAll(hcPkgConfig, "\n", "\r\n"),
	})

	for name, content := range unix.files {
		assert.NotContains(t, content, "Use(", name)
		assert.Equal(t, content, strings.ReplaceAll(windows.files[name], "\r", ""), name)
	}
	assert.Equal(t, unix.notes, windows.notes)
}
