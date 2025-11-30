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
	assert.Contains(t, content, ".Redirect().To(\"/foo\")")
	assert.Contains(t, content, "__fiberRedirectTarget := \"/bar\"")
	assert.Contains(t, content, "__fiberRedirectStatus := fiber.StatusPermanentRedirect")
	assert.Contains(t, content, "return c.Redirect().Status(__fiberRedirectStatus).To(__fiberRedirectTarget)")
	assert.Contains(t, content, ".Redirect().Back()")
	assert.Contains(t, content, "__fiberRedirectTarget := \"/fallback\"")
	assert.Contains(t, content, "__fiberRedirectStatus := 301")
	assert.Contains(t, content, "return c.Redirect().Status(__fiberRedirectStatus).Back(__fiberRedirectTarget)")
	assert.Contains(t, content, ".Redirect().Route(\"home\")")
	assert.Contains(t, content, "__fiberRedirectRouteArg0 := \"home-redirect\"")
	assert.Contains(t, content, "return c.Redirect().Status(__fiberRedirectStatus).Route(__fiberRedirectRouteArg0)")
	assert.Contains(t, content, "__fiberRedirectRouteArg1 := fiber.Map{}")
	assert.Contains(t, content, "return c.Redirect().Status(__fiberRedirectStatus).Route(__fiberRedirectRouteArg0, __fiberRedirectRouteArg1)")
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
	assert.Equal(t, 1, strings.Count(content, "__fiberRedirectStatus := 302"))
	assert.Contains(t, content, "return c.Redirect().Status(__fiberRedirectStatus).To(__fiberRedirectTarget)")
}
