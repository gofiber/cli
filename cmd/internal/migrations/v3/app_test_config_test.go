package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateAppTestConfig(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestHit(t *testing.T) {
	app := fiber.New()
	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	_, _ = app.Test(req, 2000)
	timeoutMS := 250
	_, _ = app.Test(req, timeoutMS)
	_, _ = app.Test(req, int(2*time.Second/time.Millisecond))
}
`)

	out := appRun(t, dir, v3.MigrateAppTestConfig)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "app.Test(req, fiber.TestConfig{Timeout: time.Duration(2000) * time.Millisecond, FailOnTimeout: true})")
	assert.Contains(t, content, "app.Test(req, fiber.TestConfig{Timeout: time.Duration(timeoutMS) * time.Millisecond, FailOnTimeout: true})")
	assert.Contains(t, content, "fiber.TestConfig{Timeout: time.Duration(int(2*time.Second/time.Millisecond)) * time.Millisecond, FailOnTimeout: true}")
	assert.Contains(t, out, "Migrating app.Test usages")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateAppTestConfig_DisableTimeout(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http/httptest"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	_, _ = app.Test(req, -1)
}
`)

	appRun(t, dir, v3.MigrateAppTestConfig)

	assert.Contains(t, appRead(t, dir, "main.go"), `app.Test(req, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})`)
}

func Test_MigrateAppTestConfig_NoSecondArg(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"net/http/httptest"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	_, _ = app.Test(httptest.NewRequest("GET", "/", nil))
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateAppTestConfig)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateAppTestConfig_OtherTypes(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

type matcher struct{}

func (matcher) Test(a, b string) bool { return a == b }

func main() {
	var m matcher
	_ = m.Test("a", "a")

	app := fiber.New()
	grp := app.Group("/x")
	_ = grp
	println("app.Test(req, 100)") // app.Test(req, 100)
}
`)
	before := appRead(t, dir, "main.go")

	out := appRun(t, dir, v3.MigrateAppTestConfig)

	assert.Equal(t, before, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateAppTestConfig_Spread(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

func hit(app *fiber.App, req *http.Request, timeoutMS ...int) (*http.Response, error) {
	return app.Test(req, timeoutMS...)
}
`)
	before := appRead(t, dir, "main.go")

	appRun(t, dir, v3.MigrateAppTestConfig)

	assert.Equal(t, before, appRead(t, dir, "main.go"))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, 10, notes[0].Line)
	assert.Contains(t, notes[0].Message, "fiber.TestConfig")
}

func Test_MigrateAppTestConfig_AppFromAnotherFile(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"app.go": `package main

import "github.com/gofiber/fiber/v2"

func newApp() *fiber.App { return fiber.New() }
`,
		"app_test.go": `package main

import (
	"net/http/httptest"
	"testing"
)

func TestSlow(t *testing.T) {
	app := newApp()
	_, _ = app.Test(httptest.NewRequest("GET", "/", nil), 50)
}
`,
	})

	appRun(t, dir, v3.MigrateAppTestConfig)

	content := appRead(t, dir, "app_test.go")
	assert.Contains(t, content, `app.Test(httptest.NewRequest("GET", "/", nil), fiber.TestConfig{Timeout: time.Duration(50) * time.Millisecond, FailOnTimeout: true})`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v2"`)
	assert.Contains(t, content, `"time"`)
}

func Test_MigrateAppTestConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"net/http/httptest"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	timeoutMS := 2000
	_, _ = app.Test(req, timeoutMS)
	_, _ = app.Test(req, -1)
}
`)

	appRun(t, dir, v3.MigrateAppTestConfig)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateAppTestConfig)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateAppTestConfig_ConfigVariable(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"net/http/httptest"
	"time"

	"github.com/gofiber/fiber/v3"
)

func main() {
	app := fiber.New()
	cfg := fiber.TestConfig{Timeout: time.Second, FailOnTimeout: true}
	_, _ = app.Test(httptest.NewRequest("GET", "/", nil), cfg)
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateAppTestConfig)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}
