package ctxshim

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/fiber/v3"
)

// mirror is what a project writes to hand the context to its handlers: the methods of
// the context of Fiber v2.
type mirror interface {
	AllParams() map[string]string
	BodyParser(out any) error
	CookieParser(out any) error
	ParamsInt(key string, defaultValue ...int) (int, error)
	ParamsParser(out any) error
	Params(key string, defaultValue ...string) string
	QueryBool(key string, defaultValue ...bool) bool
	QueryFloat(key string, defaultValue ...float64) float64
	QueryInt(key string, defaultValue ...int) int
	QueryParser(out any) error
	RedirectBack(fallback string, status ...int) error
	RedirectToRoute(routeName string, params fiber.Map, status ...int) error
	ReqHeaderParser(out any) error
	SetUserContext(ctx context.Context)
	UserContext() context.Context
}

var (
	_ mirror    = LegacyCtx{}
	_ fiber.Ctx = LegacyCtx{}
)

type form struct {
	Name string `json:"name" form:"name" query:"name" cookie:"name" uri:"name" header:"X-Name"`
}

func run(t *testing.T, app *fiber.App, method, target, body string, headers ...string) (int, string, http.Header) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(out), resp.Header
}

func Test_LegacyCtx_ParamsInt(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/:n", func(c fiber.Ctx) error {
		var m mirror = LegacyCtx{Ctx: c}
		plain, err := m.ParamsInt("n")
		withDefault, errDefault := m.ParamsInt("n", 7, 8)
		out := strings.Join([]string{strconv.Itoa(plain), errText(err), strconv.Itoa(withDefault), errText(errDefault)}, "|")
		return c.SendString(out)
	})

	for target, want := range map[string]string{
		"/5":                   "5||5|",
		"/-5":                  "-5||-5|",
		"/abc":                 `0|failed to convert: strconv.Atoi: parsing "abc": invalid syntax|7|`,
		"/9223372036854775808": `0|failed to convert: strconv.Atoi: parsing "9223372036854775808": value out of range|7|`,
	} {
		status, body, _ := run(t, app, http.MethodGet, target, "")
		assert.Equal(t, http.StatusOK, status, target)
		assert.Equal(t, want, body, target)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func Test_LegacyCtx_Queries(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		m := LegacyCtx{Ctx: c}
		return c.SendString(strings.Join([]string{
			strconv.Itoa(m.QueryInt("i")), strconv.Itoa(m.QueryInt("missing", 4)), strconv.Itoa(m.QueryInt("bad", 9)),
			strconv.FormatFloat(m.QueryFloat("f"), 'g', -1, 64), strconv.FormatFloat(m.QueryFloat("bad", 1.5), 'g', -1, 64),
			strconv.FormatBool(m.QueryBool("b")), strconv.FormatBool(m.QueryBool("missing", true)),
		}, "|"))
	})

	_, body, _ := run(t, app, http.MethodGet, "/?i=12&bad=x&f=2.5&b=true", "")
	assert.Equal(t, "12|4|9|2.5|1.5|true|true", body)
}

func Test_LegacyCtx_Parsers(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Post("/body/:name", func(c fiber.Ctx) error {
		var m mirror = LegacyCtx{Ctx: c}
		var fromBody, fromQuery, fromCookie, fromParams, fromHeader form
		if err := m.BodyParser(&fromBody); err != nil {
			return err
		}
		if err := m.QueryParser(&fromQuery); err != nil {
			return err
		}
		if err := m.CookieParser(&fromCookie); err != nil {
			return err
		}
		if err := m.ParamsParser(&fromParams); err != nil {
			return err
		}
		if err := m.ReqHeaderParser(&fromHeader); err != nil {
			return err
		}
		return c.SendString(strings.Join([]string{fromBody.Name, fromQuery.Name, fromCookie.Name, fromParams.Name, fromHeader.Name}, "|"))
	})

	status, body, _ := run(t, app, http.MethodPost, "/body/uri?name=query", `{"name":"body"}`,
		"Content-Type", "application/json", "Cookie", "name=cookie", "X-Name", "header")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "body|query|cookie|uri|header", body)

	status, _, _ = run(t, app, http.MethodPost, "/body/uri", `{"name":`, "Content-Type", "application/json")
	assert.Equal(t, http.StatusInternalServerError, status, "the error of the parser reaches the handler")
}

func Test_LegacyCtx_Redirects(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/user/:id", func(c fiber.Ctx) error { return c.SendString("user") }).Name("user")
	app.Get("/back", func(c fiber.Ctx) error { return LegacyCtx{Ctx: c}.RedirectBack("/fallback") })
	app.Get("/back301", func(c fiber.Ctx) error {
		return LegacyCtx{Ctx: c}.RedirectBack("/fallback", fiber.StatusMovedPermanently)
	})
	app.Get("/route", func(c fiber.Ctx) error {
		return LegacyCtx{Ctx: c}.RedirectToRoute("user", fiber.Map{"id": 5, "queries": map[string]string{"page": "2"}})
	})
	app.Get("/route303", func(c fiber.Ctx) error {
		return LegacyCtx{Ctx: c}.RedirectToRoute("user", fiber.Map{"id": 6}, fiber.StatusSeeOther)
	})

	for _, tc := range []struct {
		target, referer, location string
		status                    int
	}{
		{"/back", "", "/fallback", http.StatusFound},
		{"/back", "/from", "/from", http.StatusFound},
		{"/back301", "", "/fallback", http.StatusMovedPermanently},
		{"/route", "", "/user/5?page=2", http.StatusFound},
		{"/route303", "", "/user/6", http.StatusSeeOther},
	} {
		var headers []string
		if tc.referer != "" {
			headers = []string{"Referer", tc.referer}
		}
		status, _, header := run(t, app, http.MethodGet, tc.target, "", headers...)
		assert.Equal(t, tc.status, status, tc.target)
		assert.Equal(t, tc.location, header.Get("Location"), tc.target)
	}
}

func Test_LegacyCtx_UserContext(t *testing.T) {
	t.Parallel()

	type key struct{}
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		var m mirror = LegacyCtx{Ctx: c}
		before := m.UserContext().Value(key{}) == nil
		m.SetUserContext(context.WithValue(context.Background(), key{}, "set"))
		return c.SendString(strconv.FormatBool(before) + "|" + m.UserContext().Value(key{}).(string))
	})

	_, body, _ := run(t, app, http.MethodGet, "/", "")
	assert.Equal(t, "true|set", body)
}

func Test_LegacyCtx_AllParams(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/:a/:b", func(c fiber.Ctx) error {
		params := LegacyCtx{Ctx: c}.AllParams()
		return c.SendString(params["a"] + "|" + params["b"] + "|" + strconv.Itoa(len(params)))
	})

	_, body, _ := run(t, app, http.MethodGet, "/x/y", "")
	assert.Equal(t, "x|y|2", body)
}
