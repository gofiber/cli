package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateClientUsage(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclient")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "encoding/json"

    "github.com/gofiber/fiber/v3"
)

func getSomething(c *fiber.Ctx) (err error) {
    agent := fiber.Get("https://example.com")
    statusCode, body, errs := agent.Bytes()
    if len(errs) > 0 {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "errs": errs,
        })
    }

    var something fiber.Map
    err = json.Unmarshal(body, &something)
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "err": err,
        })
    }

    postAgent := fiber.Post("https://example.com")
    postAgent.BodyString("{\"name\":\"fiber\"}")
    postCode, postBody, errs := postAgent.Bytes()
    if len(errs) > 0 {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "errs": errs,
        })
    }

    text := fiber.Get("https://example.com/text")
    textStatus, textBody, errs := text.String()
    if len(errs) > 0 {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "errs": errs,
        })
    }

    var structured map[string]any
    structAgent := fiber.Get("https://example.com/json")
    structStatus, structBody, errs := structAgent.Struct(&structured)
    if len(errs) > 0 {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "errs": errs,
        })
    }

    return c.Status(statusCode + postCode + textStatus + structStatus).JSON(fiber.Map{
        "first":    something,
        "second":   string(postBody),
        "third":    textBody,
        "fourth":   structBody,
        "combined": structured,
    })
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "\"github.com/gofiber/fiber/v3/client\"")
	assert.Contains(t, content, "agent, err := client.Get(\"https://example.com\")")
	assert.Contains(t, content, "statusCode := agent.StatusCode()")
	assert.Contains(t, content, "body := agent.Body()")
	assert.Contains(t, content, "postAgent, err := client.Post(\"https://example.com\", client.Config{Body: \"{\\\"name\\\":\\\"fiber\\\"}\"})")
	assert.Contains(t, content, "postCode := postAgent.StatusCode()")
	assert.Contains(t, content, "postBody := postAgent.Body()")
	assert.Contains(t, content, "text, err := client.Get(\"https://example.com/text\")")
	assert.Contains(t, content, "textBody := text.String()")
	assert.Contains(t, content, "structAgent, err := client.Get(\"https://example.com/json\")")
	assert.Contains(t, content, "structBody := structAgent.Body()")
	assert.Contains(t, content, "err = structAgent.JSON(&structured)")
	assert.NotContains(t, content, "errs := agent.Bytes")
	assert.NotContains(t, content, "len(errs)")
	assert.Contains(t, buf.String(), "Migrating client usage")
}

func Test_MigrateClientUsage_NoChanges(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientnone")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v3"

func handler(c *fiber.Ctx) error {
    return c.SendStatus(fiber.StatusOK)
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "SendStatus")
	assert.Empty(t, buf.String())
}

func Test_MigrateClientUsage_UpdatesSingleImports(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientsingle")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v3"

func deleteSomething() {
    agent := fiber.Delete("https://example.com/delete")
    statusCode, body, errs := agent.Bytes()
    if len(errs) > 0 {
        panic(errs)
    }

    _ = statusCode
    _ = body
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "import \"github.com/gofiber/fiber/v3/client\"")
	assert.Contains(t, content, "agent, err := client.Delete(\"https://example.com/delete\")")
	assert.Contains(t, content, "statusCode := agent.StatusCode()")
	assert.Contains(t, content, "body := agent.Body()")
	assert.NotContains(t, content, "errs := agent.Bytes()")
	assert.Contains(t, content, "if err != nil {")
}

func Test_MigrateClientUsage_RewritesAcquireAgentStruct(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientagent")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func handler(ctx *fiber.Ctx, code string) error {
    var (
        retCode int
        retBody []byte
        errs    []error
        t       map[string]any
    )

    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.Header.Set("accept", "application/json")
    req.SetRequestURI(fmt.Sprintf("https://github.com/login/oauth/access_token?code=%s", code))
    if err := a.Parse(); err != nil {
        return err
    }

    if retCode, retBody, errs = a.Struct(&t); len(errs) > 0 {
        return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "errs": errs,
        })
    }

    _ = retCode
    _ = retBody
    return nil
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "\"github.com/gofiber/fiber/v3/client\"")
	assert.Contains(t, content, "resp, err := client.Post(fmt.Sprintf(\"https://github.com/login/oauth/access_token?code=%s\", code), client.Config{Header: map[string]string{\"accept\": \"application/json\"}})")
	assert.Contains(t, content, "retCode = resp.StatusCode()")
	assert.Contains(t, content, "retBody = resp.Body()")
	assert.Contains(t, content, "err = resp.JSON(&t)")
	assert.Contains(t, content, "if err != nil {")
}
