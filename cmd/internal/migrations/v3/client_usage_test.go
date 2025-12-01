package v3_test

import (
	"bytes"
	"go/format"
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
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "encoding/json"

    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/client"
)

func getSomething(c *fiber.Ctx) (err error) {
    agent, err := client.Get("https://example.com")
    var statusCode int
    var body []byte
    if err == nil {
        statusCode = agent.StatusCode()
        body = agent.Body()
    }
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "err": err,
        })
    }

    var something fiber.Map
    err = json.Unmarshal(body, &something)
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "err": err,
        })
    }

    postAgent, err := client.Post("https://example.com", client.Config{Body: "{\"name\":\"fiber\"}"})
    var postCode int
    var postBody []byte
    if err == nil {
        postCode = postAgent.StatusCode()
        postBody = postAgent.Body()
    }
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "err": err,
        })
    }

    text, err := client.Get("https://example.com/text")
    var textStatus int
    var textBody string
    if err == nil {
        textStatus = text.StatusCode()
        textBody = text.String()
    }
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "err": err,
        })
    }

    var structured map[string]any
    structAgent, err := client.Get("https://example.com/json")
    var structStatus int
    var structBody []byte
    if err == nil {
        structStatus = structAgent.StatusCode()
        structBody = structAgent.Body()
        err = structAgent.JSON(&structured)
    }
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "err": err,
        })
    }

    return c.Status(statusCode + postCode + textStatus + structStatus).JSON(fiber.Map{
        "first":    something,
        "second":   string(postBody),
        "third":    textBody,
        "fourth":   structBody,
        "combined": structured,
    })
}`)

	assert.Equal(t, expected, content)
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
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import "github.com/gofiber/fiber/v3"

func handler(c *fiber.Ctx) error {
    return c.SendStatus(fiber.StatusOK)
}`)

	assert.Equal(t, expected, content)
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
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "github.com/gofiber/fiber/v3/client"
)

func deleteSomething() {
    agent, err := client.Delete("https://example.com/delete")
    var statusCode int
    var body []byte
    if err == nil {
        statusCode = agent.StatusCode()
        body = agent.Body()
    }
    if err != nil {
        panic(err)
    }

    _ = statusCode
    _ = body
}`)

	assert.Equal(t, expected, content)
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

	content := gofmtSource(t, readFile(t, file))
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
        err     error
        t       map[string]any
    )

    resp, err := client.Post(fmt.Sprintf("https://github.com/login/oauth/access_token?code=%s", code), client.Config{Header: map[string]string{"accept": "application/json"}})
    if err != nil {
        return err
    }
    retCode = resp.StatusCode()
    retBody = resp.Body()
    if err == nil {
        err = resp.JSON(&t)
    }
    if err != nil {
        return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "err": err,
        })
    }

    _ = retCode
    _ = retBody
    return nil
}`)

	assert.Equal(t, expected, content)
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
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "fmt"
    "time"

    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/client"
)

func demo() {
    agent, err := client.Get("https://api.example.com/data", client.Config{Header: map[string]string{"X-Custom-Header": "my-value"}, Param: map[string]string{"active": "true", "user": "john"}})
    var statusCode int
    var body []byte
    if err == nil {
        statusCode = agent.StatusCode()
        body = agent.Body()
    }
    if err != nil {
        fmt.Println("Request failed:", err)
    }

    data := fiber.Map{"name": "Alice", "age": 30}
    poster, err := client.Post("https://api.example.com/users", client.Config{Body: data})
    var postStatus int
    var postBody []byte
    if err == nil {
        postStatus = poster.StatusCode()
        postBody = poster.Body()
    }
    if err != nil {
        fmt.Println("Error:", err)
    }

    slow, err := client.Get("https://api.example.com/slow-data", client.Config{Timeout: 2 * time.Second})
    var slowStatus int
    var slowBody string
    if err == nil {
        slowStatus = slow.StatusCode()
        slowBody = slow.String()
    }
    if err != nil {
        fmt.Println("Request timed out or failed:", err)
    }

    _ = statusCode
    _ = body
    _ = postStatus
    _ = postBody
    _ = slowStatus
    _ = slowBody
}`)

	assert.Equal(t, expected, content)
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

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "encoding/json"
    "fmt"

    "github.com/gofiber/fiber/v3/client"
)

func main() {
    resp, err := client.Get("https://httpbin.org/json")
    if err != nil {
        panic(err)
    }
    status := resp.StatusCode()
    body := resp.Body()
    if err != nil {
        panic(err)
    }

    var out map[string]any
    if err := json.Unmarshal(body, &out); err != nil {
        panic(err)
    }

    fmt.Println("Status:", status)
    fmt.Println("Title:", out["slideshow"])
}`)

	assert.Equal(t, expected, content)
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

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3/client"
)

func main() {
    agent, err := client.Get("http://localhost:3000", client.Config{Header: map[string]string{"Authorization": "Basic am9objpkb2U="}})
    var status int
    var body []byte
    if err == nil {
        status = agent.StatusCode()
        body = agent.Body()
    }
    if err != nil {
        panic(err)
    }

    fmt.Println("Status:", status)
    fmt.Println("Body:", string(body))
}`)

	assert.Equal(t, expected, content)
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

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "os"

    "github.com/gofiber/fiber/v3/client"
)

func main() {
    pool, _ := x509.SystemCertPool()
    cert, _ := os.ReadFile("ssl.cert")
    pool.AppendCertsFromPEM(cert)

    client.C().SetTLSConfig(&tls.Config{RootCAs: pool})
    agent, err := client.Get("https://localhost:3000")
    var status int
    var body []byte
    if err == nil {
        status = agent.StatusCode()
        body = agent.Body()
    }
    if err != nil {
        panic(err)
    }

    fmt.Println("Status:", status)
    fmt.Println("Body:", string(body))
}`)

	assert.Equal(t, expected, content)
}

func gofmtSource(t *testing.T, src string) string {
	t.Helper()

	formatted, err := format.Source([]byte(src))
	require.NoError(t, err)
	return string(formatted)
}
