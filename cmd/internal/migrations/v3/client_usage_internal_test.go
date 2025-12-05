package v3

import (
	"go/format"
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

	updated, changed := rewriteAcquireAgentBlocksWithAlias(content, "fiber")
	require.True(t, changed, "expected rewrite")
	formatted := gofmtSource(t, updated)
	expected := gofmtSource(t, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/client"
)

func handler(ctx *fiber.Ctx, code string) error {
    var (
        retCode int
        retBody []byte
        errs    []error
        t       map[string]any
    )

    resp, err := client.Post(fmt.Sprintf("https://github.com/login/oauth/access_token?code=%s", code), client.Config{Header: map[string]string{"accept": "application/json"}})
    if err != nil {
        return err
    }
    var retCode int
    var retBody []byte
    if err == nil {
        retCode = resp.StatusCode()
        retBody = resp.Body()
        err = resp.JSON(&t)
    }
    if err != nil {
        return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "errs": errs,
        })
    }

    _ = retCode
    _ = retBody
    return nil
}`)

	assert.Equal(t, expected, formatted)
}

func gofmtSource(t *testing.T, src string) string {
	t.Helper()

	formatted, err := format.Source([]byte(src))
	require.NoError(t, err)
	return string(formatted)
}
