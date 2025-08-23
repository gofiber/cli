package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateReqHeaderParser(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mrhp")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    var v any
    c.ReqHeaderParser(&v)
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateReqHeaderParser(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `.Bind().Header(&v)`)
	assert.Contains(t, buf.String(), "Migrating request header parser helper")
}
