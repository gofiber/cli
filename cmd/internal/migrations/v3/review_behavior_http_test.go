package v3_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewNotesWith returns the notes of the review that contain every one of the parts.
func reviewNotesWith(notes []string, parts ...string) []string {
	var out []string
	for _, note := range notes {
		if all := func() bool {
			for _, part := range parts {
				if !strings.Contains(note, part) {
					return false
				}
			}
			return true
		}(); all {
			out = append(out, note)
		}
	}
	return out
}

func Test_Review_ProxyDifferencesBeyondTheSSRFPolicy(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{"gw/gw.go": `package gw

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/proxy"
)

func Routes(app *fiber.App) {
	app.Use(proxy.Balancer(proxy.Config{Servers: []string{"http://a:8080"}}))
}
`})

	require.Len(t, notes, 1, "%v", notes)
	note := notes[0]
	assert.True(t, strings.HasPrefix(note, "gw/gw.go:5: proxy: Fiber v3 refuses upstreams that resolve to loopback or private addresses"), note)
	for _, part := range []string{
		// X-Real-IP
		"Balancer, Forward, DomainForward and BalancerForward set X-Real-IP to the peer address on every upstream request (v2 did it for BalancerForward only)",
		// hop-by-hop headers
		"Keep-Alive, Proxy-Authenticate, Proxy-Authorization, TE, Trailer, Transfer-Encoding, Upgrade) are removed from the request and the response",
		"SecurityPolicy.KeepHopByHopHeaders",
		// DomainForward
		"DomainForward matches the Host with or without its port and calls Next for another host (v2 answered an empty 200)",
		// a Servers entry with a path
		"a Balancer server with a path (http://backend:8080/api) panics with ErrUpstreamNotOrigin (v2 ignored the path)",
		// %2F and //
		"a request path with %2F or // is answered with 400 by Balancer, BalancerForward and DomainForward",
		"SecurityPolicy.AllowAmbiguousSlashes",
	} {
		assert.Contains(t, note, part)
	}
	assert.NotContains(t, note, "Tests that proxy", "no test file uses httptest here")
}

func Test_Review_ProxyNamesTheTestsThatStartUpstreams(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{
		"internal/gateway/balance.go": `package gateway

import "github.com/gofiber/fiber/v3/middleware/proxy"

var _ = proxy.Balancer
`,
		"internal/gateway/gateway_test.go": `package gateway

import (
	"net/http/httptest"
	"testing"
)

func TestGateway(t *testing.T) {
	srv := httptest.NewServer(nil)
	defer srv.Close()
}
`,
		"internal/gateway/other_test.go": `package gateway

import "testing"

func TestOther(t *testing.T) {}
`,
		"internal/elsewhere/e_test.go": `package elsewhere

import (
	"net/http/httptest"
	"testing"
)

func TestElsewhere(t *testing.T) { _ = httptest.NewRecorder() }
`,
	})

	proxyNotes := reviewNotesWith(notes, "proxy: Fiber v3 refuses upstreams")
	require.Len(t, proxyNotes, 1, "%v", notes)
	assert.True(t, strings.HasPrefix(proxyNotes[0], "internal/gateway/balance.go:3: proxy:"), proxyNotes[0])
	assert.Contains(t, proxyNotes[0], "Tests that proxy to a net/http/httptest server hit this as well, since such servers listen on 127.0.0.1 (internal/gateway/gateway_test.go).")
	assert.NotContains(t, proxyNotes[0], "other_test.go", "a test without httptest is not named")
	assert.NotContains(t, proxyNotes[0], "elsewhere", "only the tests next to the file are looked at")
}

func Test_Review_ProxyNoteInATestFileNeedsNoTestNames(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{"gw_test.go": `package gw

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3/middleware/proxy"
)

func TestGW(t *testing.T) { _ = httptest.NewRecorder(); _ = proxy.Balancer }
`})

	proxyNotes := reviewNotesWith(notes, "proxy: Fiber v3 refuses upstreams")
	require.Len(t, proxyNotes, 1, "%v", notes)
	assert.NotContains(t, proxyNotes[0], "Tests that proxy")
}

func Test_Review_HTTPBehaviorRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// source is the whole of main.go
		source string
		// line is the line of the import the note stands on, want a part of the note
		want string
		line int
	}{
		{
			name: "jwt answers with a challenge",
			source: `package main

import (
	jwtware "github.com/gofiber/contrib/v3/jwt"
)

var _ = jwtware.New
`,
			line: 4,
			want: `jwt: a request the middleware rejects now carries a WWW-Authenticate challenge, Bearer realm="Restricted", with error="invalid_token"`,
		},
		{
			name: "cors joins its lists and sends the max age on preflights only",
			source: `package main

import "github.com/gofiber/fiber/v3/middleware/cors"

var _ = cors.New
`,
			line: 3,
			want: `cors: the list headers (Access-Control-Allow-Methods, Access-Control-Allow-Headers and Access-Control-Expose-Headers) are joined with ", " instead of ","`,
		},
		{
			name: "compress negotiates zstd and varies on every response",
			source: `package main

import "github.com/gofiber/fiber/v3/middleware/compress"

var _ = compress.New
`,
			line: 3,
			want: "compress: the encoding is picked by the weights of the client's Accept-Encoding among br, zstd, gzip and deflate",
		},
		{
			name: "etag answers If-None-Match star with 304",
			source: `package main

import "github.com/gofiber/fiber/v3/middleware/etag"

var _ = etag.New
`,
			line: 3,
			want: "etag: a request with If-None-Match: * is answered with 304 Not Modified, where v2 answered 200 with the body.",
		},
		{
			name: "static with ByteRange ignores a multi range request",
			source: `package main

import "github.com/gofiber/fiber/v3/middleware/static"

var _ = static.Config{
	ByteRange: true,
}
`,
			line: 3,
			want: "static: with ByteRange, a Range header that asks for several ranges (bytes=0-1,3-4) or names another unit than bytes is ignored and the whole file answers 200, where v2 answered 416",
		},
		{
			name: "SendFile with ByteRange ignores a multi range request",
			source: `package main

import f "github.com/gofiber/fiber/v3"

var _ = f.SendFile{ByteRange: true}
`,
			line: 3,
			want: "SendFile: with ByteRange, a Range header that asks for several ranges (bytes=0-1,3-4)",
		},
		{
			name: "Download sends filename star",
			source: `package main

import "github.com/gofiber/fiber/v3"

func h(c fiber.Ctx) error { return c.Download("a.txt", "b.txt") }
`,
			line: 3,
			want: `Download and Attachment: a file name with non-ASCII characters is sent as filename="<name>" and filename*=UTF-8''<percent-encoded name> in Content-Disposition`,
		},
		{
			name: "Attachment sends filename star",
			source: `package main

import "github.com/gofiber/fiber/v3"

func h(c fiber.Ctx) error { c.Attachment("b.txt"); return nil }
`,
			line: 3,
			want: "Download and Attachment:",
		},
		{
			name: "app.Test reports i/o timeout",
			source: `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
)

var _ = fiber.TestConfig{Timeout: time.Second, FailOnTimeout: true}

const want = "test: timeout error 10ms"
`,
			line: 6,
			want: `app.Test: a timeout is reported as os.ErrDeadlineExceeded, whose text is "i/o timeout", where v2 returned "test: timeout error 10ms"`,
		},
		{
			name: "the client words its errors with prefixes",
			source: `package main

import "github.com/gofiber/fiber/v3/client"

var _ = client.New
`,
			line: 3,
			want: `client: errors of the transport are worded by the fasthttp that Fiber v3 requires and by the new client: a refused connection reads "error when dialing host:port: dial tcp ..."`,
		},
		{
			name: "session Destroy expires with max-age",
			source: `package main

import "github.com/gofiber/fiber/v3/middleware/session"

func h(s *session.Session) error { return s.Destroy() }
`,
			line: 3,
			want: "session: Destroy expires the cookie with max-age=0, where v2 set an expires date",
		},
		{
			name: "csrf hands new error texts to its ErrorHandler",
			source: `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
)

var _ = csrf.Config{ErrorHandler: func(c fiber.Ctx, err error) error { return err }}
`,
			line: 5,
			want: `csrf: the errors the middleware hands to Config.ErrorHandler have new texts ("csrf: token not found" where v2 said "missing csrf token in header"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			notes := reviewNotes(t, map[string]string{"main.go": tc.source})
			got := reviewNotesWith(notes, tc.want)
			require.Len(t, got, 1, "%v", notes)
			assert.True(t, strings.HasPrefix(got[0], "main.go:"+strconv.Itoa(tc.line)+": "), "the note stands on the import line %d: %s", tc.line, got[0])
		})
	}
}

func Test_Review_HTTPBehaviorRulesStaySilentWithoutTheirCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		// absent is a part no note may have
		absent string
	}{
		{
			name: "static without ByteRange",
			source: `package main

import "github.com/gofiber/fiber/v3/middleware/static"

var _ = static.Config{Compress: true}
`,
			absent: "static: with ByteRange",
		},
		{
			name: "a ByteRange of another type",
			source: `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
)

type other struct{ ByteRange bool }

var _ = other{ByteRange: true}
var _ = fiber.Map{"ByteRange": true}
var _ = static.New
`,
			absent: "with ByteRange",
		},
		{
			name: "SendFile without ByteRange",
			source: `package main

import "github.com/gofiber/fiber/v3"

var _ = fiber.SendFile{Compress: true}
`,
			absent: "SendFile: with ByteRange",
		},
		{
			name: "a Download of something that is not a context",
			source: `package main

import "github.com/gofiber/fiber/v3"

type bucket struct{}

func (bucket) Download(string) error { return nil }

func h(c fiber.Ctx, b bucket) error { return b.Download("a") }
`,
			absent: "Download and Attachment",
		},
		{
			name: "app.Test with a timeout and no text of v2 to compare with",
			source: `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
)

var _ = fiber.TestConfig{Timeout: time.Second, FailOnTimeout: true}
`,
			absent: "app.Test: a timeout",
		},
		{
			name: "session without Destroy",
			source: `package main

import "github.com/gofiber/fiber/v3/middleware/session"

func h(s *session.Session) error { return s.Save() }
`,
			absent: "session: Destroy",
		},
		{
			name: "an ErrorHandler of the app and a csrf config without one",
			source: `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
)

var _ = fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error { return err }}
var _ = csrf.Config{CookieName: "x"}
`,
			absent: "csrf: the errors the middleware hands",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, note := range reviewNotes(t, map[string]string{"main.go": tc.source}) {
				assert.NotContains(t, note, tc.absent)
			}
		})
	}
}

func Test_Review_HTTPBehaviorRulesAreReportedOncePerProject(t *testing.T) {
	t.Parallel()

	file := func(name string) string {
		return `package ` + name + `

import "github.com/gofiber/fiber/v3/middleware/cors"

var _ = cors.New
`
	}
	notes := reviewNotes(t, map[string]string{"a/a.go": file("a"), "b/b.go": file("b")})

	got := reviewNotesWith(notes, "cors: the list headers")
	require.Len(t, got, 1, "%v", notes)
	assert.True(t, strings.HasPrefix(got[0], "a/a.go:3: "), got[0])
}
