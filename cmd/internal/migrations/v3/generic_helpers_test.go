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
	assert.Contains(t, content, "targetedUserID, err := strconv.Atoi(c.Params(\"userID\"))")
	assert.Contains(t, content, "targetedAge, err2 := fiber.Query[int](c, \"age\"), error(nil)")
	assert.Contains(t, content, "fiber.Params[int](c, \"id\"")
	assert.Contains(t, content, "fiber.Query[int](c, \"level\"")
	assert.Contains(t, content, "fiber.Query[float64](c, \"score\"")
	assert.Contains(t, content, "fiber.Query[bool](c, \"ok\"")
	assert.Contains(t, buf.String(), "Migrating generic helpers")
}

// v2 returned an error for a parameter that is no integer and the code answered it, with a
// 400 for /users/abc. The call became fiber.Params[int](c, "id"), error(nil), which never
// fails: the handler went on with 0 and answered 200.
func Test_MigrateGenericHelpers_ParamsIntKeepsItsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return fiber.ErrBadRequest
	}
	if page, err := c.ParamsInt("page"); err != nil || page < 1 {
		return fiber.ErrBadRequest
	}
	other, _ := c.Status(200).ParamsInt("other")
	var a, b = c.ParamsInt("a")
	withDefault, err := c.ParamsInt("id", 7)
	_, _, _, _, _ = id, other, a, b, withDefault
	return err
}

func idOf(c *fiber.Ctx) (int, error) {
	return c.ParamsInt("id")
}
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateGenericHelpers(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `id, err := strconv.Atoi(c.Params("id"))`)
	assert.Contains(t, content, `if page, err := strconv.Atoi(c.Params("page")); err != nil || page < 1 {`)
	assert.Contains(t, content, `other, _ := strconv.Atoi(c.Status(200).Params("other"))`)
	assert.Contains(t, content, `var a, b = strconv.Atoi(c.Params("a"))`)
	assert.Contains(t, content, `return strconv.Atoi(c.Params("id"))`)
	assert.Contains(t, content, `withDefault, err := fiber.Params[int](c, "id", 7), error(nil)`, "a call with a default never returned an error")
	assert.Contains(t, content, `"strconv"`, "the import is added")
	assert.NotContains(t, content, ".ParamsInt(")
}

func Test_MigrateGenericHelpers_ParamsIntUsesTheNameOfStrconv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	sc "strconv"

	"github.com/gofiber/fiber/v2"
)

var _ = sc.Itoa

func handler(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	_ = id
	return err
}
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateGenericHelpers(newCmd(&buf), dir, nil, nil))

	assert.Contains(t, readFile(t, file), `id, err := sc.Atoi(c.Params("id"))`)
}

func Test_MigrateGenericHelpers_ParamsIntIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	_ = id
	return err
}
`)

	var first bytes.Buffer
	require.NoError(t, v3.MigrateGenericHelpers(newCmd(&first), dir, nil, nil))
	migrated := readFile(t, file)
	assert.NotEmpty(t, first.String())

	var second bytes.Buffer
	require.NoError(t, v3.MigrateGenericHelpers(newCmd(&second), dir, nil, nil))
	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, second.String())
}
