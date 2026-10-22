package agentshim

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

// seen is what the server received.
type seen struct {
	header   http.Header
	form     url.Values
	files    map[string]string
	method   string
	uri      string
	host     string
	userinfo string
	body     []byte
}

// recorder is a test server that remembers its last request.
type recorder struct {
	srv  *httptest.Server
	last seen
	mu   sync.Mutex
	hits atomic.Int32
}

// newRecorder starts a server that answers every request with the status, the
// headers and the body given and remembers what it received. When handler is
// not nil it answers instead.
func newRecorder(t *testing.T, handler http.HandlerFunc) *recorder {
	t.Helper()

	rec := &recorder{}
	rec.srv = httptest.NewServer(rec.wrap(t, handler))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (rec *recorder) wrap(t *testing.T, handler http.HandlerFunc) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		rec.hits.Add(1)

		got := seen{
			header: r.Header.Clone(),
			method: r.Method,
			uri:    r.RequestURI,
			host:   r.Host,
			files:  map[string]string{},
		}
		if user, pass, ok := r.BasicAuth(); ok {
			got.userinfo = user + ":" + pass
		}
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && mt == "multipart/form-data" {
			assert.NoError(t, r.ParseMultipartForm(1<<20))
			got.form = r.MultipartForm.Value
			for field, headers := range r.MultipartForm.File {
				for _, fh := range headers {
					f, err := fh.Open()
					assert.NoError(t, err)
					content, err := io.ReadAll(f)
					assert.NoError(t, err)
					assert.NoError(t, f.Close())
					got.files[field+"|"+fh.Filename] = string(content)
				}
			}
		} else {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			got.body = body
			if mt == "application/x-www-form-urlencoded" {
				form, err := url.ParseQuery(string(body))
				assert.NoError(t, err)
				got.form = form
			}
		}

		rec.mu.Lock()
		rec.last = got
		rec.mu.Unlock()

		if handler != nil {
			handler(w, r)
			return
		}
		w.Header().Set("X-Reply", "yes")
		w.WriteHeader(http.StatusAccepted)
		_, err := io.WriteString(w, "pong")
		assert.NoError(t, err)
	}
}

// request returns what the server received last.
func (rec *recorder) request() seen {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.last
}

func Test_Agent_Methods(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	for method, newAgent := range map[string]func(string) *Agent{
		http.MethodGet:    Get,
		http.MethodHead:   Head,
		http.MethodPost:   Post,
		http.MethodPut:    Put,
		http.MethodPatch:  Patch,
		http.MethodDelete: Delete,
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			code, body, errs := newAgent(rec.srv.URL + "/path?x=1&y=2").Bytes()
			require.Empty(t, errs)
			assert.Equal(t, http.StatusAccepted, code)
			if method == http.MethodHead {
				assert.Empty(t, body)
			} else {
				assert.Equal(t, "pong", string(body))
			}
		})
	}

	t.Run("request line", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		code, body, errs := Get(rec.srv.URL + "/path?x=1&y=2").String()
		require.Empty(t, errs)
		assert.Equal(t, http.StatusAccepted, code)
		assert.Equal(t, "pong", body)

		got := rec.request()
		assert.Equal(t, http.MethodGet, got.method)
		assert.Equal(t, "/path?x=1&y=2", got.uri)
		assert.Equal(t, strings.TrimPrefix(rec.srv.URL, "http://"), got.host)
		assert.Equal(t, "fiber", got.header.Get("User-Agent"))
	})
}

func Test_Agent_Client(t *testing.T) {
	t.Parallel()

	t.Run("methods", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		c := &Client{}
		for method, newAgent := range map[string]func(string) *Agent{
			http.MethodGet:    c.Get,
			http.MethodHead:   c.Head,
			http.MethodPost:   c.Post,
			http.MethodPut:    c.Put,
			http.MethodPatch:  c.Patch,
			http.MethodDelete: c.Delete,
		} {
			_, _, errs := newAgent(rec.srv.URL).Bytes()
			require.Empty(t, errs)
			assert.Equal(t, method, rec.request().method)
		}
	})

	t.Run("user agent", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		c := &Client{UserAgent: "svc/1.0"}
		_, _, errs := c.Get(rec.srv.URL).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "svc/1.0", rec.request().header.Get("User-Agent"))
	})

	t.Run("no default user agent", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		c := &Client{NoDefaultUserAgentHeader: true}
		_, _, errs := c.Get(rec.srv.URL).Bytes()
		require.Empty(t, errs)
		assert.NotContains(t, rec.request().header, "User-Agent")
	})

	t.Run("json codecs", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
			_, err := io.WriteString(w, `{"name":"x"}`)
			assert.NoError(t, err)
		})
		var encoded, decoded atomic.Bool
		c := &Client{
			JSONEncoder: func(v any) ([]byte, error) { encoded.Store(true); return json.Marshal(v) },
			JSONDecoder: func(data []byte, v any) error { decoded.Store(true); return json.Unmarshal(data, v) },
		}

		var out struct{ Name string }
		_, _, errs := c.Post(rec.srv.URL).JSON(map[string]int{"a": 1}).Struct(&out)
		require.Empty(t, errs)
		assert.Equal(t, "x", out.Name)
		assert.True(t, encoded.Load())
		assert.True(t, decoded.Load())
		assert.JSONEq(t, `{"a":1}`, string(rec.request().body))
	})

	t.Run("pool", func(t *testing.T) {
		t.Parallel()

		c := AcquireClient()
		c.UserAgent = "x"
		c.NoDefaultUserAgentHeader = true
		c.JSONEncoder = json.Marshal
		c.JSONDecoder = json.Unmarshal
		ReleaseClient(c)

		assert.Empty(t, c.UserAgent)
		assert.False(t, c.NoDefaultUserAgentHeader)
		assert.Nil(t, c.JSONEncoder)
		assert.Nil(t, c.JSONDecoder)

		again := AcquireClient()
		assert.NotNil(t, again)
	})
}

func Test_Agent_Headers(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	_, _, errs := Get(rec.srv.URL).
		Set("X-Set", "1").
		SetBytesK([]byte("X-Set-K"), "2").
		SetBytesV("X-Set-V", []byte("3")).
		SetBytesKV([]byte("X-Set-KV"), []byte("4")).
		Add("X-Add", "a").
		Add("X-Add", "b").
		AddBytesK([]byte("X-Add-K"), "c").
		AddBytesV("X-Add-V", []byte("d")).
		AddBytesKV([]byte("X-Add-KV"), []byte("e")).
		ConnectionClose().
		UserAgent("svc/2.0").
		Referer("https://referer.example/").
		ContentType("text/plain").
		Bytes()
	require.Empty(t, errs)

	got := rec.request()
	assert.Equal(t, "1", got.header.Get("X-Set"))
	assert.Equal(t, "2", got.header.Get("X-Set-K"))
	assert.Equal(t, "3", got.header.Get("X-Set-V"))
	assert.Equal(t, "4", got.header.Get("X-Set-KV"))
	assert.Equal(t, []string{"a", "b"}, got.header.Values("X-Add"))
	assert.Equal(t, "c", got.header.Get("X-Add-K"))
	assert.Equal(t, "d", got.header.Get("X-Add-V"))
	assert.Equal(t, "e", got.header.Get("X-Add-KV"))
	assert.Equal(t, []string{"close"}, got.header.Values("Connection"))
	assert.Equal(t, "svc/2.0", got.header.Get("User-Agent"))
	assert.Equal(t, "https://referer.example/", got.header.Get("Referer"))
	assert.Equal(t, "text/plain", got.header.Get("Content-Type"))

	_, _, errs = Get(rec.srv.URL).
		UserAgentBytes([]byte("bytes/1.0")).
		RefererBytes([]byte("https://bytes.example/")).
		ContentTypeBytes([]byte("text/csv")).
		Bytes()
	require.Empty(t, errs)

	got = rec.request()
	assert.Equal(t, "bytes/1.0", got.header.Get("User-Agent"))
	assert.Equal(t, "https://bytes.example/", got.header.Get("Referer"))
	assert.Equal(t, "text/csv", got.header.Get("Content-Type"))
}

func Test_Agent_Cookies(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	_, _, errs := Get(rec.srv.URL).
		Cookie("a", "1").
		CookieBytesK([]byte("b"), "2").
		CookieBytesKV([]byte("c"), []byte("3")).
		Cookies("d", "4", "e", "5", "dangling").
		CookiesBytesKV([]byte("f"), []byte("6"), []byte("g"), []byte("7"), []byte("dangling")).
		Bytes()
	require.Empty(t, errs)

	cookies := map[string]string{}
	for _, header := range rec.request().header.Values("Cookie") {
		for part := range strings.SplitSeq(header, "; ") {
			name, value, _ := strings.Cut(part, "=")
			cookies[name] = value
		}
	}
	assert.Equal(t, map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5", "f": "6", "g": "7"}, cookies)
}

func Test_Agent_BasicAuth(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	_, _, errs := Get(rec.srv.URL).BasicAuth("user", "pass").Bytes()
	require.Empty(t, errs)
	assert.Equal(t, "user:pass", rec.request().userinfo)
	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")), rec.request().header.Get("Authorization"))

	_, _, errs = Get(rec.srv.URL).BasicAuthBytes([]byte("bytes"), []byte("secret")).Bytes()
	require.Empty(t, errs)
	assert.Equal(t, "bytes:secret", rec.request().userinfo)
}

func Test_Agent_Query(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	// The query string replaces the one of the URL and is sent as it is.
	_, _, errs := Get(rec.srv.URL + "/p?old=1").QueryString("a=1&b=two+words").Bytes()
	require.Empty(t, errs)
	assert.Equal(t, "/p?a=1&b=two+words", rec.request().uri)

	_, _, errs = Get(rec.srv.URL + "/p?old=1").QueryStringBytes([]byte("c=3")).Bytes()
	require.Empty(t, errs)
	assert.Equal(t, "/p?c=3", rec.request().uri)
}

func Test_Agent_Host(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	// The request goes to the server of the URL, the Host header is the one given.
	code, _, errs := Get(rec.srv.URL + "/virtual").Host("example.com").Bytes()
	require.Empty(t, errs)
	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "example.com", rec.request().host)
	assert.Equal(t, "/virtual", rec.request().uri)

	_, _, errs = Get(rec.srv.URL).HostBytes([]byte("bytes.example.com:8080")).Bytes()
	require.Empty(t, errs)
	assert.Equal(t, "bytes.example.com:8080", rec.request().host)

	// The same through the request.
	a := Get(rec.srv.URL)
	a.Request().URI().SetHost("request.example.com")
	_, _, errs = a.Bytes()
	require.Empty(t, errs)
	assert.Equal(t, "request.example.com", rec.request().host)
}

func Test_Agent_Bodies(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name string `json:"name" xml:"name"`
	}

	t.Run("body", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).Body([]byte("raw")).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "raw", string(rec.request().body))

		_, _, errs = Post(rec.srv.URL).BodyString("text").Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "text", string(rec.request().body))

		_, _, errs = Post(rec.srv.URL).BodyStream(strings.NewReader("streamed"), len("streamed")).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "streamed", string(rec.request().body))
	})

	t.Run("json", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).JSON(payload{Name: "n"}).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "application/json", rec.request().header.Get("Content-Type"))
		assert.JSONEq(t, `{"name":"n"}`, string(rec.request().body))
	})

	t.Run("json with a content type", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).JSON(payload{Name: "n"}, "application/vnd.api+json").Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "application/vnd.api+json", rec.request().header.Get("Content-Type"))
		assert.JSONEq(t, `{"name":"n"}`, string(rec.request().body))
	})

	t.Run("json encoder", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).
			JSONEncoder(func(any) ([]byte, error) { return []byte(`{"custom":true}`), nil }).
			JSON(payload{Name: "ignored"}).
			Bytes()
		require.Empty(t, errs)
		assert.JSONEq(t, `{"custom":true}`, string(rec.request().body))
	})

	t.Run("json error", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		code, body, errs := Post(rec.srv.URL).JSON(make(chan int)).Bytes()
		require.Len(t, errs, 1)
		var unsupported *json.UnsupportedTypeError
		require.ErrorAs(t, errs[0], &unsupported)
		assert.Zero(t, code)
		assert.Empty(t, body)
		assert.Zero(t, rec.hits.Load(), "a request with an error is not sent")
	})

	t.Run("xml", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).XML(payload{Name: "n"}).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "application/xml", rec.request().header.Get("Content-Type"))
		assert.Equal(t, `<payload><name>n</name></payload>`, string(rec.request().body))

		_, _, errs = Post(rec.srv.URL).XML(make(chan int)).Bytes()
		require.Len(t, errs, 1)
	})

	t.Run("form", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		args := fasthttp.AcquireArgs()
		defer fasthttp.ReleaseArgs(args)
		args.Set("a", "1")
		args.Add("b", "two words")

		_, _, errs := Post(rec.srv.URL).Form(args).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "application/x-www-form-urlencoded", rec.request().header.Get("Content-Type"))
		assert.Equal(t, "a=1&b=two+words", string(rec.request().body))
		assert.Equal(t, url.Values{"a": {"1"}, "b": {"two words"}}, rec.request().form)

		// A nil form sets the content type only.
		_, _, errs = Post(rec.srv.URL).Form(nil).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "application/x-www-form-urlencoded", rec.request().header.Get("Content-Type"))
		assert.Empty(t, rec.request().body)
	})
}

func Test_Agent_Multipart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.txt")
	require.NoError(t, os.WriteFile(first, []byte("one"), 0o600))
	require.NoError(t, os.WriteFile(second, []byte("two"), 0o600))

	args := func() *fasthttp.Args {
		a := &fasthttp.Args{}
		a.Set("k", "v")
		return a
	}

	t.Run("fields and files", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		ff := AcquireFormFile()
		ff.Fieldname, ff.Name, ff.Content = "doc", "doc.txt", []byte("content")
		defer ReleaseFormFile(ff)

		_, _, errs := Post(rec.srv.URL).FileData(ff).SendFile(first, "up").SendFile(second).MultipartForm(args()).Bytes()
		require.Empty(t, errs)

		got := rec.request()
		assert.Equal(t, url.Values{"k": {"v"}}, got.form)
		assert.Equal(t, map[string]string{
			"doc|doc.txt":      "content",
			"up|first.txt":     "one",
			"file3|second.txt": "two",
		}, got.files)
	})

	t.Run("send files", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).SendFiles(first, "a", second).MultipartForm(nil).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, map[string]string{"a|first.txt": "one", "file2|second.txt": "two"}, rec.request().files)
	})

	t.Run("fields only", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).MultipartForm(args()).Bytes()
		require.Empty(t, errs)

		mediaType, params, err := mime.ParseMediaType(rec.request().header.Get("Content-Type"))
		require.NoError(t, err)
		assert.Equal(t, "multipart/form-data", mediaType)
		assert.NotEmpty(t, params["boundary"])
		assert.Equal(t, url.Values{"k": {"v"}}, rec.request().form)
		assert.Empty(t, rec.request().files)
	})

	t.Run("boundary", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).Boundary("myboundary").MultipartForm(args()).Bytes()
		require.Empty(t, errs)

		_, params, err := mime.ParseMediaType(rec.request().header.Get("Content-Type"))
		require.NoError(t, err)
		assert.Equal(t, "myboundary", params["boundary"])

		_, _, errs = Post(rec.srv.URL).Boundary("bad boundary!").MultipartForm(args()).Bytes()
		require.Len(t, errs, 1, "an invalid boundary is an error of the request")
	})

	t.Run("files without a multipart form are not sent", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).SendFile(first, "up").Bytes()
		require.Empty(t, errs)

		got := rec.request()
		assert.Empty(t, got.files)
		assert.Empty(t, got.body)
		assert.Empty(t, got.header.Get("Content-Type"))
	})

	t.Run("a file that cannot be read", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		_, _, errs := Post(rec.srv.URL).SendFile(filepath.Join(dir, "missing.txt")).MultipartForm(nil).Bytes()
		require.Len(t, errs, 1)
		require.ErrorIs(t, errs[0], os.ErrNotExist)
		assert.Zero(t, rec.hits.Load())
	})

	t.Run("a form file can be sent again", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		ff := &FormFile{Fieldname: "f", Name: "n.txt", Content: []byte("shared")}
		for range 2 {
			_, _, errs := Post(rec.srv.URL).FileData(ff).MultipartForm(nil).Bytes()
			require.Empty(t, errs)
			assert.Equal(t, map[string]string{"f|n.txt": "shared"}, rec.request().files)
		}
	})
}

func Test_Agent_Response(t *testing.T) {
	t.Parallel()

	t.Run("set response", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		resp := fasthttp.AcquireResponse()
		defer fasthttp.ReleaseResponse(resp)

		code, body, errs := Get(rec.srv.URL).SetResponse(resp).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, http.StatusAccepted, code)
		assert.Equal(t, "pong", string(body))
		assert.Equal(t, http.StatusAccepted, resp.StatusCode())
		assert.Equal(t, "yes", string(resp.Header.Peek("X-Reply")))
		assert.Equal(t, "pong", string(resp.Body()))
	})

	t.Run("dest", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		dest := make([]byte, 0, 64)
		_, body, errs := Get(rec.srv.URL).Dest(dest).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "pong", string(body))
		assert.Same(t, &dest[:1][0], &body[0], "the body is appended to dest")

		_, body, errs = Get(rec.srv.URL).Dest([]byte("prefix:")).Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "prefix:pong", string(body))
	})

	t.Run("struct", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
			_, err := io.WriteString(w, `{"name":"n","count":2}`)
			assert.NoError(t, err)
		})
		var out struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		code, body, errs := Get(rec.srv.URL).Struct(&out)
		require.Empty(t, errs)
		assert.Equal(t, http.StatusOK, code)
		assert.JSONEq(t, `{"name":"n","count":2}`, string(body))
		assert.Equal(t, "n", out.Name)
		assert.Equal(t, 2, out.Count)

		decoded := false
		_, _, errs = Get(rec.srv.URL).
			JSONDecoder(func(data []byte, v any) error { decoded = true; return json.Unmarshal(data, v) }).
			Struct(&out)
		require.Empty(t, errs)
		assert.True(t, decoded)
	})

	t.Run("struct with a body that is not json", func(t *testing.T) {
		t.Parallel()

		rec := newRecorder(t, nil)
		var out map[string]any
		code, body, errs := Get(rec.srv.URL).Struct(&out)
		require.Len(t, errs, 1)
		var syntax *json.SyntaxError
		require.ErrorAs(t, errs[0], &syntax)
		assert.Equal(t, http.StatusAccepted, code)
		assert.Equal(t, "pong", string(body))
	})

	t.Run("string and bytes of a failed request", func(t *testing.T) {
		t.Parallel()

		code, body, errs := Get("http://127.0.0.1:1").Bytes()
		require.Len(t, errs, 1)
		assert.Zero(t, code)
		assert.Empty(t, body)

		code, str, errs := Get("http://127.0.0.1:1").String()
		require.Len(t, errs, 1)
		assert.Zero(t, code)
		assert.Empty(t, str)

		var out map[string]any
		code, body, errs = Get("http://127.0.0.1:1").Struct(&out)
		require.Len(t, errs, 1, "there is nothing to decode")
		assert.Zero(t, code)
		assert.Empty(t, body)
		assert.Nil(t, out)
	})
}

func Test_Agent_Parse(t *testing.T) {
	t.Parallel()

	t.Run("unsupported protocol", func(t *testing.T) {
		t.Parallel()

		a := Get("ftp://example.com/file")
		_, _, errs := a.Bytes()
		require.Len(t, errs, 1)
		assert.EqualError(t, errs[0], `unsupported protocol "ftp". http and https are supported`)
	})

	t.Run("a second parse changes nothing", func(t *testing.T) {
		t.Parallel()

		a := AcquireAgent()
		defer ReleaseAgent(a)
		a.Request().Header.SetMethod(http.MethodGet)
		a.Request().SetRequestURI("http://example.com:8080/")

		require.NoError(t, a.Parse())
		host := a.HostClient
		require.NotNil(t, host)
		assert.Equal(t, "example.com:8080", host.Addr)
		assert.Equal(t, "fiber", host.Name)
		assert.False(t, host.IsTLS)

		a.Request().SetRequestURI("ftp://example.com/")
		require.NoError(t, a.Parse())
		assert.Same(t, host, a.HostClient)
	})

	t.Run("the name of the agent is the user agent", func(t *testing.T) {
		t.Parallel()

		a := AcquireAgent()
		defer ReleaseAgent(a)
		a.Name = "named"
		a.Request().SetRequestURI("https://example.com/")

		require.NoError(t, a.Parse())
		assert.Equal(t, "named", a.HostClient.Name)
		assert.True(t, a.IsTLS)
		assert.Equal(t, "example.com:443", a.Addr)
	})
}

func Test_Agent_Retry(t *testing.T) {
	t.Parallel()

	// flaky closes the connection on the first request, as a server that went
	// away would.
	flaky := func(t *testing.T) (*httptest.Server, *atomic.Int32) {
		t.Helper()

		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if hits.Add(1) == 1 {
				hijacker, ok := w.(http.Hijacker)
				if !assert.True(t, ok) {
					return
				}
				conn, _, err := hijacker.Hijack()
				assert.NoError(t, err)
				assert.NoError(t, conn.Close())
				return
			}
			_, err := io.WriteString(w, "second")
			assert.NoError(t, err)
		}))
		t.Cleanup(srv.Close)
		return srv, &hits
	}

	t.Run("a post is not retried by default", func(t *testing.T) {
		t.Parallel()

		srv, hits := flaky(t)
		_, _, errs := Post(srv.URL).Bytes()
		require.Len(t, errs, 1)
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("retry if", func(t *testing.T) {
		t.Parallel()

		srv, hits := flaky(t)
		var asked atomic.Int32
		code, body, errs := Post(srv.URL).
			RetryIf(func(*fasthttp.Request) bool { asked.Add(1); return true }).
			Bytes()
		require.Empty(t, errs)
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "second", string(body))
		assert.EqualValues(t, 2, hits.Load())
		assert.EqualValues(t, 1, asked.Load())
	})

	t.Run("retry if says no", func(t *testing.T) {
		t.Parallel()

		srv, hits := flaky(t)
		_, _, errs := Get(srv.URL).
			RetryIf(func(*fasthttp.Request) bool { return false }).
			Bytes()
		require.Len(t, errs, 1)
		assert.EqualValues(t, 1, hits.Load(), "a get is retried by default, RetryIf replaces the default")
	})

	t.Run("retry if gets the request", func(t *testing.T) {
		t.Parallel()

		srv, hits := flaky(t)
		var method, uri string
		_, _, errs := Post(srv.URL + "/path?x=1").
			RetryIf(func(req *fasthttp.Request) bool {
				method, uri = string(req.Header.Method()), string(req.URI().PathOriginal())
				return true
			}).
			Bytes()
		require.Empty(t, errs)
		assert.EqualValues(t, 2, hits.Load())
		assert.Equal(t, http.MethodPost, method)
		assert.Equal(t, "/path", uri)
	})

	t.Run("no function brings back the default", func(t *testing.T) {
		t.Parallel()

		srv, hits := flaky(t)
		_, body, errs := Get(srv.URL).
			RetryIf(func(*fasthttp.Request) bool { return false }).
			RetryIf(nil).
			Bytes()
		require.Empty(t, errs)
		assert.Equal(t, "second", string(body))
		assert.EqualValues(t, 2, hits.Load(), "a get is retried by default")
	})
}

func Test_Agent_Timeout(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(func() {
		close(done)
		srv.Close()
	})

	start := time.Now()
	_, _, errs := Get(srv.URL).Timeout(50 * time.Millisecond).Bytes()
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], fasthttp.ErrTimeout)
	assert.Less(t, time.Since(start), 3*time.Second)
}

func Test_Agent_Redirects(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		case "/b":
			http.Redirect(w, r, "/c", http.StatusFound)
		case "/c":
			_, err := io.WriteString(w, "landed on c")
			assert.NoError(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	code, body, errs := Get(srv.URL + "/a").Bytes()
	require.Empty(t, errs)
	assert.Equal(t, http.StatusFound, code, "redirects are not followed by default")
	assert.Contains(t, string(body), "/b")

	code, body, errs = Get(srv.URL + "/a").MaxRedirectsCount(5).Bytes()
	require.Empty(t, errs)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "landed on c", string(body))

	_, _, errs = Get(srv.URL + "/a").MaxRedirectsCount(1).Bytes()
	require.Len(t, errs, 1, "too many redirects")

	// MaxRedirectsCount only applies to GET and HEAD.
	code, _, errs = Post(srv.URL + "/a").MaxRedirectsCount(5).Bytes()
	require.Empty(t, errs)
	assert.Equal(t, http.StatusFound, code)
}

func Test_Agent_TLS(t *testing.T) {
	t.Parallel()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(w, "secure")
		assert.NoError(t, err)
	}))
	// The handshakes of the clients that do not trust the certificate fail.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	_, _, errs := Get(srv.URL).Bytes()
	require.Len(t, errs, 1, "the certificate of the test server is not trusted")
	var unknown x509.UnknownAuthorityError
	require.ErrorAs(t, errs[0], &unknown)

	code, body, errs := Get(srv.URL).InsecureSkipVerify().Bytes()
	require.Empty(t, errs)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "secure", string(body))

	// A configuration that was set is kept and made insecure.
	a := Get(srv.URL).TLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	a.InsecureSkipVerify()
	assert.True(t, a.HostClient.TLSConfig.InsecureSkipVerify)
	assert.EqualValues(t, tls.VersionTLS12, a.HostClient.TLSConfig.MinVersion)
	_, body2, errs := a.Bytes()
	require.Empty(t, errs)
	assert.Equal(t, "secure", string(body2))

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	_, body, errs = Get(srv.URL).TLSConfig(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}).Bytes()
	require.Empty(t, errs)
	assert.Equal(t, "secure", string(body))
}

func Test_Agent_Debug(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)

	var buf bytes.Buffer
	_, _, errs := Post(rec.srv.URL + "/dump").BodyString("payload").Debug(&buf).Bytes()
	require.Empty(t, errs)

	dump := buf.String()
	host := strings.TrimPrefix(rec.srv.URL, "http://")
	assert.True(t, strings.HasPrefix(dump, "Connected to "+host+"("+host+")\r\n\r\n"), dump)
	assert.Contains(t, dump, "POST /dump HTTP/1.1\r\n")
	assert.Contains(t, dump, "payload")
	assert.Contains(t, dump, "HTTP/1.1 202 Accepted\r\n")
	assert.Contains(t, dump, "pong")

	// Without a writer the dump goes to the standard output.
	a := Get(rec.srv.URL).Debug()
	assert.Same(t, os.Stdout, a.debugWriter)
	assert.NotNil(t, a)
}

func Test_Agent_Reuse(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	a := Get(rec.srv.URL+"/reused").Reuse().Set("X-Once", "1")
	defer ReleaseAgent(a)

	for range 2 {
		code, body, errs := a.Bytes()
		require.Empty(t, errs)
		assert.Equal(t, http.StatusAccepted, code)
		assert.Equal(t, "pong", string(body))
		assert.Equal(t, "/reused", rec.request().uri)
		assert.Equal(t, "1", rec.request().header.Get("X-Once"))
	}
	assert.EqualValues(t, 2, rec.hits.Load())

	// The errors of a request do not stay for the next one.
	b := Post(rec.srv.URL).Reuse().JSON(make(chan int))
	defer ReleaseAgent(b)
	_, _, errs := b.Bytes()
	assert.Len(t, errs, 1)
	_, _, errs = b.Bytes()
	assert.Empty(t, errs)
}

func Test_Agent_Pools(t *testing.T) {
	t.Parallel()

	t.Run("agent", func(t *testing.T) {
		t.Parallel()

		a := AcquireAgent()
		a.Name = "x"
		a.NoDefaultUserAgentHeader = true
		a.Request().SetRequestURI("http://example.com/")
		a.Timeout(time.Second).MaxRedirectsCount(3).Boundary("b").Reuse().Debug()
		a.FileData(&FormFile{Fieldname: "f"})
		a.SetResponse(fasthttp.AcquireResponse()).Dest([]byte("d"))
		require.NoError(t, a.Parse())
		ReleaseAgent(a)

		assert.Empty(t, a.Name)
		assert.False(t, a.NoDefaultUserAgentHeader)
		assert.Nil(t, a.HostClient)
		assert.Empty(t, a.Request().URI().Host())
		assert.Nil(t, a.resp)
		assert.Nil(t, a.dest)
		assert.Zero(t, a.timeout)
		assert.Zero(t, a.maxRedirectsCount)
		assert.Empty(t, a.boundary)
		assert.False(t, a.reuse)
		assert.False(t, a.parsed)
		assert.Nil(t, a.debugWriter)
		assert.Empty(t, a.formFiles)
		assert.Empty(t, a.errs)
	})

	t.Run("form file", func(t *testing.T) {
		t.Parallel()

		ff := AcquireFormFile()
		ff.Fieldname, ff.Name, ff.Content = "f", "n", []byte("content")
		ff.autoRelease = true
		ReleaseFormFile(ff)

		assert.Empty(t, ff.Fieldname)
		assert.Empty(t, ff.Name)
		assert.Empty(t, ff.Content)
		assert.False(t, ff.autoRelease)
	})

	t.Run("files read by SendFile go back to the pool", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "f.txt")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

		a := AcquireAgent()
		a.SendFile(path)
		require.Len(t, a.formFiles, 1)
		ff := a.formFiles[0]
		assert.True(t, ff.autoRelease)
		assert.Equal(t, "file1", ff.Fieldname)
		ReleaseAgent(a)
		assert.Empty(t, ff.Name, "the file of SendFile is released with the agent")
	})
}

func Test_Agent_Concurrent(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			_, body, errs := Post(rec.srv.URL).BodyString(strings.Repeat("x", i)).Bytes()
			assert.Empty(t, errs)
			assert.Equal(t, "pong", string(body))
		})
	}
	wg.Wait()
	assert.EqualValues(t, 8, rec.hits.Load())
}

func Test_Agent_Request(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, nil)
	// An agent from the pool is filled through its request, as the code that
	// the migration leaves alone does.
	a := AcquireAgent()
	req := a.Request()
	req.Header.SetMethod(http.MethodPut)
	req.SetRequestURI(rec.srv.URL + "/put")
	req.SetBodyString("body")
	req.Header.Set("X-Direct", "1")
	require.NoError(t, a.Parse())

	code, _, errs := a.Bytes()
	require.Empty(t, errs)
	assert.Equal(t, http.StatusAccepted, code)
	got := rec.request()
	assert.Equal(t, http.MethodPut, got.method)
	assert.Equal(t, "/put", got.uri)
	assert.Equal(t, "body", string(got.body))
	assert.Equal(t, "1", got.header.Get("X-Direct"))
}

func Test_Agent_HostClient(t *testing.T) {
	t.Parallel()

	// The embedded fasthttp client is reachable, as it was in Fiber v2.
	a := Get("http://127.0.0.1:9/")
	a.MaxConns = 7
	assert.Equal(t, 7, a.MaxConns)

	var dialed atomic.Bool
	a.Dial = func(string) (net.Conn, error) {
		dialed.Store(true)
		return nil, errors.New("no dialing in this test")
	}
	_, _, errs := a.Bytes()
	require.Len(t, errs, 1)
	assert.True(t, dialed.Load())
}

// brokenWriter is a multipart writer whose calls fail the way it was told to.
type brokenWriter struct {
	boundary, field, file, write, closing error
}

func (b brokenWriter) Boundary() string { return "boundary" }

func (b brokenWriter) SetBoundary(string) error { return b.boundary }

func (b brokenWriter) CreateFormFile(string, string) (io.Writer, error) {
	if b.file != nil {
		return nil, b.file
	}
	return failingWriter{err: b.write}, nil
}

func (b brokenWriter) WriteField(string, string) error { return b.field }

func (b brokenWriter) Close() error { return b.closing }

// failingWriter is an io.Writer that fails with its error.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func Test_Agent_MultipartErrors(t *testing.T) {
	t.Parallel()

	errBoundary := errors.New("boundary")
	errField := errors.New("field")
	errFile := errors.New("file")
	errWrite := errors.New("write")
	errClose := errors.New("close")

	args := &fasthttp.Args{}
	args.Set("k", "v")

	tests := []struct {
		name string
		mw   brokenWriter
		want []error
	}{
		{name: "boundary", mw: brokenWriter{boundary: errBoundary}, want: []error{errBoundary}},
		{name: "field", mw: brokenWriter{field: errField}, want: []error{errField}},
		{name: "file", mw: brokenWriter{file: errFile}, want: []error{errFile}},
		{name: "content of the file", mw: brokenWriter{write: errWrite}, want: []error{errWrite}},
		{name: "close", mw: brokenWriter{closing: errClose}, want: []error{errClose}},
		{name: "all of them", mw: brokenWriter{field: errField, write: errWrite, closing: errClose}, want: []error{errField, errWrite, errClose}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newRecorder(t, nil)
			a := Post(rec.srv.URL)
			a.mw = tt.mw
			a.Boundary("custom")
			a.FileData(&FormFile{Fieldname: "f", Name: "n", Content: []byte("c")})
			a.MultipartForm(args)

			_, _, errs := a.Bytes()
			assert.Equal(t, tt.want, errs, "a request with an error is not sent")
			assert.Zero(t, rec.hits.Load())
		})
	}
}

func Test_Agent_StructOfAnAgentWithoutDecoder(t *testing.T) {
	t.Parallel()

	rec := newRecorder(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(w, `{"name":"n"}`)
		assert.NoError(t, err)
	})

	// An agent that no client made has no decoder.
	a := &Agent{req: &fasthttp.Request{}}
	a.Request().SetRequestURI(rec.srv.URL)
	require.NoError(t, a.Parse())

	var out struct {
		Name string `json:"name"`
	}
	_, _, errs := a.Struct(&out)
	require.Empty(t, errs)
	assert.Equal(t, "n", out.Name)
}
