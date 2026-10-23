package v3

import (
	"regexp"
)

// reviewBehaviorRule is a behavior change of a package that no rewrite can reproduce.
// It is reported once per project, at the first import of the package.
type reviewBehaviorRule struct {
	// pkg matches the import path of the package the rule is about.
	pkg *regexp.Regexp
	// uses, when set, must also match the text of the file for the rule to apply.
	uses *regexp.Regexp
	// unless, when set, switches the rule off for files whose text matches it.
	unless *regexp.Regexp
	// when, when set, must also hold for the file: for a condition that the text
	// cannot state, such as a field of one type's literal (see review_when.go).
	when    func(rf *reviewFile) bool
	message string
	// tests adds the test files next to the file that use httptest to the message,
	// for a change that fails in tests first (see reviewFile.httptestNote).
	tests bool
}

// reviewMiddlewarePath matches the import path of a built-in middleware, at v2 or v3.
func reviewMiddlewarePath(name string) *regexp.Regexp {
	return regexp.MustCompile(`^github\.com/gofiber/fiber/v[23]/middleware/` + name + `$`)
}

// reviewCorePkgRe matches the import path of the core package of Fiber v3.
var reviewCorePkgRe = regexp.MustCompile(`^github\.com/gofiber/fiber/v3$`)

// reviewRangeNote is the change in how a Range header with several ranges is answered, which the
// static middleware and SendFile share once ByteRange is set.
func reviewRangeNote(what string) string {
	return what + ": with ByteRange, a Range header that asks for several ranges (bytes=0-1,3-4) or names another unit than bytes is ignored and the whole file answers 200, " +
		"where v2 answered 416; a single range and a range that cannot be satisfied behave as before."
}

// reviewContribPath matches the import path of a contrib module, before or after its
// move below contrib/v3, under any of its names.
func reviewContribPath(names string) *regexp.Regexp {
	return regexp.MustCompile(`^github\.com/gofiber/contrib/(v3/)?(` + names + `)(/v\d+)?$`)
}

// reviewBehaviorRules are the behavior changes between Fiber v2 and v3 that stay
// with the user. Each message states what changed and what to do about it.
var reviewBehaviorRules = []reviewBehaviorRule{
	{
		pkg: reviewMiddlewarePath("proxy"),
		message: "proxy: Fiber v3 refuses upstreams that resolve to loopback or private addresses (SSRF protection), so proxying to localhost or an internal host panics at startup or answers 500. Allow them with proxy.WithSecurityPolicy(proxy.SecurityPolicy{AllowPrivateIPs: true}) or Config.SecurityPolicy. " +
			"The middleware differs from v2 in five more ways: Balancer, Forward, DomainForward and BalancerForward set X-Real-IP to the peer address on every upstream request (v2 did it for BalancerForward only); " +
			"the hop-by-hop headers (Connection and every header it names, Keep-Alive, Proxy-Authenticate, Proxy-Authorization, TE, Trailer, Transfer-Encoding, Upgrade) are removed from the request and the response, so a WebSocket upgrade does not pass unless SecurityPolicy.KeepHopByHopHeaders is set; " +
			"DomainForward matches the Host with or without its port and calls Next for another host (v2 answered an empty 200); " +
			"a Balancer server with a path (http://backend:8080/api) panics with ErrUpstreamNotOrigin (v2 ignored the path); " +
			"a request path with %2F or // is answered with 400 by Balancer, BalancerForward and DomainForward (v2 forwarded it as it was, SecurityPolicy.AllowAmbiguousSlashes allows it).",
		tests: true,
	},
	{
		pkg:     reviewMiddlewarePath("timeout"),
		message: "timeout: the middleware returns as soon as the time is up, without waiting for the handler, which should watch c.Context().Done().",
	},
	{
		// the migration gives every config it writes an OnTimeout that keeps the 408
		pkg:     reviewMiddlewarePath("timeout"),
		unless:  regexp.MustCompile(`\bOnTimeout\b`),
		message: "timeout: a timed-out handler now answers 503 instead of 408 (return fiber.ErrRequestTimeout from Config.OnTimeout to keep 408).",
	},
	{
		pkg:     reviewMiddlewarePath("healthcheck"),
		message: "healthcheck: each probe is a route of its own now, so HEAD requests are answered too, and another method on a probe path answers 405 where no other route takes it, instead of 404.",
	},
	{
		pkg:     reviewMiddlewarePath("cache"),
		message: "cache: Fiber v3 sends Cache-Control headers by default (set DisableCacheControl to stop that), raises the default Expiration from 1 to 5 minutes and builds its cache keys differently, so every cache starts cold after the upgrade.",
	},
	{
		pkg:     reviewMiddlewarePath("envvar"),
		message: "envvar: Fiber v3 only exposes the variables listed in ExportVars (ExcludeVars is gone); list the variables you want to serve.",
	},
	{
		pkg:     reviewMiddlewarePath("basicauth"),
		message: "basicauth: Fiber v3 only accepts hashed passwords in Users (bcrypt, {SHA256} or {SHA512}) and panics at startup on plaintext ones, and the password is no longer kept in the request context.",
	},
	{
		pkg:     reviewMiddlewarePath("keyauth"),
		message: "keyauth: Fiber v3 only accepts keys that are valid RFC 7235 token68 values (letters, digits and -._~+/ with optional trailing =), and answers failures with a WWW-Authenticate challenge (see Realm and Challenge) and the body \"missing or invalid API Key\", where v2 said \"missing or malformed API Key\" for a request without a key and \"Invalid or expired API Key\" for one the validator rejected.",
	},
	{
		pkg:     reviewCorePkgRe,
		when:    func(rf *reviewFile) bool { return rf.literalHasKey(reviewCorePath, reviewConfigType, "UnescapePath") },
		message: `Config.UnescapePath: a "+" in the path stays a "+" in Fiber v3, a character of a path (RFC 3986), where v2 decoded it to a space like a query string does, so a route parameter or c.Path() that carried a "+" reads differently.`,
	},
	{
		pkg:     reviewMiddlewarePath("requestid"),
		message: "requestid: the default ID is now a secure random token instead of a UUID; set Config.Generator to utils.UUIDv4 if something relies on the UUID format.",
	},
	{
		pkg:     regexp.MustCompile(`^github\.com/gofiber/fiber/v3$`),
		uses:    regexp.MustCompile(`\bRedirect\(\)\s*(?:\.Status\([^)]*\)\s*)?\.Back\(`),
		message: "Redirect().Back only follows a Referer that comes from the same origin and takes the fallback otherwise; RedirectBack of v2 followed any Referer.",
	},
	{
		pkg:     reviewMiddlewarePath("session"),
		unless:  regexp.MustCompile(`\bKeyGenerator\b`),
		message: "session: the default session ID is now a secure random token instead of a UUID; set Config.KeyGenerator to utils.UUIDv4 if something relies on the UUID format.",
	},
	{
		pkg:     reviewMiddlewarePath("logger"),
		unless:  regexp.MustCompile(`\bFormat:`),
		message: `logger: the default Format changed to "[${time}] ${ip} ${status} - ${latency} ${method} ${path} ${error}"; set Format to keep the v2 layout.`,
	},
	{
		pkg:     reviewMiddlewarePath("limiter"),
		uses:    regexp.MustCompile(`\bSkip(Failed|Successful)Requests\b`),
		message: "limiter: SkipFailedRequests and SkipSuccessfulRequests now judge the status a request ends with, errors included; in v2 only the response status counted, which is still 200 when a handler returns an error, so such requests now count as failed.",
	},
	{
		pkg:     reviewMiddlewarePath("encryptcookie"),
		message: "encryptcookie: Fiber v3 binds the cookie name to the encrypted value, so cookies encrypted by v2 fail to decrypt and are discarded once; users may have to sign in again.",
	},
	{
		pkg:     reviewMiddlewarePath("csrf"),
		message: "csrf: the default token lifetime (IdleTimeout, formerly Expiration) is 30 minutes instead of 1 hour, and unsafe requests with an invalid Sec-Fetch-Site header are rejected.",
	},
	{
		pkg:     reviewMiddlewarePath("static"),
		message: `static: a directory without an index file answers 404 instead of 403, the body of a missing file is "Not Found", HEAD responses carry Cache-Control, and a path with an encoded NUL byte (%00) answers 404 instead of 400 "Are you a hacker?"; check the clients and tests that depend on them.`,
	},
	{
		pkg:     reviewContribPath("opa|opafiber"),
		message: "opa: the v3 module evaluates policies as Rego v1; policies written for Rego v0 (for example `allow { ... }` without `if`) fail at startup. Convert them with `opa fmt --v0-v1`.",
	},
	{
		pkg: reviewContribPath("otel|otelfiber"),
		message: "otel: the metric names changed (http.server.duration is now http.server.request.duration, measured in seconds, and the size metrics gained .body) " +
			"and the spans are named after the method and the route (GET /orders/:id, where it was /orders/:id). From otelfiber 1.x the attributes change too, " +
			"to the stable HTTP conventions (http.method is http.request.method, http.status_code is http.response.status_code, http.target and http.url are " +
			"url.path and url.query, net.host.name is server.address, http.user_agent is user_agent.original, http.client_ip is client.address), " +
			"and the metrics no longer carry the host. Update the dashboards, alerts and sampling rules that use the old names.",
	},
	{
		pkg: reviewContribPath("sentry|fibersentry"),
		message: "sentry: the v3 module requires sentry-go v0.50 or newer. What it removed is rewritten or reported where the code uses it (Scope.SetExtra, Event.Extra, sentry.Logger, the logs and metrics options, " +
			"the span options that lost their names such as sentry.TransactionName, Scope.SetTransaction); " +
			"what it changed without a compile error is that transactions are grouped by their parameterized path (0.47) and that captured panics no longer have the frames of sentry.Recover (0.49), so issues and transactions may regroup after the upgrade.",
	},
	{
		pkg:     reviewContribPath("jwt"),
		message: `jwt: a request the middleware rejects now carries a WWW-Authenticate challenge, Bearer realm="Restricted", with error="invalid_token" and an error_description for a token that failed verification, which v2 did not send; set Config.Realm to change the realm. An ErrorHandler that writes the header itself keeps its value.`,
	},
	{
		pkg:     reviewMiddlewarePath("cors"),
		message: `cors: the list headers (Access-Control-Allow-Methods, Access-Control-Allow-Headers and Access-Control-Expose-Headers) are joined with ", " instead of ",", and Access-Control-Max-Age is sent on preflight responses only, where v2 sent it on every CORS response; check the clients and tests that match the exact values.`,
	},
	{
		pkg:     reviewMiddlewarePath("compress"),
		message: "compress: the encoding is picked by the weights of the client's Accept-Encoding among br, zstd, gzip and deflate, so a client that accepts zstd now gets Content-Encoding: zstd (v2 on an older fasthttp sent the body uncompressed), and Vary: Accept-Encoding is added to every response that passes through, also to the ones it leaves uncompressed (v2 added it to compressed ones only).",
	},
	{
		pkg:     reviewMiddlewarePath("etag"),
		message: "etag: a request with If-None-Match: * is answered with 304 Not Modified, where v2 answered 200 with the body.",
	},
	{
		pkg: reviewMiddlewarePath("static"),
		when: func(rf *reviewFile) bool {
			return rf.literalHasKey(reviewV3Prefix+"middleware/static", "Config", "ByteRange")
		},
		message: reviewRangeNote("static"),
	},
	{
		pkg:     reviewCorePkgRe,
		when:    func(rf *reviewFile) bool { return rf.literalHasKey(reviewCorePath, "SendFile", "ByteRange") },
		message: reviewRangeNote("SendFile"),
	},
	{
		pkg:     reviewCorePkgRe,
		when:    func(rf *reviewFile) bool { return rf.callsCtxMethod("Download", "Attachment") },
		message: `Download and Attachment: a file name with non-ASCII characters is sent as filename="<name>" and filename*=UTF-8''<percent-encoded name> in Content-Disposition, where v2 sent only the percent-encoded name in filename="..."; check the clients and tests that read the header.`,
	},
	{
		// the text only matters to code that shows or compares it: a test that names the text of v2
		pkg:     reviewCorePkgRe,
		uses:    regexp.MustCompile(`\btimeout error\b`),
		message: `app.Test: a timeout is reported as os.ErrDeadlineExceeded, whose text is "i/o timeout", where v2 returned "test: timeout error 10ms"; compare with errors.Is(err, os.ErrDeadlineExceeded) instead of the text.`,
	},
	{
		pkg: regexp.MustCompile(`^github\.com/gofiber/fiber/v3/client$`),
		message: `client: errors of the transport are worded by the fasthttp that Fiber v3 requires and by the new client: a refused connection reads "error when dialing host:port: dial tcp ..." (the v2 Agent returned "dial tcp ..."), ` +
			`a timeout of the client reads "timeout or cancel" (v2: "timeout") and a response over the size limit reads "fasthttp: body size exceeds the given limit" (v2: without the prefix); ` +
			`a program that prints or compares err.Error() shows or breaks on the new texts, errors.Is with client.ErrTimeoutOrCancel does not.`,
	},
	{
		pkg:     reviewMiddlewarePath("session"),
		uses:    regexp.MustCompile(`\.Destroy\(`),
		message: "session: Destroy expires the cookie with max-age=0, where v2 set an expires date; a client or test that reads expires from the Set-Cookie header has to read max-age.",
	},
	{
		pkg: reviewMiddlewarePath("csrf"),
		when: func(rf *reviewFile) bool {
			return rf.literalHasKey(reviewV3Prefix+"middleware/csrf", "Config", "ErrorHandler")
		},
		message: `csrf: the errors the middleware hands to Config.ErrorHandler have new texts ("csrf: token not found" where v2 said "missing csrf token in header", "csrf: token invalid" where it said "csrf token invalid"), ` +
			"so a handler or test that puts err.Error() into the response shows them; match with errors.Is(err, csrf.ErrTokenNotFound) and csrf.ErrTokenInvalid instead.",
	},
}
