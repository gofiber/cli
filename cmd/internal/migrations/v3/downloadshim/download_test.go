package downloadshim

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/fiber/v3"
)

// fixtures writes the files the tests send and returns their directory.
func fixtures(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for name, body := range map[string]string{
		"data.json":  `{"a":1}` + "\n",
		"plain.txt":  "plain text\n",
		"ranges.txt": strings.Repeat("0123456789", 20),
		"noext":      "%PDF-1.4 a file with no extension that is a PDF\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	return dir
}

// reply is what a request got.
type reply struct {
	header http.Header
	body   string
	status int
}

func send(t *testing.T, app *fiber.App, target string, headers ...string) reply {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	resp.Header.Del("Date")
	resp.Header.Del("Last-Modified")
	resp.Header.Del("Etag")
	return reply{status: resp.StatusCode, header: resp.Header, body: string(body)}
}

// handlers makes an app that serves a file with the shim on /shim/... and with Ctx.Download
// of Fiber v3 on /v3/..., each under the same name, after the setup of the handler.
func handlers(dir, name string, setup func(fiber.Ctx)) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}})
	names := func() []string {
		if name == "" {
			return nil
		}
		return []string{name}
	}
	app.Get("/shim/:file", func(c fiber.Ctx) error {
		if setup != nil {
			setup(c)
		}
		return Download(c, filepath.Join(dir, c.Params("file")), names()...)
	})
	app.Get("/v3/:file", func(c fiber.Ctx) error {
		if setup != nil {
			setup(c)
		}
		return c.Download(filepath.Join(dir, c.Params("file")), names()...)
	})
	return app
}

// Download is Ctx.Download of Fiber v3, with the header that announces the ranges.
func Test_Download_IsTheDownloadOfV3(t *testing.T) {
	t.Parallel()

	dir := fixtures(t)
	for _, name := range []string{"", "export.csv", "résumé final.pdf", "../etc/passwd", "report.JSON"} {
		app := handlers(dir, name, nil)
		for _, file := range []string{"data.json", "plain.txt", "ranges.txt", "noext", "missing.bin"} {
			got, want := send(t, app, "/shim/"+file), send(t, app, "/v3/"+file)

			assert.Equal(t, want.status, got.status, "%q %s", name, file)
			assert.Equal(t, want.body, got.body, "%q %s", name, file)
			if got.status == fiber.StatusOK {
				assert.Equal(t, "bytes", got.header.Get("Accept-Ranges"), "%q %s", name, file)
				got.header.Del("Accept-Ranges")
			}
			assert.Equal(t, want.header, got.header, "%q %s", name, file)
		}
	}
}

func Test_Download_ServesByteRanges(t *testing.T) {
	t.Parallel()

	dir := fixtures(t)
	app := handlers(dir, "numbers.txt", nil)

	part := send(t, app, "/shim/ranges.txt", "Range", "bytes=10-19")
	assert.Equal(t, fiber.StatusPartialContent, part.status)
	assert.Equal(t, "0123456789", part.body)
	assert.Equal(t, "bytes 10-19/200", part.header.Get("Content-Range"))
	assert.Contains(t, part.header.Get("Content-Disposition"), `filename="numbers.txt"`)

	tail := send(t, app, "/shim/ranges.txt", "Range", "bytes=-5")
	assert.Equal(t, fiber.StatusPartialContent, tail.status)
	assert.Equal(t, "56789", tail.body)

	past := send(t, app, "/shim/ranges.txt", "Range", "bytes=900-")
	assert.Equal(t, fiber.StatusRequestedRangeNotSatisfiable, past.status)

	// without the shim the whole file comes back
	whole := send(t, app, "/v3/ranges.txt", "Range", "bytes=10-19")
	assert.Equal(t, fiber.StatusOK, whole.status)
	assert.Len(t, whole.body, 200)
}

// The type of the response is the one of the file, found by SendFile, and never the one
// of the name that Attachment would give it.
func Test_Download_TypeIsTheOneOfTheFile(t *testing.T) {
	t.Parallel()

	dir := fixtures(t)
	for name, want := range map[string]string{
		"":           "application/json",
		"export.csv": "application/json",
		"export.pdf": "application/json",
		"export":     "application/json",
	} {
		reply := send(t, handlers(dir, name, nil), "/shim/data.json")
		assert.Equal(t, want, reply.header.Get("Content-Type"), "name %q", name)
	}

	// a file with no extension is a PDF to SendFile, which looks into it
	reply := send(t, handlers(dir, "x.json", nil), "/shim/noext")
	assert.Equal(t, "application/pdf", reply.header.Get("Content-Type"))
}

func Test_Download_KeepsTheTypeTheHandlerSet(t *testing.T) {
	t.Parallel()

	dir := fixtures(t)
	for _, setup := range []func(fiber.Ctx){
		func(c fiber.Ctx) { c.Type("xlsx") },
		func(c fiber.Ctx) { c.Set(fiber.HeaderContentType, "application/x-custom") },
		func(c fiber.Ctx) { c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8) },
	} {
		app := handlers(dir, "export.csv", setup)
		got, want := send(t, app, "/shim/plain.txt"), send(t, app, "/v3/plain.txt")

		assert.NotEmpty(t, want.header.Get("Content-Type"))
		assert.Equal(t, want.header.Get("Content-Type"), got.header.Get("Content-Type"))
	}
}

// A file that is not there leaves the response as Download of v3 does: the name is in the
// header, the type is the handler's.
func Test_Download_MissingFile(t *testing.T) {
	t.Parallel()

	dir := fixtures(t)
	app := fiber.New()
	app.Get("/shim", func(c fiber.Ctx) error {
		err := Download(c, filepath.Join(dir, "missing.json"), "export.json")
		assert.Contains(t, c.GetRespHeader(fiber.HeaderContentDisposition), `filename="export.json"`)
		assert.Equal(t, fiber.MIMETextPlainCharsetUTF8, c.GetRespHeader(fiber.HeaderContentType), "not the type of export.json")
		return err
	})

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/shim", http.NoBody))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}
