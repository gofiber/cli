package v3_test

import (
	"testing"
)

func Test_MigrateClientUsage_Requests(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "client_level_settings",
			files: map[string]string{
				"main.go": `package main

import (
	"crypto/tls"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

func insecure(url string) ([]byte, error) {
	a := fiber.Get(url)
	a.InsecureSkipVerify()
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func withTLS(url string, cfg *tls.Config) ([]byte, error) {
	_, body, errs := fiber.Get(url).TLSConfig(cfg).Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func codecs(url string, v any) error {
	_, _, errs := fiber.Post(url).JSONEncoder(json.Marshal).JSONDecoder(json.Unmarshal).JSON(v).Bytes()
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
	"crypto/tls"
	"encoding/json"

	"github.com/gofiber/fiber/v3/client"
)

func insecure(url string) ([]byte, error) {
	a := client.New().R().SetMethod("GET").SetURL(url)
	a.Client().SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func withTLS(url string, cfg *tls.Config) ([]byte, error) {
	resp, err := client.New().
		SetTLSConfig(cfg).
		R().
		SetMethod("GET").
		SetURL(url).
		Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func codecs(url string, v any) error {
	_, err := client.New().
		SetJSONMarshal(json.Marshal).
		SetJSONUnmarshal(json.Unmarshal).
		R().
		SetMethod("POST").
		SetURL(url).
		SetJSON(v).
		Send()
	if err != nil {
		return err
	}
	return nil
}
`,
			},
		},
		{
			name: "decl_line_chain",
			files: map[string]string{
				"main.go": `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

func quote() ([]byte, error) {
	agent := fiber.Get("https://example.com/api/random").Timeout(3 * time.Second)
	_, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func chained(url, token string) ([]byte, error) {
	agent := fiber.Post(url).Set("Authorization", "Bearer "+token).
		JSON(map[string]string{"k": "v"})
	_, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func trailing(url string) ([]byte, error) {
	agent := fiber.Get(url) // the request
	// the response
	_, body, errs := agent.Bytes()
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
	"time"

	"github.com/gofiber/fiber/v3/client"
)

func quote() ([]byte, error) {
	resp, err := client.Get("https://example.com/api/random", client.Config{Timeout: 3 * time.Second})
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func chained(url, token string) ([]byte, error) {
	resp, err := client.Post(url, client.Config{
		Header: map[string]string{"Authorization": "Bearer " + token},
		Body:   map[string]string{"k": "v"},
	})
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func trailing(url string) ([]byte, error) {
	// the request
	// the response
	resp, err := client.Get(url)
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
			name: "fluent_chains",
			files: map[string]string{
				"main.go": `package main

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

func reassign(url string) (int, error) {
	var code int
	var errs []error
	code, _, errs = fiber.Head(url).Bytes()
	if len(errs) > 0 {
		return 0, errs[0]
	}
	return code, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

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
	if err != nil {
		return nil, fmt.Errorf("get failed: %v", err)
	}
	code, body := resp.StatusCode(), resp.Body()
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
	if err != nil {
		return nil, err
	}
	status, body := resp.StatusCode(), resp.Body()
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
	if err != nil {
		return "", err
	}
	body := string(resp.Body())
	return body, nil
}

func reassign(url string) (int, error) {
	var code int
	var errs []error
	resp, err := client.Head(url)
	code = 0
	errs = nil
	if err == nil {
		code = resp.StatusCode()
	}
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return 0, errs[0]
	}
	return code, nil
}
`,
			},
		},
		{
			name: "json_headers_and_debug",
			files: map[string]string{
				"main.go": `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
)

func jsonAccept(url string, v map[string]string) error {
	a := fiber.Post(url)
	a.Set("Accept", "text/plain")
	a.JSON(v)
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func jsonContentTypeFirst(url string, v map[string]string) error {
	a := fiber.Post(url)
	a.JSON(v)
	a.ContentType("application/vnd.api+json")
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func jsonMatching(url string, v map[string]string) error {
	a := fiber.Post(url)
	a.ContentType("application/json")
	a.Set("Accept", "application/json")
	a.JSON(v)
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func debugging(url string) error {
	a := fiber.Get(url)
	a.Debug()
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	b := fiber.Get(url).Debug(os.Stderr)
	_, _, errs = b.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func reuse(url string) error {
	_, _, errs := fiber.Get(url).Reuse().Bytes()
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

func jsonAccept(url string, v map[string]string) error {
	_, err := client.Post(url, client.Config{Header: map[string]string{"Accept": "text/plain"}, Body: v})
	if err != nil {
		return err
	}
	return nil
}

func jsonContentTypeFirst(url string, v map[string]string) error {
	_, err := client.Post(url, client.Config{
		Body:   v,
		Header: map[string]string{"Content-Type": "application/vnd.api+json"},
	})
	if err != nil {
		return err
	}
	return nil
}

func jsonMatching(url string, v map[string]string) error {
	_, err := client.Post(url, client.Config{
		Header: map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		Body:   v,
	})
	if err != nil {
		return err
	}
	return nil
}

func debugging(url string) error {
	_, err := client.Get(url)
	var errs []error
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	_, err = client.Get(url)
	errs = nil
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func reuse(url string) error {
	_, err := client.Get(url)
	if err != nil {
		return err
	}
	return nil
}
`,
			},
			notes: []string{
				"main.go:11: the v3 client sets Accept: application/json for a body it encodes as json, which replaces this header",
				"main.go:23: the v3 client sets Content-Type: application/json for a body it encodes as json, which replaces this header",
				"main.go:45: Debug() has no per-request equivalent in the v3 client and was removed, so the request/response dump is gone; call Debug() on the client (client.C().Debug() or client.New().Debug()) to log every request through its logger",
				"main.go:50: Debug() has no per-request equivalent in the v3 client and was removed, so the request/response dump is gone; call Debug() on the client (client.C().Debug() or client.New().Debug()) to log every request through its logger",
			},
		},
		{
			name: "query_string",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func literal(base string) ([]byte, error) {
	a := fiber.Get("http://localhost:3000/echo")
	a.QueryString("q=hello%20world&tags=a&tags=b")
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func onVariableURL(base string) ([]byte, error) {
	a := fiber.Get(base + "/echo")
	a.QueryString("q=hello%20world&tags=a&tags=b")
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func replacesQuery(base string) ([]byte, error) {
	a := fiber.Get(base + "/echo?x=1")
	a.QueryString("y=2")
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func notALiteral(base, qs string) ([]byte, error) {
	a := fiber.Get(base)
	a.QueryString(qs)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func requestArgs(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	req.URI().QueryArgs().Add("a", "1")
	req.URI().QueryArgs().Add("a", "2")
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
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func literal(base string) ([]byte, error) {
	resp, err := client.Get("http://localhost:3000/echo?q=hello%20world&tags=a&tags=b")
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func onVariableURL(base string) ([]byte, error) {
	a := client.C().R().SetMethod("GET").SetURL(base + "/echo")
	a.AddParam("q", "hello world").AddParam("tags", "a").AddParam("tags", "b")
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func replacesQuery(base string) ([]byte, error) {
	a := client.C().R().SetMethod("GET").SetURL(base + "/echo")
	a.AddParam("y", "2")
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func notALiteral(base, qs string) ([]byte, error) {
	a := fiber.Get(base)
	a.QueryString(qs)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func requestArgs(url string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	a.AddParam("a", "1")
	a.AddParam("a", "2")
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}
`,
			},
			notes: []string{
				"main.go:28: QueryString() replaced the query of this URL in Fiber v2, so the query was removed from the URL; the v3 client would send both",
				"main.go:38: left as Fiber v2 code: QueryString() with a value that is not a string literal (put the query in the URL instead)",
			},
		},
		{
			name: "setters_body",
			files: map[string]string{
				"main.go": `package main

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

type item struct{ Name string }

func rawBodies(url, line string, data []byte) error {
	a := fiber.Post(url)
	a.Body([]byte(line))
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}

	b := fiber.Post(url)
	b.Body(data)
	if _, _, errs := b.Bytes(); len(errs) > 0 {
		return errs[0]
	}

	c := fiber.Post(url)
	c.BodyString(strings.ToUpper(line))
	_, _, errs = c.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func jsonBodies(url string, items []item, m map[string]any) error {
	_, _, errs := fiber.Post(url).JSON(items).Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	_, _, errs = fiber.Post(url).JSON(fiber.Map{"n": strings.ToUpper(url), "m": m}).Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	_, _, errs = fiber.Post(url).XML(items).Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func jsonString(url, text string) error {
	_, _, errs := fiber.Post(url).JSON(text).Bytes()
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
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

type item struct{ Name string }

func rawBodies(url, line string, data []byte) error {
	_, err := client.Post(url, client.Config{Body: []byte(line)})
	var errs []error
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return errs[0]
	}

	{
		_, err = client.Post(url, client.Config{Body: data})
		if err != nil {
			return err
		}
	}

	_, err = client.Post(url, client.Config{Body: strings.ToUpper(line)})
	errs = nil
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func jsonBodies(url string, items []item, m map[string]any) error {
	_, err := client.Post(url, client.Config{Body: items})
	var errs []error
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	_, err = client.Post(url, client.Config{Body: fiber.Map{"n": strings.ToUpper(url), "m": m}})
	errs = nil
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	_, err = client.C().R().
		SetMethod("POST").
		SetURL(url).
		SetXML(items).
		Send()
	errs = nil
	if err != nil {
		errs = []error{err}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func jsonString(url, text string) error {
	_, err := client.C().R().
		SetMethod("POST").
		SetURL(url).
		SetJSON(text).
		Send()
	if err != nil {
		return err
	}
	return nil
}
`,
			},
		},
		{
			name: "setters_config",
			files: map[string]string{
				"main.go": `package main

import (
	"encoding/base64"
	"time"

	"github.com/gofiber/fiber/v2"
)

type body struct{ N int }

func literals(url string) ([]byte, error) {
	agent := fiber.Get(url)
	agent.Set("X-Static", "1")
	agent.ContentType("application/json")
	agent.UserAgent("svc/2.0")
	agent.Referer("https://example.com/")
	agent.Cookie("sid", "abc")
	agent.Cookie("theme", "dark")
	agent.MaxRedirectsCount(3)
	agent.Timeout(2 * time.Second)
	_, b, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return b, nil
}

func dynamic(url, token, ua string, n int) ([]byte, error) {
	agent := fiber.Post(url)
	agent.Set("Authorization", "Bearer "+token)
	agent.Add("X-Multi", "a")
	agent.UserAgent(ua)
	agent.BasicAuth("user", "secret")
	agent.JSON(body{N: n})
	_, b, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return b, nil
}

func basicVars(url, user, pass string) ([]byte, error) {
	code, b, errs := fiber.Get(url).BasicAuth(user, pass).Bytes()
	if len(errs) > 0 || code != 200 {
		return nil, nil
	}
	return b, nil
}

func bytesVariants(url string, k, v []byte) ([]byte, error) {
	_, b, errs := fiber.Get(url).SetBytesKV(k, v).SetBytesK(k, "x").AddBytesV("A", v).ContentTypeBytes(k).
		UserAgentBytes(v).RefererBytes(v).CookieBytesKV(k, v).Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return b, nil
}

func closeAndEncode(url string) string {
	return base64.StdEncoding.EncodeToString([]byte(url))
}

func closing(url string) ([]byte, error) {
	_, b, errs := fiber.Get(url).ConnectionClose().Cookies("a", "1", "b", "2").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return b, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"encoding/base64"
	"time"

	"github.com/gofiber/fiber/v3/client"
)

type body struct{ N int }

func literals(url string) ([]byte, error) {
	resp, err := client.Get(url, client.Config{
		Header:       map[string]string{"X-Static": "1", "Content-Type": "application/json"},
		UserAgent:    "svc/2.0",
		Referer:      "https://example.com/",
		Cookie:       map[string]string{"sid": "abc", "theme": "dark"},
		MaxRedirects: 3,
		Timeout:      2 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	b := resp.Body()
	return b, nil
}

func dynamic(url, token, ua string, n int) ([]byte, error) {
	agent := client.C().R().SetMethod("POST").SetURL(url)
	agent.SetHeader("Authorization", "Bearer "+token)
	agent.AddHeader("X-Multi", "a")
	agent.SetUserAgent(ua)
	agent.SetHeader("Authorization", "Basic dXNlcjpzZWNyZXQ=")
	agent.SetJSON(body{N: n})
	resp, err := agent.Send()
	if err != nil {
		return nil, err
	}
	b := resp.Body()
	return b, nil
}

func basicVars(url, user, pass string) ([]byte, error) {
	resp, err := client.Get(url, client.Config{
		Header: map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))},
	})
	var code int
	var b []byte
	if err == nil {
		code = resp.StatusCode()
		b = resp.Body()
	}
	if err != nil || code != 200 {
		return nil, nil
	}
	return b, nil
}

func bytesVariants(url string, k, v []byte) ([]byte, error) {
	resp, err := client.C().R().
		SetMethod("GET").
		SetURL(url).
		SetHeader(string(k), string(v)).
		SetHeader(string(k), "x").
		AddHeader("A", string(v)).
		SetHeader("Content-Type", string(k)).
		SetUserAgent(string(v)).
		SetReferer(string(v)).
		SetCookie(string(k), string(v)).
		Send()
	if err != nil {
		return nil, err
	}
	b := resp.Body()
	return b, nil
}

func closeAndEncode(url string) string {
	return base64.StdEncoding.EncodeToString([]byte(url))
}

func closing(url string) ([]byte, error) {
	resp, err := client.Get(url, client.Config{
		Header: map[string]string{"Connection": "close"},
		Cookie: map[string]string{"a": "1", "b": "2"},
	})
	if err != nil {
		return nil, err
	}
	b := resp.Body()
	return b, nil
}
`,
			},
		},
		{
			name: "terminal_shapes",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type out struct{ ID int }

func ifInit(url string) error {
	agent := fiber.Get(url)
	if code, body, errs := agent.Bytes(); len(errs) == 0 {
		fmt.Println(code, string(body))
		return nil
	} else {
		return errs[0]
	}
}

func ifInitStruct(url string) (*out, error) {
	var o out
	a := fiber.Get(url)
	if code, _, errs := a.Struct(&o); len(errs) > 0 {
		return nil, fmt.Errorf("%d: %v", code, errs)
	}
	return &o, nil
}

func varBetween(url string) (*out, error) {
	a := fiber.Get(url)
	var o out
	code, _, errs := a.Struct(&o)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	_ = code
	return &o, nil
}

func blankErrs(url string) []byte {
	_, body, _ := fiber.Get(url).Bytes()
	return body
}

func otherNames(url string) ([]byte, error) {
	status, payload, problems := fiber.Get(url).Bytes()
	if len(problems) > 0 {
		return nil, problems[0]
	}
	_ = status
	return payload, nil
}

func noStatement(url string) {
	fiber.Get(url).Bytes()
}

func comments(url string) ([]byte, error) {
	// fetch the page
	agent := fiber.Get(url)

	// send it
	code, body, errs := agent.Bytes() // all in one
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
	"fmt"

	"github.com/gofiber/fiber/v3/client"
)

type out struct{ ID int }

func ifInit(url string) error {
	resp, err := client.Get(url)
	var code int
	var body []byte
	if err == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if err == nil {
		fmt.Println(code, string(body))
		return nil
	} else {
		return err
	}
}

func ifInitStruct(url string) (*out, error) {
	var o out
	resp, err := client.Get(url)
	var code int
	if err == nil {
		code = resp.StatusCode()
		err = resp.JSON(&o)
	}
	if err != nil {
		return nil, fmt.Errorf("%d: %v", code, err)
	}
	return &o, nil
}

func varBetween(url string) (*out, error) {
	var o out
	resp, err := client.Get(url)
	var code int
	if err == nil {
		code = resp.StatusCode()
		err = resp.JSON(&o)
	}
	if err != nil {
		return nil, err
	}
	_ = code
	return &o, nil
}

func blankErrs(url string) []byte {
	resp, err := client.Get(url)
	var body []byte
	if err == nil {
		body = resp.Body()
	}
	return body
}

func otherNames(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	status, payload := resp.StatusCode(), resp.Body()
	_ = status
	return payload, nil
}

func noStatement(url string) {
	_, _ = client.Get(url)
}

func comments(url string) ([]byte, error) {
	// fetch the page

	// send it
	resp, err := client.Get(url) // all in one
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
			name: "unsupported_settings",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func host(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Host("virtual.example.com").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func hostHeader(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Set("Host", "virtual.example.com").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func response(url string) error {
	resp := fiber.AcquireResponse()
	defer fiber.ReleaseResponse(resp)
	a := fiber.Get(url)
	a.SetResponse(resp)
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func dest(url string, dst []byte) ([]byte, error) {
	_, body, errs := fiber.Get(url).Dest(dst).Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func jsonContentType(url string, v any) error {
	_, _, errs := fiber.Post(url).JSON(v, "application/vnd.api+json").Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func nameOfFiberGet(url string) error {
	a := fiber.Get(url)
	a.Name = "svc"
	_, _, errs := a.Bytes()
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
	"github.com/valyala/fasthttp"
)

func host(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Host("virtual.example.com").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func hostHeader(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Set("Host", "virtual.example.com").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func response(url string) error {
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)
	a := fiber.Get(url)
	a.SetResponse(resp)
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func dest(url string, dst []byte) ([]byte, error) {
	_, body, errs := fiber.Get(url).Dest(dst).Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func jsonContentType(url string, v any) error {
	_, _, errs := fiber.Post(url).JSON(v, "application/vnd.api+json").Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func nameOfFiberGet(url string) error {
	a := fiber.Get(url)
	a.Name = "svc"
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
`,
			},
			notes: []string{
				"main.go:8: left as Fiber v2 code: Host(): the v3 client cannot send a Host header that differs from the URL (use a custom dialer, client.New().SetDial(...), with the virtual host in the URL)",
				"main.go:16: left as Fiber v2 code: a Host header (the v3 client cannot send a Host header that differs from the URL)",
				"main.go:26: left as Fiber v2 code: SetResponse() has no v3 equivalent (use the client.Response that Send returns, or client.NewWithHostClient for fasthttp tuning)",
				"main.go:36: left as Fiber v2 code: Dest() has no v3 equivalent (use the client.Response that Send returns, or client.NewWithHostClient for fasthttp tuning)",
				"main.go:44: left as Fiber v2 code: JSON() with a custom content type (the v3 client always sends application/json for a JSON body)",
				"main.go:52: left as Fiber v2 code: setting Name of an agent that fiber.Get made, which has no effect in v2",
			},
		},
	})
}
