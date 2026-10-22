package v3

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_importsProxyBase(t *testing.T) {
	// Not parallel: it sets an environment variable.
	tests := []struct {
		name     string
		goproxy  string
		want     string
		unset    bool
		wantOkay bool
	}{
		{name: "not set", unset: true, want: "https://proxy.golang.org", wantOkay: true},
		{name: "empty", goproxy: "", want: "https://proxy.golang.org", wantOkay: true},
		{name: "off", goproxy: "off"},
		{name: "direct only", goproxy: "direct"},
		{name: "a file proxy is not usable", goproxy: "file:///var/cache/goproxy"},
		{name: "the first usable entry", goproxy: "off,https://proxy.example.com/,direct", want: "https://proxy.example.com", wantOkay: true},
		{name: "fallback separator", goproxy: "file:///x|http://localhost:3000", want: "http://localhost:3000", wantOkay: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOPROXY", tc.goproxy)
			if tc.unset {
				require.NoError(t, os.Unsetenv("GOPROXY"))
			}

			got, ok := importsProxyBase()
			assert.Equal(t, tc.wantOkay, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_importsLookupOnProxy(t *testing.T) {
	// Not parallel: it sets an environment variable.
	const (
		info   = `{"Version":"v1.1.0","Time":"2025-05-01T10:00:00Z"}`
		latest = `{"Version":"v1.4.0","Time":"2026-03-01T10:00:00Z"}`
	)

	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/github.com/gofiber/storage/newdb/@v/v1.1.0.info":
			fmt.Fprint(w, info)
		case "/github.com/gofiber/storage/newdb/@latest":
			fmt.Fprint(w, latest)
		case "/github.com/gofiber/storage/broken/@v/v1.0.0.info":
			fmt.Fprint(w, "not json")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GOPROXY", srv.URL)

	got, err := importsLookupOnProxy("github.com/gofiber/storage/newdb", "v1.1.0")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2025, time.May, 1, 10, 0, 0, 0, time.UTC), got.published)
	assert.Equal(t, "v1.4.0", got.latest)
	assert.Equal(t, []string{
		"/github.com/gofiber/storage/newdb/@v/v1.1.0.info",
		"/github.com/gofiber/storage/newdb/@latest",
	}, requests)

	_, err = importsLookupOnProxy("github.com/gofiber/storage/missing", "v1.0.0")
	require.ErrorContains(t, err, "unexpected status 404")

	_, err = importsLookupOnProxy("github.com/gofiber/storage/broken", "v1.0.0")
	require.ErrorContains(t, err, "parse")

	t.Setenv("GOPROXY", "off")
	_, err = importsLookupOnProxy("github.com/gofiber/storage/newdb", "v1.1.0")
	require.ErrorIs(t, err, errImportsNoProxy)
}
