package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_rewriteAcquireAgentBlocks(t *testing.T) {
	content := `package main

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
}`

	updated, changed := rewriteAcquireAgentBlocks(content)
	require.True(t, changed, "expected rewrite")
	assert.Contains(t, updated, "github.com/gofiber/fiber/v3/client")
	assert.Contains(t, updated, "resp, err := client.Post")
	assert.Contains(t, updated, "retCode = resp.StatusCode()")
	assert.Contains(t, updated, "retBody = resp.Body()")
	assert.Contains(t, updated, "err = resp.JSON(&t)")
	assert.Contains(t, updated, "if err != nil {")
}
