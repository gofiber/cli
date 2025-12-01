package v3

import (
	"strings"
	"testing"
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
	if !changed {
		t.Fatalf("expected rewrite")
	}
	if !strings.Contains(updated, "github.com/gofiber/fiber/v3/client") {
		t.Fatalf("client import missing: %s", updated)
	}
	if !strings.Contains(updated, "resp, err := client.Post") {
		t.Fatalf("post call missing: %s", updated)
	}
	if !strings.Contains(updated, "retCode = resp.StatusCode()") {
		t.Fatalf("status rewrite missing: %s", updated)
	}
	if !strings.Contains(updated, "retBody = resp.Body()") {
		t.Fatalf("body rewrite missing: %s", updated)
	}
	if !strings.Contains(updated, "err = resp.JSON(&t)") {
		t.Fatalf("json decode missing: %s", updated)
	}
	if !strings.Contains(updated, "if err != nil {") {
		t.Fatalf("error handling missing: %s", updated)
	}
}
