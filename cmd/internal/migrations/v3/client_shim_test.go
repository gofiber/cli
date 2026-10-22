package v3_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// clientShimFile is the file the migration adds for the requests that run on the
// copy of the Fiber v2 agent, in the directory of the module.
const clientShimFile = "internal/fibermigrate/agent.go"

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

import "example.com/app/internal/fibermigrate"

func get(url string) ([]byte, error) {
	_, body, errs := fibermigrate.Get(url).Host("virtual.example.com").Bytes()
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

	"example.com/app/internal/fibermigrate"
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
	a := fibermigrate.Get(url)
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
				"main.go:11: " + clientErrsNote,
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

	"example.com/app/internal/fibermigrate"
)

var httpClient = &fibermigrate.Client{UserAgent: "svc/1.0"}

func plain(url string) {
	_, body, errs := httpClient.Get(url).Bytes()
	fmt.Println(string(body), errs)
}

func virtual(url string) {
	c := fibermigrate.AcquireClient()
	defer fibermigrate.ReleaseClient(c)
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

	"example.com/app/internal/fibermigrate"
)

var httpClient = &fibermigrate.Client{UserAgent: "svc/1.0"}

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

import "example.com/app/internal/fibermigrate"

func newReq(url string) *fibermigrate.Agent {
	return fibermigrate.Get(url).Set("Accept", "application/json")
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

import "example.com/app/internal/fibermigrate"

func newReq(url string) *fibermigrate.Agent {
	return fibermigrate.Get(url).Set("Accept", "application/json")
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

	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v3/client"
)

type holder struct {
	agent *fibermigrate.Agent
}

func (h *holder) set(url string) {
	h.agent = fibermigrate.Get(url)
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
				"main.go:10: `fiber.Agent` has no counterpart in the v3 client package; it now uses the copy of the Fiber v2 agent in internal/fibermigrate/agent.go",
				"main.go:19: " + clientErrsNote,
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

import "example.com/app/internal/fibermigrate"

func newReq(url string) *fibermigrate.Agent {
	return fibermigrate.Get(url)
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

	"example.com/app/internal/fibermigrate"
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
		_, body2, errs2 := fibermigrate.Get(u2).Host("virtual.example.com").Bytes()
		fmt.Println(string(body2), errs2)
	}
}

func inArgument(u1, u2 string) {
	fmt.Println(fibermigrate.Get(u1).Set("X-A", func() string {
		_, b, _ := fibermigrate.Get(u2).Host("h").String()
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
			name: "a_name_set_after_parse_has_no_effect_and_stays",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func before(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	a.Name = "svc/1.0"
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

func after(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return nil, err
	}
	a.Name = "svc/1.0"
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func before(url string) ([]byte, error) {
	a := client.C().R()
	a.SetUserAgent("svc/1.0")
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func after(url string) ([]byte, error) {
	a := fibermigrate.AcquireAgent()
	defer fibermigrate.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return nil, err
	}
	a.Name = "svc/1.0"
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
			},
			notes: []string{
				"main.go:25: setting Name after Parse(), which has no effect in v2" + clientShimNote,
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

import "example.com/app/internal/fibermigrate"

func newFile(name string, data []byte) *fibermigrate.FormFile {
	ff := fibermigrate.AcquireFormFile()
	ff.Fieldname = "file"
	ff.Name = name
	ff.Content = data
	return ff
}

func release(ff *fibermigrate.FormFile) {
	fibermigrate.ReleaseFormFile(ff)
}
`,
			},
			notes: []string{
				"main.go:5: `fiber.FormFile` has no counterpart in the v3 client package; it now uses the copy of the Fiber v2 agent in internal/fibermigrate/agent.go",
			},
		},
		{
			// The copy declares the type under the name the fiber package gave it, so an
			// embedded field keeps the name it has.
			name: "an_embedded_agent",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

type api struct {
	*fiber.Agent
}

func newAPI(url string) api {
	return api{Agent: fiber.Get(url)}
}

func (a api) fetch() ([]byte, []error) {
	_, body, errs := a.Set("X-Id", "1").Bytes()
	return body, errs
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import "example.com/app/internal/fibermigrate"

type api struct {
	*fibermigrate.Agent
}

func newAPI(url string) api {
	return api{Agent: fibermigrate.Get(url)}
}

func (a api) fetch() ([]byte, []error) {
	_, body, errs := a.Set("X-Id", "1").Bytes()
	return body, errs
}
`,
			},
			notes: []string{
				"main.go:6: `fiber.Agent` has no counterpart in the v3 client package; it now uses the copy of the Fiber v2 agent in internal/fibermigrate/agent.go",
			},
		},
	})
}

// clientExportNote is the start of the note at an exported declaration that uses the
// agent, the client or the form file of Fiber v2.
const clientExportNote = " is exported and uses the Fiber v2 "

func Test_MigrateClientUsage_ExportedAgentsAndClients(t *testing.T) {
	t.Parallel()

	const stays = " stay on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go, so that the packages that import this one keep compiling"

	clientRunCases(t, []clientCase{
		{
			name: "an_exported_helper_and_client_of_a_package_others_import",
			files: map[string]string{
				"httpx.go": `package httpx

import "github.com/gofiber/fiber/v2"

// New builds the request.
func New(url string) *fiber.Agent {
	return fiber.Get(url).Set("X-Project", "sample")
}

// Client is the client of the project.
var Client = &fiber.Client{UserAgent: "sample/1.0"}

// API holds a client.
type API struct {
	Client *fiber.Client
	hidden *fiber.Client
}

// NewClient makes a client.
func NewClient() *fiber.Client { return &fiber.Client{} }

// Shared is a method of an exported type.
func (a *API) Shared() *fiber.Client { return a.Client }

func (a *API) private() *fiber.Client { return a.hidden }

func (a api) Hidden() *fiber.Client { return nil }

type api struct{}

var unexported = &fiber.Client{}

func lower(url string) *fiber.Agent { return fiber.Get(url) }
`,
			},
			want: map[string]string{
				"httpx.go": `package httpx

import "example.com/app/internal/fibermigrate"

// New builds the request.
func New(url string) *fibermigrate.Agent {
	return fibermigrate.Get(url).Set("X-Project", "sample")
}

// Client is the client of the project.
var Client = &fibermigrate.Client{UserAgent: "sample/1.0"}

// API holds a client.
type API struct {
	Client *fibermigrate.Client
	hidden *fibermigrate.Client
}

// NewClient makes a client.
func NewClient() *fibermigrate.Client { return &fibermigrate.Client{} }

// Shared is a method of an exported type.
func (a *API) Shared() *fibermigrate.Client { return a.Client }

func (a *API) private() *fibermigrate.Client { return a.hidden }

func (a api) Hidden() *fibermigrate.Client { return nil }

type api struct{}

var unexported = &fibermigrate.Client{}

func lower(url string) *fibermigrate.Agent { return fibermigrate.Get(url) }
`,
			},
			notes: []string{
				"httpx.go:6: New" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:11: Client" + clientExportNote + "client: the clients of this package" + stays,
				"httpx.go:15: API.Client" + clientExportNote + "client: the clients of this package" + stays,
				"httpx.go:20: NewClient" + clientExportNote + "client: the clients of this package" + stays,
				"httpx.go:23: API.Shared" + clientExportNote + "client: the clients of this package" + stays,
				"httpx.go:27: api.Hidden" + clientExportNote + "client: the clients of this package" + stays,
				"httpx.go:33: " + clientShareNote,
			},
		},
		{
			name: "the_fields_the_types_and_the_values_of_a_package_are_exports_too",
			files: map[string]string{
				"httpx.go": `package httpx

import "github.com/gofiber/fiber/v2"

// Request is another name of the agent.
type Request = *fiber.Agent

// Sender takes an agent.
type Sender interface {
	Send(a *fiber.Agent) error
	hidden(a *fiber.Agent)
}

// Hook takes a client.
type Hook func(c *fiber.Client)

// Embedded has an embedded agent.
type Embedded struct {
	*fiber.Agent
}

// Default is an agent that is made when the package starts.
var Default = fiber.Get("http://localhost")

// Build is a function value.
var Build = newRequest

// Make is a function literal.
var Make = func(url string) *fiber.Agent { return fiber.Get(url) }

// File is a form file.
var File *fiber.FormFile

func newRequest(url string) *fiber.Agent { return fiber.Post(url) }
`,
			},
			want: map[string]string{
				"httpx.go": `package httpx

import "example.com/app/internal/fibermigrate"

// Request is another name of the agent.
type Request = *fibermigrate.Agent

// Sender takes an agent.
type Sender interface {
	Send(a *fibermigrate.Agent) error
	hidden(a *fibermigrate.Agent)
}

// Hook takes a client.
type Hook func(c *fibermigrate.Client)

// Embedded has an embedded agent.
type Embedded struct {
	*fibermigrate.Agent
}

// Default is an agent that is made when the package starts.
var Default = fibermigrate.Get("http://localhost")

// Build is a function value.
var Build = newRequest

// Make is a function literal.
var Make = func(url string) *fibermigrate.Agent { return fibermigrate.Get(url) }

// File is a form file.
var File *fibermigrate.FormFile

func newRequest(url string) *fibermigrate.Agent { return fibermigrate.Post(url) }
`,
			},
			notes: []string{
				"httpx.go:6: Request" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:10: Sender.Send" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:15: Hook" + clientExportNote + "client: the clients of this package" + stays,
				"httpx.go:19: Embedded.Agent" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:23: Default" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:26: Build" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:29: Make" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:32: File" + clientExportNote + "form file: the form files of this package" + stays,
				"httpx.go:34: " + clientShareNote,
			},
		},
		{
			name: "a_main_package_is_not_imported",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

func New(url string) *fiber.Agent {
	return fiber.Get(url).Set("X-Project", "sample")
}

var Client = &fiber.Client{UserAgent: "sample/1.0"}
`,
			},
			want: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v3/client"

func New(url string) *client.Request {
	return client.C().R().SetMethod("GET").SetURL(url).SetHeader("X-Project", "sample")
}

var Client = client.New().SetUserAgent("sample/1.0")
`,
			},
		},
		{
			name: "an_external_test_package_is_not_imported",
			files: map[string]string{
				"httpx_test.go": `package httpx_test

import "github.com/gofiber/fiber/v2"

func New(url string) *fiber.Agent {
	return fiber.Get(url)
}

var Client = &fiber.Client{}
`,
			},
			want: map[string]string{
				"httpx_test.go": `package httpx_test

import "github.com/gofiber/fiber/v3/client"

func New(url string) *client.Request {
	return client.C().R().SetMethod("GET").SetURL(url)
}

var Client = client.New()
`,
			},
		},
		{
			name: "a_package_on_the_copy_keeps_the_methods_its_users_call",
			files: map[string]string{
				"httpx.go": `package httpx

import "github.com/gofiber/fiber/v2"

// New builds the request.
func New(url string) *fiber.Agent {
	return fiber.Get(url).Set("X-Project", "sample")
}

// Virtual builds a request with a Host that the client package cannot send.
func Virtual(url string) *fiber.Agent {
	a := fiber.Get(url)
	a.Host("virtual.example.com")
	return a
}

// Client is the client of the project.
var Client = &fiber.Client{UserAgent: "sample/1.0"}

func use(url string) {
	_, _, _ = Client.Get(url).Host("virtual.example.com").Bytes()
}
`,
			},
			want: map[string]string{
				"httpx.go": `package httpx

import "example.com/app/internal/fibermigrate"

// New builds the request.
func New(url string) *fibermigrate.Agent {
	return fibermigrate.Get(url).Set("X-Project", "sample")
}

// Virtual builds a request with a Host that the client package cannot send.
func Virtual(url string) *fibermigrate.Agent {
	a := fibermigrate.Get(url)
	a.Host("virtual.example.com")
	return a
}

// Client is the client of the project.
var Client = &fibermigrate.Client{UserAgent: "sample/1.0"}

func use(url string) {
	_, _, _ = Client.Get(url).Host("virtual.example.com").Bytes()
}
`,
			},
			notes: []string{
				"httpx.go:6: New" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:11: Virtual" + clientExportNote + "agent: the agents of this package" + stays,
				"httpx.go:12: Host(): the v3 client cannot send a Host header that differs from the URL" + clientShimNote,
				"httpx.go:18: Client" + clientExportNote + "client: the clients of this package" + stays,
				"httpx.go:21: Host(): the v3 client cannot send a Host header that differs from the URL" + clientShimNote,
			},
		},
	})
}

func Test_MigrateClientUsage_PackagesThatImportAnExporter(t *testing.T) {
	t.Parallel()

	const exporter = `package httpx

import "github.com/gofiber/fiber/v2"

// New builds the request.
func New(url string) *fiber.Agent {
	return fiber.Get(url).Set("X-Project", "sample")
}
`
	const exported = `package httpx

import "example.com/app/internal/fibermigrate"

// New builds the request.
func New(url string) *fibermigrate.Agent {
	return fibermigrate.Get(url).Set("X-Project", "sample")
}
`
	const exportNote = "httpx/httpx.go:6: New is exported and uses the Fiber v2 agent: the agents of this package stay on the copy of the Fiber v2 agent in " +
		"internal/fibermigrate/agent.go, so that the packages that import this one keep compiling"
	const meet = ", which stay on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go, so this request does too"

	clientRunCases(t, []clientCase{
		{
			name: "callers_that_do_not_import_fiber_are_not_touched_and_keep_compiling",
			files: map[string]string{
				"httpx/httpx.go": exporter,
				"app/app.go": `package app

import "example.com/app/httpx"

func Fetch(url string) ([]byte, []error) {
	_, body, errs := httpx.New(url).Set("X-Id", "1").Bytes()
	return body, errs
}
`,
				"main.go": `package main

import (
	"fmt"

	"example.com/app/app"
)

func main() {
	body, errs := app.Fetch("http://localhost")
	fmt.Println(string(body), errs)
}
`,
			},
			want: map[string]string{
				"httpx/httpx.go": exported,
				"app/app.go": `package app

import "example.com/app/httpx"

func Fetch(url string) ([]byte, []error) {
	_, body, errs := httpx.New(url).Set("X-Id", "1").Bytes()
	return body, errs
}
`,
				"main.go": `package main

import (
	"fmt"

	"example.com/app/app"
)

func main() {
	body, errs := app.Fetch("http://localhost")
	fmt.Println(string(body), errs)
}
`,
			},
			notes: []string{exportNote},
		},
		{
			name: "a_caller_that_names_the_type_gets_the_copys_type",
			files: map[string]string{
				"httpx/httpx.go": exporter,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"github.com/gofiber/fiber/v2"
)

func send(a *fiber.Agent) ([]byte, []error) {
	_, body, errs := a.Bytes()
	return body, errs
}

func viaHelper(url string) ([]byte, []error) {
	return send(httpx.New(url))
}

func own(url string) ([]byte, []error) {
	a := fiber.Get(url)
	a.Set("X-Own", "1")
	return send(a)
}

func alone(url string) []byte {
	_, body, _ := fiber.Get(url).Set("X-Alone", "1").Bytes()
	return body
}
`,
			},
			want: map[string]string{
				"httpx/httpx.go": exported,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v3/client"
)

func send(a *fibermigrate.Agent) ([]byte, []error) {
	_, body, errs := a.Bytes()
	return body, errs
}

func viaHelper(url string) ([]byte, []error) {
	return send(httpx.New(url))
}

func own(url string) ([]byte, []error) {
	a := fibermigrate.Get(url)
	a.Set("X-Own", "1")
	return send(a)
}

func alone(url string) []byte {
	resp, err := client.Get(url, client.Config{Header: map[string]string{"X-Alone": "1"}})
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	return body
}
`,
			},
			notes: []string{
				exportNote,
				"app/app.go:8: its agent may meet the agents of example.com/app/httpx" + meet,
				"app/app.go:18: `return send(a)` is not understood by the migration; this request still runs on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go",
			},
		},
		{
			name: "the_external_test_package_of_the_exporter",
			files: map[string]string{
				"httpx/httpx.go": exporter,
				"httpx/httpx_test.go": `package httpx_test

import (
	"testing"

	"example.com/app/httpx"
	"github.com/gofiber/fiber/v2"
)

func use(a *fiber.Agent) []error {
	_, _, errs := a.Bytes()
	return errs
}

func Test_New(t *testing.T) {
	if errs := use(httpx.New("http://localhost")); len(errs) == 0 {
		t.Fatal("nothing is listening")
	}
}
`,
			},
			want: map[string]string{
				"httpx/httpx.go": exported,
				"httpx/httpx_test.go": `package httpx_test

import (
	"testing"

	"example.com/app/httpx"
	"example.com/app/internal/fibermigrate"
)

func use(a *fibermigrate.Agent) []error {
	_, _, errs := a.Bytes()
	return errs
}

func Test_New(t *testing.T) {
	if errs := use(httpx.New("http://localhost")); len(errs) == 0 {
		t.Fatal("nothing is listening")
	}
}
`,
			},
			notes: []string{
				exportNote,
				"httpx/httpx_test.go:10: its agent may meet the agents of example.com/app/httpx" + meet,
			},
		},
		{
			name: "a_package_main_that_imports_the_exporter",
			files: map[string]string{
				"httpx/httpx.go": exporter,
				"main.go": `package main

import (
	"fmt"

	"example.com/app/httpx"
	"github.com/gofiber/fiber/v2"
)

func show(a *fiber.Agent) {
	_, body, errs := a.String()
	fmt.Println(body, errs)
}

func main() {
	show(httpx.New("http://localhost"))
}
`,
			},
			want: map[string]string{
				"httpx/httpx.go": exported,
				"main.go": `package main

import (
	"fmt"

	"example.com/app/httpx"
	"example.com/app/internal/fibermigrate"
)

func show(a *fibermigrate.Agent) {
	_, body, errs := a.String()
	fmt.Println(body, errs)
}

func main() {
	show(httpx.New("http://localhost"))
}
`,
			},
			notes: []string{
				exportNote,
				"main.go:10: its agent may meet the agents of example.com/app/httpx" + meet,
			},
		},
		{
			// The same code as the next case, which only differs in the package it
			// imports: one that has no agents.
			name: "a_helper_in_a_package_that_imports_one_without_agents_migrates_as_before",
			files: map[string]string{
				"util/util.go": `package util

// Name is not about requests.
func Name() string { return "util" }
`,
				"app/app.go": `package app

import (
	"example.com/app/util"
	"github.com/gofiber/fiber/v2"
)

func newReq(url string) *fiber.Agent {
	return fiber.Get(url).Set("X-Id", util.Name())
}

func fetch(url string) []byte {
	a := newReq(url)
	_, body, _ := a.Bytes()
	return body
}
`,
			},
			want: map[string]string{
				"app/app.go": `package app

import (
	"example.com/app/util"
	"github.com/gofiber/fiber/v3/client"
)

func newReq(url string) *client.Request {
	return client.C().R().SetMethod("GET").SetURL(url).SetHeader("X-Id", util.Name())
}

func fetch(url string) []byte {
	a := newReq(url)
	resp, err := a.Send()
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	return body
}
`,
			},
		},
		{
			name: "a_helper_in_a_package_that_imports_the_exporter_stays_on_the_copy",
			files: map[string]string{
				"httpx/httpx.go": exporter,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"github.com/gofiber/fiber/v2"
)

func newReq(url string) *fiber.Agent {
	return httpx.New(url).Set("X-Id", "1")
}

func fetch(url string) []byte {
	a := newReq(url)
	_, body, _ := a.Bytes()
	return body
}
`,
			},
			want: map[string]string{
				"httpx/httpx.go": exported,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"example.com/app/internal/fibermigrate"
)

func newReq(url string) *fibermigrate.Agent {
	return httpx.New(url).Set("X-Id", "1")
}

func fetch(url string) []byte {
	a := newReq(url)
	_, body, _ := a.Bytes()
	return body
}
`,
			},
			notes: []string{
				exportNote,
				"app/app.go:13: its agent may meet the agents of example.com/app/httpx" + meet,
			},
		},
		{
			name: "the_test_files_of_a_package_reach_only_its_external_test_package",
			files: map[string]string{
				"httpx/httpx.go": `package httpx

// Name is not about requests.
func Name() string { return "httpx" }
`,
				"httpx/export_test.go": `package httpx

import "github.com/gofiber/fiber/v2"

// NewForTest is for the tests of the package.
func NewForTest(url string) *fiber.Agent {
	return fiber.Get(url)
}
`,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"github.com/gofiber/fiber/v2"
)

func newReq(url string) *fiber.Agent {
	return fiber.Get(url).Set("X-Id", httpx.Name())
}

func fetch(url string) []byte {
	a := newReq(url)
	_, body, _ := a.Bytes()
	return body
}
`,
			},
			want: map[string]string{
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"github.com/gofiber/fiber/v3/client"
)

func newReq(url string) *client.Request {
	return client.C().R().SetMethod("GET").SetURL(url).SetHeader("X-Id", httpx.Name())
}

func fetch(url string) []byte {
	a := newReq(url)
	resp, err := a.Send()
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	return body
}
`,
			},
			notes: []string{
				"httpx/export_test.go:6: NewForTest is exported and uses the Fiber v2 agent",
			},
		},
		{
			// The agent the exported function returns is made by a client, so the clients
			// of the package stay on the copy as well, and the requests that go through
			// them.
			name: "an_exported_agent_made_by_a_client_keeps_the_clients_too",
			files: map[string]string{
				"httpx/httpx.go": `package httpx

import "github.com/gofiber/fiber/v2"

// New builds the request.
func New(url string) *fiber.Agent {
	c := &fiber.Client{UserAgent: "sample/1.0"}
	return c.Get(url)
}

func ping(url string) []error {
	c := &fiber.Client{}
	_, _, errs := c.Get(url).Bytes()
	return errs
}
`,
			},
			want: map[string]string{
				"httpx/httpx.go": `package httpx

import "example.com/app/internal/fibermigrate"

// New builds the request.
func New(url string) *fibermigrate.Agent {
	c := &fibermigrate.Client{UserAgent: "sample/1.0"}
	return c.Get(url)
}

func ping(url string) []error {
	c := &fibermigrate.Client{}
	_, _, errs := c.Get(url).Bytes()
	return errs
}
`,
			},
			notes: []string{
				"httpx/httpx.go:6: New is exported and uses the Fiber v2 agent",
				"httpx/httpx.go:13: its client is shared with requests that stay on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go, so this request does too",
			},
		},
		{
			name: "an_exporter_at_the_root_of_the_module",
			files: map[string]string{
				"root.go": `package app

import "github.com/gofiber/fiber/v2"

// New builds the request.
func New(url string) *fiber.Agent {
	return fiber.Get(url)
}
`,
				"cmd/tool/main.go": `package main

import (
	"example.com/app"
	"github.com/gofiber/fiber/v2"
)

func show(a *fiber.Agent) {
	_, _, _ = a.String()
}

func main() {
	show(app.New("http://localhost"))
}
`,
			},
			want: map[string]string{
				"root.go": `package app

import "example.com/app/internal/fibermigrate"

// New builds the request.
func New(url string) *fibermigrate.Agent {
	return fibermigrate.Get(url)
}
`,
				"cmd/tool/main.go": `package main

import (
	"example.com/app"
	"example.com/app/internal/fibermigrate"
)

func show(a *fibermigrate.Agent) {
	_, _, _ = a.String()
}

func main() {
	show(app.New("http://localhost"))
}
`,
			},
			notes: []string{
				"root.go:6: New is exported and uses the Fiber v2 agent",
				"cmd/tool/main.go:8: its agent may meet the agents of example.com/app" + meet,
			},
		},
		{
			// A function that does not say it returns an agent can still hand one to
			// code in another package, which turns it back into the agent of Fiber v2.
			name: "an_agent_that_travels_as_any_or_an_interface",
			files: map[string]string{
				"build/build.go": `package build

import "github.com/gofiber/fiber/v2"

// Request returns the agent as any.
func Request(url string) any {
	return fiber.Get(url).Set("X-Id", "1")
}

// Variable returns an agent that was configured.
func Variable(url string) any {
	a := fiber.Get(url)
	a.Set("X-Id", "2")
	return a
}

// Sender is the part of the agent that the caller needs.
type Sender interface {
	String() (int, string, []error)
}

// Boxed returns the agent as an interface.
func Boxed(url string) Sender {
	return fiber.Get(url)
}
`,
				"use/use.go": `package use

import (
	"example.com/app/build"
	"github.com/gofiber/fiber/v2"
)

func Send(url string) int {
	a := build.Request(url).(*fiber.Agent)
	code, _, _ := a.Bytes()
	return code
}
`,
			},
			want: map[string]string{
				"build/build.go": `package build

import "example.com/app/internal/fibermigrate"

// Request returns the agent as any.
func Request(url string) any {
	return fibermigrate.Get(url).Set("X-Id", "1")
}

// Variable returns an agent that was configured.
func Variable(url string) any {
	a := fibermigrate.Get(url)
	a.Set("X-Id", "2")
	return a
}

// Sender is the part of the agent that the caller needs.
type Sender interface {
	String() (int, string, []error)
}

// Boxed returns the agent as an interface.
func Boxed(url string) Sender {
	return fibermigrate.Get(url)
}
`,
				"use/use.go": `package use

import (
	"example.com/app/build"
	"example.com/app/internal/fibermigrate"
)

func Send(url string) int {
	a := build.Request(url).(*fibermigrate.Agent)
	code, _, _ := a.Bytes()
	return code
}
`,
			},
			notes: []string{
				"build/build.go:7: its agent is returned as a value of another type" + clientShimNote,
				"build/build.go:12: its agent is returned as a value of another type" + clientShimNote,
				"build/build.go:24: its agent is returned as a value of another type" + clientShimNote,
				"use/use.go:9: `fiber.Agent` has no counterpart in the v3 client package; it now uses the copy of the Fiber v2 agent in internal/fibermigrate/agent.go",
			},
		},
		{
			name: "an_exporter_with_an_aliased_import",
			files: map[string]string{
				"httpx/httpx.go": `package httpx

import f "github.com/gofiber/fiber/v2"

type API struct {
	Agent *f.Agent
}
`,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	f "github.com/gofiber/fiber/v2"
)

func use(a *f.Agent) *httpx.API {
	return &httpx.API{Agent: a}
}
`,
			},
			want: map[string]string{
				"httpx/httpx.go": `package httpx

import "example.com/app/internal/fibermigrate"

type API struct {
	Agent *fibermigrate.Agent
}
`,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"example.com/app/internal/fibermigrate"
)

func use(a *fibermigrate.Agent) *httpx.API {
	return &httpx.API{Agent: a}
}
`,
			},
			notes: []string{
				"httpx/httpx.go:6: API.Agent is exported and uses the Fiber v2 agent",
				"app/app.go:8: `return &httpx.API{Agent: a}` is not understood by the migration; this request still runs on the copy of the Fiber v2 agent in internal/fibermigrate/agent.go",
			},
		},
		{
			name: "a_form_file_from_another_package",
			files: map[string]string{
				"httpx/httpx.go": `package httpx

import "github.com/gofiber/fiber/v2"

// NewFile makes a file to upload.
func NewFile(name string) *fiber.FormFile {
	ff := fiber.AcquireFormFile()
	ff.Fieldname = "file"
	ff.Name = name
	ff.Content = []byte("content")
	return ff
}
`,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"github.com/gofiber/fiber/v2"
)

func upload(url string) []error {
	ff := httpx.NewFile("a.txt")
	_, _, errs := fiber.Post(url).FileData(ff).MultipartForm(nil).Bytes()
	return errs
}
`,
			},
			want: map[string]string{
				"httpx/httpx.go": `package httpx

import "example.com/app/internal/fibermigrate"

// NewFile makes a file to upload.
func NewFile(name string) *fibermigrate.FormFile {
	ff := fibermigrate.AcquireFormFile()
	ff.Fieldname = "file"
	ff.Name = name
	ff.Content = []byte("content")
	return ff
}
`,
				"app/app.go": `package app

import (
	"example.com/app/httpx"
	"example.com/app/internal/fibermigrate"
)

func upload(url string) []error {
	ff := httpx.NewFile("a.txt")
	_, _, errs := fibermigrate.Post(url).FileData(ff).MultipartForm(nil).Bytes()
	return errs
}
`,
			},
			notes: []string{
				"httpx/httpx.go:6: NewFile is exported and uses the Fiber v2 form file",
				"app/app.go:10: FileData() with a file that is not a fiber.FormFile literal or a variable of the same block" + clientShimNote,
			},
		},
	})
}

// clientShimRead returns the file the migration added to the directory.
func clientShimRead(t *testing.T, dir string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(clientShimFile))) // #nosec G304 -- a temporary directory of the test
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

	t.Run("one file for the module, shared by its packages", func(t *testing.T) {
		t.Parallel()

		run := clientRunMigration(t, map[string]string{
			"a.go":                 source("app", "first"),
			"b.go":                 source("app", "second"),
			"sub/c.go":             source("other", "third"),
			"sub/c_test.go":        source("other", "fourth"),
			"sub/c_extern_test.go": source("other_test", "fifth"),
		})

		shim := clientShimRead(t, run.dir)
		assert.Contains(t, shim, "\npackage fibermigrate\n")
		assert.Contains(t, shim, `This file was added by "fiber migrate"`)
		assert.Contains(t, shim, "func Get(url string) *Agent")
		assert.Regexp(t, `^// Code generated by "fiber migrate"; DO NOT EDIT\.\n`, shim)
		entries, err := os.ReadDir(filepath.Join(run.dir, "internal", "fibermigrate"))
		require.NoError(t, err)
		assert.Len(t, entries, 1, "the module has the one file, whatever its packages are")

		assert.Len(t, run.notes, 5)
		for _, name := range []string{"a.go", "b.go", "sub/c.go", "sub/c_test.go", "sub/c_extern_test.go"} {
			assert.NotContains(t, run.files[name], "fiber.", name)
			assert.Contains(t, run.files[name], "fibermigrate.Get(url)", name)
			assert.Contains(t, run.files[name], `"`+clientShimImport+`"`, name)
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
		assert.Contains(t, clientShimRead(t, run.dir), "\npackage fibermigrate\n")
		assert.Contains(t, run.files["main_test.go"], "fibermigrate.Get(url)")
	})

	t.Run("none when every request is migrated", func(t *testing.T) {
		t.Parallel()

		run := clientMigrateSource(t, `package main

import "github.com/gofiber/fiber/v2"

func get(url string) {
	_, _, _ = fiber.Get(url).Bytes()
}
`)
		assert.NoFileExists(t, filepath.Join(run.dir, filepath.FromSlash(clientShimFile)))
		assert.Empty(t, run.notes)
	})

	t.Run("an external test package uses it as well", func(t *testing.T) {
		t.Parallel()

		run := clientRunMigration(t, map[string]string{
			"main.go":             source("main", "get"),
			"main_extern_test.go": source("main_test", "getExtern"),
		})
		assert.FileExists(t, filepath.Join(run.dir, filepath.FromSlash(clientShimFile)))
		assert.Contains(t, run.files["main.go"], "fibermigrate.Get(url)")
		assert.Contains(t, run.files["main_extern_test.go"], "fibermigrate.Get(url)")
		assert.NotContains(t, run.files["main_extern_test.go"], "fiber.Get(url)")
		assert.Len(t, run.notes, 2)
	})

	t.Run("the names of the project do not matter", func(t *testing.T) {
		t.Parallel()

		// The copy is a package of its own: the declarations of the packages that use
		// it (a Get, an Agent, a json) cannot clash with it.
		run := clientRunMigration(t, map[string]string{
			"main.go":  source("main", "get"),
			"other.go": "package main\n\nvar Get, Agent, json, bytes, tls, time, fibermigrate = 1, 2, 3, 4, 5, 6, 7\n",
		})
		assert.Contains(t, clientShimRead(t, run.dir), "\npackage fibermigrate\n")
		assert.Contains(t, run.files["main.go"], `fibermigratepkg "`+clientShimImport+`"`, "a package level fibermigrate is not the package")
		assert.Contains(t, run.files["main.go"], "fibermigratepkg.Get(url)")
	})

	t.Run("a nested module has a copy of its own", func(t *testing.T) {
		t.Parallel()

		run := clientRunMigration(t, map[string]string{
			"main.go":       source("main", "get"),
			"tools/go.mod":  "module example.com/tools\n\ngo 1.25\n",
			"tools/main.go": source("main", "get"),
		})
		assert.Contains(t, run.files["main.go"], `"example.com/app/internal/fibermigrate"`)
		assert.Contains(t, run.files["tools/main.go"], `"example.com/tools/internal/fibermigrate"`)
		assert.FileExists(t, filepath.Join(run.dir, "internal", "fibermigrate", "agent.go"))
		assert.FileExists(t, filepath.Join(run.dir, "tools", "internal", "fibermigrate", "agent.go"))
	})

	t.Run("a file of that name that the migration did not write stays", func(t *testing.T) {
		t.Parallel()

		mine := "package fibermigrate\n\n// my own file\n"
		run := clientRunMigration(t, map[string]string{
			"main.go":      source("main", "get"),
			clientShimFile: mine,
		})
		assert.Equal(t, mine, run.files[clientShimFile])
		assert.Contains(t, run.files["main.go"], "fiber.Get(url)")
		require.Len(t, run.notes, 1)
		assert.Contains(t, run.notes[0], "left as Fiber v2 code")
		assert.Contains(t, run.notes[0], "holds a Go file that this migration did not write")
	})

	t.Run("a file of that name that another migration wrote stays", func(t *testing.T) {
		t.Parallel()

		other := "// This file was added by \"fiber migrate\": something else.\n\npackage fibermigrate\n"
		run := clientRunMigration(t, map[string]string{
			"main.go":      source("main", "get"),
			clientShimFile: other,
		})
		assert.Equal(t, other, run.files[clientShimFile])
		assert.Contains(t, run.files["main.go"], "fiber.Get(url)")
		require.Len(t, run.notes, 1)
		assert.Contains(t, run.notes[0], "has an agent.go that this migration did not write")
	})

	t.Run("a package of the project of that name stays", func(t *testing.T) {
		t.Parallel()

		run := clientRunMigration(t, map[string]string{
			"main.go":                        source("main", "get"),
			"internal/fibermigrate/other.go": "package fibermigrate\n\nvar X = 1\n",
		})
		assert.NoFileExists(t, filepath.Join(run.dir, filepath.FromSlash(clientShimFile)))
		assert.Contains(t, run.files["main.go"], "fiber.Get(url)")
		require.Len(t, run.notes, 1)
		assert.Contains(t, run.notes[0], "left as Fiber v2 code")
		assert.Contains(t, run.notes[0], "holds a Go file that this migration did not write")
	})

	t.Run("a project without a go.mod cannot have it", func(t *testing.T) {
		t.Parallel()

		run := clientRunProject(t, map[string]string{"main.go": source("main", "get")}, false)
		assert.NoFileExists(t, filepath.Join(run.dir, filepath.FromSlash(clientShimFile)))
		assert.Contains(t, run.files["main.go"], "fiber.Get(url)")
		require.Len(t, run.notes, 1)
		assert.Contains(t, run.notes[0], "left as Fiber v2 code")
		assert.Contains(t, run.notes[0], "not in a Go module")
	})
}
