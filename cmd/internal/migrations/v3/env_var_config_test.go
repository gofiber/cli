package v3_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const envvarImports = `import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/envvar"
)
`

func Test_MigrateEnvVarConfig(t *testing.T) {
	t.Parallel()

	// the example of the v2 documentation
	res := mw1Run(t, v3.MigrateEnvVarConfig, `package main

`+envvarImports+`
func main() {
	app := fiber.New()
	app.Use("/expose/envvars", envvar.New(
		envvar.Config{
			ExportVars:  map[string]string{"testKey": "", "testDefaultKey": "testDefaultVal"},
			ExcludeVars: map[string]string{"excludeKey": ""},
		}),
	)
}
`)

	assert.Equal(t, `package main

`+envvarImports+`
func main() {
	app := fiber.New()
	app.Use("/expose/envvars", envvar.New(
		envvar.Config{
			ExportVars: map[string]string{"testKey": "", "testDefaultKey": "testDefaultVal"},
		}),
	)
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating EnvVar middleware configs")
	assert.Empty(t, res.notes)
}

func Test_MigrateEnvVarConfig_ExcludeVarsInAnyShape(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateEnvVarConfig, `package main

`+envvarImports+`
var (
	multi = envvar.Config{
		ExportVars: map[string]string{"APP_ENV": "dev"},
		ExcludeVars: map[string]string{
			"DB_PASSWORD": "",
			"API_TOKEN":   "",
		},
	}

	first = envvar.Config{
		ExcludeVars: map[string]string{"A": "", "B": ""}, // never exported
		ExportVars:  map[string]string{"APP_ENV": "dev"},
	}

	lastField = envvar.New(
		envvar.Config{
			ExportVars:  map[string]string{"testKey": "", "testDefaultKey": "testDefaultVal"},
			ExcludeVars: map[string]string{"excludeKey": ""}},
	)

	oneLine = envvar.Config{ExportVars: map[string]string{"A": ""}, ExcludeVars: map[string]string{"B": ""}}

	noComma = envvar.Config{
		ExportVars:  map[string]string{"A": ""},
		ExcludeVars: map[string]string{"B": ""}}
)
`)

	assert.Equal(t, `package main

import (
	"github.com/gofiber/fiber/v2/middleware/envvar"
)

var (
	multi = envvar.Config{
		ExportVars: map[string]string{"APP_ENV": "dev"},
	}

	first = envvar.Config{
		ExportVars: map[string]string{"APP_ENV": "dev"},
	}

	lastField = envvar.New(
		envvar.Config{
			ExportVars: map[string]string{"testKey": "", "testDefaultKey": "testDefaultVal"},
		},
	)

	oneLine = envvar.Config{ExportVars: map[string]string{"A": ""}}

	noComma = envvar.Config{
		ExportVars: map[string]string{"A": ""},
	}
)
`, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateEnvVarConfig_WithoutExportVars(t *testing.T) {
	t.Parallel()

	src := `package main

` + envvarImports + `
func main() {
	app := fiber.New()
	app.Use("/all", envvar.New())
	app.Use("/excl", envvar.New(envvar.Config{
		ExcludeVars: map[string]string{"DB_PASSWORD": "", "API_TOKEN": ""},
	}))
	app.Use("/empty", envvar.New(envvar.Config{ExportVars: map[string]string{}}))
}
`
	res := mw1Run(t, v3.MigrateEnvVarConfig, src)

	// the excluded variables go, the request for the variables to export stays
	assert.NotContains(t, res.file(), "ExcludeVars")
	if assert.Len(t, res.notes, 3) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: envvar.New() exposes nothing without ExportVars in v3", mw1LineOf(t, res.file(), `envvar.New())`)))
		assert.Contains(t, res.notes[1], `v2 exposed every variable of the environment except "DB_PASSWORD", "API_TOKEN": list the variables to expose in ExportVars`)
		assert.Contains(t, res.notes[2], "envvar exposes nothing without ExportVars in v3")
	}
}

func Test_MigrateEnvVarConfig_ExportVarsSetLater(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateEnvVarConfig, `package main

`+envvarImports+`
func handler(vars map[string]string) fiber.Handler {
	cfg := envvar.Config{}
	cfg.ExportVars = vars
	cfg.ExcludeVars = map[string]string{"A": ""}
	return envvar.New(cfg)
}
`)

	assert.NotContains(t, res.file(), "ExcludeVars")
	assert.Contains(t, res.file(), "cfg.ExportVars = vars")
	assert.Empty(t, res.notes)
}

func Test_MigrateEnvVarConfig_IgnoresOtherStructs(t *testing.T) {
	t.Parallel()

	src := `package main

` + envvarImports + `
type exposure struct {
	ExcludeVars map[string]string
}

var e = exposure{ExcludeVars: map[string]string{"A": ""}}

var _ = envvar.Config{ExportVars: map[string]string{"B": ""}}
`
	res := mw1Run(t, v3.MigrateEnvVarConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)

	other := `package main

type exposure struct {
	ExcludeVars []string
}

var e = exposure{ExcludeVars: []string{"SECRET"}}
`
	res = mw1Run(t, v3.MigrateEnvVarConfig, other)
	assert.Equal(t, other, res.file())
}

func Test_MigrateEnvVarConfig_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateEnvVarConfig, `package main

`+envvarImports+`
var _ = envvar.New(envvar.Config{
	ExportVars:  map[string]string{"A": ""},
	ExcludeVars: map[string]string{"B": ""},
})
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateEnvVarConfig, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
	assert.Empty(t, second.notes)
}
