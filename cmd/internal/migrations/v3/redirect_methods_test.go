package v3_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateRedirectMethods(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mrtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    c.Redirect("/foo")
    c.Redirect("/bar", fiber.StatusPermanentRedirect)
    c.RedirectBack()
    c.RedirectBack("/fallback", 301)
    c.RedirectToRoute("home")
    c.RedirectToRoute("home-redirect", 301)
    c.RedirectToRoute("dashboard", fiber.Map{}, 308)
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateRedirectMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "c.Redirect().To(\"/foo\")")
	assert.Contains(t, content, "c.Redirect().Status(fiber.StatusPermanentRedirect).To(\"/bar\")")
	assert.Contains(t, content, "c.Redirect().Back()")
	assert.Contains(t, content, "c.Redirect().Status(301).Back(\"/fallback\")")
	assert.Contains(t, content, "c.Redirect().Route(\"home\")")
	assert.Contains(t, content, "c.Redirect().Status(301).Route(\"home-redirect\")")
	assert.Contains(t, content, "c.Redirect().Status(308).Route(\"dashboard\", fiber.Map{})")
	assert.Contains(t, buf.String(), "Migrating redirect methods")
}

func Test_MigrateRedirectMethodsTwice(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mrtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    c.Redirect("/foo", 302)
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateRedirectMethods(cmd, dir, nil, nil))

	cmd = newCmd(&buf)
	require.NoError(t, v3.MigrateRedirectMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Equal(t, 1, strings.Count(content, "c.Redirect().Status(302).To(\"/foo\")"))
}
