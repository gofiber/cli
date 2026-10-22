package healthshim

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/fiber/v3"
)

// get sends a request to the app and returns the status and the body.
func get(t *testing.T, app *fiber.App, method, target string, headers ...string) (int, string) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// other answers what nothing else did.
func other(c fiber.Ctx) error { return c.Status(fiber.StatusTeapot).SendString("other " + c.Path()) }

func Test_Health_DefaultConfig(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(Health())
	app.Use(other)

	for target, want := range map[string]int{"/livez": 200, "/readyz": 200, "/livez/": 200, "/readyz/": 200, "/other": 418, "/live": 418, "/": 418} {
		status, _ := get(t, app, http.MethodGet, target)
		assert.Equal(t, want, status, target)
	}
}

func Test_Health_OnlyGetIsAProbe(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(Health())
	app.Use(other)

	// v2 handed the other methods to the next handler, HEAD included
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		status, _ := get(t, app, method, "/livez")
		assert.Equal(t, 418, status, method)
	}
}

func Test_Health_ProbesAndEndpoints(t *testing.T) {
	t.Parallel()

	ready := false
	app := fiber.New()
	app.Use(Health(HealthConfig{
		LivenessProbe:     func(c fiber.Ctx) bool { return c.Get("X-Alive") != "no" },
		LivenessEndpoint:  "/alive",
		ReadinessProbe:    func(fiber.Ctx) bool { return ready },
		ReadinessEndpoint: "/ready",
	}))
	app.Use(other)

	status, body := get(t, app, http.MethodGet, "/alive")
	assert.Equal(t, 200, status)
	assert.Equal(t, "OK", body)
	status, body = get(t, app, http.MethodGet, "/alive", "X-Alive", "no")
	assert.Equal(t, 503, status)
	assert.Equal(t, "Service Unavailable", body)

	status, _ = get(t, app, http.MethodGet, "/ready")
	assert.Equal(t, 503, status)
	ready = true
	status, _ = get(t, app, http.MethodGet, "/ready")
	assert.Equal(t, 200, status)

	// the endpoints that were replaced are no longer served
	for _, target := range []string{"/livez", "/readyz"} {
		status, _ = get(t, app, http.MethodGet, target)
		assert.Equal(t, 418, status, target)
	}
}

func Test_Health_NextSkipsTheMiddleware(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(Health(HealthConfig{Next: func(c fiber.Ctx) bool { return c.Query("skip") != "" }}))
	app.Use(other)

	status, _ := get(t, app, http.MethodGet, "/livez")
	assert.Equal(t, 200, status)
	status, _ = get(t, app, http.MethodGet, "/livez?skip=1")
	assert.Equal(t, 418, status)
}

func Test_Health_ReadinessWinsWhenBothEndpointsAreTheSame(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(Health(HealthConfig{
		LivenessProbe:     func(fiber.Ctx) bool { return true },
		ReadinessProbe:    func(fiber.Ctx) bool { return false },
		LivenessEndpoint:  "/same",
		ReadinessEndpoint: "/same",
	}))

	status, _ := get(t, app, http.MethodGet, "/same")
	assert.Equal(t, 503, status)
}

func Test_Health_MountedBelowAPrefix(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use("/internal/", Health())
	api := app.Group("/api")
	api.Use(Health(HealthConfig{LivenessEndpoint: "/up"}))
	app.Use(other)

	for target, want := range map[string]int{
		"/internal/livez": 200, "/internal/readyz": 200, "/internal/livez/": 200,
		"/livez": 418, "/readyz": 418,
		"/api/up": 200, "/api/readyz": 200, "/api/livez": 418, "/up": 418,
	} {
		status, _ := get(t, app, http.MethodGet, target)
		assert.Equal(t, want, status, target)
	}
}

func Test_Health_StrictRouting(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{StrictRouting: true})
	app.Use(Health())
	app.Use(other)

	status, _ := get(t, app, http.MethodGet, "/livez")
	assert.Equal(t, 200, status)
	status, _ = get(t, app, http.MethodGet, "/livez/")
	assert.Equal(t, 418, status, "a trailing slash is another path when the routing is strict")
}

func Test_HealthConfigDefault(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/livez", HealthConfigDefault.LivenessEndpoint)
	assert.Equal(t, "/readyz", HealthConfigDefault.ReadinessEndpoint)
	assert.True(t, HealthConfigDefault.LivenessProbe(nil))
	assert.True(t, HealthConfigDefault.ReadinessProbe(nil))
	assert.Nil(t, HealthConfigDefault.Next)

	// the default config can be changed on a copy
	cfg := HealthConfigDefault
	cfg.ReadinessEndpoint = "/ready"
	app := fiber.New()
	app.Use(Health(cfg))
	app.Use(other)
	status, _ := get(t, app, http.MethodGet, "/ready")
	assert.Equal(t, 200, status)
	status, _ = get(t, app, http.MethodGet, "/readyz")
	assert.Equal(t, 418, status)
}

func Test_Health_AMoreThanOneConfigUsesTheFirst(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(Health(HealthConfig{LivenessEndpoint: "/first"}, HealthConfig{LivenessEndpoint: "/second"}))
	app.Use(other)

	status, _ := get(t, app, http.MethodGet, "/first")
	assert.Equal(t, 200, status)
	status, _ = get(t, app, http.MethodGet, "/second")
	assert.Equal(t, 418, status)
}
