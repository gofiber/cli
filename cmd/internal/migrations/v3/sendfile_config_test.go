package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSendFileConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msendfile")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"

func handler(c fiber.Ctx) error {
    if err := c.SendFile("./public/index.html", true); err != nil {
        return err
    }
    return c.SendFile("./public/file.txt", compressEnabled)
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `c.SendFile("./public/index.html", fiber.SendFile{Compress: true, ByteRange: true})`)
	assert.Contains(t, content, `c.SendFile("./public/file.txt", fiber.SendFile{Compress: compressEnabled, ByteRange: true})`)
	assert.Contains(t, buf.String(), "Migrating SendFile calls")
}

// v2 served the byte ranges of a file whatever the call, v3 only with ByteRange.
func Test_MigrateSendFileConfig_KeepsTheByteRanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v3"

func one(c fiber.Ctx) error {
	return c.SendFile("./public/index.html")
}

func chained(c *fiber.Ctx) error {
	return c.Status(200).SendFile(path)
}

func multiline(c fiber.Ctx) error {
	if err := c.SendFile(
		"./public/a.html",
	); err != nil {
		return err
	}
	return c.SendFile(
		"./public/b.html",
		true,
	)
}
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateSendFileConfig(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, `package main

import "github.com/gofiber/fiber/v3"

func one(c fiber.Ctx) error {
	return c.SendFile("./public/index.html", fiber.SendFile{ByteRange: true})
}

func chained(c *fiber.Ctx) error {
	return c.Status(200).SendFile(path, fiber.SendFile{ByteRange: true})
}

func multiline(c fiber.Ctx) error {
	if err := c.SendFile(
		"./public/a.html", fiber.SendFile{ByteRange: true},
	); err != nil {
		return err
	}
	return c.SendFile(
		"./public/b.html",
		fiber.SendFile{Compress: true, ByteRange: true},
	)
}
`, readFile(t, file))
}

// The v2 client had an Agent.SendFile(path, field) of its own, and text that merely looks
// like a call is no call.
func Test_MigrateSendFileConfig_LeavesWhatIsNotACtxCall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import "github.com/gofiber/fiber/v3"

// c.SendFile("./commented.html", true) stays a comment.
const doc = "c.SendFile(path, true)"

func upload(agent *fiber.Agent, c fiber.Ctx) {
	agent.SendFile("./report.pdf", "report")
	uploader.SendFile("./report.pdf", "report")
	_ = c.SendFile(files...)
}
`
	file := writeTempFile(t, dir, source)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateSendFileConfig(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, buf.String())
}

func Test_MigrateSendFileConfig_LeavesTheConfigOfV3(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	_ = c.SendFile("./a.html", fiber.SendFile{Compress: true})
	return c.SendFile("./b.html", &fiber.SendFile{ByteRange: true})
}
`
	file := writeTempFile(t, dir, source)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateSendFileConfig(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, buf.String())
}

func Test_MigrateSendFileConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msendfile-idempotent")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"

func handler(c fiber.Ctx) error {
    if err := c.SendFile("./public/a.html"); err != nil {
        return err
    }
    return c.SendFile("./public/index.html", true)
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	migrated := readFile(t, file)
	assert.Contains(t, migrated, `c.SendFile("./public/a.html", fiber.SendFile{ByteRange: true})`)
	assert.Contains(t, migrated, `c.SendFile("./public/index.html", fiber.SendFile{Compress: true, ByteRange: true})`)
	assert.Contains(t, buf.String(), "Migrating SendFile calls")

	var second bytes.Buffer
	secondCmd := newCmd(&second)
	require.NoError(t, v3.MigrateSendFileConfig(secondCmd, dir, nil, nil))

	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, second.String())
}
