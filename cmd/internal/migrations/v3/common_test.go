package v3

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "main.go")
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	return string(b)
}

func newCmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd
}

func Test_MigrateHandlerSignatures(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mhstest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c *fiber.Ctx) error { return nil }
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateHandlerSignatures(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "*fiber.Ctx")
	assert.Contains(t, content, "fiber.Ctx")
	assert.Contains(t, buf.String(), "Migrating handler signatures")
}

func Test_MigrateParserMethods(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mptest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    var v any
    c.BodyParser(&v)
    c.CookieParser(&v)
    c.ParamsParser(&v)
    c.QueryParser(&v)
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateParserMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Bind().Body(&v)")
	assert.Contains(t, content, ".Bind().Cookie(&v)")
	assert.Contains(t, content, ".Bind().URI(&v)")
	assert.Contains(t, content, ".Bind().Query(&v)")
	assert.Contains(t, buf.String(), "Migrating parser methods")
}

func Test_MigrateRedirectMethods(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mrtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    c.Redirect("/foo")
    c.RedirectBack()
    c.RedirectToRoute("home")
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateRedirectMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Redirect().To(\"/foo\")")
	assert.Contains(t, content, ".Redirect().Back()")
	assert.Contains(t, content, ".Redirect().Route(\"home\")")
	assert.Contains(t, buf.String(), "Migrating redirect methods")
}

func Test_MigrateGenericHelpers(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mghtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    _ = c.ParamsInt("id", 0)
    _ = c.QueryInt("age", 0)
    _ = c.QueryFloat("score", 0.5)
    _ = c.QueryBool("ok", true)
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateGenericHelpers(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "fiber.Params[int](c, \"id\"")
	assert.Contains(t, content, "fiber.Query[int](c, \"age\"")
	assert.Contains(t, content, "fiber.Query[float64](c, \"score\"")
	assert.Contains(t, content, "fiber.Query[bool](c, \"ok\"")
	assert.Contains(t, buf.String(), "Migrating generic helpers")
}

func Test_MigrateContextMethods(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcmtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    ctx := c.Context()
    uc := c.UserContext()
    c.SetUserContext(ctx)
    _ = uc
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateContextMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".RequestCtx()")
	assert.Contains(t, content, ".Context()")
	assert.Contains(t, content, ".SetContext(")
	assert.Contains(t, buf.String(), "Migrating context methods")
}

func Test_MigrateViewBind(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mvbtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    return c.Bind("index", fiber.Map{})
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateViewBind(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".ViewBind(")
	assert.NotContains(t, content, "c.Bind(")
	assert.Contains(t, buf.String(), "Migrating view binding helpers")
}

func Test_MigrateAllParams(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "maptest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    var p any
    c.AllParams(&p)
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateAllParams(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Bind().URI(&p)")
	assert.Contains(t, buf.String(), "Migrating AllParams")
}

func Test_MigrateMount(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mmtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New()
    api := fiber.New()
    app.Mount("/api", api)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateMount(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Use(\"/api\", api)")
	assert.Contains(t, buf.String(), "Migrating Mount usages")
}

func Test_MigrateAddMethod(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "maddtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New()
    app.Add(fiber.MethodGet, "/foo", func(c fiber.Ctx) error { return nil })
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateAddMethod(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `Add([]string{fiber.MethodGet}, "/foo"`)
	assert.Contains(t, buf.String(), "Migrating Add method calls")
}

func Test_MigrateMimeConstants(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mmimetest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
const mime = fiber.MIMEApplicationJavaScript
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateMimeConstants(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "MIMEApplicationJavaScript")
	assert.Contains(t, content, "MIMETextJavaScript")
	assert.Contains(t, buf.String(), "Migrating MIME constants")
}

func Test_MigrateLoggerTags(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mloggertest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/logger"
)
var _ = logger.TagHeader
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateLoggerTags(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TagHeader")
	assert.Contains(t, content, "TagReqHeader")
	assert.Contains(t, buf.String(), "Migrating logger tag constants")
}

func Test_MigrateStaticRoutes(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msrtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New()
    app.Static("/", "./public")
    app.Static("/prefix", "./public", Static{Index: "index.htm"})
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateStaticRoutes(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `.Get("/*", static.New("./public"))`)
	assert.Contains(t, content, `static.New("./public", static.Config{IndexNames: []string{"index.htm"}})`)
	assert.Contains(t, buf.String(), "Migrating app.Static usage")
}

func Test_MigrateTrustedProxyConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtpctest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New(fiber.Config{
        EnableTrustedProxyCheck: true,
        TrustedProxies: []string{"0.8.0.0"},
    })
    _ = app
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateTrustedProxyConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "TrustProxy: true")
	assert.Contains(t, content, "TrustProxyConfig: fiber.TrustProxyConfig{Proxies: []string{\"0.8.0.0\"}},")
	assert.Contains(t, buf.String(), "Migrating trusted proxy config")
}

func Test_MigrateCORSConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcors")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/cors"
var _ = cors.New(cors.Config{
    AllowOrigins: "https://a.com,https://b.com",
    AllowMethods: "GET,POST",
    AllowHeaders: "Content-Type",
    ExposeHeaders: "Content-Length",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateCORSConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `AllowOrigins: []string{"https://a.com", "https://b.com"}`)
	assert.Contains(t, content, `AllowMethods: []string{"GET", "POST"}`)
	assert.Contains(t, content, `AllowHeaders: []string{"Content-Type"}`)
	assert.Contains(t, content, `ExposeHeaders: []string{"Content-Length"}`)
	assert.Contains(t, buf.String(), "Migrating CORS middleware configs")
}

func Test_MigrateCSRFConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mcsrf")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/csrf"
    "time"
)
var _ = csrf.New(csrf.Config{
    Expiration: 10 * time.Minute,
    SessionKey: "csrf",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateCSRFConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "IdleTimeout:")
	assert.NotContains(t, content, "Expiration:")
	assert.NotContains(t, content, "SessionKey")
	assert.Contains(t, buf.String(), "Migrating CSRF middleware configs")
}

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
	require.NoError(t, MigrateMonitorImport(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/contrib/monitor")
	assert.NotContains(t, content, "fiber/v2/middleware/monitor")
	assert.Contains(t, buf.String(), "Migrating monitor middleware import")
}

func Test_MigrateProxyTLSConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mproxy")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/proxy"
    "crypto/tls"
)
func main() {
    proxy.WithTlsConfig(&tls.Config{InsecureSkipVerify: true})
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateProxyTLSConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "proxy.WithClient(&fasthttp.Client{TLSConfig: &tls.Config{InsecureSkipVerify: true}})")
	assert.Contains(t, buf.String(), "Migrating proxy TLS config")
}
func Test_MigrateFilesystemMiddleware(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mfstest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "net/http"
    "github.com/gofiber/fiber/v2/middleware/filesystem"
)
var _ = filesystem.New(filesystem.Config{
    Root: http.Dir("./assets"),
    Index: "index.html",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateFilesystemMiddleware(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/fiber/v3/middleware/static")
	assert.Contains(t, content, `static.New("", static.Config{`)
	assert.Contains(t, content, `FS: os.DirFS("./assets")`)
	assert.Contains(t, content, `IndexNames: []string{"index.html"}`)
	assert.Contains(t, buf.String(), "Migrating filesystem middleware usage")
}

func Test_MigrateLimiterConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlimit")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "time"
    "github.com/gofiber/fiber/v2/middleware/limiter"
)
var _ = limiter.New(limiter.Config{
    Duration: time.Second,
    Store: nil,
    Key: func(c fiber.Ctx) string { return "" },
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateLimiterConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "Duration:")
	assert.Contains(t, content, "Expiration:")
	assert.NotContains(t, content, "Store:")
	assert.Contains(t, content, "Storage:")
	assert.NotContains(t, content, "Key:")
	assert.Contains(t, content, "KeyGenerator:")
	assert.Contains(t, buf.String(), "Migrating limiter middleware configs")
}

func Test_MigrateEnvVarConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "menvvar")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/envvar"
var _ = envvar.New(envvar.Config{
    ExportVars: []string{"A"},
    ExcludeVars: []string{"B"},
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateEnvVarConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "ExcludeVars")
	assert.Contains(t, buf.String(), "Migrating envvar middleware configs")
}

func Test_MigrateAppTestConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mtestcfg")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "net/http/httptest"
    "time"
)
func main() {
    app := fiber.New()
    req := httptest.NewRequest(fiber.MethodGet, "/", nil)
    _ = app.Test(req, 2*time.Second)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateAppTestConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `app.Test(req, fiber.TestConfig{Timeout: 2*time.Second})`)
	assert.Contains(t, buf.String(), "Migrating app.Test usages")
}

func Test_MigrateMiddlewareLocals(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlocals")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    id := c.Locals("requestid")
    _ = id
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, MigrateMiddlewareLocals(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `requestid.FromContext(c)`)
	assert.Contains(t, buf.String(), "Migrating middleware locals")
}
