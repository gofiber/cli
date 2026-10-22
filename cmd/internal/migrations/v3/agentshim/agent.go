// Package agentshim is the source of agent.go of the package
// <module>/internal/fibermigrate, the file that "fiber migrate" adds to a module
// for the requests that the client package of Fiber v3 cannot make the way the
// Fiber v2 agent did (a Host that differs from the URL, SetResponse, RetryIf, ...).
//
// The package exists so that the code is built, vetted, linted and tested together
// with the CLI. The migration embeds agent.go and writes everything below the
// package clause into the module, with the package clause replaced. The file must
// compile on its own and import nothing but the standard library and fasthttp, and
// its unexported top level names start with agent, because the files of the other
// migrations share the package (module_shim_collision_internal_test.go checks it).
package agentshim

// The code below is the HTTP client of Fiber v2.52.15 (client.go) and behaves like
// it, under the names the fiber package gave it: Agent, Client, FormFile, Get, Head,
// Post, Put, Patch, Delete, AcquireAgent, ReleaseAgent, AcquireClient, ReleaseClient,
// AcquireFormFile and ReleaseFormFile. What is not here are the aliases of fasthttp
// that the fiber package declared (Args, Request, Response, RetryIfFunc and their
// pools), which are the fasthttp types and functions themselves, and the helpers of
// fiber/utils, which are plain conversions.
//
// The original code is licensed under the MIT license:
//
//	MIT License
//
//	Copyright (c) 2019-present Fenny and Contributors
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

// The media types the agent sets.
const (
	agentMIMEApplicationJSON = "application/json"
	agentMIMEApplicationXML  = "application/xml"
	agentMIMEApplicationForm = "application/x-www-form-urlencoded"
)

// The user agent of a request that was not given one.
const agentDefaultUserAgent = "fiber"

var agentDefaultClient Client

// Client implements http client.
//
// It is safe calling Client methods from concurrently running goroutines.
type Client struct {
	// JSONEncoder is the implementation of JSONMarshal that requests use.
	//
	// Allowing for flexibility in using another json library for encoding
	JSONEncoder func(v any) ([]byte, error)

	// JSONDecoder is the implementation of JSONUnmarshal that requests use.
	//
	// Allowing for flexibility in using another json library for decoding
	JSONDecoder func(data []byte, v any) error

	// UserAgent is used in User-Agent request header.
	UserAgent string

	mutex sync.RWMutex

	// NoDefaultUserAgentHeader when set to true, causes the default
	// User-Agent header to be excluded from the Request.
	NoDefaultUserAgentHeader bool
}

// Get returns an agent with http method GET.
func Get(url string) *Agent { return agentDefaultClient.Get(url) }

// Get returns an agent with http method GET.
func (c *Client) Get(url string) *Agent {
	return c.createAgent(fasthttp.MethodGet, url)
}

// Head returns an agent with http method HEAD.
func Head(url string) *Agent { return agentDefaultClient.Head(url) }

// Head returns an agent with http method HEAD.
func (c *Client) Head(url string) *Agent {
	return c.createAgent(fasthttp.MethodHead, url)
}

// Post sends POST request to the given URL.
func Post(url string) *Agent { return agentDefaultClient.Post(url) }

// Post sends POST request to the given URL.
func (c *Client) Post(url string) *Agent {
	return c.createAgent(fasthttp.MethodPost, url)
}

// Put sends PUT request to the given URL.
func Put(url string) *Agent { return agentDefaultClient.Put(url) }

// Put sends PUT request to the given URL.
func (c *Client) Put(url string) *Agent {
	return c.createAgent(fasthttp.MethodPut, url)
}

// Patch sends PATCH request to the given URL.
func Patch(url string) *Agent { return agentDefaultClient.Patch(url) }

// Patch sends PATCH request to the given URL.
func (c *Client) Patch(url string) *Agent {
	return c.createAgent(fasthttp.MethodPatch, url)
}

// Delete sends DELETE request to the given URL.
func Delete(url string) *Agent {
	return agentDefaultClient.Delete(url)
}

// Delete sends DELETE request to the given URL.
func (c *Client) Delete(url string) *Agent {
	return c.createAgent(fasthttp.MethodDelete, url)
}

func (c *Client) createAgent(method, url string) *Agent {
	a := AcquireAgent()
	a.req.Header.SetMethod(method)
	a.req.SetRequestURI(url)

	c.mutex.RLock()
	a.Name = c.UserAgent
	a.NoDefaultUserAgentHeader = c.NoDefaultUserAgentHeader
	a.jsonDecoder = c.JSONDecoder
	a.jsonEncoder = c.JSONEncoder
	if a.jsonDecoder == nil {
		a.jsonDecoder = json.Unmarshal
	}
	c.mutex.RUnlock()

	if err := a.Parse(); err != nil {
		a.errs = append(a.errs, err)
	}

	return a
}

// Agent is an object storing all request data for client.
// Agent instance MUST NOT be used from concurrently running goroutines.
type Agent struct {
	// HostClient is an embedded fasthttp HostClient
	*fasthttp.HostClient

	req         *fasthttp.Request
	resp        *fasthttp.Response
	args        *fasthttp.Args
	debugWriter io.Writer
	mw          agentMultipartWriter
	jsonEncoder func(v any) ([]byte, error)
	jsonDecoder func(data []byte, v any) error

	// Name is used in User-Agent request header.
	Name string

	boundary  string
	dest      []byte
	errs      []error
	formFiles []*FormFile

	timeout           time.Duration
	maxRedirectsCount int

	// NoDefaultUserAgentHeader when set to true, causes the default
	// User-Agent header to be excluded from the Request.
	NoDefaultUserAgentHeader bool

	reuse  bool
	parsed bool
}

// Parse initializes URI and HostClient.
func (a *Agent) Parse() error {
	if a.parsed {
		return nil
	}
	a.parsed = true

	uri := a.req.URI()

	var isTLS bool
	scheme := uri.Scheme()
	if bytes.Equal(scheme, []byte("https")) {
		isTLS = true
	} else if !bytes.Equal(scheme, []byte("http")) {
		return fmt.Errorf("unsupported protocol %q. http and https are supported", scheme)
	}

	name := a.Name
	if name == "" && !a.NoDefaultUserAgentHeader {
		name = agentDefaultUserAgent
	}

	a.HostClient = &fasthttp.HostClient{
		Addr:                     fasthttp.AddMissingPort(string(uri.Host()), isTLS),
		Name:                     name,
		NoDefaultUserAgentHeader: a.NoDefaultUserAgentHeader,
		IsTLS:                    isTLS,
	}

	return nil
}

/************************** Header Setting **************************/

// Set sets the given 'key: value' header.
//
// Use Add for setting multiple header values under the same key.
func (a *Agent) Set(k, v string) *Agent {
	a.req.Header.Set(k, v)

	return a
}

// SetBytesK sets the given 'key: value' header.
//
// Use AddBytesK for setting multiple header values under the same key.
func (a *Agent) SetBytesK(k []byte, v string) *Agent {
	a.req.Header.SetBytesK(k, v)

	return a
}

// SetBytesV sets the given 'key: value' header.
//
// Use AddBytesV for setting multiple header values under the same key.
func (a *Agent) SetBytesV(k string, v []byte) *Agent {
	a.req.Header.SetBytesV(k, v)

	return a
}

// SetBytesKV sets the given 'key: value' header.
//
// Use AddBytesKV for setting multiple header values under the same key.
func (a *Agent) SetBytesKV(k, v []byte) *Agent {
	a.req.Header.SetBytesKV(k, v)

	return a
}

// Add adds the given 'key: value' header.
//
// Multiple headers with the same key may be added with this function.
// Use Set for setting a single header for the given key.
func (a *Agent) Add(k, v string) *Agent {
	a.req.Header.Add(k, v)

	return a
}

// AddBytesK adds the given 'key: value' header.
//
// Multiple headers with the same key may be added with this function.
// Use SetBytesK for setting a single header for the given key.
func (a *Agent) AddBytesK(k []byte, v string) *Agent {
	a.req.Header.AddBytesK(k, v)

	return a
}

// AddBytesV adds the given 'key: value' header.
//
// Multiple headers with the same key may be added with this function.
// Use SetBytesV for setting a single header for the given key.
func (a *Agent) AddBytesV(k string, v []byte) *Agent {
	a.req.Header.AddBytesV(k, v)

	return a
}

// AddBytesKV adds the given 'key: value' header.
//
// Multiple headers with the same key may be added with this function.
// Use SetBytesKV for setting a single header for the given key.
func (a *Agent) AddBytesKV(k, v []byte) *Agent {
	a.req.Header.AddBytesKV(k, v)

	return a
}

// ConnectionClose sets 'Connection: close' header.
func (a *Agent) ConnectionClose() *Agent {
	a.req.Header.SetConnectionClose()

	return a
}

// UserAgent sets User-Agent header value.
func (a *Agent) UserAgent(userAgent string) *Agent {
	a.req.Header.SetUserAgent(userAgent)

	return a
}

// UserAgentBytes sets User-Agent header value.
func (a *Agent) UserAgentBytes(userAgent []byte) *Agent {
	a.req.Header.SetUserAgentBytes(userAgent)

	return a
}

// Cookie sets one 'key: value' cookie.
func (a *Agent) Cookie(key, value string) *Agent {
	a.req.Header.SetCookie(key, value)

	return a
}

// CookieBytesK sets one 'key: value' cookie.
func (a *Agent) CookieBytesK(key []byte, value string) *Agent {
	a.req.Header.SetCookieBytesK(key, value)

	return a
}

// CookieBytesKV sets one 'key: value' cookie.
func (a *Agent) CookieBytesKV(key, value []byte) *Agent {
	a.req.Header.SetCookieBytesKV(key, value)

	return a
}

// Cookies sets multiple 'key: value' cookies.
func (a *Agent) Cookies(kv ...string) *Agent {
	for i := 1; i < len(kv); i += 2 {
		a.req.Header.SetCookie(kv[i-1], kv[i])
	}

	return a
}

// CookiesBytesKV sets multiple 'key: value' cookies.
func (a *Agent) CookiesBytesKV(kv ...[]byte) *Agent {
	for i := 1; i < len(kv); i += 2 {
		a.req.Header.SetCookieBytesKV(kv[i-1], kv[i])
	}

	return a
}

// Referer sets Referer header value.
func (a *Agent) Referer(referer string) *Agent {
	a.req.Header.SetReferer(referer)

	return a
}

// RefererBytes sets Referer header value.
func (a *Agent) RefererBytes(referer []byte) *Agent {
	a.req.Header.SetRefererBytes(referer)

	return a
}

// ContentType sets Content-Type header value.
func (a *Agent) ContentType(contentType string) *Agent {
	a.req.Header.SetContentType(contentType)

	return a
}

// ContentTypeBytes sets Content-Type header value.
func (a *Agent) ContentTypeBytes(contentType []byte) *Agent {
	a.req.Header.SetContentTypeBytes(contentType)

	return a
}

/************************** End Header Setting **************************/

/************************** URI Setting **************************/

// Host sets host for the URI.
func (a *Agent) Host(host string) *Agent {
	a.req.URI().SetHost(host)

	return a
}

// HostBytes sets host for the URI.
func (a *Agent) HostBytes(host []byte) *Agent {
	a.req.URI().SetHostBytes(host)

	return a
}

// QueryString sets URI query string.
func (a *Agent) QueryString(queryString string) *Agent {
	a.req.URI().SetQueryString(queryString)

	return a
}

// QueryStringBytes sets URI query string.
func (a *Agent) QueryStringBytes(queryString []byte) *Agent {
	a.req.URI().SetQueryStringBytes(queryString)

	return a
}

// BasicAuth sets URI username and password.
func (a *Agent) BasicAuth(username, password string) *Agent {
	a.req.URI().SetUsername(username)
	a.req.URI().SetPassword(password)

	return a
}

// BasicAuthBytes sets URI username and password.
func (a *Agent) BasicAuthBytes(username, password []byte) *Agent {
	a.req.URI().SetUsernameBytes(username)
	a.req.URI().SetPasswordBytes(password)

	return a
}

/************************** End URI Setting **************************/

/************************** Request Setting **************************/

// BodyString sets request body.
func (a *Agent) BodyString(bodyString string) *Agent {
	a.req.SetBodyString(bodyString)

	return a
}

// Body sets request body.
func (a *Agent) Body(body []byte) *Agent {
	a.req.SetBody(body)

	return a
}

// BodyStream sets request body stream and, optionally body size.
//
// If bodySize is >= 0, then the bodyStream must provide exactly bodySize bytes
// before returning io.EOF.
//
// If bodySize < 0, then bodyStream is read until io.EOF.
//
// bodyStream.Close() is called after finishing reading all body data
// if it implements io.Closer.
//
// Note that GET and HEAD requests cannot have body.
func (a *Agent) BodyStream(bodyStream io.Reader, bodySize int) *Agent {
	a.req.SetBodyStream(bodyStream, bodySize)

	return a
}

// JSON sends a JSON request.
func (a *Agent) JSON(v any, ctype ...string) *Agent {
	if a.jsonEncoder == nil {
		a.jsonEncoder = json.Marshal
	}

	if len(ctype) > 0 {
		a.req.Header.SetContentType(ctype[0])
	} else {
		a.req.Header.SetContentType(agentMIMEApplicationJSON)
	}

	if body, err := a.jsonEncoder(v); err != nil {
		a.errs = append(a.errs, err)
	} else {
		a.req.SetBody(body)
	}

	return a
}

// XML sends an XML request.
func (a *Agent) XML(v any) *Agent {
	a.req.Header.SetContentType(agentMIMEApplicationXML)

	if body, err := xml.Marshal(v); err != nil {
		a.errs = append(a.errs, err)
	} else {
		a.req.SetBody(body)
	}

	return a
}

// Form sends form request with body if args is non-nil.
//
// It is recommended obtaining args via AcquireArgs and release it
// manually in performance-critical code.
func (a *Agent) Form(args *fasthttp.Args) *Agent {
	a.req.Header.SetContentType(agentMIMEApplicationForm)

	if args != nil {
		a.req.SetBody(args.QueryString())
	}

	return a
}

// FormFile represents multipart form file
type FormFile struct {
	// Fieldname is form file's field name
	Fieldname string
	// Name is form file's name
	Name string
	// Content is form file's content
	Content []byte
	// autoRelease indicates if returns the object
	// acquired via AcquireFormFile to the pool.
	autoRelease bool
}

// FileData appends files for multipart form request.
//
// It is recommended obtaining formFile via AcquireFormFile and release it
// manually in performance-critical code.
func (a *Agent) FileData(formFiles ...*FormFile) *Agent {
	a.formFiles = append(a.formFiles, formFiles...)

	return a
}

// SendFile reads file and appends it to multipart form request.
func (a *Agent) SendFile(filename string, fieldname ...string) *Agent {
	content, err := os.ReadFile(filepath.Clean(filename))
	if err != nil {
		a.errs = append(a.errs, err)
		return a
	}

	ff := AcquireFormFile()
	if len(fieldname) > 0 && fieldname[0] != "" {
		ff.Fieldname = fieldname[0]
	} else {
		ff.Fieldname = "file" + strconv.Itoa(len(a.formFiles)+1)
	}
	ff.Name = filepath.Base(filename)
	ff.Content = append(ff.Content, content...)
	ff.autoRelease = true

	a.formFiles = append(a.formFiles, ff)

	return a
}

// SendFiles reads files and appends them to multipart form request.
//
// Examples:
//
//	SendFile("/path/to/file1", "fieldname1", "/path/to/file2")
func (a *Agent) SendFiles(filenamesAndFieldnames ...string) *Agent {
	pairs := len(filenamesAndFieldnames)
	if pairs&1 == 1 {
		filenamesAndFieldnames = append(filenamesAndFieldnames, "")
	}

	for i := 0; i < pairs; i += 2 {
		a.SendFile(filenamesAndFieldnames[i], filenamesAndFieldnames[i+1])
	}

	return a
}

// Boundary sets boundary for multipart form request.
func (a *Agent) Boundary(boundary string) *Agent {
	a.boundary = boundary

	return a
}

// MultipartForm sends multipart form request with k-v and files.
//
// It is recommended obtaining args via AcquireArgs and release it
// manually in performance-critical code.
func (a *Agent) MultipartForm(args *fasthttp.Args) *Agent {
	if a.mw == nil {
		a.mw = multipart.NewWriter(a.req.BodyWriter())
	}

	if a.boundary != "" {
		if err := a.mw.SetBoundary(a.boundary); err != nil {
			a.errs = append(a.errs, err)
			return a
		}
	}

	a.req.Header.SetMultipartFormBoundary(a.mw.Boundary())

	if args != nil {
		args.VisitAll(func(key, value []byte) { //nolint:staticcheck // VisitAll exists in every fasthttp version, All only in the newer ones
			if err := a.mw.WriteField(string(key), string(value)); err != nil {
				a.errs = append(a.errs, err)
			}
		})
	}

	for _, ff := range a.formFiles {
		w, err := a.mw.CreateFormFile(ff.Fieldname, ff.Name)
		if err != nil {
			a.errs = append(a.errs, err)
			continue
		}
		if _, err = w.Write(ff.Content); err != nil {
			a.errs = append(a.errs, err)
		}
	}

	if err := a.mw.Close(); err != nil {
		a.errs = append(a.errs, err)
	}

	return a
}

/************************** End Request Setting **************************/

/************************** Agent Setting **************************/

// Debug mode enables logging request and response detail
func (a *Agent) Debug(w ...io.Writer) *Agent {
	a.debugWriter = os.Stdout
	if len(w) > 0 {
		a.debugWriter = w[0]
	}

	return a
}

// Timeout sets request timeout duration.
func (a *Agent) Timeout(timeout time.Duration) *Agent {
	a.timeout = timeout

	return a
}

// Reuse enables the Agent instance to be used again after one request.
//
// If agent is reusable, then it should be released manually when it is no
// longer used.
func (a *Agent) Reuse() *Agent {
	a.reuse = true

	return a
}

// InsecureSkipVerify controls whether the Agent verifies the server
// certificate chain and host name.
func (a *Agent) InsecureSkipVerify() *Agent {
	if a.HostClient.TLSConfig == nil {
		a.HostClient.TLSConfig = new(tls.Config)
	}
	a.HostClient.TLSConfig.InsecureSkipVerify = true

	return a
}

// TLSConfig sets tls config.
func (a *Agent) TLSConfig(config *tls.Config) *Agent {
	a.HostClient.TLSConfig = config

	return a
}

// MaxRedirectsCount sets max redirect count for GET and HEAD.
func (a *Agent) MaxRedirectsCount(count int) *Agent {
	a.maxRedirectsCount = count

	return a
}

// JSONEncoder sets custom json encoder.
func (a *Agent) JSONEncoder(jsonEncoder func(v any) ([]byte, error)) *Agent {
	a.jsonEncoder = jsonEncoder

	return a
}

// JSONDecoder sets custom json decoder.
func (a *Agent) JSONDecoder(jsonDecoder func(data []byte, v any) error) *Agent {
	a.jsonDecoder = jsonDecoder

	return a
}

// Request returns Agent request instance.
func (a *Agent) Request() *fasthttp.Request {
	return a.req
}

// SetResponse sets custom response for the Agent instance.
//
// It is recommended obtaining custom response via AcquireResponse and release it
// manually in performance-critical code.
func (a *Agent) SetResponse(customResp *fasthttp.Response) *Agent {
	a.resp = customResp

	return a
}

// Dest sets custom dest.
//
// The contents of dest will be replaced by the response body, if the dest
// is too small a new slice will be allocated.
func (a *Agent) Dest(dest []byte) *Agent {
	a.dest = dest

	return a
}

// RetryIf controls whether a retry should be attempted after an error.
//
// By default, will use isIdempotent function from fasthttp
func (a *Agent) RetryIf(retryIf fasthttp.RetryIfFunc) *Agent {
	a.HostClient.RetryIf = retryIf //nolint:staticcheck // RetryIf exists in every fasthttp version, RetryIfErr only in the newer ones

	return a
}

/************************** End Agent Setting **************************/

// Bytes returns the status code, bytes body and errors of url.
//
// it's not safe to use Agent after calling [Agent.Bytes]
func (a *Agent) Bytes() (int, []byte, []error) {
	defer a.release()
	return a.send()
}

func (a *Agent) send() (code int, body []byte, errs []error) {
	if errs = append(errs, a.errs...); len(errs) > 0 {
		return code, body, errs
	}

	var (
		req     = a.req
		resp    *fasthttp.Response
		nilResp bool
	)

	if a.resp == nil {
		resp = fasthttp.AcquireResponse()
		nilResp = true
	} else {
		resp = a.resp
	}

	defer func() {
		if a.debugWriter != nil {
			agentPrintDebugInfo(req, resp, a.debugWriter)
		}

		if len(errs) == 0 {
			code = resp.StatusCode()
		}

		body = agentAppend(a.dest, resp.Body())

		if nilResp {
			fasthttp.ReleaseResponse(resp)
		}
	}()

	if a.timeout > 0 {
		if err := a.DoTimeout(req, resp, a.timeout); err != nil {
			errs = append(errs, err)
			return code, body, errs
		}
	} else if a.maxRedirectsCount > 0 && (string(req.Header.Method()) == fasthttp.MethodGet || string(req.Header.Method()) == fasthttp.MethodHead) {
		if err := a.DoRedirects(req, resp, a.maxRedirectsCount); err != nil {
			errs = append(errs, err)
			return code, body, errs
		}
	} else if err := a.Do(req, resp); err != nil {
		errs = append(errs, err)
	}

	return code, body, errs
}

// agentAppend appends src to dst, which is what the agent does with the body of a
// response and the slice the caller gave it for it: the array of dst is used when
// it is big enough.
func agentAppend(dst, src []byte) []byte {
	return append(dst, src...)
}

// agentIgnore takes the errors that nothing can be done about.
func agentIgnore(...error) {}

func agentPrintDebugInfo(req *fasthttp.Request, resp *fasthttp.Response, w io.Writer) {
	msg := fmt.Sprintf("Connected to %s(%s)\r\n\r\n", req.URI().Host(), resp.RemoteAddr())
	// The dump is a debugging aid: a writer that fails does not fail the request,
	// and the three parts are written whatever happens to the ones before.
	_, errMsg := w.Write([]byte(msg))
	_, errReq := req.WriteTo(w)
	_, errResp := resp.WriteTo(w)
	agentIgnore(errMsg, errReq, errResp)
}

// String returns the status code, string body and errors of url.
//
// it's not safe to use Agent after calling [Agent.String]
func (a *Agent) String() (int, string, []error) {
	defer a.release()
	code, body, errs := a.send()
	return code, string(body), errs
}

// Struct returns the status code, bytes body and errors of URL.
// And bytes body will be unmarshalled to given v.
//
// it's not safe to use Agent after calling [Agent.Struct]
func (a *Agent) Struct(v any) (int, []byte, []error) {
	defer a.release()

	code, body, errs := a.send()
	if len(errs) > 0 {
		return code, body, errs
	}

	if a.jsonDecoder == nil {
		a.jsonDecoder = json.Unmarshal
	}

	if err := a.jsonDecoder(body, v); err != nil {
		errs = append(errs, err)
	}

	return code, body, errs
}

func (a *Agent) release() {
	if !a.reuse {
		ReleaseAgent(a)
	} else {
		a.errs = a.errs[:0]
	}
}

func (a *Agent) reset() {
	a.HostClient = nil
	a.req.Reset()
	a.resp = nil
	a.dest = nil
	a.timeout = 0
	a.args = nil
	a.errs = a.errs[:0]
	a.debugWriter = nil
	a.mw = nil
	a.reuse = false
	a.parsed = false
	a.maxRedirectsCount = 0
	a.boundary = ""
	a.Name = ""
	a.NoDefaultUserAgentHeader = false
	for i, ff := range a.formFiles {
		if ff.autoRelease {
			ReleaseFormFile(ff)
		}
		a.formFiles[i] = nil
	}
	a.formFiles = a.formFiles[:0]
}

var (
	agentClientPool sync.Pool
	agentPool       = sync.Pool{
		New: func() any {
			return &Agent{req: &fasthttp.Request{}}
		},
	}
	agentFormFilePool sync.Pool
)

// AcquireClient returns an empty Client instance from client pool.
//
// The returned Client instance may be passed to ReleaseClient when it is
// no longer needed. This allows Client recycling, reduces GC pressure
// and usually improves performance.
func AcquireClient() *Client {
	v := agentClientPool.Get()
	if v == nil {
		return &Client{}
	}
	c, ok := v.(*Client)
	if !ok {
		panic(errors.New("failed to type-assert to *Client"))
	}
	return c
}

// ReleaseClient returns c acquired via AcquireClient to client pool.
//
// It is forbidden accessing req and/or it's members after returning
// it to client pool.
func ReleaseClient(c *Client) {
	c.UserAgent = ""
	c.NoDefaultUserAgentHeader = false
	c.JSONEncoder = nil
	c.JSONDecoder = nil

	agentClientPool.Put(c)
}

// AcquireAgent returns an empty Agent instance from Agent pool.
//
// The returned Agent instance may be passed to ReleaseAgent when it is
// no longer needed. This allows Agent recycling, reduces GC pressure
// and usually improves performance.
func AcquireAgent() *Agent {
	a, ok := agentPool.Get().(*Agent)
	if !ok {
		panic(errors.New("failed to type-assert to *Agent"))
	}
	return a
}

// ReleaseAgent returns an acquired via AcquireAgent to Agent pool.
//
// It is forbidden accessing req and/or it's members after returning
// it to Agent pool.
func ReleaseAgent(a *Agent) {
	a.reset()
	agentPool.Put(a)
}

// AcquireFormFile returns an empty FormFile object from the pool.
//
// The returned FormFile may be returned to the pool with ReleaseFormFile
// when no longer needed. This allows reducing GC load.
func AcquireFormFile() *FormFile {
	v := agentFormFilePool.Get()
	if v == nil {
		return &FormFile{}
	}
	ff, ok := v.(*FormFile)
	if !ok {
		panic(errors.New("failed to type-assert to *FormFile"))
	}
	return ff
}

// ReleaseFormFile returns the object acquired via AcquireFormFile to the pool.
//
// String not access the released FormFile object, otherwise data races may occur.
func ReleaseFormFile(ff *FormFile) {
	ff.Fieldname = ""
	ff.Name = ""
	ff.Content = ff.Content[:0]
	ff.autoRelease = false

	agentFormFilePool.Put(ff)
}

type agentMultipartWriter interface {
	Boundary() string
	SetBoundary(boundary string) error
	CreateFormFile(fieldname, filename string) (io.Writer, error)
	WriteField(fieldname, value string) error
	Close() error
}
