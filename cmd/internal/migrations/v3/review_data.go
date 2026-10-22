// Code generated from the exported API of Fiber v2.52.15 and v3.6.1 (identifiers that exist in v2 and not in v3); DO NOT EDIT.

package v3

// reviewRemovedPackages are the Fiber v2 packages that are not part of the v3 module.
var reviewRemovedPackages = map[string]string{
	"github.com/gofiber/fiber/v2/middleware/filesystem": "replaced by the static middleware",
	"github.com/gofiber/fiber/v2/middleware/monitor":    "moved to github.com/gofiber/contrib/v3/monitor",
}

// reviewRemovedNames lists, by import path of the v3 package, the exported constants, functions, types and
// variables of the v2 package that are gone in v3.
var reviewRemovedNames = map[string][]string{
	"github.com/gofiber/fiber/v3":                        {"AcquireAgent", "AcquireArgs", "AcquireClient", "AcquireFormFile", "AcquireResponse", "Agent", "Args", "Client", "ConstraintGuid", "DefaultCompressedFileSuffix", "Delete", "FormFile", "Get", "Head", "HeaderFeaturePolicy", "IndexRune", "MIMEApplicationJavaScriptCharsetUTF8", "OnShutdownHandler", "ParserConfig", "ParserType", "Patch", "Post", "Put", "ReleaseAgent", "ReleaseArgs", "ReleaseClient", "ReleaseFormFile", "ReleaseResponse", "Request", "Response", "RetryIfFunc", "SetParserDecoder", "Static"},
	"github.com/gofiber/fiber/v3/log":                    {"ControlLogger"},
	"github.com/gofiber/fiber/v3/middleware/csrf":        {"CSRFHandler", "CsrfFromCookie", "CsrfFromForm", "CsrfFromHeader", "CsrfFromParam", "CsrfFromQuery", "ErrBadReferer", "ErrMissingCookie", "ErrMissingForm", "ErrMissingHeader", "ErrMissingParam", "ErrMissingQuery", "ErrNoReferer"},
	"github.com/gofiber/fiber/v3/middleware/healthcheck": {"DefaultLivenessEndpoint", "DefaultReadinessEndpoint", "HealthChecker", "HealthCheckerHandler"},
	"github.com/gofiber/fiber/v3/middleware/limiter":     {"LimiterHandler"},
	"github.com/gofiber/fiber/v3/middleware/logger":      {"TagHeader"},
	"github.com/gofiber/fiber/v3/middleware/proxy":       {"New", "WithTlsConfig"},
	"github.com/gofiber/fiber/v3/middleware/session":     {"Source", "SourceCookie", "SourceHeader", "SourceURLQuery"},
	"github.com/gofiber/fiber/v3/middleware/timeout":     {"NewWithContext"},
}

// reviewRemovedFields lists, by "import path.Type", the fields of the v2 struct that the v3 struct no longer has.
var reviewRemovedFields = map[string][]string{
	"github.com/gofiber/fiber/v3.Config":                        {"CompressedFileSuffix", "DisableStartupMessage", "ETag", "EnablePrintRoutes", "EnableTrustedProxyCheck", "Network", "Prefork", "TrustedProxies"},
	"github.com/gofiber/fiber/v3/middleware/basicauth.Config":   {"ContextPassword", "ContextUsername"},
	"github.com/gofiber/fiber/v3/middleware/cache.Config":       {"CacheControl", "Key", "Store"},
	"github.com/gofiber/fiber/v3/middleware/csrf.Config":        {"ContextKey", "Cookie", "CookieExpires", "Expiration", "HandlerContextKey", "KeyLookup", "SessionKey", "TokenLookup"},
	"github.com/gofiber/fiber/v3/middleware/envvar.Config":      {"ExcludeVars"},
	"github.com/gofiber/fiber/v3/middleware/healthcheck.Config": {"LivenessEndpoint", "LivenessProbe", "ReadinessEndpoint", "ReadinessProbe"},
	"github.com/gofiber/fiber/v3/middleware/keyauth.Config":     {"AuthScheme", "ContextKey", "KeyLookup"},
	"github.com/gofiber/fiber/v3/middleware/limiter.Config":     {"Duration", "Key", "Store"},
	"github.com/gofiber/fiber/v3/middleware/logger.Config":      {"Output"},
	"github.com/gofiber/fiber/v3/middleware/proxy.Config":       {"TlsConfig"},
	"github.com/gofiber/fiber/v3/middleware/requestid.Config":   {"ContextKey"},
	"github.com/gofiber/fiber/v3/middleware/session.Config":     {"CookieName", "Expiration", "KeyLookup"},
}

// reviewRemovedMethods lists, by "import path.Type", the methods of the v2 type that the v3 type no longer has.
var reviewRemovedMethods = map[string][]string{
	"github.com/gofiber/fiber/v3.App":    {"ListenMutualTLS", "ListenMutualTLSWithCertificate", "ListenTLS", "ListenTLSWithCertificate", "Mount", "Static"},
	"github.com/gofiber/fiber/v3.Ctx":    {"AllParams", "BodyParser", "CookieParser", "ParamsInt", "ParamsParser", "QueryBool", "QueryFloat", "QueryInt", "QueryParser", "RedirectBack", "RedirectToRoute", "ReqHeaderParser", "SetUserContext", "UserContext"},
	"github.com/gofiber/fiber/v3.Group":  {"Mount", "Static"},
	"github.com/gofiber/fiber/v3.Hooks":  {"OnShutdown"},
	"github.com/gofiber/fiber/v3.Router": {"Mount", "Static"},
}
