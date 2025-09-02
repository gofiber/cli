package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateGenericHelpers(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mghtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error {
    targetedUserID, err := c.ParamsInt("userID")
    targetedAge, err2 := c.QueryInt("age")
    _ = c.ParamsInt("id", 0)
    _ = c.QueryInt("level", 0)
    _ = c.QueryFloat("score", 0.5)
    _ = c.QueryBool("ok", true)
    _ = targetedUserID
    _ = targetedAge
    _ = err
    _ = err2
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateGenericHelpers(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "targetedUserID, err := fiber.Params[int](c, \"userID\"), nil")
	assert.Contains(t, content, "targetedAge, err2 := fiber.Query[int](c, \"age\"), nil")
	assert.Contains(t, content, "fiber.Params[int](c, \"id\"")
	assert.Contains(t, content, "fiber.Query[int](c, \"level\"")
	assert.Contains(t, content, "fiber.Query[float64](c, \"score\"")
	assert.Contains(t, content, "fiber.Query[bool](c, \"ok\"")
	assert.Contains(t, buf.String(), "Migrating generic helpers")
}
