package v3_test

import (
	"testing"
)

func Test_MigrateClientUsage_Forms(t *testing.T) {
	t.Parallel()

	clientRunCases(t, []clientCase{
		{
			name: "forms_left_alone",
			files: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

// The files are only written to the body by MultipartForm.
func notSent(api string) error {
	a := fiber.Post(api)
	a.SendFile("./go.mod", "f")
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// A multipart form without files is multipart in v2, url-encoded in v3.
func fieldsOnly(api string) error {
	args := fiber.AcquireArgs()
	args.Set("k", "v")
	a := fiber.Post(api)
	a.MultipartForm(args)
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// A FormFile that two requests send.
func shared(api string, data []byte) error {
	ff := fiber.AcquireFormFile()
	ff.Fieldname = "f"
	ff.Name = "n"
	ff.Content = data
	a := fiber.Post(api)
	a.FileData(ff)
	a.MultipartForm(nil)
	if _, _, errs := a.Bytes(); len(errs) > 0 {
		return errs[0]
	}
	b := fiber.Post(api)
	b.FileData(ff)
	b.MultipartForm(nil)
	if _, _, errs := b.Bytes(); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func formFileResult() *fiber.FormFile {
	return fiber.AcquireFormFile()
}
`,
			},
			want: map[string]string{
				"main.go": `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// The files are only written to the body by MultipartForm.
func notSent(api string) error {
	a := fiber.Post(api)
	a.SendFile("./go.mod", "f")
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// A multipart form without files is multipart in v2, url-encoded in v3.
func fieldsOnly(api string) error {
	args := fasthttp.AcquireArgs()
	args.Set("k", "v")
	a := fiber.Post(api)
	a.MultipartForm(args)
	_, _, errs := a.Bytes()
	if len(errs) > 0 {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// A FormFile that two requests send.
func shared(api string, data []byte) error {
	ff := fiber.AcquireFormFile()
	ff.Fieldname = "f"
	ff.Name = "n"
	ff.Content = data
	a := fiber.Post(api)
	a.FileData(ff)
	a.MultipartForm(nil)
	if _, _, errs := a.Bytes(); len(errs) > 0 {
		return errs[0]
	}
	b := fiber.Post(api)
	b.FileData(ff)
	b.MultipartForm(nil)
	if _, _, errs := b.Bytes(); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func formFileResult() *fiber.FormFile {
	return fiber.AcquireFormFile()
}
`,
			},
			notes: []string{
				"main.go:11: left as Fiber v2 code: FileData()/SendFile() without a MultipartForm() call after them: v2 only sends the files from MultipartForm()",
				"main.go:24: left as Fiber v2 code: MultipartForm() without a file: the v3 client sends a form without files url-encoded, never as multipart",
				"main.go:39: left as Fiber v2 code: FileData() with a FormFile that more than one call sends (a v3 file can be read once)",
				"main.go:45: left as Fiber v2 code: FileData() with a FormFile that more than one call sends (a v3 file can be read once)",
				"main.go:35: `fiber.AcquireFormFile` does not exist in Fiber v3 and was not migrated; use the client package (client.Get, client.Post, client.New().R(), ...)",
				"main.go:54: `fiber.FormFile` does not exist in Fiber v3 and was not migrated; use the client package (client.Get, client.Post, client.New().R(), ...)",
				"main.go:55: `fiber.AcquireFormFile` does not exist in Fiber v3 and was not migrated; use the client package (client.Get, client.Post, client.New().R(), ...)",
			},
		},
		{
			name: "forms_multipart",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func upload(api, session string, img []byte) error {
	agent := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(agent)

	req := agent.Request()
	req.Header.SetMethod(fiber.MethodPost)
	req.SetRequestURI(api)

	args := fiber.AcquireArgs()
	args.Set("session_id", session)
	agent.Boundary("xyz-boundary")
	agent.FileData(&fiber.FormFile{Fieldname: "image", Name: "frame.png", Content: img})
	agent.MultipartForm(args)

	if err := agent.Parse(); err != nil {
		return err
	}
	code, _, errs := agent.Bytes()
	if len(errs) > 0 {
		return errs[0]
	}
	_ = code
	return nil
}

func uploadVar(api string, content []byte) ([]byte, error) {
	ff := fiber.AcquireFormFile()
	defer fiber.ReleaseFormFile(ff)
	ff.Fieldname = "doc"
	ff.Name = "doc.txt"
	ff.Content = content

	a := fiber.Post(api)
	a.FileData(ff)
	a.MultipartForm(nil)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func sendDisk(api string) ([]byte, error) {
	a := fiber.Post(api)
	a.SendFile("./go.mod", "manifest")
	a.SendFile("./main.go")
	a.MultipartForm(nil)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func sendMany(api string) ([]byte, error) {
	a := fiber.Post(api)
	a.SendFiles("./go.mod", "one", "./main.go")
	args := fiber.AcquireArgs()
	args.Set("k", "v")
	a.MultipartForm(args)
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
	"bytes"
	"io"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
	"github.com/valyala/fasthttp"
)

func upload(api, session string, img []byte) error {
	agent := client.C().R()

	agent.SetMethod(fiber.MethodPost)
	agent.SetURL(api)

	args := fasthttp.AcquireArgs()
	args.Set("session_id", session)
	agent.SetBoundary("xyz-boundary")
	agent.AddFiles(client.AcquireFile(client.SetFileFieldName("image"), client.SetFileName("frame.png"), client.SetFileReader(io.NopCloser(bytes.NewReader(img)))))
	args.VisitAll(func(key, value []byte) {
		agent.AddFormData(string(key), string(value))
	})

	resp, err := agent.Send()
	if err != nil {
		return err
	}
	code := resp.StatusCode()
	_ = code
	return nil
}

func uploadVar(api string, content []byte) ([]byte, error) {
	ff := client.AcquireFile()
	ff.SetFieldName("doc")
	ff.SetName("doc.txt")
	ff.SetReader(io.NopCloser(bytes.NewReader(content)))

	a := client.C().R().SetMethod("POST").SetURL(api)
	a.AddFiles(ff)
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func sendDisk(api string) ([]byte, error) {
	a := client.C().R().SetMethod("POST").SetURL(api)
	a.AddFiles(client.AcquireFile(client.SetFilePath("./go.mod"), client.SetFileFieldName("manifest")))
	a.AddFile("./main.go")
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func sendMany(api string) ([]byte, error) {
	a := client.C().R().SetMethod("POST").SetURL(api)
	a.AddFiles(client.AcquireFile(client.SetFilePath("./go.mod"), client.SetFileFieldName("one")), client.AcquireFile(client.SetFilePath("./main.go")))
	args := fasthttp.AcquireArgs()
	args.Set("k", "v")
	args.VisitAll(func(key, value []byte) {
		a.AddFormData(string(key), string(value))
	})
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
			name: "forms_urlencoded",
			files: map[string]string{
				"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
)

func login(url, user, hash string) ([]byte, error) {
	agent := fiber.AcquireAgent()
	defer fiber.ReleaseAgent(agent)
	req := agent.Request()
	req.Header.SetMethod(fiber.MethodPost)
	req.SetRequestURI(url)
	args := fiber.AcquireArgs()
	defer fiber.ReleaseArgs(args)
	args.Set("a", user)
	args.Add("b", hash)
	agent.Form(args)
	if err := agent.Parse(); err != nil {
		return nil, err
	}
	_, body, errs := agent.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func fromParam(url string, args *fiber.Args) ([]byte, error) {
	a := fiber.Post(url)
	a.Form(args)
	_, body, errs := a.Bytes()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return body, nil
}

func chained(url string, args *fiber.Args) (int, error) {
	code, _, errs := fiber.Post(url).Form(args).Bytes()
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
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
	"github.com/valyala/fasthttp"
)

func login(url, user, hash string) ([]byte, error) {
	agent := client.C().R()
	agent.SetMethod(fiber.MethodPost)
	agent.SetURL(url)
	args := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(args)
	args.Set("a", user)
	args.Add("b", hash)
	args.VisitAll(func(key, value []byte) {
		agent.AddFormData(string(key), string(value))
	})
	resp, err := agent.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func fromParam(url string, args *fasthttp.Args) ([]byte, error) {
	a := client.C().R().SetMethod("POST").SetURL(url)
	if args != nil {
		args.VisitAll(func(key, value []byte) {
			a.AddFormData(string(key), string(value))
		})
	}
	resp, err := a.Send()
	if err != nil {
		return nil, err
	}
	body := resp.Body()
	return body, nil
}

func chained(url string, args *fasthttp.Args) (int, error) {
	req := client.C().R().SetMethod("POST").SetURL(url)
	if args != nil {
		args.VisitAll(func(key, value []byte) {
			req.AddFormData(string(key), string(value))
		})
	}
	resp, err := req.Send()
	if err != nil {
		return 0, err
	}
	code := resp.StatusCode()
	return code, nil
}
`,
			},
		},
	})
}
