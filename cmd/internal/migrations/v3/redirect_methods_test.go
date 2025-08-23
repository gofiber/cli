package v3_test

import (
	"bytes"
	"os"
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
    c.RedirectBack()
    c.RedirectToRoute("home")
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateRedirectMethods(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Redirect().To(\"/foo\")")
	assert.Contains(t, content, ".Redirect().Back()")
	assert.Contains(t, content, ".Redirect().Route(\"home\")")
	assert.Contains(t, buf.String(), "Migrating redirect methods")
}
