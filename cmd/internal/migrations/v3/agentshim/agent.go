// Package agentshim is the source of fiber_migrate_agent.go, the file that
// "fiber migrate" adds to a project for the requests that the client package of
// Fiber v3 cannot make the way the Fiber v2 agent did (a Host that differs from
// the URL, SetResponse, RetryIf, ...).
//
// The package exists so that the code is built, vetted, linted and tested together
// with the CLI. The migration embeds agent.go and writes everything below the
// package clause into the project, so the file must not import anything but the
// standard library and fasthttp, and must declare nothing but names that start with
// fiberMigrate and are unexported (see the tests of the migration).
package agentshim

// The code below is the HTTP client of Fiber v2.52.15 (client.go) and behaves like
// it. The differences are the names, which all start with fiberMigrate and are
// unexported, imports included, so that this file cannot collide with a name of the
// package it is added to; the aliases of fasthttp that the fiber package declared
// (Args, Request, Response, RetryIfFunc and their pools), which are the fasthttp
// types and functions themselves; and the helpers of fiber/utils, which are plain
// conversions.
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
	fmbytes "bytes"
	fmtls "crypto/tls"
	fmjson "encoding/json"
	fmxml "encoding/xml"
	fmerrors "errors"
	fmfmt "fmt"
	fmio "io"
	fmmultipart "mime/multipart"
	fmos "os"
	fmfilepath "path/filepath"
	fmstrconv "strconv"
	fmsync "sync"
	fmtime "time"

	fmfasthttp "github.com/valyala/fasthttp"
)

// The media types the agent sets.
const (
	fiberMigrateMIMEApplicationJSON = "application/json"
	fiberMigrateMIMEApplicationXML  = "application/xml"
	fiberMigrateMIMEApplicationForm = "application/x-www-form-urlencoded"
)

// The user agent of a request that was not given one.
const fiberMigrateDefaultUserAgent = "fiber"

var fiberMigrateDefaultClient fiberMigrateClient

// fiberMigrateClient implements http client.
//
// It is safe calling fiberMigrateClient methods from concurrently running goroutines.
type fiberMigrateClient struct {
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

	mutex fmsync.RWMutex

	// NoDefaultUserAgentHeader when set to true, causes the default
	// User-Agent header to be excluded from the Request.
	NoDefaultUserAgentHeader bool
}

// fiberMigrateGet returns an agent with http method GET.
func fiberMigrateGet(url string) *fiberMigrateAgent { return fiberMigrateDefaultClient.Get(url) }

// Get returns an agent with http method GET.
func (c *fiberMigrateClient) Get(url string) *fiberMigrateAgent {
	return c.createAgent(fmfasthttp.MethodGet, url)
}

// fiberMigrateHead returns an agent with http method HEAD.
func fiberMigrateHead(url string) *fiberMigrateAgent { return fiberMigrateDefaultClient.Head(url) }

// Head returns an agent with http method HEAD.
func (c *fiberMigrateClient) Head(url string) *fiberMigrateAgent {
	return c.createAgent(fmfasthttp.MethodHead, url)
}

// fiberMigratePost sends POST request to the given URL.
func fiberMigratePost(url string) *fiberMigrateAgent { return fiberMigrateDefaultClient.Post(url) }

// Post sends POST request to the given URL.
func (c *fiberMigrateClient) Post(url string) *fiberMigrateAgent {
	return c.createAgent(fmfasthttp.MethodPost, url)
}

// fiberMigratePut sends PUT request to the given URL.
func fiberMigratePut(url string) *fiberMigrateAgent { return fiberMigrateDefaultClient.Put(url) }

// Put sends PUT request to the given URL.
func (c *fiberMigrateClient) Put(url string) *fiberMigrateAgent {
	return c.createAgent(fmfasthttp.MethodPut, url)
}

// fiberMigratePatch sends PATCH request to the given URL.
func fiberMigratePatch(url string) *fiberMigrateAgent { return fiberMigrateDefaultClient.Patch(url) }

// Patch sends PATCH request to the given URL.
func (c *fiberMigrateClient) Patch(url string) *fiberMigrateAgent {
	return c.createAgent(fmfasthttp.MethodPatch, url)
}

// fiberMigrateDelete sends DELETE request to the given URL.
func fiberMigrateDelete(url string) *fiberMigrateAgent {
	return fiberMigrateDefaultClient.Delete(url)
}

// Delete sends DELETE request to the given URL.
func (c *fiberMigrateClient) Delete(url string) *fiberMigrateAgent {
	return c.createAgent(fmfasthttp.MethodDelete, url)
}

func (c *fiberMigrateClient) createAgent(method, url string) *fiberMigrateAgent {
	a := fiberMigrateAcquireAgent()
	a.req.Header.SetMethod(method)
	a.req.SetRequestURI(url)

	c.mutex.RLock()
	a.Name = c.UserAgent
	a.NoDefaultUserAgentHeader = c.NoDefaultUserAgentHeader
	a.jsonDecoder = c.JSONDecoder
	a.jsonEncoder = c.JSONEncoder
	if a.jsonDecoder == nil {
		a.jsonDecoder = fmjson.Unmarshal
	}
	c.mutex.RUnlock()

	if err := a.Parse(); err != nil {
		a.errs = append(a.errs, err)
	}

	return a
}

// fiberMigrateAgent is an object storing all request data for client.
// fiberMigrateAgent instance MUST NOT be used from concurrently running goroutines.
type fiberMigrateAgent struct {
	// HostClient is an embedded fasthttp HostClient
	*fmfasthttp.HostClient

	req         *fmfasthttp.Request
	resp        *fmfasthttp.Response
	args        *fmfasthttp.Args
	debugWriter fmio.Writer
	mw          fiberMigrateMultipartWriter
	jsonEncoder func(v any) ([]byte, error)
	jsonDecoder func(data []byte, v any) error

	// Name is used in User-Agent request header.
	Name string

	boundary  string
	dest      []byte
	errs      []error
	formFiles []*fiberMigrateFormFile

	timeout           fmtime.Duration
	maxRedirectsCount int

	// NoDefaultUserAgentHeader when set to true, causes the default
	// User-Agent header to be excluded from the Request.
	NoDefaultUserAgentHeader bool

	reuse  bool
	parsed bool
}

// Parse initializes URI and HostClient.
func (a *fiberMigrateAgent) Parse() error {
	if a.parsed {
		return nil
	}
	a.parsed = true

	uri := a.req.URI()

	var isTLS bool
	scheme := uri.Scheme()
	if fmbytes.Equal(scheme, []byte("https")) {
		isTLS = true
	} else if !fmbytes.Equal(scheme, []byte("http")) {
		return fmfmt.Errorf("unsupported protocol %q. http and https are supported", scheme)
	}

	name := a.Name
	if name == "" && !a.NoDefaultUserAgentHeader {
		name = fiberMigrateDefaultUserAgent
	}

	a.HostClient = &fmfasthttp.HostClient{
		Addr:                     fmfasthttp.AddMissingPort(string(uri.Host()), isTLS),
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
func (a *fiberMigrateAgent) Set(k, v string) *fiberMigrateAgent {
	a.req.Header.Set(k, v)

	return a
}

// SetBytesK sets the given 'key: value' header.
//
// Use AddBytesK for setting multiple header values under the same key.
func (a *fiberMigrateAgent) SetBytesK(k []byte, v string) *fiberMigrateAgent {
	a.req.Header.SetBytesK(k, v)

	return a
}

// SetBytesV sets the given 'key: value' header.
//
// Use AddBytesV for setting multiple header values under the same key.
func (a *fiberMigrateAgent) SetBytesV(k string, v []byte) *fiberMigrateAgent {
	a.req.Header.SetBytesV(k, v)

	return a
}

// SetBytesKV sets the given 'key: value' header.
//
// Use AddBytesKV for setting multiple header values under the same key.
func (a *fiberMigrateAgent) SetBytesKV(k, v []byte) *fiberMigrateAgent {
	a.req.Header.SetBytesKV(k, v)

	return a
}

// Add adds the given 'key: value' header.
//
// Multiple headers with the same key may be added with this function.
// Use Set for setting a single header for the given key.
func (a *fiberMigrateAgent) Add(k, v string) *fiberMigrateAgent {
	a.req.Header.Add(k, v)

	return a
}

// AddBytesK adds the given 'key: value' header.
//
// Multiple headers with the same key may be added with this function.
// Use SetBytesK for setting a single header for the given key.
func (a *fiberMigrateAgent) AddBytesK(k []byte, v string) *fiberMigrateAgent {
	a.req.Header.AddBytesK(k, v)

	return a
}

// AddBytesV adds the given 'key: value' header.
//
// Multiple headers with the same key may be added with this function.
// Use SetBytesV for setting a single header for the given key.
func (a *fiberMigrateAgent) AddBytesV(k string, v []byte) *fiberMigrateAgent {
	a.req.Header.AddBytesV(k, v)

	return a
}

// AddBytesKV adds the given 'key: value' header.
//
// Multiple headers with the same key may be added with this function.
// Use SetBytesKV for setting a single header for the given key.
func (a *fiberMigrateAgent) AddBytesKV(k, v []byte) *fiberMigrateAgent {
	a.req.Header.AddBytesKV(k, v)

	return a
}

// ConnectionClose sets 'Connection: close' header.
func (a *fiberMigrateAgent) ConnectionClose() *fiberMigrateAgent {
	a.req.Header.SetConnectionClose()

	return a
}

// UserAgent sets User-Agent header value.
func (a *fiberMigrateAgent) UserAgent(userAgent string) *fiberMigrateAgent {
	a.req.Header.SetUserAgent(userAgent)

	return a
}

// UserAgentBytes sets User-Agent header value.
func (a *fiberMigrateAgent) UserAgentBytes(userAgent []byte) *fiberMigrateAgent {
	a.req.Header.SetUserAgentBytes(userAgent)

	return a
}

// Cookie sets one 'key: value' cookie.
func (a *fiberMigrateAgent) Cookie(key, value string) *fiberMigrateAgent {
	a.req.Header.SetCookie(key, value)

	return a
}

// CookieBytesK sets one 'key: value' cookie.
func (a *fiberMigrateAgent) CookieBytesK(key []byte, value string) *fiberMigrateAgent {
	a.req.Header.SetCookieBytesK(key, value)

	return a
}

// CookieBytesKV sets one 'key: value' cookie.
func (a *fiberMigrateAgent) CookieBytesKV(key, value []byte) *fiberMigrateAgent {
	a.req.Header.SetCookieBytesKV(key, value)

	return a
}

// Cookies sets multiple 'key: value' cookies.
func (a *fiberMigrateAgent) Cookies(kv ...string) *fiberMigrateAgent {
	for i := 1; i < len(kv); i += 2 {
		a.req.Header.SetCookie(kv[i-1], kv[i])
	}

	return a
}

// CookiesBytesKV sets multiple 'key: value' cookies.
func (a *fiberMigrateAgent) CookiesBytesKV(kv ...[]byte) *fiberMigrateAgent {
	for i := 1; i < len(kv); i += 2 {
		a.req.Header.SetCookieBytesKV(kv[i-1], kv[i])
	}

	return a
}

// Referer sets Referer header value.
func (a *fiberMigrateAgent) Referer(referer string) *fiberMigrateAgent {
	a.req.Header.SetReferer(referer)

	return a
}

// RefererBytes sets Referer header value.
func (a *fiberMigrateAgent) RefererBytes(referer []byte) *fiberMigrateAgent {
	a.req.Header.SetRefererBytes(referer)

	return a
}

// ContentType sets Content-Type header value.
func (a *fiberMigrateAgent) ContentType(contentType string) *fiberMigrateAgent {
	a.req.Header.SetContentType(contentType)

	return a
}

// ContentTypeBytes sets Content-Type header value.
func (a *fiberMigrateAgent) ContentTypeBytes(contentType []byte) *fiberMigrateAgent {
	a.req.Header.SetContentTypeBytes(contentType)

	return a
}

/************************** End Header Setting **************************/

/************************** URI Setting **************************/

// Host sets host for the URI.
func (a *fiberMigrateAgent) Host(host string) *fiberMigrateAgent {
	a.req.URI().SetHost(host)

	return a
}

// HostBytes sets host for the URI.
func (a *fiberMigrateAgent) HostBytes(host []byte) *fiberMigrateAgent {
	a.req.URI().SetHostBytes(host)

	return a
}

// QueryString sets URI query string.
func (a *fiberMigrateAgent) QueryString(queryString string) *fiberMigrateAgent {
	a.req.URI().SetQueryString(queryString)

	return a
}

// QueryStringBytes sets URI query string.
func (a *fiberMigrateAgent) QueryStringBytes(queryString []byte) *fiberMigrateAgent {
	a.req.URI().SetQueryStringBytes(queryString)

	return a
}

// BasicAuth sets URI username and password.
func (a *fiberMigrateAgent) BasicAuth(username, password string) *fiberMigrateAgent {
	a.req.URI().SetUsername(username)
	a.req.URI().SetPassword(password)

	return a
}

// BasicAuthBytes sets URI username and password.
func (a *fiberMigrateAgent) BasicAuthBytes(username, password []byte) *fiberMigrateAgent {
	a.req.URI().SetUsernameBytes(username)
	a.req.URI().SetPasswordBytes(password)

	return a
}

/************************** End URI Setting **************************/

/************************** Request Setting **************************/

// BodyString sets request body.
func (a *fiberMigrateAgent) BodyString(bodyString string) *fiberMigrateAgent {
	a.req.SetBodyString(bodyString)

	return a
}

// Body sets request body.
func (a *fiberMigrateAgent) Body(body []byte) *fiberMigrateAgent {
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
func (a *fiberMigrateAgent) BodyStream(bodyStream fmio.Reader, bodySize int) *fiberMigrateAgent {
	a.req.SetBodyStream(bodyStream, bodySize)

	return a
}

// JSON sends a JSON request.
func (a *fiberMigrateAgent) JSON(v any, ctype ...string) *fiberMigrateAgent {
	if a.jsonEncoder == nil {
		a.jsonEncoder = fmjson.Marshal
	}

	if len(ctype) > 0 {
		a.req.Header.SetContentType(ctype[0])
	} else {
		a.req.Header.SetContentType(fiberMigrateMIMEApplicationJSON)
	}

	if body, err := a.jsonEncoder(v); err != nil {
		a.errs = append(a.errs, err)
	} else {
		a.req.SetBody(body)
	}

	return a
}

// XML sends an XML request.
func (a *fiberMigrateAgent) XML(v any) *fiberMigrateAgent {
	a.req.Header.SetContentType(fiberMigrateMIMEApplicationXML)

	if body, err := fmxml.Marshal(v); err != nil {
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
func (a *fiberMigrateAgent) Form(args *fmfasthttp.Args) *fiberMigrateAgent {
	a.req.Header.SetContentType(fiberMigrateMIMEApplicationForm)

	if args != nil {
		a.req.SetBody(args.QueryString())
	}

	return a
}

// fiberMigrateFormFile represents multipart form file
type fiberMigrateFormFile struct {
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
func (a *fiberMigrateAgent) FileData(formFiles ...*fiberMigrateFormFile) *fiberMigrateAgent {
	a.formFiles = append(a.formFiles, formFiles...)

	return a
}

// SendFile reads file and appends it to multipart form request.
func (a *fiberMigrateAgent) SendFile(filename string, fieldname ...string) *fiberMigrateAgent {
	content, err := fmos.ReadFile(fmfilepath.Clean(filename))
	if err != nil {
		a.errs = append(a.errs, err)
		return a
	}

	ff := fiberMigrateAcquireFormFile()
	if len(fieldname) > 0 && fieldname[0] != "" {
		ff.Fieldname = fieldname[0]
	} else {
		ff.Fieldname = "file" + fmstrconv.Itoa(len(a.formFiles)+1)
	}
	ff.Name = fmfilepath.Base(filename)
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
func (a *fiberMigrateAgent) SendFiles(filenamesAndFieldnames ...string) *fiberMigrateAgent {
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
func (a *fiberMigrateAgent) Boundary(boundary string) *fiberMigrateAgent {
	a.boundary = boundary

	return a
}

// MultipartForm sends multipart form request with k-v and files.
//
// It is recommended obtaining args via AcquireArgs and release it
// manually in performance-critical code.
func (a *fiberMigrateAgent) MultipartForm(args *fmfasthttp.Args) *fiberMigrateAgent {
	if a.mw == nil {
		a.mw = fmmultipart.NewWriter(a.req.BodyWriter())
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
func (a *fiberMigrateAgent) Debug(w ...fmio.Writer) *fiberMigrateAgent {
	a.debugWriter = fmos.Stdout
	if len(w) > 0 {
		a.debugWriter = w[0]
	}

	return a
}

// Timeout sets request timeout duration.
func (a *fiberMigrateAgent) Timeout(timeout fmtime.Duration) *fiberMigrateAgent {
	a.timeout = timeout

	return a
}

// Reuse enables the Agent instance to be used again after one request.
//
// If agent is reusable, then it should be released manually when it is no
// longer used.
func (a *fiberMigrateAgent) Reuse() *fiberMigrateAgent {
	a.reuse = true

	return a
}

// InsecureSkipVerify controls whether the Agent verifies the server
// certificate chain and host name.
func (a *fiberMigrateAgent) InsecureSkipVerify() *fiberMigrateAgent {
	if a.HostClient.TLSConfig == nil {
		a.HostClient.TLSConfig = &fmtls.Config{InsecureSkipVerify: true} //nolint:gosec // We explicitly let the user set insecure mode here
	} else {
		a.HostClient.TLSConfig.InsecureSkipVerify = true
	}

	return a
}

// TLSConfig sets tls config.
func (a *fiberMigrateAgent) TLSConfig(config *fmtls.Config) *fiberMigrateAgent {
	a.HostClient.TLSConfig = config

	return a
}

// MaxRedirectsCount sets max redirect count for GET and HEAD.
func (a *fiberMigrateAgent) MaxRedirectsCount(count int) *fiberMigrateAgent {
	a.maxRedirectsCount = count

	return a
}

// JSONEncoder sets custom json encoder.
func (a *fiberMigrateAgent) JSONEncoder(jsonEncoder func(v any) ([]byte, error)) *fiberMigrateAgent {
	a.jsonEncoder = jsonEncoder

	return a
}

// JSONDecoder sets custom json decoder.
func (a *fiberMigrateAgent) JSONDecoder(jsonDecoder func(data []byte, v any) error) *fiberMigrateAgent {
	a.jsonDecoder = jsonDecoder

	return a
}

// Request returns Agent request instance.
func (a *fiberMigrateAgent) Request() *fmfasthttp.Request {
	return a.req
}

// SetResponse sets custom response for the Agent instance.
//
// It is recommended obtaining custom response via AcquireResponse and release it
// manually in performance-critical code.
func (a *fiberMigrateAgent) SetResponse(customResp *fmfasthttp.Response) *fiberMigrateAgent {
	a.resp = customResp

	return a
}

// Dest sets custom dest.
//
// The contents of dest will be replaced by the response body, if the dest
// is too small a new slice will be allocated.
func (a *fiberMigrateAgent) Dest(dest []byte) *fiberMigrateAgent {
	a.dest = dest

	return a
}

// RetryIf controls whether a retry should be attempted after an error.
//
// By default, will use isIdempotent function from fasthttp
func (a *fiberMigrateAgent) RetryIf(retryIf fmfasthttp.RetryIfFunc) *fiberMigrateAgent {
	a.HostClient.RetryIf = retryIf //nolint:staticcheck // the agent of Fiber v2 set the field that fasthttp deprecated later, the request behaves as before
	return a
}

/************************** End Agent Setting **************************/

// Bytes returns the status code, bytes body and errors of url.
//
// it's not safe to use Agent after calling [Agent.Bytes]
func (a *fiberMigrateAgent) Bytes() (int, []byte, []error) {
	defer a.release()
	return a.send()
}

func (a *fiberMigrateAgent) send() (code int, body []byte, errs []error) {
	if errs = append(errs, a.errs...); len(errs) > 0 {
		return code, body, errs
	}

	var (
		req     = a.req
		resp    *fmfasthttp.Response
		nilResp bool
	)

	if a.resp == nil {
		resp = fmfasthttp.AcquireResponse()
		nilResp = true
	} else {
		resp = a.resp
	}

	defer func() {
		if a.debugWriter != nil {
			fiberMigratePrintDebugInfo(req, resp, a.debugWriter)
		}

		if len(errs) == 0 {
			code = resp.StatusCode()
		}

		body = append(a.dest, resp.Body()...) //nolint:gocritic // The body is appended to the dest of the request, as in Fiber v2.

		if nilResp {
			fmfasthttp.ReleaseResponse(resp)
		}
	}()

	if a.timeout > 0 {
		if err := a.DoTimeout(req, resp, a.timeout); err != nil {
			errs = append(errs, err)
			return code, body, errs
		}
	} else if a.maxRedirectsCount > 0 && (string(req.Header.Method()) == fmfasthttp.MethodGet || string(req.Header.Method()) == fmfasthttp.MethodHead) {
		if err := a.DoRedirects(req, resp, a.maxRedirectsCount); err != nil {
			errs = append(errs, err)
			return code, body, errs
		}
	} else if err := a.Do(req, resp); err != nil {
		errs = append(errs, err)
	}

	return code, body, errs
}

func fiberMigratePrintDebugInfo(req *fmfasthttp.Request, resp *fmfasthttp.Response, w fmio.Writer) {
	msg := fmfmt.Sprintf("Connected to %s(%s)\r\n\r\n", req.URI().Host(), resp.RemoteAddr())
	_, _ = w.Write([]byte(msg)) //nolint:errcheck // This will never fail
	_, _ = req.WriteTo(w)       //nolint:errcheck // This will never fail
	_, _ = resp.WriteTo(w)      //nolint:errcheck // This will never fail
}

// String returns the status code, string body and errors of url.
//
// it's not safe to use Agent after calling [Agent.String]
func (a *fiberMigrateAgent) String() (int, string, []error) {
	defer a.release()
	code, body, errs := a.send()
	return code, string(body), errs
}

// Struct returns the status code, bytes body and errors of URL.
// And bytes body will be unmarshalled to given v.
//
// it's not safe to use Agent after calling [Agent.Struct]
func (a *fiberMigrateAgent) Struct(v any) (int, []byte, []error) {
	defer a.release()

	code, body, errs := a.send()
	if len(errs) > 0 {
		return code, body, errs
	}

	if a.jsonDecoder == nil {
		a.jsonDecoder = fmjson.Unmarshal
	}

	if err := a.jsonDecoder(body, v); err != nil {
		errs = append(errs, err)
	}

	return code, body, errs
}

func (a *fiberMigrateAgent) release() {
	if !a.reuse {
		fiberMigrateReleaseAgent(a)
	} else {
		a.errs = a.errs[:0]
	}
}

func (a *fiberMigrateAgent) reset() {
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
			fiberMigrateReleaseFormFile(ff)
		}
		a.formFiles[i] = nil
	}
	a.formFiles = a.formFiles[:0]
}

var (
	fiberMigrateClientPool fmsync.Pool
	fiberMigrateAgentPool  = fmsync.Pool{
		New: func() any {
			return &fiberMigrateAgent{req: &fmfasthttp.Request{}}
		},
	}
	fiberMigrateFormFilePool fmsync.Pool
)

// fiberMigrateAcquireClient returns an empty Client instance from client pool.
//
// The returned Client instance may be passed to ReleaseClient when it is
// no longer needed. This allows Client recycling, reduces GC pressure
// and usually improves performance.
func fiberMigrateAcquireClient() *fiberMigrateClient {
	v := fiberMigrateClientPool.Get()
	if v == nil {
		return &fiberMigrateClient{}
	}
	c, ok := v.(*fiberMigrateClient)
	if !ok {
		panic(fmerrors.New("failed to type-assert to *Client"))
	}
	return c
}

// fiberMigrateReleaseClient returns c acquired via AcquireClient to client pool.
//
// It is forbidden accessing req and/or it's members after returning
// it to client pool.
func fiberMigrateReleaseClient(c *fiberMigrateClient) {
	c.UserAgent = ""
	c.NoDefaultUserAgentHeader = false
	c.JSONEncoder = nil
	c.JSONDecoder = nil

	fiberMigrateClientPool.Put(c)
}

// fiberMigrateAcquireAgent returns an empty Agent instance from Agent pool.
//
// The returned Agent instance may be passed to ReleaseAgent when it is
// no longer needed. This allows Agent recycling, reduces GC pressure
// and usually improves performance.
func fiberMigrateAcquireAgent() *fiberMigrateAgent {
	a, ok := fiberMigrateAgentPool.Get().(*fiberMigrateAgent)
	if !ok {
		panic(fmerrors.New("failed to type-assert to *Agent"))
	}
	return a
}

// fiberMigrateReleaseAgent returns an acquired via AcquireAgent to Agent pool.
//
// It is forbidden accessing req and/or it's members after returning
// it to Agent pool.
func fiberMigrateReleaseAgent(a *fiberMigrateAgent) {
	a.reset()
	fiberMigrateAgentPool.Put(a)
}

// fiberMigrateAcquireFormFile returns an empty FormFile object from the pool.
//
// The returned FormFile may be returned to the pool with ReleaseFormFile
// when no longer needed. This allows reducing GC load.
func fiberMigrateAcquireFormFile() *fiberMigrateFormFile {
	v := fiberMigrateFormFilePool.Get()
	if v == nil {
		return &fiberMigrateFormFile{}
	}
	ff, ok := v.(*fiberMigrateFormFile)
	if !ok {
		panic(fmerrors.New("failed to type-assert to *FormFile"))
	}
	return ff
}

// fiberMigrateReleaseFormFile returns the object acquired via AcquireFormFile to the pool.
//
// String not access the released FormFile object, otherwise data races may occur.
func fiberMigrateReleaseFormFile(ff *fiberMigrateFormFile) {
	ff.Fieldname = ""
	ff.Name = ""
	ff.Content = ff.Content[:0]
	ff.autoRelease = false

	fiberMigrateFormFilePool.Put(ff)
}

type fiberMigrateMultipartWriter interface {
	Boundary() string
	SetBoundary(boundary string) error
	CreateFormFile(fieldname, filename string) (fmio.Writer, error)
	WriteField(fieldname, value string) error
	Close() error
}
