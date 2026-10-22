package v3_test

import (
	"testing"
)

func Test_MigrateClientUsage_AcquireAgent(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "acquire_basic",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func call(method, url string, payload []byte) ([]byte, int, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(method)
	req.SetRequestURI(url)
	req.SetBody(payload)
	if err := a.Parse(); err != nil {
		return nil, 0, err
	}
	code, respBody, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, 0, errs[0]
	}
	return respBody, code, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v3/client"
)

func call(method, url string, payload []byte) ([]byte, int, error) {
	a := client.C().R()
	a.SetMethod(method)
	a.SetURL(url)
	a.SetRawBody(payload)
	resp, err := a.Send()
	if err != nil {
		return nil, 0, err
	}
	code, respBody := resp.StatusCode(), resp.Body()
	return respBody, code, nil
}
`,
			},
		},
		{
			name: "acquire_blank_values",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func newAgent(url string) *fiber.Agent {
	a := fiber.AcquireAgent()
	a.Request().SetRequestURI(url)
	return a
}

func onlyBody(url string) ([]byte, error) {
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

func onlyCode(url string) (int, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return 0, err
	}
	code, _, errs := a.Bytes()
	if len(errs) > 0 {
		return 0, errs[0]
	}
	return code, nil
}

func neither(url string) error {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodDelete)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return err
	}
	_, _, errs := a.String()
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
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func newAgent(url string) *client.Request {
	a := client.C().R()
	a.SetURL(url)
	return a
}

func onlyBody(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func onlyCode(url string) (int, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if err != nil {
		return 0, err
	}
	code := resp.StatusCode()
	return code, nil
}

func neither(url string) error {
	a := client.C().R()
	a.SetMethod(fiber.MethodDelete)
	a.SetURL(url)
	_, err := a.Send()
	if err != nil {
		return err
	}
	return nil
}
`,
			},
		},
		{
			name: "acquire_declined",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func hostHeader(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	a.Host("virtual.example.com")
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func parseOnly(url string) error {
	a := fiber.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return err
	}
	return nil
}

func escapes(url string, use func(*fiber.Agent)) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.SetRequestURI(url)
	go use(a)
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func hostHeader(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	a.Host("virtual.example.com")
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func parseOnly(url string) error {
	a := fiber.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return err
	}
	return nil
}

func escapes(url string, use func(*client.Request)) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.SetRequestURI(url)
	go use(a)
}
`,
			},
			notes: []string{
				"main.go:8: left as Fiber v2 code: Host(): the v3 client cannot send a Host header that differs from the URL (use a custom dialer, client.New().SetDial(...), with the virtual host in the URL)",
				"main.go:25: left as Fiber v2 code: Parse() is checked, but no request is sent after it in the same block",
				"main.go:36: left as Fiber v2 code: `go use(a)` is not understood by the migration",
			},
		},
		{
			name: "acquire_name_collisions",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func bodyCalledResp(host string, body []byte) error {
	agent := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(agent)
	req := agent.Request()
	req.Header.SetMethod(fiber.MethodPost)
	req.SetBody(body)
	req.SetRequestURI("http://" + host)
	if err := agent.Parse(); err != nil {
		return err
	}
	code, resp, errs := agent.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	fmt.Println(code, string(resp))
	return nil
}

func twoAgents(u1, u2 string) error {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(u1)
	if err := a.Parse(); err != nil {
		return err
	}
	c1, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}

	b := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(b)
	req2 := b.Request()
	req2.Header.SetMethod(fiber.MethodGet)
	req2.SetRequestURI(u2)
	if err := b.Parse(); err != nil {
		return err
	}
	c2, _, errs := b.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	fmt.Println(c1, c2)
	return nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func bodyCalledResp(host string, body []byte) error {
	agent := client.C().R()
	agent.SetMethod(fiber.MethodPost)
	agent.SetRawBody(body)
	agent.SetURL("http://" + host)
	resp2, err := agent.Send()
	if err != nil {
		return err
	}
	code, resp := resp2.StatusCode(), resp2.Body()
	fmt.Println(code, string(resp))
	return nil
}

func twoAgents(u1, u2 string) error {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(u1)
	resp, err := a.Send()
	if err != nil {
		return err
	}
	c1 := resp.StatusCode()

	b := client.C().R()
	b.SetMethod(fiber.MethodGet)
	b.SetURL(u2)
	resp2, err := b.Send()
	if err != nil {
		return err
	}
	c2 := resp2.StatusCode()
	fmt.Println(c1, c2)
	return nil
}
`,
			},
		},
		{
			name: "acquire_parse_forms",
			files: map[string]string{
				"main.go": `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

func declared(url string) ([]byte, error) {
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

func audit(user string) string { return "audit:" + user }

func shortName(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if e := a.Parse(); e != nil {
		return nil, e
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func ignoredParse(url string) []byte {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	_ = a.Parse()
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		log.Println(errs)
	}
	return body
}

func overwritten(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
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
	err = nil
	return body, err
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"log"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func declared(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func audit(user string) string { return "audit:" + user }

func shortName(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func ignoredParse(url string) []byte {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	if err != nil {
		log.Println(err)
	}
	return body
}

func overwritten(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	err = nil
	return body, err
}
`,
			},
		},
		{
			name: "acquire_parse_timing",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

func timeoutAfterParse(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return nil, err
	}
	a.Timeout(5 * time.Second)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func insecure(url string) (string, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return "", err
	}
	a.InsecureSkipVerify()
	code, body, errs := a.String()
	if len(errs) > 0 {
		return "", fmt.Errorf("failed: %v", errs)
	}
	_ = code
	return body, nil
}

func logBetween(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return nil, err
	}
	fmt.Println("sending", url)
	code, body, errs := a.Bytes() // the request is sent here
	if len(errs) > 0 {
		return nil, errs[0]
	}
	_ = code
	return body, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func timeoutAfterParse(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	a.SetTimeout(5 * time.Second)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func insecure(url string) (string, error) {
	a := client.New().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	a.Client().SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
	resp, err := a.Send()
	if errors.Is(err, client.ErrURLFormat) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("failed: %v", err)
	}
	code, body := resp.StatusCode(), string(resp.Body())
	_ = code
	return body, nil
}

func logBetween(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	fmt.Println("sending", url)
	resp, err := a.Send() // the request is sent here
	if err != nil {
		return nil, err
	}
	code, body := resp.StatusCode(), resp.Body()
	_ = code
	return body, nil
}
`,
			},
		},
		{
			name: "acquire_release_and_inline",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func explicitRelease(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		fiber.ReleaseAgent(a)
		return nil, err
	}
	_, body, errs := a.Bytes()
	fiber.ReleaseAgent(a)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func inlineRequest(url string) ([]byte, error) {
	agent := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(agent)
	agent.Request().Header.SetMethod(fiber.MethodGet)
	agent.Request().SetRequestURI(url)
	if err := agent.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func parseReadLater(url string) ([]byte, error) {
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
		return body, errs[0]
	}
	return body, err
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func explicitRelease(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if errors.Is(err, client.ErrURLFormat) {
		return nil, err
	}
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	if err != nil {
		return nil, err
	}
	return body, nil
}

func inlineRequest(url string) ([]byte, error) {
	agent := client.C().R()
	agent.SetMethod(fiber.MethodGet)
	agent.SetURL(url)
	resp, err := agent.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func parseReadLater(url string) ([]byte, error) {
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
		return body, errs[0]
	}
	return body, err
}
`,
			},
			notes: []string{
				"main.go:40: left as Fiber v2 code: the error of Parse() is read after the request is sent, where it was nil in v2",
			},
		},
		{
			name: "acquire_request_calls",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func configured(url, token, ua string, payload []byte, headers map[string]string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodPost)
	req.Header.SetContentType("application/json")
	req.Header.SetUserAgent(ua)
	req.Header.SetCookie("sid", "abc")
	req.Header.Add("X-Trace", "1")
	req.Header.Set("X-Version", fmt.Sprintf("v%d", 3))
	req.URI().QueryArgs().Add("debug", "1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.SetRequestURI(url)
	req.SetBody(payload)
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func agentSetters(url string, v map[string]any) (int, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	a.Name = "svc/1.0"
	req := a.Request()
	req.Header.SetMethod(fiber.MethodPost)
	req.SetRequestURI(url)
	a.JSON(v)
	a.Set("X-Id", "1")
	a.Cookie("theme", "dark")
	if err := a.Parse(); err != nil {
		return 0, err
	}
	code, _, errs := a.Bytes()
	if len(errs) > 0 {
		return 0, errs[0]
	}
	return code, nil
}

func userAgentWithParen(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
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
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func configured(url, token, ua string, payload []byte, headers map[string]string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodPost)
	a.SetHeader("Content-Type", "application/json")
	a.SetUserAgent(ua)
	a.SetCookie("sid", "abc")
	a.AddHeader("X-Trace", "1")
	a.SetHeader("X-Version", fmt.Sprintf("v%d", 3))
	a.AddParam("debug", "1")
	if token != "" {
		a.SetHeader("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		a.SetHeader(k, v)
	}
	a.SetURL(url)
	a.SetRawBody(payload)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func agentSetters(url string, v map[string]any) (int, error) {
	a := client.C().R()
	a.SetUserAgent("svc/1.0")
	a.SetMethod(fiber.MethodPost)
	a.SetURL(url)
	a.SetJSON(v)
	a.SetHeader("X-Id", "1")
	a.SetCookie("theme", "dark")
	resp, err := a.Send()
	if err != nil {
		return 0, err
	}
	code := resp.StatusCode()
	return code, nil
}

func userAgentWithParen(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetHeader("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	a.SetURL(url)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}
`,
			},
		},
		{
			name: "acquire_struct_targets",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

type reply struct {
	Code int
	Body []byte
}

type user struct{ Name string }

func fill(r *reply, url string) error {
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return err
	}
	var errs []error
	r.Code, r.Body, errs = a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func decode(url string) (user, error) {
	var (
		retCode int
		retBody []byte
		errs    []error
		u       user
	)
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return u, err
	}
	if retCode, retBody, errs = a.Struct(&u); len(errs) > 0 {
		return u, errs[0]
	}
	_, _ = retCode, retBody
	return u, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

type reply struct {
	Code int
	Body []byte
}

type user struct{ Name string }

func fill(r *reply, url string) error {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	var errs []error
	resp, err := a.Send()
	if errors.Is(err, client.ErrURLFormat) {
		return err
	}
	r.Code = 0
	r.Body = nil
	errs = nil
	if err == nil {
		r.Code = resp.StatusCode()
		r.Body = resp.Body()
	}
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func decode(url string) (user, error) {
	var (
		retCode int
		retBody []byte
		errs    []error
		u       user
	)
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if errors.Is(err, client.ErrURLFormat) {
		return u, err
	}
	retCode = 0
	retBody = nil
	errs = nil
	if err == nil {
		retCode = resp.StatusCode()
		retBody = resp.Body()
		err = resp.JSON(&u)
	}
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return u, errs[0]
	}
	_, _ = retCode, retBody
	return u, nil
}
`,
			},
		},
		{
			name: "acquire_two_handlers",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func change(host string, port int, body []byte) error {
	agent := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(agent)
	req := agent.Request()
	req.Header.SetMethod(fiber.MethodPost)
	req.Header.SetContentType(fiber.MIMEApplicationJSON)
	req.SetBody(body)
	req.SetRequestURI(fmt.Sprintf("http://%s:%d/account/change", host, port))
	if err := agent.Parse(); err != nil {
		return fmt.Errorf("req issues: %w", err)
	}
	code, resp, errs := agent.Bytes()
	if len(errs) > 0 {
		return fmt.Errorf("transport issues: %v", errs)
	}
	log.Println(code, string(resp))
	return nil
}

func ignored(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, _ := a.Bytes()
	return body, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func change(host string, port int, body []byte) error {
	agent := client.C().R()
	agent.SetMethod(fiber.MethodPost)
	agent.SetHeader("Content-Type", fiber.MIMEApplicationJSON)
	agent.SetRawBody(body)
	agent.SetURL(fmt.Sprintf("http://%s:%d/account/change", host, port))
	resp2, err := agent.Send()
	if errors.Is(err, client.ErrURLFormat) {
		return fmt.Errorf("req issues: %w", err)
	}
	if err != nil {
		return fmt.Errorf("transport issues: %v", err)
	}
	code, resp := resp2.StatusCode(), resp2.Body()
	log.Println(code, string(resp))
	return nil
}

func ignored(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	resp, err := a.Send()
	if errors.Is(err, client.ErrURLFormat) {
		return nil, err
	}
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	return body, nil
}
`,
			},
		},
	})
}
