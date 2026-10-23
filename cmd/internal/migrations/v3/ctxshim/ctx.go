// Package ctxshim is the source of ctx.go of the package
// <module>/internal/fibermigrate, the file that "fiber migrate" adds to a module that
// hands a fiber.Ctx to an interface of its own which mirrors methods of the context of
// Fiber v2 that v3 removed (ParamsInt, QueryInt, BodyParser, UserContext, ...).
//
// The package exists so that the code is built, vetted, linted and tested together
// with the CLI. The migration embeds ctx.go and writes everything below the package
// clause into the module, with the package clause replaced. The file must compile on
// its own and import nothing but the standard library and Fiber, and its unexported top
// level names start with legacy, because the files of the other migrations share the
// package (module_shim_collision_internal_test.go checks it).
package ctxshim

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v3"
)

// LegacyCtx is a fiber.Ctx that also has the methods of the context of Fiber v2 which
// Fiber v3 removed, behaving as they did in v2 (the parsers are the binders of v3). A
// LegacyCtx is a fiber.Ctx too, so it can go where one is required.
type LegacyCtx struct {
	fiber.Ctx
}

// AllParams returns the route parameters by name.
func (c LegacyCtx) AllParams() map[string]string {
	params := make(map[string]string, len(c.Route().Params))
	for _, name := range c.Route().Params {
		params[name] = c.Params(name)
	}
	return params
}

// BodyParser binds the body of the request to out.
func (c LegacyCtx) BodyParser(out any) error { return c.Bind().Body(out) }

// CookieParser binds the cookies of the request to out.
func (c LegacyCtx) CookieParser(out any) error { return c.Bind().Cookie(out) }

// ParamsInt returns the route parameter as an int: the first default and no error when it
// is no integer and there is a default, 0 and the error when there is none.
func (c LegacyCtx) ParamsInt(key string, defaultValue ...int) (int, error) {
	value, err := strconv.Atoi(c.Params(key))
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0], nil
		}
		return 0, fmt.Errorf("failed to convert: %w", err)
	}
	return value, nil
}

// ParamsParser binds the route parameters to out.
func (c LegacyCtx) ParamsParser(out any) error { return c.Bind().URI(out) }

// QueryBool returns the query parameter as a bool.
func (c LegacyCtx) QueryBool(key string, defaultValue ...bool) bool {
	return fiber.Query[bool](c.Ctx, key, defaultValue...)
}

// QueryFloat returns the query parameter as a float64: the first default, or 0 when there is
// none, if it is no number that strconv.ParseFloat reads (it reads inf, nan, hexadecimal numbers
// and underscores between the digits, which fiber.Query[float64] does not).
func (c LegacyCtx) QueryFloat(key string, defaultValue ...float64) float64 {
	value, err := strconv.ParseFloat(c.Query(key), 64)
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return value
}

// QueryInt returns the query parameter as an int.
func (c LegacyCtx) QueryInt(key string, defaultValue ...int) int {
	return fiber.Query[int](c.Ctx, key, defaultValue...)
}

// QueryParser binds the query string of the request to out.
func (c LegacyCtx) QueryParser(out any) error { return c.Bind().Query(out) }

// RedirectBack redirects to the Referer of the request, or else to the fallback. The
// status is the first of the ones given, 302 Found when there is none.
func (c LegacyCtx) RedirectBack(fallback string, status ...int) error {
	code := fiber.StatusFound
	if len(status) > 0 {
		code = status[0]
	}
	return c.Redirect().Status(code).Back(fallback)
}

// RedirectToRoute redirects to the route with the name; the "queries" entry of the params
// is the query string. The status is the first of the ones given, 302 Found when there is
// none.
func (c LegacyCtx) RedirectToRoute(routeName string, params fiber.Map, status ...int) error {
	code := fiber.StatusFound
	if len(status) > 0 {
		code = status[0]
	}
	cfg := fiber.RedirectConfig{Params: params}
	if queries, ok := params["queries"].(map[string]string); ok {
		cfg.Queries = queries
	}
	return c.Redirect().Status(code).Route(routeName, cfg)
}

// ReqHeaderParser binds the headers of the request to out.
func (c LegacyCtx) ReqHeaderParser(out any) error { return c.Bind().Header(out) }

// SetUserContext sets the context of the request.
func (c LegacyCtx) SetUserContext(ctx context.Context) { c.SetContext(ctx) }

// UserContext returns the context of the request.
func (c LegacyCtx) UserContext() context.Context { return c.Context() }
