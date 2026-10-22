package v3_test

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateClientUsage_NoChanges(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusOK)
}
`
	run := clientMigrateSource(t, src)
	assert.Equal(t, src, run.files["main.go"])
	assert.Empty(t, run.output)
	assert.Empty(t, run.notes)
}

func Test_MigrateClientUsage_PrintsWhenSomethingChanged(t *testing.T) {
	t.Parallel()

	run := clientMigrateSource(t, `package main

import "github.com/gofiber/fiber/v2"

func get(url string) error {
	_, _, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
`)
	assert.Contains(t, run.output, "Migrating client usage")
	assert.Empty(t, run.notes)

	// A request that moves to the copy of the v2 agent changes its file too.
	run = clientMigrateSource(t, `package main

import "github.com/gofiber/fiber/v2"

func get(url string) error {
	_, _, errs := fiber.Get(url).Host("x").Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
`)
	assert.Contains(t, run.output, "Migrating client usage")
	assert.Len(t, run.notes, 1)

	// Nothing is printed for a request that is only reported, because its module
	// cannot have the copy.
	run = clientRunProject(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v2"

func get(url string) error {
	_, _, errs := fiber.Get(url).Host("x").Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
`}, false)
	assert.Empty(t, run.output)
	assert.Len(t, run.notes, 1)
}

func Test_MigrateClientUsage_StringIsNotTrimmed(t *testing.T) {
	t.Parallel()

	run := clientMigrateSource(t, `package main

import "github.com/gofiber/fiber/v2"

func get(url string) string {
	_, body, _ := fiber.Get(url).String()
	return body
}
`)

	// The v3 Response.String trims the body, v2 returned it as it was.
	assert.Contains(t, run.files["main.go"], "body = string(resp.Body())")
	assert.NotContains(t, run.files["main.go"], "resp.String()")
}

func Test_MigrateClientUsage_ManyRequestsInOneFile(t *testing.T) {
	t.Parallel()

	src := []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\n\t\"github.com/gofiber/fiber/v3\"\n)\n\n")
	for i := 1; i <= 8; i++ {
		src = fmt.Appendf(src, `func fn%d() {
	agent := fiber.Get("http://localhost:3000/%d")
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		fmt.Println(errs)
	}
	fmt.Println(code, string(body))
}

`, i, i)
	}
	src = append(src, "func main() {\n\tdata, err := os.ReadFile(\"x\")\n\tfmt.Println(string(data), err)\n}\n"...)

	dir := t.TempDir()
	file := writeTempFile(t, dir, string(src))

	// A file with many requests and a plain err := later used to send the
	// migration into an endless loop.
	done := make(chan error, 1)
	go func() { done <- v3.MigrateClientUsage(newCmd(&bytes.Buffer{}), dir, nil, nil) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("the migration does not terminate")
	}

	got := readFile(t, file)
	assert.NotContains(t, got, "fiber.Get")
	assert.Equal(t, 8, strings.Count(got, "client.Get("))
	assert.Contains(t, got, `data, err := os.ReadFile("x")`)
}

func Test_MigrateClientUsage_KeepsUnrelatedFunctions(t *testing.T) {
	t.Parallel()

	run := clientMigrateSource(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func first(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	err := a.Parse()
	if err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

// audit is unrelated business logic between the two client helpers.
func audit(user string) string {
	return "audit:" + user
}

func second(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func main() {
	fmt.Println(first("x"))
	fmt.Println(audit("bob"))
	fmt.Println(second("x"))
}
`)

	got := run.files["main.go"]
	assert.Contains(t, got, "// audit is unrelated business logic between the two client helpers.")
	assert.Contains(t, got, "func audit(user string) string {")
	assert.Contains(t, got, `return "audit:" + user`)
	assert.Contains(t, got, "func second(url string)")
	assert.Contains(t, got, "func main()")
	assert.NotContains(t, got, "fiber.AcquireAgent")
	assert.NotContains(t, got, "ReleaseAgent")
	_, err := format.Source([]byte(got))
	require.NoError(t, err)
}

func Test_MigrateClientUsage_CopiedRequestKeepsItsCalls(t *testing.T) {
	t.Parallel()

	// A request that runs on the copy of the v2 agent keeps every call, deferred
	// release included: only the names of the copy replace the ones of Fiber v2.
	run := clientMigrateSource(t, `package main

import "github.com/gofiber/fiber/v2"

func get(url string) error {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.SetRequestURI(url)
	a.Host("virtual")
	if err := a.Parse(); err != nil {
		return err
	}
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
`)
	got := run.files["main.go"]
	assert.Contains(t, got, "a := fibermigrate.AcquireAgent()")
	assert.Contains(t, got, "defer fibermigrate.ReleaseAgent(a)")
	assert.Contains(t, got, "a.Host(\"virtual\")")
	assert.Contains(t, got, "_, _, errs := a.Bytes()")
	assert.NotContains(t, got, "client.")
	assert.NotContains(t, got, "gofiber")
	assert.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0], "this request still runs on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go")
}

func Test_MigrateClientUsage_LeftAloneRequestKeepsItsRelease(t *testing.T) {
	t.Parallel()

	// Where the module cannot have the copy, the request is not touched, deferred
	// release included.
	run := clientRunProject(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v2"

func get(url string) error {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.SetRequestURI(url)
	a.Host("virtual")
	if err := a.Parse(); err != nil {
		return err
	}
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
`}, false)
	got := run.files["main.go"]
	assert.Contains(t, got, "defer fiber.ReleaseAgent(a)")
	assert.Contains(t, got, "a.Host(\"virtual\")")
	assert.Contains(t, got, "_, _, errs := a.Bytes()")
	assert.NotContains(t, got, "client.")
	assert.NotContains(t, got, "fibermigrate")
	assert.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0], "left as Fiber v2 code")
	assert.Contains(t, run.notes[0], "not in a Go module")
}

func clientGofmtSource(t *testing.T, src string) string {
	t.Helper()

	formatted, err := format.Source([]byte(src))
	require.NoError(t, err, "the source does not parse:\n%s", src)
	return string(formatted)
}
