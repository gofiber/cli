package v3_test

import (
	"testing"
)

func Test_MigrateClientUsage_Types(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "agents_in_signatures",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func newReq(url string) *fiber.Agent {
	return fiber.Get(url).Set("Accept", "application/json")
}

func send(a *fiber.Agent) ([]byte, error) {
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func build(method, url string) *fiber.Agent {
	a := fiber.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(method)
	req.SetRequestURI(url)
	return a
}

func useHelpers(url string) ([]byte, error) {
	a := build(fiber.MethodPost, url)
	a.Set("X-Id", "1")
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func viaParam(a *fiber.Agent) (int, error) {
	a.Set("X-Trace", "1")
	code, _, errs := a.Bytes()
	if len(errs) > 0 {
		return 0, errs[0]
	}
	return code, nil
}

type holder struct {
	agent *fiber.Agent
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func newReq(url string) *client.Request {
	return client.C().R().SetMethod("GET").SetURL(url).SetHeader("Accept", "application/json")
}

func send(a *client.Request) ([]byte, error) {
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func build(method, url string) *client.Request {
	a := client.C().R()
	a.SetMethod(method)
	a.SetURL(url)
	return a
}

func useHelpers(url string) ([]byte, error) {
	a := build(fiber.MethodPost, url)
	a.SetHeader("X-Id", "1")
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func viaParam(a *client.Request) (int, error) {
	a.SetHeader("X-Trace", "1")
	resp, err := a.Send()
	if err != nil {
		return 0, err
	}
	code := resp.StatusCode()
	return code, nil
}

type holder struct {
	agent *fiber.Agent
}
`,
			},
			notes: []string{
				"main.go:50: `fiber.Agent` does not exist in Fiber v3 and was not migrated; use the client package (client.Get, client.Post, client.New().R(), ...)",
			},
		},
		{
			name: "clients_acquire",
			files: map[string]string{
				"main.go": `package main

import (
	"encoding/json"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func viaAcquired(url string) ([]byte, error) {
	client := fiber.AcquireClient()
	defer fiber.ReleaseClient(client)
	agent := client.Get(url)
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed: %v", errs)
	}
	_ = code
	return body, nil
}

func literal(url string) ([]byte, error) {
	c := &fiber.Client{UserAgent: "svc/1.0"}
	agent := c.Post(url)
	agent.BodyString("x")
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed: %v", errs)
	}
	_ = code
	return body, nil
}

var httpClient = &fiber.Client{
	UserAgent:   "svc/2.0",
	JSONEncoder: json.Marshal,
	JSONDecoder: json.Unmarshal,
}

func viaPackageVar(url string) ([]byte, error) {
	a := httpClient.Get(url)
	code, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, fmt.Errorf("failed: %v", errs)
	}
	_ = code
	return body, nil
}

func fieldAssignment() *fiber.Client {
	c := fiber.AcquireClient()
	c.UserAgent = "agent/3.0"
	c.JSONEncoder = json.Marshal
	return c
}

func newClient() *fiber.Client {
	return new(fiber.Client)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"encoding/json"
	"fmt"

	fiberclient "github.com/gofiber/fiber/v3/client"
)

func viaAcquired(url string) ([]byte, error) {
	client := fiberclient.New()
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed: %v", err)
	}
	code, body := resp.StatusCode(), resp.Body()
	_ = code
	return body, nil
}

func literal(url string) ([]byte, error) {
	c := fiberclient.New().SetUserAgent("svc/1.0")
	resp, err := c.Post(url, fiberclient.Config{Body: "x"})
	if err != nil {
		return nil, fmt.Errorf("failed: %v", err)
	}
	code, body := resp.StatusCode(), resp.Body()
	_ = code
	return body, nil
}

var httpClient = fiberclient.New().SetUserAgent("svc/2.0").SetJSONMarshal(json.Marshal).SetJSONUnmarshal(json.Unmarshal)

func viaPackageVar(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed: %v", err)
	}
	code, body := resp.StatusCode(), resp.Body()
	_ = code
	return body, nil
}

func fieldAssignment() *fiberclient.Client {
	c := fiberclient.New()
	c.SetUserAgent("agent/3.0")
	c.SetJSONMarshal(json.Marshal)
	return c
}

func newClient() *fiberclient.Client {
	return fiberclient.New()
}
`,
			},
		},
		{
			name: "clients_in_structs",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

type API struct {
	cl      *fiber.Client
	baseURL string
}

func NewAPI(base string) *API {
	return &API{cl: &fiber.Client{UserAgent: "api"}, baseURL: base}
}

func (a *API) Get(path string, out any) error {
	agent := a.cl.Get(a.baseURL + path)
	_, _, errs := agent.Struct(out)
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v3/client"
)

type API struct {
	cl      *client.Client
	baseURL string
}

func NewAPI(base string) *API {
	return &API{cl: client.New().SetUserAgent("api"), baseURL: base}
}

func (a *API) Get(path string, out any) error {
	resp, err := a.cl.Get(a.baseURL + path)
	if err == nil {
		err = resp.JSON(out)
	}
	if err != nil {
		return err
	}
	return nil
}
`,
			},
		},
		{
			name: "fasthttp_aliases",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func responses() {
	resp := fiber.AcquireResponse()
	defer fiber.ReleaseResponse(resp)
	var req *fiber.Request
	_ = req
	_ = resp.StatusCode()
}

func handlers(r *fiber.Request, w *fiber.Response) {
	_ = r.URI()
	_ = w.StatusCode()
}

func args() string {
	a := fiber.AcquireArgs()
	defer fiber.ReleaseArgs(a)
	a.Set("a", "1")
	a.Add("b", "2")
	return a.String()
}

func argsValue() *fiber.Args {
	var a fiber.Args
	a.Set("x", "y")
	return &a
}

var retry fiber.RetryIfFunc
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"github.com/valyala/fasthttp"
)

func responses() {
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)
	var req *fasthttp.Request
	_ = req
	_ = resp.StatusCode()
}

func handlers(r *fasthttp.Request, w *fasthttp.Response) {
	_ = r.URI()
	_ = w.StatusCode()
}

func args() string {
	a := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(a)
	a.Set("a", "1")
	a.Add("b", "2")
	return a.String()
}

func argsValue() *fasthttp.Args {
	var a fasthttp.Args
	a.Set("x", "y")
	return &a
}

var retry fasthttp.RetryIfFunc
`,
			},
		},
	})
}
