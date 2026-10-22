package listenshim

import (
	"context"
	"encoding/json"
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

func Test_ListenConfig(t *testing.T) {
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
				app = NewApp(fiber.Config{}, *tt.recorded)
			} else {
				app = fiber.New()
			}

			assert.Equal(t, tt.want, ListenConfig(app, tt.explicit...))
			runtime.KeepAlive(app)
		})
	}

	t.Run("no app", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, fiber.ListenConfig{CertFile: "a.pem"}, ListenConfig(nil, fiber.ListenConfig{CertFile: "a.pem"}))
		assert.Equal(t, fiber.ListenConfig{}, ListenConfig(nil))
	})

	t.Run("the apps keep their own settings", func(t *testing.T) {
		t.Parallel()

		first := NewApp(fiber.Config{}, fiber.ListenConfig{EnablePrefork: true})
		second := NewApp(fiber.Config{}, fiber.ListenConfig{DisableStartupMessage: true})
		plain := fiber.New()

		assert.Equal(t, fiber.ListenConfig{EnablePrefork: true}, ListenConfig(first))
		assert.Equal(t, fiber.ListenConfig{DisableStartupMessage: true}, ListenConfig(second))
		assert.Equal(t, fiber.ListenConfig{}, ListenConfig(plain))
		runtime.KeepAlive(first)
		runtime.KeepAlive(second)
	})

	t.Run("the settings are copied when the app is made", func(t *testing.T) {
		t.Parallel()

		lc := fiber.ListenConfig{EnablePrintRoutes: true}
		app := NewApp(fiber.Config{}, lc)
		lc.EnablePrintRoutes = false
		lc.EnablePrefork = true

		assert.Equal(t, fiber.ListenConfig{EnablePrintRoutes: true}, ListenConfig(app))
		runtime.KeepAlive(app)
	})
}

func Test_NewApp_ConfigIsFibers(t *testing.T) {
	t.Parallel()

	app := NewApp(fiber.Config{AppName: "demo", ServerHeader: "x"}, fiber.ListenConfig{})

	assert.Equal(t, "demo", app.Config().AppName)
	assert.Equal(t, "x", app.Config().ServerHeader)
}

// The settings of the config of v2: the same fields, the same meaning, and the
// other fields of the config are those of v3.
func Test_Config(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Config:                fiber.Config{AppName: "demo"},
		Network:               fiber.NetworkTCP4,
		Prefork:               true,
		DisableStartupMessage: true,
		EnablePrintRoutes:     true,
	}

	t.Run("the settings", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, fiber.ListenConfig{
			ListenerNetwork:       fiber.NetworkTCP4,
			EnablePrefork:         true,
			DisableStartupMessage: true,
			EnablePrintRoutes:     true,
		}, cfg.Settings())
		assert.Equal(t, fiber.ListenConfig{}, Config{}.Settings())
	})

	t.Run("the fields of fiber.Config are promoted", func(t *testing.T) {
		t.Parallel()

		c := cfg
		c.AppName = "other"
		c.Prefork = false

		assert.Equal(t, "other", c.AppName)
		assert.Equal(t, fiber.ListenConfig{
			ListenerNetwork:       fiber.NetworkTCP4,
			DisableStartupMessage: true,
			EnablePrintRoutes:     true,
		}, c.Settings())
	})

	t.Run("New", func(t *testing.T) {
		t.Parallel()

		app := New(cfg, Config{Config: fiber.Config{AppName: "second"}, Prefork: false})

		assert.Equal(t, "demo", app.Config().AppName, "only the first config counts, as in v2")
		assert.Equal(t, cfg.Settings(), ListenConfig(app))
		runtime.KeepAlive(app)
	})

	t.Run("New without a config", func(t *testing.T) {
		t.Parallel()

		app := New()

		assert.Equal(t, fiber.ListenConfig{}, ListenConfig(app))
		assert.Empty(t, app.Config().AppName)
	})

	t.Run("ConfigOf gives back what the app was made with", func(t *testing.T) {
		t.Parallel()

		app := New(cfg)
		got := ConfigOf(app)

		assert.Equal(t, cfg.Network, got.Network)
		assert.Equal(t, cfg.Prefork, got.Prefork)
		assert.Equal(t, cfg.DisableStartupMessage, got.DisableStartupMessage)
		assert.Equal(t, cfg.EnablePrintRoutes, got.EnablePrintRoutes)
		assert.Equal(t, "demo", got.AppName)
		runtime.KeepAlive(app)
	})

	t.Run("ConfigOf an app that is not recorded", func(t *testing.T) {
		t.Parallel()

		got := ConfigOf(fiber.New(fiber.Config{AppName: "plain"}))

		assert.Equal(t, "plain", got.AppName)
		assert.Equal(t, fiber.NetworkTCP4, got.Network, "the network v2 filled in")
		assert.Equal(t, fiber.ListenConfig{ListenerNetwork: fiber.NetworkTCP4}, got.Settings())
	})
}

// The registry must not keep an app alive: it is keyed by weak pointers and
// forgets an app that was collected.
func Test_NewApp_DoesNotKeepAppsAlive(t *testing.T) {
	t.Parallel()

	const apps = 50
	keys := func() []weak.Pointer[fiber.App] {
		keys := make([]weak.Pointer[fiber.App], 0, apps)
		for range apps {
			app := NewApp(fiber.Config{}, fiber.ListenConfig{EnablePrefork: true})
			key := weak.Make(app)
			_, found := listenApps.Load(key)
			require.True(t, found, "the app is recorded while it lives")
			keys = append(keys, key)
		}
		return keys
	}()

	require.Eventually(t, func() bool {
		runtime.GC()
		for _, key := range keys {
			if _, found := listenApps.Load(key); found {
				return false
			}
		}
		return true
	}, 10*time.Second, 10*time.Millisecond, "the registry forgets the apps that were collected")
	for _, key := range keys {
		assert.Nil(t, key.Value(), "the app was collected")
	}
}

// An app without settings costs the registry nothing.
func Test_NewApp_RecordsSettingsOnly(t *testing.T) {
	t.Parallel()

	app := NewApp(fiber.Config{}, fiber.ListenConfig{CertFile: "cert.pem"})
	_, found := listenApps.Load(weak.Make(app))

	assert.False(t, found, "a setting of ListenConfig that is not one of the four is not the registry's")
	runtime.KeepAlive(app)
}

func Test_NewApp_Concurrent(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for range 50 {
				app := NewApp(fiber.Config{}, fiber.ListenConfig{EnablePrintRoutes: i%2 == 0})
				got := ListenConfig(app)
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

func newListener(t *testing.T) net.Listener {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return ln
}

func Test_ListenConfig_StartupOutput(t *testing.T) { //nolint:paralleltest // the startup message goes to the standard output, which the test redirects
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
			app := NewApp(fiber.Config{}, tt.recorded)
			app.Get("/shim-route", func(c fiber.Ctx) error { return c.SendString("ok") })
			ln := newListener(t)

			out := capture(t, func() {
				serve(t, app, func() error { return app.Listener(ln, ListenConfig(app)) })
			})

			assert.Equal(t, tt.banner, strings.Contains(out, "Server started on"), out)
			assert.Equal(t, tt.routeInfo, strings.Contains(out, "/shim-route"), out)
		})
	}
}

func Test_Listen_Helpers(t *testing.T) { //nolint:paralleltest // the startup message goes to the standard output, which the test redirects
	t.Run("listen", func(t *testing.T) {
		app := NewApp(fiber.Config{}, fiber.ListenConfig{DisableStartupMessage: true})

		out := capture(t, func() {
			serve(t, app, func() error { return Listen(app, "127.0.0.1:0") })
		})

		assert.Empty(t, out)
	})

	t.Run("listen with a config", func(t *testing.T) {
		app := NewApp(fiber.Config{}, fiber.ListenConfig{EnablePrintRoutes: true})
		app.Get("/shim-route", func(c fiber.Ctx) error { return c.SendString("ok") })

		out := capture(t, func() {
			serve(t, app, func() error {
				return Listen(app, "127.0.0.1:0", fiber.ListenConfig{DisableStartupMessage: true})
			})
		})

		assert.NotContains(t, out, "Server started on")
		assert.Contains(t, out, "/shim-route")
	})

	t.Run("listener", func(t *testing.T) {
		app := NewApp(fiber.Config{}, fiber.ListenConfig{DisableStartupMessage: true})
		ln := newListener(t)

		out := capture(t, func() {
			serve(t, app, func() error { return Listener(app, ln) })
		})

		assert.Empty(t, out)
	})

	t.Run("the method value of Listen", func(t *testing.T) {
		app := New(Config{DisableStartupMessage: true})
		start := ListenFunc(app)

		out := capture(t, func() {
			serve(t, app, func() error { return start("127.0.0.1:0") })
		})

		assert.Empty(t, out)
	})

	t.Run("the method value of Listener", func(t *testing.T) {
		app := New(Config{DisableStartupMessage: true})
		start := ListenerFunc(app)
		ln := newListener(t)

		out := capture(t, func() {
			serve(t, app, func() error { return start(ln) })
		})

		assert.Empty(t, out)
	})
}

// The config reads JSON the way the config of Fiber v2 did, for code that loads it from a
// file.
func Test_Config_JSON(t *testing.T) {
	t.Parallel()

	var cfg Config
	require.NoError(t, json.Unmarshal([]byte(`{"prefork": true, "disable_startup_message": true, "enable_print_routes": true, "network": "tcp6", "app_name": "demo"}`), &cfg))

	assert.True(t, cfg.Prefork)
	assert.True(t, cfg.DisableStartupMessage)
	assert.True(t, cfg.EnablePrintRoutes)
	assert.Equal(t, "tcp6", cfg.Network)
	assert.Equal(t, "demo", cfg.AppName)
}
