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
			name: "a test that reaches the app through a helper package of the module",
			file: "svc/svc_test.go",
			others: map[string]string{"svc/svc.go": `package svc

import "github.com/gofiber/fiber/v2"

func New() *fiber.App { return fiber.New() }
`},
			in: `package svc_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/app/svc"
)

func TestContentTypes(t *testing.T) {
	app := svc.New()
	resp, _ := app.Test(httptest.NewRequest("GET", "/json", nil))
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content type %q", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Content-Type") == "text/plain; charset=utf-8" {
		t.Error("kept")
	}
}

func TestNotFound(t *testing.T) {
	resp, _ := svc.New().Test(httptest.NewRequest("GET", "/nope", nil))
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "Cannot GET /nope") {
		t.Fatal("body")
	}
}
`,
			notes: []string{"svc/svc_test.go:15: Fiber v3 adds", "svc/svc_test.go:26: Fiber v3 answers an unmatched route"},
		},
		{
			name: "a test of the package that has another file with the app",
			file: "svc/svc_test.go",
			others: map[string]string{"svc/svc.go": `package svc

import "github.com/gofiber/fiber/v2"

func New() *fiber.App { return fiber.New() }
`},
			in: `package svc

import (
	"net/http/httptest"
	"testing"
)

func TestPackage(t *testing.T) {
	resp, _ := New().Test(httptest.NewRequest("GET", "/json", nil))
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Error("type")
	}
}
`,
			notes: []string{"svc/svc_test.go:10: Fiber v3 adds"},
		},
		{
			name: "a test of a package that cannot reach fiber stays silent",
			file: "util/util_test.go",
			others: map[string]string{"util/util.go": `package util

func Name() string { return "x" }
`},
			in: `package util_test

import (
	"net/http/httptest"
	"testing"

	"example.com/app/util"
)

func TestOther(t *testing.T) {
	rec := httptest.NewRecorder()
	if rec.Header().Get("Content-Type") != "application/json" || util.Name() == "Cannot GET /x" {
		t.Error("x")
	}
}
`,
		},
		{
			name: "a file that is no test and has no fiber import stays silent",
			file: "svc/helper.go",
			others: map[string]string{"svc/svc.go": `package svc

import "github.com/gofiber/fiber/v2"

func New() *fiber.App { return fiber.New() }
`},
			in: `package svc

const missing = "Cannot GET /x"
`,
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
