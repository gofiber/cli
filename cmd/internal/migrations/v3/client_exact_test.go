package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// These cases are the requests whose behavior the v3 client cannot reproduce with
// a call of its own. They are written so that they behave as they did in Fiber v2,
// or stay on the copy of the v2 agent.

func Test_MigrateClientUsage_Cookies(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "one_cookie_has_no_order",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

func single(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Cookie("sid", "abc").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func pairs(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Cookies("sid", "abc").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v3/client"

func single(url string) ([]byte, error) {
	resp, err := client.Get(url, client.Config{Cookie: map[string]string{"sid": "abc"}})
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func pairs(url string) ([]byte, error) {
	resp, err := client.Get(url, client.Config{Cookie: map[string]string{"sid": "abc"}})
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
			name: "several_cookies_are_sent_in_the_order_they_were_set",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

func several(url, a, b string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Cookie("sid", a).CookieBytesKV([]byte("theme"), []byte(b)).Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func pairs(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Cookies("a", "1", "b", "2", "c", "3").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func mixed(url string) ([]byte, error) {
	agent := fiber.Get(url)
	agent.Cookie("x", "0")
	agent.Set("X-Id", "7")
	agent.Cookies("a", "1")
	_, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func fromRequest(url string) ([]byte, error) {
	agent := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(agent)
	req := agent.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	req.Header.SetCookie("first", "1")
	req.Header.SetCookie("second", "2")
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
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func several(url, a, b string) ([]byte, error) {
	req := client.C().R().SetMethod("GET").SetURL(url)
	req.RawRequest.Header.SetCookie("sid", a)
	req.RawRequest.Header.SetCookie("theme", string([]byte(b)))
	resp, err := req.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func pairs(url string) ([]byte, error) {
	req := client.C().R().SetMethod("GET").SetURL(url)
	req.RawRequest.Header.SetCookie("a", "1")
	req.RawRequest.Header.SetCookie("b", "2")
	req.RawRequest.Header.SetCookie("c", "3")
	resp, err := req.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func mixed(url string) ([]byte, error) {
	agent := client.C().R().SetMethod("GET").SetURL(url)
	agent.RawRequest.Header.SetCookie("x", "0")
	agent.SetHeader("X-Id", "7")
	agent.RawRequest.Header.SetCookie("a", "1")
	resp, err := agent.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func fromRequest(url string) ([]byte, error) {
	agent := client.C().R()
	agent.SetMethod(fiber.MethodGet)
	agent.SetURL(url)
	agent.RawRequest.Header.SetCookie("first", "1")
	agent.RawRequest.Header.SetCookie("second", "2")
	resp, err := agent.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}
`,
			},
		},
	})
}

func Test_MigrateClientUsage_Schemes(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			// The v2 agent sent a URL that starts with // over http, the v3 client
			// refuses it. A URL without a scheme was refused by v2 as well.
			name: "a_url_that_starts_with_two_slashes",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

func literal() ([]byte, error) {
	_, body, errs := fiber.Get("//localhost:3000/echo").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func concat(host string) ([]byte, error) {
	_, body, errs := fiber.Get("//" + host + "/echo").Set("X-Id", "1").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func withQuery() ([]byte, error) {
	a := fiber.Get("//localhost:3000/echo?x=1")
	a.QueryString("y=2")
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func acquired(host string) ([]byte, error) {
	a := fiber.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI("//" + host)
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func noScheme() ([]byte, error) {
	_, body, errs := fiber.Get("localhost:3000/echo").Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func upper() ([]byte, error) {
	_, body, errs := fiber.Get("HTTP://localhost:3000/echo").Bytes()
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

func literal() ([]byte, error) {
	resp, err := client.Get("http://localhost:3000/echo")
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func concat(host string) ([]byte, error) {
	resp, err := client.Get("http://"+host+"/echo", client.Config{Header: map[string]string{"X-Id": "1"}})
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func withQuery() ([]byte, error) {
	resp, err := client.Get("http://localhost:3000/echo?y=2")
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func acquired(host string) ([]byte, error) {
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL("http://" + host)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func noScheme() ([]byte, error) {
	resp, err := client.Get("localhost:3000/echo")
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func upper() ([]byte, error) {
	resp, err := client.Get("HTTP://localhost:3000/echo")
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}
`,
			},
			notes: []string{"main.go:22: " + clientQueryNote},
		},
	})
}

func Test_MigrateClientUsage_QueriesStayExact(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "a_query_that_v2_replaced_or_discarded",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v2"

func setThenReplace(url string) ([]byte, error) {
	a := fiber.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI("http://localhost:3000/echo?x=1")
	a.QueryString("y=2")
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func inTheMiddle(base string) ([]byte, error) {
	a := fiber.Get(base + "?x=1&y=" + base)
	a.QueryString("z=3")
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func atTheEnd(base string) ([]byte, error) {
	a := fiber.Get(base + "/echo?x=1")
	a.QueryString("z=3")
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

func setThenReplace(url string) ([]byte, error) {
	a := fibermigrate.AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI("http://localhost:3000/echo?x=1")
	a.QueryString("y=2")
	if err := a.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func inTheMiddle(base string) ([]byte, error) {
	a := fibermigrate.Get(base + "?x=1&y=" + base)
	a.QueryString("z=3")
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func atTheEnd(base string) ([]byte, error) {
	a := client.C().R().SetMethod("GET").SetURL(base + "/echo")
	a.AddParam("z", "3")
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
				"main.go:6: QueryString() replaces the query of the URL that SetRequestURI() sets, where the v3 client adds the parameters to it" + clientShimNote,
				"main.go:22: QueryString() replaces the query of the URL, which the migration cannot take out of it" + clientShimNote,
				"main.go:32: " + clientQueryNote,
			},
		},
	})
}

func Test_MigrateClientUsage_MultipartFormWithNilFields(t *testing.T) {
	t.Parallel()

	run := clientMigrateSource(t, `package main

import "github.com/gofiber/fiber/v2"

func upload(url string, args *fiber.Args) ([]byte, error) {
	a := fiber.Post(url)
	a.SendFile("./go.mod", "f")
	a.MultipartForm(args)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`)
	got := run.files["main.go"]
	assert.Contains(t, got, "if args != nil {", "a nil *Args adds no field")
	assert.NotContains(t, got, "Content-Type", "a multipart form has no content type to keep")
	assert.NotContains(t, got, "else")
}

func Test_MigrateClientUsage_NotesOncePerProject(t *testing.T) {
	t.Parallel()

	const file = `package %s

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func sendJSON(url string, v map[string]string) {
	_, _, errs := fiber.Post(url).JSON(v).Bytes()
	fmt.Println("json", errs)
}

func sendQuery(url string) {
	_, _, errs := fiber.Get(url + "?q=a%20b").Bytes()
	fmt.Println("query", errs)
}
`
	src := func(pkg string) string { return strings.Replace(file, "%s", pkg, 1) }
	run := clientRunMigration(t, map[string]string{
		"a/a.go": src("a"),
		"b/b.go": src("b"),
		"c/c.go": src("c"),
	})
	count := func(substr string) (n int) {
		for _, note := range run.notes {
			if strings.Contains(note, substr) {
				n++
			}
		}
		return n
	}
	assert.Equal(t, 1, count(clientAcceptNote), "%v", run.notes)
	assert.Equal(t, 1, count(clientQueryNote), "%v", run.notes)
	assert.Equal(t, 1, count(clientErrsNote), "%v", run.notes)
	assert.Len(t, run.notes, 3)
}
