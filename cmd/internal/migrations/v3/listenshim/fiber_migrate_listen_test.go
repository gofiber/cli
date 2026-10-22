package listenshim

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/fiber/v3"
)

func Test_fiberMigrateListenConfig(t *testing.T) {
	t.Parallel()

	all := fiber.ListenConfig{
		EnablePrefork:         true,
		ListenerNetwork:       fiber.NetworkTCP6,
		DisableStartupMessage: true,
		EnablePrintRoutes:     true,
	}

	tests := []struct {
		name     string
		recorded *fiber.ListenConfig
		explicit []fiber.ListenConfig
		want     fiber.ListenConfig
	}{
		{name: "an app without settings and no config", want: fiber.ListenConfig{}},
		{
			name:     "an app without settings keeps the config it is given",
			explicit: []fiber.ListenConfig{{CertFile: "cert.pem", ShutdownTimeout: time.Second}},
			want:     fiber.ListenConfig{CertFile: "cert.pem", ShutdownTimeout: time.Second},
		},
		{name: "the settings of the app", recorded: &all, want: all},
		{
			name:     "the settings of the app are added to a config",
			recorded: &fiber.ListenConfig{EnablePrefork: true, ListenerNetwork: fiber.NetworkTCP4},
			explicit: []fiber.ListenConfig{{CertFile: "cert.pem", CertKeyFile: "key.pem", ShutdownTimeout: time.Minute}},
			want: fiber.ListenConfig{
				CertFile: "cert.pem", CertKeyFile: "key.pem", ShutdownTimeout: time.Minute,
				EnablePrefork: true, ListenerNetwork: fiber.NetworkTCP4,
			},
		},
		{
			name:     "a network the config names wins",
			recorded: &fiber.ListenConfig{ListenerNetwork: fiber.NetworkTCP4},
			explicit: []fiber.ListenConfig{{ListenerNetwork: fiber.NetworkTCP6}},
			want:     fiber.ListenConfig{ListenerNetwork: fiber.NetworkTCP6},
		},
		{
			name:     "a true setting wins over a false one",
			recorded: &fiber.ListenConfig{DisableStartupMessage: true},
			explicit: []fiber.ListenConfig{{DisableStartupMessage: false, EnablePrintRoutes: true}},
			want:     fiber.ListenConfig{DisableStartupMessage: true, EnablePrintRoutes: true},
		},
		{
			name:     "a setting of the config stays when nothing was recorded for it",
			recorded: &fiber.ListenConfig{},
			explicit: []fiber.ListenConfig{{EnablePrefork: true}},
			want:     fiber.ListenConfig{EnablePrefork: true},
		},
		{
			name:     "only the first config counts, as for Listen",
			recorded: &fiber.ListenConfig{EnablePrintRoutes: true},
			explicit: []fiber.ListenConfig{{CertFile: "a.pem"}, {CertFile: "b.pem"}},
			want:     fiber.ListenConfig{CertFile: "a.pem", EnablePrintRoutes: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var app *fiber.App
			if tt.recorded != nil {
				app = fiberMigrateNew(fiber.Config{}, *tt.recorded)
			} else {
				app = fiber.New()
			}

			assert.Equal(t, tt.want, fiberMigrateListenConfig(app, tt.explicit...))
		})
	}

	t.Run("no app", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, fiber.ListenConfig{CertFile: "a.pem"}, fiberMigrateListenConfig(nil, fiber.ListenConfig{CertFile: "a.pem"}))
		assert.Equal(t, fiber.ListenConfig{}, fiberMigrateListenConfig(nil))
	})

	t.Run("the apps keep their own settings", func(t *testing.T) {
		t.Parallel()

		first := fiberMigrateNew(fiber.Config{}, fiber.ListenConfig{EnablePrefork: true})
		second := fiberMigrateNew(fiber.Config{}, fiber.ListenConfig{DisableStartupMessage: true})
		plain := fiber.New()

		assert.Equal(t, fiber.ListenConfig{EnablePrefork: true}, fiberMigrateListenConfig(first))
		assert.Equal(t, fiber.ListenConfig{DisableStartupMessage: true}, fiberMigrateListenConfig(second))
		assert.Equal(t, fiber.ListenConfig{}, fiberMigrateListenConfig(plain))
		runtime.KeepAlive(first)
		runtime.KeepAlive(second)
	})

	t.Run("the settings are copied when the app is made", func(t *testing.T) {
		t.Parallel()

		lc := fiber.ListenConfig{EnablePrintRoutes: true}
		app := fiberMigrateNew(fiber.Config{}, lc)
		lc.EnablePrintRoutes = false
		lc.EnablePrefork = true

		assert.Equal(t, fiber.ListenConfig{EnablePrintRoutes: true}, fiberMigrateListenConfig(app))
	})
}

func Test_fiberMigrateNew_ConfigIsFibers(t *testing.T) {
	t.Parallel()

	app := fiberMigrateNew(fiber.Config{AppName: "demo", ServerHeader: "x"}, fiber.ListenConfig{})

	assert.Equal(t, "demo", app.Config().AppName)
	assert.Equal(t, "x", app.Config().ServerHeader)
}

// The registry must not keep an app alive: it is keyed by weak pointers and
// forgets an app that was collected.
func Test_fiberMigrateNew_DoesNotKeepAppsAlive(t *testing.T) {
	t.Parallel()

	const apps = 50
	keys := func() []weak.Pointer[fiber.App] {
		keys := make([]weak.Pointer[fiber.App], 0, apps)
		for range apps {
			app := fiberMigrateNew(fiber.Config{}, fiber.ListenConfig{EnablePrefork: true})
			key := weak.Make(app)
			_, found := fiberMigrateListenApps.Load(key)
			require.True(t, found, "the app is recorded while it lives")
			keys = append(keys, key)
		}
		return keys
	}()

	require.Eventually(t, func() bool {
		runtime.GC()
		for _, key := range keys {
			if _, found := fiberMigrateListenApps.Load(key); found {
				return false
			}
		}
		return true
	}, 10*time.Second, 10*time.Millisecond, "the registry forgets the apps that were collected")
	for _, key := range keys {
		assert.Nil(t, key.Value(), "the app was collected")
	}
}

func Test_fiberMigrateNew_Concurrent(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for range 50 {
				app := fiberMigrateNew(fiber.Config{}, fiber.ListenConfig{EnablePrintRoutes: i%2 == 0})
				got := fiberMigrateListenConfig(app)
				assert.Equal(t, i%2 == 0, got.EnablePrintRoutes)
			}
		})
	}
	wg.Wait()
}

// serve starts the app with start, waits until it answers requests, and shuts it
// down.
func serve(t *testing.T, app *fiber.App, start func() error) {
	t.Helper()

	ports := make(chan string, 1)
	app.Hooks().OnListen(func(data fiber.ListenData) error {
		ports <- data.Port
		return nil
	})

	errs := make(chan error, 1)
	go func() { errs <- start() }()

	var port string
	select {
	case port = <-ports:
	case err := <-errs:
		require.NoError(t, err, "the app stopped before it listened")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the app did not start")
	}

	// the listen hooks run before the server accepts connections: shutting down
	// earlier than the first answer would leave it serving
	client := &http.Client{Timeout: time.Second}
	require.Eventually(t, func() bool {
		resp, err := client.Get("http://127.0.0.1:" + port + "/") //nolint:noctx // a test against a server of its own
		if err != nil {
			return false
		}
		return resp.Body.Close() == nil
	}, 10*time.Second, 10*time.Millisecond, "the app does not answer")

	require.NoError(t, app.ShutdownWithTimeout(time.Second))
	require.NoError(t, <-errs)
}

// capture returns what fn writes to the standard output. The startup message of
// an app goes there, so the test cannot run in parallel with others.
func capture(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	out := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r) //nolint:errcheck // a pipe that reads to its end
		out <- string(b)
	}()

	fn()

	os.Stdout = stdout
	require.NoError(t, w.Close())
	return <-out
}

func listener(t *testing.T) net.Listener {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return ln
}

func Test_fiberMigrateListenConfig_StartupOutput(t *testing.T) { //nolint:paralleltest // the startup message goes to the standard output, which the test redirects
	tests := []struct {
		name      string
		recorded  fiber.ListenConfig
		banner    bool
		routeInfo bool
	}{
		{name: "nothing recorded", banner: true},
		{name: "quiet", recorded: fiber.ListenConfig{DisableStartupMessage: true}},
		{name: "routes", recorded: fiber.ListenConfig{EnablePrintRoutes: true}, banner: true, routeInfo: true},
		{name: "quiet with routes", recorded: fiber.ListenConfig{DisableStartupMessage: true, EnablePrintRoutes: true}, routeInfo: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiberMigrateNew(fiber.Config{}, tt.recorded)
			app.Get("/shim-route", func(c fiber.Ctx) error { return c.SendString("ok") })
			ln := listener(t)

			out := capture(t, func() {
				serve(t, app, func() error { return app.Listener(ln, fiberMigrateListenConfig(app)) })
			})

			assert.Equal(t, tt.banner, strings.Contains(out, "Server started on"), out)
			assert.Equal(t, tt.routeInfo, strings.Contains(out, "/shim-route"), out)
		})
	}
}

func Test_fiberMigrateListen_Helpers(t *testing.T) { //nolint:paralleltest // the startup message goes to the standard output, which the test redirects
	t.Run("listen", func(t *testing.T) {
		app := fiberMigrateNew(fiber.Config{}, fiber.ListenConfig{DisableStartupMessage: true})

		out := capture(t, func() {
			serve(t, app, func() error { return fiberMigrateListen(app, "127.0.0.1:0") })
		})

		assert.Empty(t, out)
	})

	t.Run("listen with a config", func(t *testing.T) {
		app := fiberMigrateNew(fiber.Config{}, fiber.ListenConfig{EnablePrintRoutes: true})
		app.Get("/shim-route", func(c fiber.Ctx) error { return c.SendString("ok") })

		out := capture(t, func() {
			serve(t, app, func() error {
				return fiberMigrateListen(app, "127.0.0.1:0", fiber.ListenConfig{DisableStartupMessage: true})
			})
		})

		assert.NotContains(t, out, "Server started on")
		assert.Contains(t, out, "/shim-route")
	})

	t.Run("listener", func(t *testing.T) {
		app := fiberMigrateNew(fiber.Config{}, fiber.ListenConfig{DisableStartupMessage: true})
		ln := listener(t)

		out := capture(t, func() {
			serve(t, app, func() error { return fiberMigrateListener(app, ln) })
		})

		assert.Empty(t, out)
	})
}
