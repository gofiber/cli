package v3_test

import (
	"testing"
)

func Test_MigrateClientUsage_Errors(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "docs_example",
			files: map[string]string{
				"main.go": `package main

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
`,
			},
			want: map[string]string{
				"main.go": `package main

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
`,
			},
		},
		{
			name: "err_name_collisions",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2"
)

func fetch(url string) ([]byte, error) {
	agent := fiber.Get(url)
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, fmt.Errorf("get failed: %v", errs)
	}
	if code != 200 {
		return nil, fmt.Errorf("status %d", code)
	}
	return body, nil
}

func loadConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func fetchAgain(url string) error {
	data, err := os.ReadFile("x")
	if err != nil {
		return err
	}
	_, body, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	fmt.Println(len(data), len(body))
	return nil
}
`,
				"other.go": `package main

// err is a package level variable: the request must not report into it.
var err error

func use() error { return err }
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v3/client"
)

func fetch(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get failed: %v", err)
	}
	code, body := resp.StatusCode(), resp.Body()
	if code != 200 {
		return nil, fmt.Errorf("status %d", code)
	}
	return body, nil
}

func loadConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func fetchAgain(url string) error {
	data, err := os.ReadFile("x")
	if err != nil {
		return err
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	body := resp.Body()
	fmt.Println(len(data), len(body))
	return nil
}
`,
				"other.go": `package main

// err is a package level variable: the request must not report into it.
var err error

func use() error { return err }
`,
			},
		},
		{
			name: "errs_compat_slices",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func accumulate(urls []string) []error {
	var all []error
	for _, u := range urls {
		_, _, errs := fiber.Get(u).Bytes()
		all = append(all, errs...)
	}
	return all
}

func returnsSlice(url string) (int, []byte, []error) {
	code, body, errs := fiber.Get(url).Bytes()
	return code, body, errs
}

func countsAndRanges(url string) {
	_, _, errs := fiber.Get(url).Bytes()
	for i, e := range errs {
		fmt.Println(i, e)
	}
	if len(errs) == 1 {
		fmt.Println("one")
	}
}

func inClosure(url string) func() int {
	_, _, errs := fiber.Get(url).Bytes()
	return func() int { return len(errs) }
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3/client"
)

func accumulate(urls []string) []error {
	var all []error
	for _, u := range urls {
		_, err := client.Get(u)
		var errs []error
		if err != nil {
			errs = []error{err}
		}
		all = append(all, errs...)
	}
	return all
}

func returnsSlice(url string) (int, []byte, []error) {
	resp, err := client.Get(url)
	var code int
	var body []byte
	var errs []error
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		errs = []error{err}
	}
	return code, body, errs
}

func countsAndRanges(url string) {
	_, err := client.Get(url)
	var errs []error
	if err != nil {
		errs = []error{err}
	}
	for i, e := range errs {
		fmt.Println(i, e)
	}
	if len(errs) == 1 {
		fmt.Println("one")
	}
}

func inClosure(url string) func() int {
	_, err := client.Get(url)
	var errs []error
	if err != nil {
		errs = []error{err}
	}
	return func() int { return len(errs) }
}
`,
			},
		},
		{
			name: "errs_idioms",
			files: map[string]string{
				"main.go": `package main

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
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"
	"log"

	"github.com/gofiber/fiber/v3/client"
)

func first(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	body := resp.Body()
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
`,
			},
		},
		{
			name: "errs_other_functions",
			files: map[string]string{
				"main.go": `package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2"
)

func validateNames(names []string) error {
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
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/gofiber/fiber/v3/client"
)

func validateNames(names []string) error {
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
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	code := resp.StatusCode()
	return code, nil
}

func load(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}
`,
			},
		},
		{
			name: "errs_slice_needed",
			files: map[string]string{
				"main.go": `package main

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
`,
			},
			want: map[string]string{
				"main.go": `package main

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
`,
			},
		},
		{
			name: "errs_test_assertions",
			files: map[string]string{
				"main.go": `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet(t *testing.T) {
	code, body, errs := fiber.Get("http://localhost:3000").String()
	assert.Equal(t, 0, len(errs))
	require.Len(t, errs, 0)
	assert.Equal(t, 200, code)
	assert.Equal(t, "ok", body)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"testing"

	"github.com/gofiber/fiber/v3/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet(t *testing.T) {
	resp, err := client.Get("http://localhost:3000")
	var code int
	var body string
	var errs []error
	if err == nil {
		code = resp.StatusCode()
		body = string(resp.Body())
	}
	if err != nil {
		errs = []error{err}
	}
	assert.Equal(t, 0, len(errs))
	require.Len(t, errs, 0)
	assert.Equal(t, 200, code)
	assert.Equal(t, "ok", body)
}
`,
			},
		},
		{
			name: "hang_shape",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2"
)

func fn1() {
	agent := fiber.Get("http://localhost:3000/1")
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		fmt.Println(errs)
	}
	fmt.Println(code, string(body))
}

func fn2() {
	agent := fiber.Get("http://localhost:3000/2")
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		fmt.Println(errs)
	}
	fmt.Println(code, string(body))
}

func fn3() {
	agent := fiber.Get("http://localhost:3000/3")
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		fmt.Println(errs)
	}
	fmt.Println(code, string(body))
}

func fn4() {
	agent := fiber.Get("http://localhost:3000/4")
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		fmt.Println(errs)
	}
	fmt.Println(code, string(body))
}

func fn5() {
	agent := fiber.Get("http://localhost:3000/5")
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		fmt.Println(errs)
	}
	fmt.Println(code, string(body))
}

func main() {
	fn1()
	fn2()
	fn3()
	fn4()
	fn5()
	data, err := os.ReadFile("x")
	fmt.Println(string(data), err)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v3/client"
)

func fn1() {
	resp, err := client.Get("http://localhost:3000/1")
	var code int
	var body []byte
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(code, string(body))
}

func fn2() {
	resp, err := client.Get("http://localhost:3000/2")
	var code int
	var body []byte
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(code, string(body))
}

func fn3() {
	resp, err := client.Get("http://localhost:3000/3")
	var code int
	var body []byte
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(code, string(body))
}

func fn4() {
	resp, err := client.Get("http://localhost:3000/4")
	var code int
	var body []byte
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(code, string(body))
}

func fn5() {
	resp, err := client.Get("http://localhost:3000/5")
	var code int
	var body []byte
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(code, string(body))
}

func main() {
	fn1()
	fn2()
	fn3()
	fn4()
	fn5()
	data, err := os.ReadFile("x")
	fmt.Println(string(data), err)
}
`,
			},
		},
		{
			name: "predeclared",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func block(url string) ([]byte, error) {
	var (
		code int
		body []byte
		errs []error
	)
	agent := fiber.Get(url)
	code, body, errs = agent.Bytes()
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed: %v", errs)
	}
	_ = code
	return body, nil
}

func partlyNew(u1, u2 string) error {
	a1 := fiber.Get(u1)
	c1, body, errs := a1.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	a2 := fiber.Get(u2)
	c2, body, errs := a2.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	fmt.Println(c1, c2, len(body))
	return nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3/client"
)

func block(url string) ([]byte, error) {
	var (
		code int
		body []byte
		errs []error
	)
	resp, err := client.Get(url)
	code = 0
	body = nil
	errs = nil
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed: %v", errs)
	}
	_ = code
	return body, nil
}

func partlyNew(u1, u2 string) error {
	resp, err := client.Get(u1)
	if err != nil {
		return err
	}
	c1, body := resp.StatusCode(), resp.Body()
	resp2, err := client.Get(u2)
	var c2 int
	body = nil
	if err == nil {
		c2 = resp2.StatusCode()
		body = resp2.Body()
	}
	if err != nil {
		return err
	}
	fmt.Println(c1, c2, len(body))
	return nil
}
`,
			},
		},
	})
}
