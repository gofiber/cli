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
	unless  *regexp.Regexp
	message string
}

// reviewMiddlewarePath matches the import path of a built-in middleware, at v2 or v3.
func reviewMiddlewarePath(name string) *regexp.Regexp {
	return regexp.MustCompile(`^github\.com/gofiber/fiber/v[23]/middleware/` + name + `$`)
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
		pkg:     reviewMiddlewarePath("proxy"),
		message: "proxy: Fiber v3 refuses upstreams that resolve to loopback or private addresses (SSRF protection), so proxying to localhost or an internal host panics at startup or answers 500. Allow them with proxy.WithSecurityPolicy(proxy.SecurityPolicy{AllowPrivateIPs: true}) or Config.SecurityPolicy.",
	},
	{
		pkg:     reviewMiddlewarePath("timeout"),
		message: "timeout: a timed-out handler now answers 503 instead of 408 (return fiber.ErrRequestTimeout from Config.OnTimeout to keep 408), and the middleware returns without waiting for the handler, which should watch c.Context().Done().",
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
		message: "keyauth: Fiber v3 only accepts keys that are valid RFC 7235 token68 values (letters, digits and -._~+/ with optional trailing =), and answers failures with a WWW-Authenticate challenge (see Realm and Challenge).",
	},
	{
		pkg:     reviewMiddlewarePath("requestid"),
		message: "requestid: the default ID is now a secure random token instead of a UUID; set Config.Generator to utils.UUIDv4 if something relies on the UUID format.",
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
		message: `static: a directory without an index file answers 404 instead of 403, the body of a missing file is "Not Found", and HEAD responses carry Cache-Control; check the clients and tests that depend on them.`,
	},
	{
		pkg:     reviewContribPath("opa|opafiber"),
		message: "opa: the v3 module evaluates policies as Rego v1; policies written for Rego v0 (for example `allow { ... }` without `if`) fail at startup. Convert them with `opa fmt --v0-v1`.",
	},
	{
		pkg:     reviewContribPath("otel|otelfiber"),
		message: "otel: the metric names changed (http.server.duration is now http.server.request.duration, measured in seconds, and the size metrics gained .body); update the dashboards and alerts that use the old names.",
	},
	{
		pkg:     reviewContribPath("sentry|fibersentry"),
		message: "sentry: the v3 module requires sentry-go v0.50 or newer, which removed some APIs (for example Scope.SetExtra); expect to adjust the code that calls them.",
	},
}
