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

func Test_MigrateClientUsage_DocsExample(t *testing.T) {
	t.Parallel()

	run := migrateClientSource(t, `package main

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

func getSomething(c *fiber.Ctx) (err error) {
	agent := fiber.Get("http://localhost:4000/something")
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

	return c.Status(statusCode).JSON(something)
}
`)

	// The []error is part of the JSON the handler answers with: it stays a
	// slice. err is the named result, so the request gets an error of its own.
	assertClientSource(t, `package main

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func getSomething(c *fiber.Ctx) (err error) {
	resp, clientErr := client.Get("http://localhost:4000/something")
	var statusCode int
	var body []byte
	var errs []error
	if clientErr == nil {
		statusCode = resp.StatusCode()
		body = resp.Body()
	}
	if clientErr != nil {
		errs = []error{clientErr}
	}
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

	return c.Status(statusCode).JSON(something)
}
`, run.files["main.go"])
	assert.Contains(t, run.output, "Migrating client usage")
	assert.Empty(t, run.notes)
}

func Test_MigrateClientUsage_NoChanges(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusOK)
}
`
	run := migrateClientSource(t, src)
	assert.Equal(t, src, run.files["main.go"])
	assert.Empty(t, run.output)
	assert.Empty(t, run.notes)
}

func Test_MigrateClientUsage_FluentChains(t *testing.T) {
	t.Parallel()

	run := migrateClientSource(t, `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

type payload struct{ Name string }
type result struct{ ID int }

func plain(url string) ([]byte, error) {
	code, body, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return nil, fmt.Errorf("get failed: %v", errs)
	}
	if code != fiber.StatusOK {
		return nil, fmt.Errorf("status %d", code)
	}
	return body, nil
}

func headers(url, token string) ([]byte, error) {
	status, body, errs := fiber.Get(url).
		Set("Authorization", "Bearer "+token).
		Set("Accept", "application/json").
		Timeout(5 * time.Second).
		Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	_ = status
	return body, nil
}

func decode(url string, p payload) (*result, error) {
	var out result
	status, _, errs := fiber.Post(url).JSON(p).Struct(&out)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	_ = status
	return &out, nil
}

func text(url string) (string, error) {
	_, body, errs := fiber.Delete(url).String()
	if len(errs) > 0 {
		return "", errs[0]
	}
	return body, nil
}
`)

	assertClientSource(t, `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

type payload struct{ Name string }
type result struct{ ID int }

func plain(url string) ([]byte, error) {
	resp, err := client.Get(url)
	var code int
	var body []byte
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		return nil, fmt.Errorf("get failed: %v", err)
	}
	if code != fiber.StatusOK {
		return nil, fmt.Errorf("status %d", code)
	}
	return body, nil
}

func headers(url, token string) ([]byte, error) {
	resp, err := client.Get(url, client.Config{
		Header:  map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json"},
		Timeout: 5 * time.Second,
	})
	var status int
	var body []byte
	if err == nil {
		status = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		return nil, err
	}
	_ = status
	return body, nil
}

func decode(url string, p payload) (*result, error) {
	var out result
	resp, err := client.Post(url, client.Config{Body: p})
	var status int
	if err == nil {
		status = resp.StatusCode()
		err = resp.JSON(&out)
	}
	if err != nil {
		return nil, err
	}
	_ = status
	return &out, nil
}

func text(url string) (string, error) {
	resp, err := client.Delete(url)
	var body string
	if err == nil {
		body = string(resp.Body())
	}
	if err != nil {
		return "", err
	}
	return body, nil
}
`, run.files["main.go"])
	assert.Empty(t, run.notes)
}

func Test_MigrateClientUsage_StringIsNotTrimmed(t *testing.T) {
	t.Parallel()

	run := migrateClientSource(t, `package main

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

func Test_MigrateClientUsage_ErrsIdioms(t *testing.T) {
	t.Parallel()

	run := migrateClientSource(t, `package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func first(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func join(url string) error {
	_, _, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func empty(url string) bool {
	code, _, errs := fiber.Get(url).Bytes()
	return len(errs) == 0 && code == 200 && errs == nil
}

func messages(url string) {
	_, _, errs := fiber.Get(url).Bytes()
	if errs != nil {
		log.Println("failed:", errs)
		panic(errs)
	}
}

func text(url string) string {
	_, _, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return errs[0].Error() + fmt.Sprint(errs)
	}
	return ""
}
`)

	assertClientSource(t, `package main

import (
	"fmt"
	"log"

	"github.com/gofiber/fiber/v3/client"
)

func first(url string) ([]byte, error) {
	resp, err := client.Get(url)
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	if err != nil {
		return nil, err
	}
	return body, nil
}

func join(url string) error {
	_, err := client.Get(url)
	if err != nil {
		return err
	}
	return nil
}

func empty(url string) bool {
	resp, err := client.Get(url)
	var code int
	if err == nil {
		code = resp.StatusCode()
	}
	return err == nil && code == 200 && err == nil
}

func messages(url string) {
	_, err := client.Get(url)
	if err != nil {
		log.Println("failed:", err)
		panic(err)
	}
}

func text(url string) string {
	_, err := client.Get(url)
	if err != nil {
		return err.Error() + fmt.Sprint(err)
	}
	return ""
}
`, run.files["main.go"])
}

func Test_MigrateClientUsage_ErrsSliceStaysWhenItIsNeeded(t *testing.T) {
	t.Parallel()

	run := migrateClientSource(t, `package main

import (
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type Response struct {
	Code int
	Errs []error
}

func logErrs(code int, errs []error) { fmt.Println(code, errs) }

func collect(urls []string) error {
	for _, u := range urls {
		_, _, errs := fiber.Get(u).Bytes()
		for _, e := range errs {
			fmt.Println(e)
		}
		if len(errs) > 1 {
			return errors.New("many")
		}
	}
	return nil
}

func wrap(url string) Response {
	code, _, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		logErrs(code, errs)
		return Response{Code: code, Errs: errs}
	}
	return Response{Code: code}
}
`)

	assertClientSource(t, `package main

import (
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v3/client"
)

type Response struct {
	Code int
	Errs []error
}

func logErrs(code int, errs []error) { fmt.Println(code, errs) }

func collect(urls []string) error {
	for _, u := range urls {
		_, err := client.Get(u)
		var errs []error
		if err != nil {
			errs = []error{err}
		}
		for _, e := range errs {
			fmt.Println(e)
		}
		if len(errs) > 1 {
			return errors.New("many")
		}
	}
	return nil
}

func wrap(url string) Response {
	resp, err := client.Get(url)
	var code int
	var errs []error
	if err == nil {
		code = resp.StatusCode()
	}
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		logErrs(code, errs)
		return Response{Code: code, Errs: errs}
	}
	return Response{Code: code}
}
`, run.files["main.go"])
}

func Test_MigrateClientUsage_ErrsOfOtherFunctionsAreLeftAlone(t *testing.T) {
	t.Parallel()

	run := migrateClientSource(t, `package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2"
)

func validate(names []string) error {
	var errs []error
	for _, n := range names {
		if n == "" {
			errs = append(errs, errors.New("empty name"))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func report(errs []error) string {
	return fmt.Sprintf("%d errors: %v", len(errs), errs)
}

func ping(url string) (int, error) {
	agent := fiber.Get(url)
	code, _, errs := agent.Bytes()
	if len(errs) > 0 {
		return 0, errs[0]
	}
	return code, nil
}

func load(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}
`)

	got := run.files["main.go"]
	assert.Contains(t, got, `return errors.Join(errs...)`, "validate keeps its own errs")
	assert.Contains(t, got, `len(errs), errs)`, "report keeps its own errs")
	assert.Contains(t, got, "resp, err := client.Get(url)", "ping uses err: no err is used around it")
	assert.Contains(t, got, `data, err := os.ReadFile(path)`)
	assert.Empty(t, run.notes)
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

	run := migrateClientSource(t, `package main

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
	assert.Contains(t, got, "func audit(user string) string {")
	assert.Contains(t, got, `return "audit:" + user`)
	assert.Contains(t, got, "func second(url string)")
	assert.Contains(t, got, "func main()")
	assert.NotContains(t, got, "fiber.AcquireAgent")
	assert.NotContains(t, got, "ReleaseAgent")
	_, err := format.Source([]byte(got))
	require.NoError(t, err)
}

func gofmtSource(t *testing.T, src string) string {
	t.Helper()

	formatted, err := format.Source([]byte(src))
	require.NoError(t, err, "the source does not parse:\n%s", src)
	return string(formatted)
}
