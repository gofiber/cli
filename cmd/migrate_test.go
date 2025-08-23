package cmd

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmdinternal "github.com/gofiber/cli/cmd/internal"
)

func readFileTB(tb testing.TB, path string) string {
	tb.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	require.NoError(tb, err)
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
    c.Redirect("/foo")
    c.RedirectBack()
    _ = c.ParamsInt("id", 0)
    ctx := c.Context()
    uc := c.UserContext()
    c.SetUserContext(uc)
    _ = ctx
    return c.Bind(fiber.Map{})
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

	cmd := newMigrateCmd()
	setupCmd()
	defer teardownCmd()
	out, err := runCobraCmd(cmd, "-t=3.0.0")
	require.NoError(t, err)

	content := readFileTB(t, filepath.Join(dir, "main.go"))
	at := assert.New(t)
	at.Contains(content, "github.com/gofiber/fiber/v3")
	at.Contains(content, "github.com/gofiber/contrib/monitor")
	at.NotContains(content, "*fiber.Ctx")
	at.Contains(content, "fiber.Ctx")
	at.Contains(content, ".Bind().Body(&v)")
	at.Contains(content, ".ViewBind(fiber.Map{})")
	at.Contains(content, ".Redirect().To(\"/foo\")")
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

	// run migration again to ensure idempotency
	cmd = newMigrateCmd()
	out2, err := runCobraCmd(cmd, "-t=3.0.0", "-f")
	require.NoError(t, err)
	at.Contains(out2, "Migration from Fiber 2.0.0 to 3.0.0")

	content2 := readFileTB(t, filepath.Join(dir, "main.go"))
	assert.Equal(t, content, content2)
}

func Test_Migrate_DefaultTarget(t *testing.T) {
	dir, err := os.MkdirTemp("", "migrate_default")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	gomod := `module example.com/demo

go 1.20

require github.com/gofiber/fiber/v2 v2.0.6
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	main := `package main
import "github.com/gofiber/fiber/v2"
func main() {
    _ = fiber.New()
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o600))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { require.NoError(t, os.Chdir(cwd)) }()

	setupCmd()
	defer teardownCmd()

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder(http.MethodGet, "https://api.github.com/repos/gofiber/fiber/releases/latest", httpmock.NewBytesResponder(200, []byte(`{"name":"v3.0.0"}`)))

	cmd := newMigrateCmd()
	out, err := runCobraCmd(cmd)
	require.NoError(t, err)
	assert.Contains(t, out, "Migration from Fiber 2.0.6 to 3.0.0")

	content := readFileTB(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, content, "github.com/gofiber/fiber/v3 v3.0.0")
}

func Test_RunGoMod(t *testing.T) {
	dir := t.TempDir()

	modContent := `module example

require github.com/gofiber/fiber/v2 v2.0.0`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	vendor := filepath.Join(dir, "vendor")
	require.NoError(t, os.Mkdir(vendor, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(vendor, "go.mod"), []byte("module vendor"), 0o600))

	origExec := cmdinternal.ExecCommand
	var cmds []*exec.Cmd
	cmdinternal.ExecCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...) // #nosec G204 -- safe for test
		env := []string{"GO_WANT_HELPER_PROCESS=1"}
		if needError {
			env = append(env, "GO_WANT_HELPER_NEED_ERR=1")
		}
		cmd.Env = env
		cmds = append(cmds, cmd)
		return cmd
	}
	defer func() {
		cmdinternal.ExecCommand = origExec
		needError = false
	}()

	require.NoError(t, cmdinternal.RunGoMod(dir))
	assert.Len(t, cmds, 3)
	for _, c := range cmds {
		assert.Equal(t, dir, c.Dir)
	}

	cmds = nil
	needError = true
	assert.Error(t, cmdinternal.RunGoMod(dir))
}

func Test_Migrate_ForceAndSkip(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example

go 1.20

require github.com/gofiber/fiber/v3 v3.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o600))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { require.NoError(t, os.Chdir(cwd)) }()

	t.Run("without force", func(t *testing.T) {
		cmd := newMigrateCmd()
		out, err := runCobraCmd(cmd, "-t=3.0.0")
		require.Error(t, err)
		assert.Contains(t, out, "not greater")
	})

	t.Run("force", func(t *testing.T) {
		origExec := cmdinternal.ExecCommand
		origCmdExec := execCommand
		var cmds []*exec.Cmd
		fake := func(name string, args ...string) *exec.Cmd {
			cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
			cmd := exec.Command(os.Args[0], cs...) // #nosec G204 -- safe for test
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
			cmds = append(cmds, cmd)
			return cmd
		}
		cmdinternal.ExecCommand = fake
		execCommand = fake
		defer func() {
			cmdinternal.ExecCommand = origExec
			execCommand = origCmdExec
		}()

		cmd := newMigrateCmd()
		out, err := runCobraCmd(cmd, "-t=3.0.0", "-f")
		require.NoError(t, err)
		assert.Contains(t, out, "Migration from Fiber 2.0.0 to 3.0.0")
		assert.NotContains(t, out, "Migrating Go packages")
		assert.Len(t, cmds, 3)
	})

	t.Run("force skip go mod", func(t *testing.T) {
		origExec := cmdinternal.ExecCommand
		origCmdExec := execCommand
		var cmds []*exec.Cmd
		fake := func(name string, args ...string) *exec.Cmd {
			cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
			cmd := exec.Command(os.Args[0], cs...) // #nosec G204 -- safe for test
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
			cmds = append(cmds, cmd)
			return cmd
		}
		cmdinternal.ExecCommand = fake
		execCommand = fake
		defer func() {
			cmdinternal.ExecCommand = origExec
			execCommand = origCmdExec
		}()

		cmd := newMigrateCmd()
		out, err := runCobraCmd(cmd, "-t=3.0.0", "-f", "-s")
		require.NoError(t, err)
		assert.Contains(t, out, "Migration from Fiber 2.0.0 to 3.0.0")
		assert.NotContains(t, out, "Migrating Go packages")
		assert.Empty(t, cmds)
	})
}

func Test_Migrate_ForcePartialV3(t *testing.T) {
	dir := t.TempDir()

	gomod := `module example

go 1.20

require github.com/gofiber/fiber/v3 v3.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	main := `package main
import "github.com/gofiber/fiber/v2"
func main() {}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o600))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { require.NoError(t, os.Chdir(cwd)) }()

	cmd := newMigrateCmd()
	setupCmd()
	defer teardownCmd()
	out, err := runCobraCmd(cmd, "-t=3.0.0", "-f")
	require.NoError(t, err)
	assert.Contains(t, out, "Migration from Fiber 2.0.0 to 3.0.0")
	assert.Contains(t, out, "Migrating Go packages")

	content := readFileTB(t, filepath.Join(dir, "main.go"))
	assert.NotContains(t, content, "github.com/gofiber/fiber/v2")
}
