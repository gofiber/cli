package v3_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// reviewNotes runs the review over the files (name -> content) and returns the
// notes as "file:line: message".
func reviewNotes(t *testing.T, files map[string]string) []string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	v3.Review(dir)

	var out []string
	for _, n := range internal.DrainNotes(dir) {
		out = append(out, n.String())
	}
	return out
}

func Test_Review_FindsWhatIsLeftOfV2(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{"main.go": `package main

import (
	"github.com/gofiber/contrib/fiberzap/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/utils"
	"github.com/gofiber/websocket/v2"
)

func main() {
	app := fiber.New()
	app.Static("/", "./public")
	app.Mount("/api", fiber.New())
	_ = app.ListenTLS(":443", "cert.pem", "key.pem")
	app.Hooks().OnShutdown(func() error { return nil })

	api := app.Group("/api")
	api.Static("/s", "./s")

	app.Use(csrf.New(csrf.Config{KeyLookup: "header:X-Csrf-Token", Expiration: 0}))
	app.Get("/", func(c fiber.Ctx) error {
		id, _ := c.ParamsInt("id")
		_ = c.BodyParser(&id)
		_ = c.UserContext()
		return c.Redirect("/other")
	})
	_ = fiber.Get("http://example.com")
	_ = fiber.Static{}
}
`})

	assert.ElementsMatch(t, []string{
		"main.go:4: still imports github.com/gofiber/contrib/fiberzap/v2, a contrib module for Fiber v2, which was not migrated",
		"main.go:5: github.com/gofiber/fiber/v2/middleware/filesystem does not exist in Fiber v3 (replaced by the static middleware)",
		"main.go:8: github.com/gofiber/fiber/v3/utils does not exist in Fiber v3, the helpers moved to github.com/gofiber/utils/v2",
		"main.go:9: still imports github.com/gofiber/websocket/v2, a Fiber v2 module that moved into github.com/gofiber/contrib/v3",
		"main.go:7: csrf: the default token lifetime (IdleTimeout, formerly Expiration) is 30 minutes instead of 1 hour, and unsafe requests with an invalid Sec-Fetch-Site header are rejected.",
		"main.go:14: Static was removed in Fiber v3: use the static middleware, github.com/gofiber/fiber/v3/middleware/static",
		"main.go:15: Mount was removed in Fiber v3: use app.Use(prefix, subApp)",
		"main.go:16: ListenTLS was removed in Fiber v3: use app.Listen with fiber.ListenConfig{CertFile, CertKeyFile}",
		"main.go:17: Hooks().OnShutdown was removed in Fiber v3: use OnPreShutdown or OnPostShutdown",
		"main.go:20: Static was removed in Fiber v3: use the static middleware, github.com/gofiber/fiber/v3/middleware/static",
		"main.go:22: csrf.Config has no KeyLookup field in Fiber v3: use Extractor with the extractors package",
		"main.go:22: csrf.Config has no Expiration field in Fiber v3: it is IdleTimeout now",
		"main.go:24: ParamsInt was removed in Fiber v3: use fiber.Params[int](c, \"name\")",
		"main.go:25: BodyParser was removed in Fiber v3: use c.Bind().Body(&v)",
		"main.go:26: UserContext was removed in Fiber v3: the Ctx itself implements context.Context, pass c where a context is needed",
		"main.go:27: Redirect takes no arguments in Fiber v3: use c.Redirect().To(url) and Status(code)",
		"main.go:29: fiber.Get was removed in Fiber v3: the HTTP client moved to github.com/gofiber/fiber/v3/client",
		"main.go:30: fiber.Static was removed in Fiber v3: use the static middleware, github.com/gofiber/fiber/v3/middleware/static",
	}, notes)
}

func Test_Review_SendFileNames(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	if err := c.SendFile("./public/index.html"); err != nil {
		return err
	}
	if err := c.SendFile(c.Params("file")); err != nil {
		return err
	}
	return c.Download(c.Params("other"))
}
`})

	require.Len(t, notes, 1, "reported once per project: %v", notes)
	assert.Contains(t, notes[0], "main.go:9: SendFile takes the file name literally in Fiber v3")
}

func Test_Review_LeavesValidV3Alone(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{"main.go": `package main

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
	"github.com/gofiber/fiber/v3/middleware/session"
	"github.com/gofiber/fiber/v3/middleware/static"
)

type server struct{ Static string }

func main() {
	app := fiber.New()
	app.Use("/", static.New("./public"))
	app.Use(session.New())
	_ = app.Listen(":3000", fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"})
	app.Hooks().OnPostShutdown(func(err error) error { return err })

	app.Get("/:id", func(c fiber.Ctx) error {
		id := fiber.Params[int](c, "id")
		var body struct{ Name string }
		if err := c.Bind().Body(&body); err != nil {
			return err
		}
		_ = context.Context(c)
		return c.Redirect().To("/other")
	})
	_, _ = client.Get("http://example.com")

	// Not fiber values: a local struct that shares method and field names.
	s := server{Static: "x"}
	_ = s.Static
	var other interface{ Static(string) }
	other.Static("y")
	var fiber2 struct{ Get func() }
	fiber2.Get()
}
`})

	// Valid v3 code leaves nothing over; the only entry is the behavior note of the static middleware.
	require.Len(t, notes, 1, "%v", notes)
	assert.Contains(t, notes[0], "static: a directory without an index file answers 404 instead of 403")
}

func Test_Review_ClientCallsOnShadowedOrUnknownReceivers(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

type db struct{}

func (db) QueryInt(string) int { return 0 }

func handler(c fiber.Ctx, d db) error {
	_ = d.QueryInt("x")
	inner := func(c db) int { return c.QueryInt("y") }
	_ = inner
	return c.SendString(fiber.Query[string](c, "z"))
}
`})

	assert.Empty(t, notes)
}

func Test_Review_AliasedAndMultiFile(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{
		"a.go": `package main

import f "github.com/gofiber/fiber/v3"

func a(app *f.App, g f.Router) {
	app.Static("/", "./public")
	g.Mount("/x", nil)
}
`,
		"sub/b.go": `package sub

import fib "github.com/gofiber/fiber/v3"

var app = fib.New()

func init() {
	app.Static("/", "./public")
	_ = fib.Agent{}
}
`,
	})

	assert.ElementsMatch(t, []string{
		"a.go:6: Static was removed in Fiber v3: use the static middleware, github.com/gofiber/fiber/v3/middleware/static",
		"a.go:7: Mount was removed in Fiber v3: use app.Use(prefix, subApp)",
		"sub/b.go:8: Static was removed in Fiber v3: use the static middleware, github.com/gofiber/fiber/v3/middleware/static",
		"sub/b.go:9: fib.Agent was removed in Fiber v3: the HTTP client moved to github.com/gofiber/fiber/v3/client",
	}, notes)
}

func Test_Review_BehaviorRules(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"a.go": `package main

import (
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/proxy"
)

var _ = limiter.Config{Max: 5}
var _ = logger.Config{}
var _ = proxy.Config{}
`,
		"b.go": `package main

import (
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/proxy"
)

var _ = limiter.Config{SkipFailedRequests: true}
var _ = logger.Config{Format: "${status}\n"}
var _ = proxy.Config{}
`,
	}
	notes := reviewNotes(t, files)

	require.Len(t, notes, 3, "a rule is reported once per project: %v", notes)
	assert.Contains(t, notes, "b.go:4: limiter: SkipFailedRequests and SkipSuccessfulRequests now judge the status a request ends with, errors included; in v2 only the response status counted, which is still 200 when a handler returns an error, so such requests now count as failed.")
	assert.Contains(t, notes, "a.go:5: logger: the default Format changed to \"[${time}] ${ip} ${status} - ${latency} ${method} ${path} ${error}\"; set Format to keep the v2 layout.")
	assert.Contains(t, notes, "a.go:6: proxy: Fiber v3 refuses upstreams that resolve to loopback or private addresses (SSRF protection), so proxying to localhost or an internal host panics at startup or answers 500. Allow them with proxy.WithSecurityPolicy(proxy.SecurityPolicy{AllowPrivateIPs: true}) or Config.SecurityPolicy.")
}

func Test_Review_StaticBehavior(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
)

func main() {
	app := fiber.New()
	app.Use("/", static.New("./public"))
}
`})

	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "main.go:5: static: a directory without an index file answers 404 instead of 403")
}

func Test_Review_ContribBehaviorAndBrokenFiles(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/contrib/v3/opa"
	otelfiber "github.com/gofiber/contrib/v3/otel"
)

var _ = opa.New
var _ = otelfiber.Middleware
`,
		"broken.go": `package main

import "github.com/gofiber/fiber/v3"

func broken( {
`,
		"unrelated.go": `package main

func unrelated( {
`,
	})

	require.Len(t, notes, 3, "%v", notes)
	assert.Contains(t, notes[0], "broken.go:5: skipped because the file does not parse")
	assert.Contains(t, notes[1], "main.go:4: opa: the v3 module evaluates policies as Rego v1")
	assert.Contains(t, notes[2], "main.go:5: otel: the metric names changed")
}
