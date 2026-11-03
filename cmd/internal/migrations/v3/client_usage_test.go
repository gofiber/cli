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
        errs    []error
        t       map[string]any
    )

    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL(fmt.Sprintf("https://github.com/login/oauth/access_token?code=%s", code))
    req.SetHeader("accept", "application/json")
    resp, err := req.Send()
    if err != nil {
        return err
    }
    if err == nil {
        retCode = resp.StatusCode()
        retBody = resp.Body()
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

func Test_MigrateClientUsage_RewritesAcquireAgentStructWithVarsBetweenParseAndCall(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientagentvars")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func handler(ctx *fiber.Ctx, code string) error {
    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.Header.Set("accept", "application/json")
    req.SetRequestURI(fmt.Sprintf("https://github.com/login/oauth/access_token?code=%s", code))
    if err := a.Parse(); err != nil {
        return err
    }

    var retCode int
    var retBody []byte
    var errs []error
    var t map[string]any

    if retCode, retBody, errs = a.Struct(&t); len(errs) > 0 {
        return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "errs": errs,
        })
    }

    var err error
    _ = err
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
    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL(fmt.Sprintf("https://github.com/login/oauth/access_token?code=%s", code))
    req.SetHeader("accept", "application/json")
    var retCode int
    var retBody []byte
    var t map[string]any
    resp, clientErr := req.Send()
    if clientErr != nil {
        return clientErr
    }
    if clientErr == nil {
        retCode = resp.StatusCode()
        retBody = resp.Body()
        clientErr = resp.JSON(&t)
    }
    if clientErr != nil {
        return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "clientErr": clientErr,
        })
    }
    _ = retCode
    _ = retBody

    var err error
    _ = err
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
    a := client.New()
    req := a.R()
    req.SetMethod("GET")
    req.SetURL("https://httpbin.org/json")
    resp, err := req.Send()
    if err != nil {
        panic(err)
    }
    var status int
    var body []byte
    if err == nil {
        status = resp.StatusCode()
        body = resp.Body()
    }
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

func Test_MigrateClientUsage_RewritesParseBytesFlowWithBody(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparsebody")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func main() {
    a := fiber.AcquireAgent()
    defer fiber.ReleaseAgent(a)

    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.Header.Set("Content-Type", "application/json")
    req.SetBodyString("{\"demo\":true}")
    req.SetRequestURI("https://httpbin.org/post")

    if err := a.Parse(); err != nil {
        panic(err)
    }

    status, body, errs := a.Bytes()
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
    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL("https://httpbin.org/post")
    req.SetHeader("Content-Type", "application/json")
    req.SetRawBody([]byte("{\"demo\":true}"))
    resp, err := req.Send()
    if err != nil {
        panic(err)
    }
    var status int
    var body []byte
    if err == nil {
        status = resp.StatusCode()
        body = resp.Body()
    }
    if err != nil {
        panic(err)
    }

    fmt.Println("Status:", status)
    fmt.Println("Body:", string(body))
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

    agent, err := client.Get("https://localhost:3000", client.Config{TLSConfig: &tls.Config{RootCAs: pool}})
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

func Test_MigrateClientUsage_RewritesDebugAndReuse(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientdebug")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func main() {
    agent := fiber.Get("http://localhost:3000")
    agent.Debug()
    agent.Reuse()
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
	// Debug() and Reuse() are removed as they don't exist in v3
	expected := gofmtSource(t, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3/client"
)

func main() {
    agent, err := client.Get("http://localhost:3000")
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

func Test_MigrateClientUsage_RemovesFiberImportWhenUnused(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientimport")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func main() {
    agent := fiber.Get("http://localhost:3000")
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
	// fiber import should be removed as it's no longer used
	expected := gofmtSource(t, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3/client"
)

func main() {
    agent, err := client.Get("http://localhost:3000")
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

func Test_MigrateClientUsage_KeepsFiberImportWhenUsed(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientkeep")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func main() {
    agent := fiber.Get("http://localhost:3000")
    status, body, errs := agent.Bytes()
    if len(errs) > 0 {
        panic(errs)
    }

    fmt.Println("Status:", status)
    fmt.Println("Body:", string(body))
    fmt.Println("StatusOK:", fiber.StatusOK)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	// fiber import should be kept as fiber.StatusOK is still used
	expected := gofmtSource(t, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/client"
)

func main() {
    agent, err := client.Get("http://localhost:3000")
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
    fmt.Println("StatusOK:", fiber.StatusOK)
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_AcquireAgentWithoutParseBlock(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientacquire")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// This is the exact example from the user's request
	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.Header.Set("accept", "application/json")
    req.SetRequestURI(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    if err := a.Parse(); err != nil {
        fmt.Printf("could not create HTTP request: %v", err)
    }

    status, body, errs := a.Bytes()
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

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    req.SetHeader("accept", "application/json")
    resp, err := req.Send()
    if err != nil {
        fmt.Printf("could not create HTTP request: %v", err)
    }
    var status int
    var body []byte
    if err == nil {
        status = resp.StatusCode()
        body = resp.Body()
    }
    if err != nil {
        panic(err)
    }

    fmt.Println("Status:", status)
    fmt.Println("Body:", string(body))
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_AcquireAgentOnlyParse(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparseonly")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/log"
)

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.Header.Set("accept", "application/json")
    req.SetRequestURI(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    if err := a.Parse(); err != nil {
        log.Errorf("could not create HTTP request: %v", err)
    }
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3/client"
    "github.com/gofiber/fiber/v3/log"
)

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    req.SetHeader("accept", "application/json")
    _, err := req.Send()
    if err != nil {
        log.Errorf("could not create HTTP request: %v", err)
    }
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_AcquireAgentOnlyParseWithExistingErr(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparsewitherr")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    var err error
    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.Header.Set("accept", "application/json")
    req.SetRequestURI(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    if err := a.Parse(); err != nil {
        panic(err)
    }
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

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    var err error
    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    req.SetHeader("accept", "application/json")
    _, err = req.Send()
    if err != nil {
        panic(err)
    }
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_AcquireAgentOnlyParseAvoidsErrCollisions(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparsecollision")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/log"
)

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.Header.Set("accept", "application/json")
    req.SetRequestURI(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    if err := a.Parse(); err != nil {
        log.Errorf("could not create HTTP request: %v", err)
    }
    var err error
    _ = err
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3/client"
    "github.com/gofiber/fiber/v3/log"
)

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    req.SetHeader("accept", "application/json")
    _, clientErr := req.Send()
    if clientErr != nil {
        log.Errorf("could not create HTTP request: %v", clientErr)
    }
    var err error
    _ = err
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_AcquireAgentOnlyParseKeepsTrailingCode(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparsepreserve")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func handler(code string) {
    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.SetRequestURI(fmt.Sprintf("https://example.com/auth?code=%s", code))
    if err := a.Parse(); err != nil {
        fmt.Println(err)
    }
    // keep
    value := 123
    _ = value
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

func handler(code string) {
    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL(fmt.Sprintf("https://example.com/auth?code=%s", code))
    _, err := req.Send()
    if err != nil {
        fmt.Println(err)
    }
    // keep
    value := 123
    _ = value
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_AcquireAgentOnlyParseIgnoresErrLikeVarNames(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparsevarblock")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "github.com/gofiber/fiber/v3"
)

var (
    errMsg string
)

func handler() {
    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodGet)
    req.SetRequestURI("https://example.com")
    if err := a.Parse(); err != nil {
        println(err)
    }
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	expected := gofmtSource(t, `package main

import (
    "github.com/gofiber/fiber/v3/client"
)

var (
    errMsg string
)

func handler() {
    a := client.New()
    req := a.R()
    req.SetMethod("GET")
    req.SetURL("https://example.com")
    _, err := req.Send()
    if err != nil {
        println(err)
    }
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_AcquireAgentOnlyParseIgnoresErrLikeNames(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparsealiases")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodPost)
    req.Header.Set("accept", "application/json")
    req.SetRequestURI(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    if err := a.Parse(); err != nil {
        fmt.Printf("could not create HTTP request: %v", err)
    }

    var errMsg string
    _ = errMsg
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

var (
    ClientID     = "id"
    ClientSecret = "secret"
)

func handler(code string) {
    a := client.New()
    req := a.R()
    req.SetMethod("POST")
    req.SetURL(fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s", ClientID, ClientSecret, code))
    req.SetHeader("accept", "application/json")
    _, err := req.Send()
    if err != nil {
        fmt.Printf("could not create HTTP request: %v", err)
    }

    var errMsg string
    _ = errMsg
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_AcquireAgentOnlyParseRespectsErrsSlice(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientparseerrs")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3"
)

func handler(url string) {
    var errs []error

    a := fiber.AcquireAgent()
    req := a.Request()
    req.Header.SetMethod(fiber.MethodGet)
    req.SetRequestURI(url)
    if err := a.Parse(); err != nil {
        errs = append(errs, err)
    }

    fmt.Println(errs)
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

func handler(url string) {
    var errs []error

    a := client.New()
    req := a.R()
    req.SetMethod("GET")
    req.SetURL(url)
    _, err := req.Send()
    if err != nil {
        errs = append(errs, err)
    }

    fmt.Println(errs)
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_RemovesSingleLineImport(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientsingle")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Single-line import without block
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v3"

func main() {
    agent := fiber.Get("http://localhost:3000")
    status, body, errs := agent.Bytes()
    if len(errs) > 0 {
        panic(errs)
    }
    _ = status
    _ = body
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	// The ensureImport function converts single-line imports to block imports
	expected := gofmtSource(t, `package main

import (
    "github.com/gofiber/fiber/v3/client"
)

func main() {
    agent, err := client.Get("http://localhost:3000")
    var status int
    var body []byte
    if err == nil {
        status = agent.StatusCode()
        body = agent.Body()
    }
    if err != nil {
        panic(err)
    }
    _ = status
    _ = body
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_RemovesAliasedImport(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientalias")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Aliased import
	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    f "github.com/gofiber/fiber/v3"
)

func main() {
    agent := f.Get("http://localhost:3000")
    status, body, errs := agent.Bytes()
    if len(errs) > 0 {
        panic(errs)
    }
    fmt.Println(status, string(body))
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	// The aliased import f "..." should be removed since f. is no longer used
	expected := gofmtSource(t, `package main

import (
    "fmt"

    "github.com/gofiber/fiber/v3/client"
)

func main() {
    agent, err := client.Get("http://localhost:3000")
    var status int
    var body []byte
    if err == nil {
        status = agent.StatusCode()
        body = agent.Body()
    }
    if err != nil {
        panic(err)
    }
    fmt.Println(status, string(body))
}`)

	assert.Equal(t, expected, content)
}

func Test_MigrateClientUsage_KeepsAliasedImportWhenUsed(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mclientaliaskeep")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Aliased import that's still used
	file := writeTempFile(t, dir, `package main

import (
    "fmt"

    f "github.com/gofiber/fiber/v3"
)

func main() {
    agent := f.Get("http://localhost:3000")
    status, body, errs := agent.Bytes()
    if len(errs) > 0 {
        panic(errs)
    }
    fmt.Println(status, string(body))
    fmt.Println("StatusOK:", f.StatusOK)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateClientUsage(cmd, dir, nil, nil))

	content := gofmtSource(t, readFile(t, file))
	// The aliased import f "..." should be kept since f.StatusOK is still used
	expected := gofmtSource(t, `package main

import (
    "fmt"

    f "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/client"
)

func main() {
    agent, err := client.Get("http://localhost:3000")
    var status int
    var body []byte
    if err == nil {
        status = agent.StatusCode()
        body = agent.Body()
    }
    if err != nil {
        panic(err)
    }
    fmt.Println(status, string(body))
    fmt.Println("StatusOK:", f.StatusOK)
}`)

	assert.Equal(t, expected, content)
}

func gofmtSource(t *testing.T, src string) string {
	t.Helper()

	formatted, err := format.Source([]byte(src))
	require.NoError(t, err)
	return string(formatted)
}
