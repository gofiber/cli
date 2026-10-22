package v3_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// clientShimFile is the file the migration adds for the requests that run on the
// copy of the Fiber v2 agent.
const clientShimFile = "fiber_migrate_agent.go"

func Test_MigrateClientUsage_Copy(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "aliased_import",
			files: map[string]string{
				"main.go": `package main

import f "github.com/gofiber/fiber/v2"

func get(url string) ([]byte, error) {
	_, body, errs := f.Get(url).Host("virtual.example.com").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

func get(url string) ([]byte, error) {
	_, body, errs := fiberMigrateGet(url).Host("virtual.example.com").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
			},
			notes: []string{
				"main.go:6: Host(): the v3 client cannot send a Host header that differs from the URL" + clientShimNote,
			},
		},
		{
			name: "mixed_file",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func plain(url string) {
	_, body, errs := fiber.Get(url).Set("X-Id", "1").Bytes()
	fmt.Println(string(body), errs)
}

func virtual(url string) {
	a := fiber.Get(url)
	a.Host("virtual.example.com")
	a.Set("X-Id", "2")
	_, body, errs := a.Bytes()
	fmt.Println(string(body), errs)
}

func plainAgain(url string) {
	_, body, errs := fiber.Delete(url).Bytes()
	fmt.Println(string(body), errs)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3/client"
)

func plain(url string) {
	resp, err := client.Get(url, client.Config{Header: map[string]string{"X-Id": "1"}})
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	fmt.Println(string(body), err)
}

func virtual(url string) {
	a := fiberMigrateGet(url)
	a.Host("virtual.example.com")
	a.Set("X-Id", "2")
	_, body, errs := a.Bytes()
	fmt.Println(string(body), errs)
}

func plainAgain(url string) {
	resp, err := client.Delete(url)
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	fmt.Println(string(body), err)
}
`,
			},
			notes: []string{
				"main.go:15: Host(): the v3 client cannot send a Host header that differs from the URL" + clientShimNote,
			},
		},
	})
}

func Test_MigrateClientUsage_CopyFlows(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "a_client_stays_with_its_requests",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

var httpClient = &fiber.Client{UserAgent: "svc/1.0"}

func plain(url string) {
	_, body, errs := httpClient.Get(url).Bytes()
	fmt.Println(string(body), errs)
}

func virtual(url string) {
	c := fiber.AcquireClient()
	defer fiber.ReleaseClient(c)
	c.UserAgent = "virtual/1.0"
	_, body, errs := c.Get(url).Host("virtual.example.com").Bytes()
	fmt.Println(string(body), errs)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"
)

var httpClient = &fiberMigrateClient{UserAgent: "svc/1.0"}

func plain(url string) {
	_, body, errs := httpClient.Get(url).Bytes()
	fmt.Println(string(body), errs)
}

func virtual(url string) {
	c := fiberMigrateAcquireClient()
	defer fiberMigrateReleaseClient(c)
	c.UserAgent = "virtual/1.0"
	_, body, errs := c.Get(url).Host("virtual.example.com").Bytes()
	fmt.Println(string(body), errs)
}
`,
			},
			notes: []string{
				"main.go:12: " + clientShareClientNote,
				"main.go:20: Host(): the v3 client cannot send a Host header that differs from the URL" + clientShimNote,
			},
		},
		{
			name: "a_request_through_a_client_that_is_no_statement_keeps_the_client",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

var httpClient = &fiber.Client{UserAgent: "svc/1.0"}

func show(url string) {
	fmt.Println(httpClient.Get(url).String())
}

func plain(url string) {
	_, body, errs := httpClient.Get(url).Bytes()
	fmt.Println(string(body), errs)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"
)

var httpClient = &fiberMigrateClient{UserAgent: "svc/1.0"}

func show(url string) {
	fmt.Println(httpClient.Get(url).String())
}

func plain(url string) {
	_, body, errs := httpClient.Get(url).Bytes()
	fmt.Println(string(body), errs)
}
`,
			},
			notes: []string{
				"main.go:16: " + clientShareClientNote,
			},
		},
		{
			name: "a_helper_of_a_file_without_the_fiber_import",
			files: map[string]string{
				"helper.go": `package main

import "github.com/gofiber/fiber/v2"

func newReq(url string) *fiber.Agent {
	return fiber.Get(url).Set("Accept", "application/json")
}
`,
				"use.go": `package main

func fetch(url string) ([]byte, error) {
	a := newReq(url)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
			},
			want: map[string]string{
				"helper.go": `package main

func newReq(url string) *fiberMigrateAgent {
	return fiberMigrateGet(url).Set("Accept", "application/json")
}
`,
				// The file does not use the package of Fiber, so the migration does not
				// touch it; it works with what the helper returns.
				"use.go": `package main

func fetch(url string) ([]byte, error) {
	a := newReq(url)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
			},
			notes: []string{
				"helper.go:6: " + clientShareNote,
			},
		},
		{
			name: "a_helper_used_in_a_chain_or_as_a_value",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

func newReq(url string) *fiber.Agent {
	return fiber.Get(url).Set("Accept", "application/json")
}

func chained(url string) ([]byte, error) {
	_, body, errs := newReq(url).Set("X-Id", "1").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

var build = newReq
`,
			},
			want: map[string]string{
				"main.go": `package main

func newReq(url string) *fiberMigrateAgent {
	return fiberMigrateGet(url).Set("Accept", "application/json")
}

func chained(url string) ([]byte, error) {
	_, body, errs := newReq(url).Set("X-Id", "1").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

var build = newReq
`,
			},
			notes: []string{
				"main.go:6: " + clientShareNote,
			},
		},
		{
			name: "a_field_leaves_the_requests_that_have_nothing_to_do_with_it",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type holder struct {
	agent *fiber.Agent
}

func (h *holder) set(url string) {
	h.agent = fiber.Get(url)
}

func unrelated(url string) {
	_, body, errs := fiber.Get(url).Bytes()
	fmt.Println(string(body), errs)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3/client"
)

type holder struct {
	agent *fiberMigrateAgent
}

func (h *holder) set(url string) {
	h.agent = fiberMigrateGet(url)
}

func unrelated(url string) {
	resp, err := client.Get(url)
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	fmt.Println(string(body), err)
}
`,
			},
			notes: []string{
				"main.go:10: `fiber.Agent` has no counterpart in the v3 client package; it now uses the copy of the Fiber v2 agent in fiber_migrate_agent.go",
			},
		},
		{
			name: "a_helper_result_that_goes_to_an_agent_parameter_is_followed",
			files: map[string]string{
				"agents.go": `package main

import "github.com/gofiber/fiber/v2"

func newReq(url string) *fiber.Agent {
	return fiber.Get(url).Set("Accept", "application/json")
}

func wrapped(url string) *fiber.Agent {
	return newReq(url)
}

func send(a *fiber.Agent) ([]byte, error) {
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
				"main.go": `package main

func fetch(url string) ([]byte, error) {
	return send(wrapped(url))
}
`,
			},
			want: map[string]string{
				"agents.go": `package main

import "github.com/gofiber/fiber/v3/client"

func newReq(url string) *client.Request {
	return client.C().R().SetMethod("GET").SetURL(url).SetHeader("Accept", "application/json")
}

func wrapped(url string) *client.Request {
	return newReq(url)
}

func send(a *client.Request) ([]byte, error) {
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}
`,
				"main.go": `package main

func fetch(url string) ([]byte, error) {
	return send(wrapped(url))
}
`,
			},
		},
		{
			name: "a_helper_result_that_goes_to_a_method_is_not",
			files: map[string]string{
				"agents.go": `package main

import "github.com/gofiber/fiber/v2"

func newReq(url string) *fiber.Agent {
	return fiber.Get(url)
}
`,
				"main.go": `package main

type sender struct{}

func (sender) send(a any) {}

func fetch(s sender, url string) {
	s.send(newReq(url))
}
`,
			},
			want: map[string]string{
				"agents.go": `package main

func newReq(url string) *fiberMigrateAgent {
	return fiberMigrateGet(url)
}
`,
			},
			notes: []string{
				"agents.go:6: " + clientShareNote,
			},
		},
		{
			name: "a_request_inside_a_migrated_one",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func nested(u1, u2 string) {
	if code, body, errs := fiber.Get(u1).Bytes(); len(errs) == 0 {
		fmt.Println(code, string(body))
		_, body2, errs2 := fiber.Get(u2).Host("virtual.example.com").Bytes()
		fmt.Println(string(body2), errs2)
	}
}

func inArgument(u1, u2 string) {
	fmt.Println(fiber.Get(u1).Set("X-A", func() string {
		_, b, _ := fiber.Get(u2).Host("h").String()
		return b
	}()).String())
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3/client"
)

func nested(u1, u2 string) {
	resp, err := client.Get(u1)
	var code int
	var body []byte
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err == nil {
		fmt.Println(code, string(body))
		_, body2, errs2 := fiberMigrateGet(u2).Host("virtual.example.com").Bytes()
		fmt.Println(string(body2), errs2)
	}
}

func inArgument(u1, u2 string) {
	fmt.Println(fiberMigrateGet(u1).Set("X-A", func() string {
		_, b, _ := fiberMigrateGet(u2).Host("h").String()
		return b
	}()).String())
}
`,
			},
			notes: []string{
				"main.go:12: Host(): the v3 client cannot send a Host header that differs from the URL" + clientShimNote,
				"main.go:19: Host(): the v3 client cannot send a Host header that differs from the URL" + clientShimNote,
			},
		},
		{
			name: "form_files_outside_of_a_request",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

func newFile(name string, data []byte) *fiber.FormFile {
	ff := fiber.AcquireFormFile()
	ff.Fieldname = "file"
	ff.Name = name
	ff.Content = data
	return ff
}

func release(ff *fiber.FormFile) {
	fiber.ReleaseFormFile(ff)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

func newFile(name string, data []byte) *fiberMigrateFormFile {
	ff := fiberMigrateAcquireFormFile()
	ff.Fieldname = "file"
	ff.Name = name
	ff.Content = data
	return ff
}

func release(ff *fiberMigrateFormFile) {
	fiberMigrateReleaseFormFile(ff)
}
`,
			},
			notes: []string{
				"main.go:5: `fiber.FormFile` has no counterpart in the v3 client package; it now uses the copy of the Fiber v2 agent in fiber_migrate_agent.go",
			},
		},
		{
			name: "an_embedded_agent_is_left_alone",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

type api struct {
	*fiber.Agent
}

func newAPI(url string) api {
	return api{Agent: fiber.Get(url)}
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v3"

type api struct {
	*fiber.Agent
}

func newAPI(url string) api {
	return api{Agent: fiberMigrateGet(url)}
}
`,
			},
			notes: []string{
				"main.go:6: `fiber.Agent` does not exist in Fiber v3 and was not migrated; use the client package (client.Get, client.Post, client.New().R(), ...) (an embedded field is named after its type: give it a name of its own and the type fiberMigrate...)",
				"main.go:10: `fiber.Get` has no counterpart in the v3 client package; it now uses the copy of the Fiber v2 agent in fiber_migrate_agent.go",
			},
		},
	})
}

// clientShimRead returns the file the migration added to the directory.
func clientShimRead(t *testing.T, dir string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, clientShimFile)) // #nosec G304 -- a temporary directory of the test
	require.NoError(t, err)
	return string(b)
}

func Test_MigrateClientUsage_CopyFile(t *testing.T) {
	t.Parallel()

	const host = `package %s

import "github.com/gofiber/fiber/v2"

func %s(url string) {
	_, _, _ = fiber.Get(url).Host("virtual.example.com").Bytes()
}
`
	source := func(pkg, fn string) string { return fmt.Sprintf(host, pkg, fn) }

	t.Run("one file per package, named after it", func(t *testing.T) {
		t.Parallel()

		run := clientRunMigration(t, map[string]string{
			"a.go":          source("app", "first"),
			"b.go":          source("app", "second"),
			"sub/c.go":      source("other", "third"),
			"sub/c_test.go": source("other", "fourth"),
		})

		shim := clientShimRead(t, run.dir)
		assert.Contains(t, shim, "\npackage app\n")
		assert.Contains(t, shim, `This file was added by "fiber migrate"`)
		assert.Contains(t, shim, "func fiberMigrateGet(url string) *fiberMigrateAgent")
		assert.Contains(t, clientShimRead(t, filepath.Join(run.dir, "sub")), "\npackage other\n")
		assert.Len(t, run.notes, 4)
		for _, name := range []string{"a.go", "b.go", "sub/c.go", "sub/c_test.go"} {
			assert.NotContains(t, run.files[name], "fiber.", name)
			assert.Contains(t, run.files[name], "fiberMigrateGet(url)", name)
		}
	})

	t.Run("a second run leaves it alone", func(t *testing.T) {
		t.Parallel()

		run := clientRunMigration(t, map[string]string{"main.go": source("main", "get")})
		before := clientShimRead(t, run.dir)

		require.NoError(t, v3.MigrateClientUsage(newCmd(&bytes.Buffer{}), run.dir, nil, nil))
		assert.Equal(t, before, clientShimRead(t, run.dir))
	})

	t.Run("a test file of the package", func(t *testing.T) {
		t.Parallel()

		run := clientRunMigration(t, map[string]string{"main_test.go": source("main", "TestGet")})
		assert.Contains(t, clientShimRead(t, run.dir), "\npackage main\n")
		assert.Contains(t, run.files["main_test.go"], "fiberMigrateGet(url)")
	})

	t.Run("none when every request is migrated", func(t *testing.T) {
		t.Parallel()

		run := clientMigrateSource(t, `package main

import "github.com/gofiber/fiber/v2"

func get(url string) {
	_, _, _ = fiber.Get(url).Bytes()
}
`)
		assert.NoFileExists(t, filepath.Join(run.dir, clientShimFile))
		assert.Empty(t, run.notes)
	})

	t.Run("an external test package cannot have it", func(t *testing.T) {
		t.Parallel()

		run := clientRunMigration(t, map[string]string{
			"main.go":             source("main", "get"),
			"main_extern_test.go": source("main_test", "getExtern"),
		})
		assert.FileExists(t, filepath.Join(run.dir, clientShimFile), "the package itself has it")
		assert.Contains(t, run.files["main.go"], "fiberMigrateGet(url)")
		assert.Contains(t, run.files["main_extern_test.go"], "fiber.Get(url)", "the external test package keeps the Fiber v2 code")
		require.Len(t, run.notes, 2)
		assert.Contains(t, strings.Join(run.notes, "\n"), "left as Fiber v2 code: Host()")
		assert.Contains(t, strings.Join(run.notes, "\n"), "an external test package")
	})

	t.Run("a file of the project of that name stays", func(t *testing.T) {
		t.Parallel()

		mine := "package main\n\n// my own file\n"
		run := clientRunMigration(t, map[string]string{
			"main.go":      source("main", "get"),
			clientShimFile: mine,
		})
		assert.Equal(t, mine, run.files[clientShimFile])
		assert.Contains(t, run.files["main.go"], "fiber.Get(url)")
		require.Len(t, run.notes, 1)
		assert.Contains(t, run.notes[0], "left as Fiber v2 code")
		assert.Contains(t, run.notes[0], "that this migration did not write")
	})

	t.Run("a name of the project stays", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"fiberMigrateAgent", "fmjson", "fiberMigrateGet"} {
			run := clientRunMigration(t, map[string]string{
				"main.go":  source("main", "get"),
				"other.go": "package main\n\nvar " + name + " = 1\n",
			})
			assert.NoFileExists(t, filepath.Join(run.dir, clientShimFile), name)
			assert.Contains(t, run.files["main.go"], "fiber.Get(url)", name)
			require.Len(t, run.notes, 1, name)
			assert.Contains(t, run.notes[0], "declares a name that the copy needs", name)
		}
	})

	t.Run("a name that only a file imports does not matter", func(t *testing.T) {
		t.Parallel()

		// The copy renames its imports, so a package level json of the project does
		// not clash with the one it would import.
		run := clientRunMigration(t, map[string]string{
			"main.go":  source("main", "get"),
			"other.go": "package main\n\nvar json, bytes, tls, time = 1, 2, 3, 4\n",
		})
		assert.Contains(t, clientShimRead(t, run.dir), "\npackage main\n")
		assert.Contains(t, run.files["main.go"], "fiberMigrateGet(url)")
	})
}
