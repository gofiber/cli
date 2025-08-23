package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateConfigListenerFields(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mconf")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file1 := filepath.Join(dir, "app.go")
	require.NoError(t, os.WriteFile(file1, []byte(`package main
import "github.com/gofiber/fiber/v2"
func newApp() *fiber.App {
    return fiber.New(fiber.Config{
        Prefork: true,
        Network: "tcp",
        DisableStartupMessage: true,
        EnablePrintRoutes: true,
    })
}`), 0o600))

	file2 := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(file2, []byte(`package main
func main() {
    app := newApp()
    app.Listen(":3000")
}`), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateConfigListenerFields(cmd, dir, nil, nil))

	content := readFile(t, file1)
	assert.NotContains(t, content, "Prefork")
	assert.NotContains(t, content, "Network")
	assert.NotContains(t, content, "DisableStartupMessage")
	assert.NotContains(t, content, "EnablePrintRoutes")
	content2 := readFile(t, file2)
	assert.Contains(t, content2, "fiber.ListenConfig{")
	assert.Contains(t, content2, "EnablePrefork: true")
	assert.Contains(t, content2, "ListenerNetwork: \"tcp\"")
	assert.Contains(t, content2, "DisableStartupMessage: true")
	assert.Contains(t, content2, "EnablePrintRoutes: true")
	assert.Contains(t, buf.String(), "Migrating listener related config fields")
}

func Test_MigrateConfigListenerFields_PreserveFormatting(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mconf_format")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func newApp() *fiber.App {
    var engine any
    return fiber.New(fiber.Config{
        Views:       engine,
        ViewsLayout: "layouts/main",
        Prefork:     true,
    })
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateConfigListenerFields(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "\tViews:       engine,\n\t\tViewsLayout: \"layouts/main\",\n\t})")
	assert.Contains(t, buf.String(), "Migrating listener related config fields")
}

func Test_MigrateConfigListenerFields_ExistingListenConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mconf2")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file1 := filepath.Join(dir, "app.go")
	require.NoError(t, os.WriteFile(file1, []byte(`package main
import "github.com/gofiber/fiber/v2"
func newApp() *fiber.App {
    return fiber.New(fiber.Config{
        Prefork: true,
        Network: "tcp",
        DisableStartupMessage: true,
        EnablePrintRoutes: true,
    })
}`), 0o600))

	file2 := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(file2, []byte(`package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := newApp()
    app.Listen(":3000", fiber.ListenConfig{EnablePrefork: true})
}`), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateConfigListenerFields(cmd, dir, nil, nil))

	content1 := readFile(t, file1)
	assert.NotContains(t, content1, "Prefork")
	assert.NotContains(t, content1, "Network")
	assert.NotContains(t, content1, "DisableStartupMessage")
	assert.NotContains(t, content1, "EnablePrintRoutes")

	content2 := readFile(t, file2)
	assert.Contains(t, content2, "EnablePrefork: true")
	assert.Contains(t, content2, "ListenerNetwork: \"tcp\"")
	assert.Contains(t, content2, "DisableStartupMessage: true")
	assert.Contains(t, content2, "EnablePrintRoutes: true")
	assert.Contains(t, buf.String(), "Migrating listener related config fields")
}

func Test_MigrateConfigListenerFields_NestedAddr(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mconf3")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file1 := filepath.Join(dir, "app.go")
	require.NoError(t, os.WriteFile(file1, []byte(`package main
import "github.com/gofiber/fiber/v2"
func newApp() *fiber.App {
    return fiber.New(fiber.Config{
        DisableStartupMessage: true,
    })
}`), 0o600))

	file2 := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(file2, []byte(`package main
import (
    "github.com/gofiber/fiber/v2"
    "net"
)
func main() {
    app := newApp()
    app.Listen(net.JoinHostPort("localhost", "3000"))
}`), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateConfigListenerFields(cmd, dir, nil, nil))

	content := readFile(t, file1)
	assert.NotContains(t, content, "DisableStartupMessage")
	content2 := readFile(t, file2)
	assert.Contains(t, content2, `app.Listen(net.JoinHostPort("localhost", "3000"), fiber.ListenConfig{DisableStartupMessage: true})`)
	assert.Contains(t, buf.String(), "Migrating listener related config fields")
}

func Test_MigrateConfigListenerFields_Idempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mconfidem")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file1 := filepath.Join(dir, "app.go")
	require.NoError(t, os.WriteFile(file1, []byte(`package main
import "github.com/gofiber/fiber/v2"
func newApp() *fiber.App {
    return fiber.New(fiber.Config{
        Prefork: true,
        Network: "tcp",
        DisableStartupMessage: true,
        EnablePrintRoutes: true,
    })
}`), 0o600))

	file2 := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(file2, []byte(`package main
func main() {
    app := newApp()
    app.Listen(":3000")
}`), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateConfigListenerFields(cmd, dir, nil, nil))
	first1 := readFile(t, file1)
	first2 := readFile(t, file2)
	require.NoError(t, v3.MigrateConfigListenerFields(cmd, dir, nil, nil))
	second1 := readFile(t, file1)
	second2 := readFile(t, file2)
	assert.Equal(t, first1, second1)
	assert.Equal(t, first2, second2)
}

func Test_MigrateConfigListenerFields_SingleFile(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mconf_single")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New(fiber.Config{
        Prefork: true,
        Network: "tcp",
        DisableStartupMessage: true,
        EnablePrintRoutes: true,
    })
    app.Listen(":3000")
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateConfigListenerFields(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "fiber.ListenConfig{EnablePrefork: true, ListenerNetwork: \"tcp\", DisableStartupMessage: true, EnablePrintRoutes: true}")
	assert.Contains(t, buf.String(), "Migrating listener related config fields")
}

func Test_MigrateConfigListenerFields_InlineConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mconf_inline")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New(fiber.Config{DisableStartupMessage: true})
    go app.Listen(":3000")
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateConfigListenerFields(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "fiber.New(fiber.Config{})")
	assert.Contains(t, content, `go app.Listen(":3000", fiber.ListenConfig{DisableStartupMessage: true})`)
	assert.Contains(t, buf.String(), "Migrating listener related config fields")
}
