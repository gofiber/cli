package v3

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0o644)
	assert.Nil(t, err)
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	assert.Nil(t, err)
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

	dir, err := ioutil.TempDir("", "mhstest")
	assert.Nil(t, err)
	defer os.RemoveAll(dir)

	file := writeTempFile(t, dir, "main.go", `package main
import "github.com/gofiber/fiber/v2"
func handler(c *fiber.Ctx) error { return nil }
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	assert.Nil(t, MigrateHandlerSignatures(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "*fiber.Ctx")
	assert.Contains(t, content, "fiber.Ctx")
	assert.Contains(t, buf.String(), "Migrating handler signatures")
}

func Test_MigrateParserMethods(t *testing.T) {
	t.Parallel()

	dir, err := ioutil.TempDir("", "mptest")
	assert.Nil(t, err)
	defer os.RemoveAll(dir)

	file := writeTempFile(t, dir, "main.go", `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    var v interface{}
    c.BodyParser(&v)
    c.CookieParser(&v)
    c.ParamsParser(&v)
    c.QueryParser(&v)
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	assert.Nil(t, MigrateParserMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Bind().Body(&v)")
	assert.Contains(t, content, ".Bind().Cookie(&v)")
	assert.Contains(t, content, ".Bind().URI(&v)")
	assert.Contains(t, content, ".Bind().Query(&v)")
	assert.Contains(t, buf.String(), "Migrating parser methods")
}

func Test_MigrateRedirectMethods(t *testing.T) {
	t.Parallel()

	dir, err := ioutil.TempDir("", "mrtest")
	assert.Nil(t, err)
	defer os.RemoveAll(dir)

	file := writeTempFile(t, dir, "main.go", `package main
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
	assert.Nil(t, MigrateRedirectMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Redirect().To(\"/foo\")")
	assert.Contains(t, content, ".Redirect().Back()")
	assert.Contains(t, content, ".Redirect().Route(\"home\")")
	assert.Contains(t, buf.String(), "Migrating redirect methods")
}

func Test_MigrateGenericHelpers(t *testing.T) {
	t.Parallel()

	dir, err := ioutil.TempDir("", "mghtest")
	assert.Nil(t, err)
	defer os.RemoveAll(dir)

	file := writeTempFile(t, dir, "main.go", `package main
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
	assert.Nil(t, MigrateGenericHelpers(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "fiber.Params[int](c, \"id\"")
	assert.Contains(t, content, "fiber.Query[int](c, \"age\"")
	assert.Contains(t, content, "fiber.Query[float64](c, \"score\"")
	assert.Contains(t, content, "fiber.Query[bool](c, \"ok\"")
	assert.Contains(t, buf.String(), "Migrating generic helpers")
}
