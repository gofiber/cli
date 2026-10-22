package v3_test

import (
	"testing"
)

func Test_MigrateClientUsage_Names(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "name_aliased_import",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	f "github.com/gofiber/fiber/v2"
)

func fetch(url string) {
	a := f.Get(url)
	code, _, errs := a.Bytes()
	if len(errs) > 0 {
		fmt.Println(errs)
		return
	}
	if code != f.StatusOK {
		fmt.Println(code)
	}
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	f "github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func fetch(url string) {
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println(err)
		return
	}
	code := resp.StatusCode()
	if code != f.StatusOK {
		fmt.Println(code)
	}
}
`,
			},
		},
		{
			name: "name_client_local",
			files: map[string]string{
				"main.go": `package main

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

func localClient(url string) (int, []byte, error) {
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()

	agent := fiber.Get(url)
	code, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return 0, nil, errs[0]
	}
	return code, body, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"net/http"

	fiberclient "github.com/gofiber/fiber/v3/client"
)

func localClient(url string) (int, []byte, error) {
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()

	resp, err := fiberclient.Get(url)
	if err != nil {
		return 0, nil, err
	}
	code, body := resp.StatusCode(), resp.Body()
	return code, body, nil
}
`,
			},
		},
		{
			name: "name_client_package_var",
			files: map[string]string{
				"http.go": `package main

import "net/http"

var client = &http.Client{}

func plain(url string) (*http.Response, error) {
	return client.Get(url)
}
`,
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func fetch(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}
`,
			},
			want: map[string]string{
				"http.go": `package main

import "net/http"

var client = &http.Client{}

func plain(url string) (*http.Response, error) {
	return client.Get(url)
}
`,
				"main.go": `package main

import (
	fiberclient "github.com/gofiber/fiber/v3/client"
)

func fetch(url string) ([]byte, error) {
	resp, err := fiberclient.Get(url)
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
			name: "name_import_comment",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt" // formatting (stdlib)
	// standard library (sorted)
	"os"

	"github.com/gofiber/fiber/v2"
)

func fetch(url string) {
	_, body, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		fmt.Fprintln(os.Stderr, errs)
		return
	}
	fmt.Println(string(body))
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt" // formatting (stdlib)
	// standard library (sorted)
	"os"

	"github.com/gofiber/fiber/v3/client"
)

func fetch(url string) {
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	body := resp.Body()
	fmt.Println(string(body))
}
`,
			},
		},
		{
			name: "name_not_fiber",
			files: map[string]string{
				"main.go": `package main

import (
	"net/http"
)

type fiber struct{}

func (fiber) Get(url string) string { return url }

func local(url string) (string, error) {
	f := fiber{}
	_ = f.Get(url)
	res, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	return res.Status, nil
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"net/http"
)

type fiber struct{}

func (fiber) Get(url string) string { return url }

func local(url string) (string, error) {
	f := fiber{}
	_ = f.Get(url)
	res, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	return res.Status, nil
}
`,
			},
		},
		{
			name: "name_scope_collisions",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func respInUse(url string) error {
	resp := "unused"
	fmt.Println(resp)
	code, body, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	fmt.Println(resp, code, len(body))
	return nil
}

func errLaterOnAnotherPath(url string) error {
	code, _, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		log.Println(errs)
	}
	if code > 399 {
		err := fmt.Errorf("status %d", code)
		return err
	}
	return nil
}

func closureSeesErr(url string) (err error) {
	defer func() {
		if err != nil {
			log.Println("failed:", err)
		}
	}()
	_, _, errs := fiber.Get(url).Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func namedResults(url string) (code int, body []byte, err error) {
	var errs []error
	code, body, errs = fiber.Get(url).Bytes()
	if len(errs) > 0 {
		err = errs[0]
	}
	return
}

func localErrors(url string) {
	errors := []string{"a"}
	a := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(a)
	req := a.Request()
	req.Header.SetMethod(fiber.MethodGet)
	req.SetRequestURI(url)
	if err := a.Parse(); err != nil {
		log.Println("bad url", err)
		return
	}
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		log.Println(errors, errs)
	}
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	errorspkg "errors"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

func respInUse(url string) error {
	resp := "unused"
	fmt.Println(resp)
	resp2, err := client.Get(url)
	if err != nil {
		return err
	}
	code, body := resp2.StatusCode(), resp2.Body()
	fmt.Println(resp, code, len(body))
	return nil
}

func errLaterOnAnotherPath(url string) error {
	resp, err := client.Get(url)
	var code int
	if err == nil {
		code = resp.StatusCode()
	}
	if err != nil {
		log.Println(err)
	}
	if code > 399 {
		err := fmt.Errorf("status %d", code)
		return err
	}
	return nil
}

func closureSeesErr(url string) (err error) {
	defer func() {
		if err != nil {
			log.Println("failed:", err)
		}
	}()
	_, clientErr := client.Get(url)
	if clientErr != nil {
		return clientErr
	}
	return nil
}

func namedResults(url string) (code int, body []byte, err error) {
	var errs []error
	resp, clientErr := client.Get(url)
	code = 0
	body = nil
	errs = nil
	if clientErr == nil {
		code = resp.StatusCode()
		body = resp.Body()
	}
	if clientErr != nil {
		errs = []error{clientErr}
	}
	if len(errs) > 0 {
		err = errs[0]
	}
	return
}

func localErrors(url string) {
	errors := []string{"a"}
	a := client.C().R()
	a.SetMethod(fiber.MethodGet)
	a.SetURL(url)
	_, err := a.Send()
	if errorspkg.Is(err, client.ErrURLFormat) {
		log.Println("bad url", err)
		return
	}
	if err != nil {
		log.Println(errors, err)
	}
}
`,
			},
		},
		{
			name: "name_shadowed_package",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

type getter struct{}

func (getter) Get(u string) string { return u }

func shadow(url string) string {
	fiber := getter{}
	return fiber.Get(url)
}

func request(url string) ([]byte, error) {
	_, body, errs := fiber.Get(url).Bytes()
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
	"github.com/gofiber/fiber/v3/client"
)

type getter struct{}

func (getter) Get(u string) string { return u }

func shadow(url string) string {
	fiber := getter{}
	return fiber.Get(url)
}

func request(url string) ([]byte, error) {
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
	})
}
