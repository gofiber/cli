package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFileTB(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func Test_Migrate_V2_to_V3(t *testing.T) {

	dir, err := os.MkdirTemp("", "migrate_v2_v3")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	gomod := `module example.com/demo

go 1.20

require github.com/gofiber/fiber/v2 v2.0.6
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	main := `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/monitor"
)

func handler(c *fiber.Ctx) error {
    var v any
    c.BodyParser(&v)
    c.RedirectBack()
    _ = c.ParamsInt("id", 0)
    ctx := c.Context()
    uc := c.UserContext()
    c.SetUserContext(uc)
    _ = ctx
    return c.Bind("index", fiber.Map{})
}

func main() {
    app := fiber.New(fiber.Config{
        EnableTrustedProxyCheck: true,
        Prefork:                 true,
        Network:                 "tcp",
    })
    app.Static("/", "./public")
    app.Add(fiber.MethodGet, "/foo", handler)
    app.Mount("/api", app)
    app.ListenTLS(":443", "cert.pem", "key.pem")
    _ = fiber.MIMEApplicationJavaScript
    _ = monitor.New()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o600))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { require.NoError(t, os.Chdir(cwd)) }()

	origFile := currentVersionFile
	currentVersionFile = "go.mod"
	defer func() { currentVersionFile = origFile }()

	origTarget := targetVersionS
	targetVersionS = ""
	defer func() { targetVersionS = origTarget }()

	out, err := runCobraCmd(migrateCmd, "-t=3.0.0")
	require.NoError(t, err)

	content := readFileTB(t, filepath.Join(dir, "main.go"))
	at := assert.New(t)
	at.Contains(content, "github.com/gofiber/fiber/v3")
	at.Contains(content, "github.com/gofiber/fiber/v3/middleware/monitor")
	at.NotContains(content, "*fiber.Ctx")
	at.Contains(content, "fiber.Ctx")
	at.Contains(content, ".ViewBind().Body(&v)")
	at.Contains(content, ".Redirect().Back()")
	at.Contains(content, "fiber.Params[int](c, \"id\"")
	at.Contains(content, ".Use(\"/api\", app)")
	at.Contains(content, ".Listen(")
	at.Contains(content, "MIMETextJavaScript")
	at.NotContains(content, "MIMEApplicationJavaScript")

	gm := readFileTB(t, filepath.Join(dir, "go.mod"))
	at.Contains(gm, "github.com/gofiber/fiber/v3 v3.0.0")

	at.Contains(out, "Migration from Fiber 2.0.6 to 3.0.0")
	at.Contains(out, "Migrating Go packages")
	at.Contains(out, "Migrating handler signatures")
}
