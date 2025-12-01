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
	assert.Contains(t, content, "github.com/gofiber/fiber/v3/client")
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

func Test_MigrateClientUsage_RewritesHeadersQueriesAndTimeout(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientheaders")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"
    "time"

    "github.com/gofiber/fiber/v3"
)

func demo() {
    agent := fiber.Get("https://api.example.com/data")
    agent.Set("X-Custom-Header", "my-value")
    agent.QueryString("user=john&active=true")
    statusCode, body, errs := agent.Bytes()
    if len(errs) > 0 {
        fmt.Println("Request failed:", errs)
    }

    data := fiber.Map{"name": "Alice", "age": 30}
    poster := fiber.Post("https://api.example.com/users")
    poster.JSON(data)
    postStatus, postBody, errs := poster.Bytes()
    if len(errs) > 0 {
        fmt.Println("Error:", errs)
    }

    slow := fiber.Get("https://api.example.com/slow-data")
    slow.Timeout(2 * time.Second)
    slowStatus, slowBody, errs := slow.String()
    if len(errs) > 0 {
        fmt.Println("Request timed out or failed:", errs)
    }

    _ = statusCode
    _ = body
    _ = postStatus
    _ = postBody
    _ = slowStatus
    _ = slowBody
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "agent, err := client.Get(\"https://api.example.com/data\", client.Config{Header: map[string]string{\"X-Custom-Header\": \"my-value\"}, Param: map[string]string{\"active\": \"true\", \"user\": \"john\"}})")
	assert.Contains(t, content, "statusCode := agent.StatusCode()")
	assert.Contains(t, content, "body := agent.Body()")
	assert.Contains(t, content, "poster, err := client.Post(\"https://api.example.com/users\", client.Config{Body: data})")
	assert.Contains(t, content, "postStatus := poster.StatusCode()")
	assert.Contains(t, content, "postBody := poster.Body()")
	assert.Contains(t, content, "slow, err := client.Get(\"https://api.example.com/slow-data\", client.Config{Timeout: 2 * time.Second})")
	assert.Contains(t, content, "slowStatus := slow.StatusCode()")
	assert.Contains(t, content, "slowBody := slow.String()")
	assert.NotContains(t, content, "QueryString(")
	assert.NotContains(t, content, "Timeout(")
}

func Test_MigrateClientUsage_RewritesParseBytesFlow(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparse")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "encoding/json"
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func main() {
    a := fiber.AcquireAgent()
    defer fiber.ReleaseAgent(a)

    req := a.Request()
    req.Header.SetMethod(fiber.MethodGet)
    req.SetRequestURI("https://httpbin.org/json")

    if err := a.Parse(); err != nil {
        panic(err)
    }

    status, body, errs := a.Bytes()
    if len(errs) > 0 {
        panic(errs)
    }

    var out map[string]any
    if err := json.Unmarshal(body, &out); err != nil {
        panic(err)
    }

    fmt.Println("Status:", status)
    fmt.Println("Title:", out["slideshow"])
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "resp, err := client.Get(\"https://httpbin.org/json\")")
	assert.Contains(t, content, "status := resp.StatusCode()")
	assert.Contains(t, content, "body := resp.Body()")
	assert.Contains(t, content, "err := json.Unmarshal(body, &out)")
	assert.Contains(t, content, "if err != nil {")
}

func Test_MigrateClientUsage_RewritesBasicAuth(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientbasic")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func main() {
    agent := fiber.Get("http://localhost:3000")
    agent.BasicAuth("john", "doe")
    status, body, errs := agent.Bytes()
    if len(errs) > 0 {
        panic(errs)
    }

    fmt.Println("Status:", status)
    fmt.Println("Body:", string(body))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "agent, err := client.Get(\"http://localhost:3000\", client.Config{Header: map[string]string{\"Authorization\": \"Basic am9objpkb2U=\"}})")
	assert.Contains(t, content, "github.com/gofiber/fiber/v3/client")
	assert.Contains(t, content, "Status:")
}

func Test_MigrateClientUsage_RewritesTLSConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclienttls")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "os"

    "github.com/gofiber/fiber/v3"
)

func main() {
    pool, _ := x509.SystemCertPool()
    cert, _ := os.ReadFile("ssl.cert")
    pool.AppendCertsFromPEM(cert)

    agent := fiber.Get("https://localhost:3000")
    agent.TLSConfig(&tls.Config{RootCAs: pool})
    status, body, errs := agent.Bytes()
    if len(errs) > 0 {
        panic(errs)
    }

    fmt.Println("Status:", status)
    fmt.Println("Body:", string(body))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "client.C().SetTLSConfig(&tls.Config{RootCAs: pool})")
	assert.Contains(t, content, "agent, err := client.Get(\"https://localhost:3000\")")
	assert.Contains(t, content, "status := agent.StatusCode()")
	assert.Contains(t, content, "body := agent.Body()")
}
