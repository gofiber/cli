package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateCtxBehaviorNotes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCtxBehaviorNotes, []ctxCase{
		{
			name: "Content-Type compared exactly",
			file: "main_test.go",
			in: `package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestJSON(t *testing.T) {
	app := fiber.New()
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.MIMEApplicationJSON, resp.Header.Get(fiber.HeaderContentType))
	require.Equal(t, "application/xml", resp.Header.Get("content-type"))
	if resp.Header.Get("Content-Type") != "text/html" {
		t.Fatal("html")
	}
	require.Equal(t, "application/json", string(app.AcquireCtx(nil).Response().Header.ContentType()))
	require.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), fiber.MIMEApplicationJSON))
	require.Equal(t, fiber.MIMEApplicationJSONCharsetUTF8, resp.Header.Get("Content-Type"))
	require.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
	require.Equal(t, "application/json", resp.Header.Get("Accept"))
}
`,
			notes: []string{"main_test.go:16: Fiber v3 adds", "main_test.go:17: Fiber v3 adds", "main_test.go:18: Fiber v3 adds", "main_test.go:21: Fiber v3 adds"},
		},
		{
			name: "Content-Type outside of tests is not reported",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	if c.GetRespHeader(fiber.HeaderContentType) == fiber.MIMEApplicationJSON {
		return nil
	}
	return c.Next()
}
`,
		},
		{
			name: "bodies of unmatched routes",
			file: "main_test.go",
			in: `package main

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestNotFound(t *testing.T) {
	app := fiber.New()
	resp, _ := app.Test(httptest.NewRequest(fiber.MethodGet, "/missing", nil))
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Cannot GET /missing" {
		t.Fatal("body")
	}
	want := §Cannot POST /a.txt§
	_ = want
	_ = "Cannot find the file"
}
`,
			notes: []string{"main_test.go:15: Fiber v3 answers an unmatched route", "main_test.go:18: Fiber v3 answers an unmatched route"},
		},
		{
			name: "files without a fiber import",
			file: "main_test.go",
			in: `package main

import "testing"

func TestBody(t *testing.T) {
	if got := "Cannot GET /x"; got != "" {
		t.Fatal(got)
	}
}
`,
		},
	})
}
