package app

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
)

func TestNewAppHealthEndpoints(t *testing.T) {
	t.Parallel()
	opts := Options{Dir: t.TempDir(), Path: "/", Health: true}
	app := NewApp(opts)

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, healthcheck.LivenessEndpoint, nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestNewAppServeIndex(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("hello"), 0o600)
	require.NoError(t, err)

	opts := Options{Dir: dir, Path: "/", Index: "index.html", Cache: time.Second}
	app := NewApp(opts)

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "hello")
}
