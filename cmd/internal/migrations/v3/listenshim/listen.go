// This file was added by "fiber migrate": the listener settings of an app.
//
// Fiber v3 moved Prefork, Network, DisableStartupMessage and EnablePrintRoutes
// from fiber.Config to the fiber.ListenConfig that Listen takes. The code of some
// apps of this module sets them away from the call that starts the app, in a
// condition, from a value that only a function knows, or in another package, so
// they travel with the app instead: NewApp creates the app and records them, and
// ListenConfig, Listen and Listener add them to the call that starts it. Config is
// the fiber.Config of Fiber v2, for code that hands a config around and creates
// the app somewhere else, and New creates the app of one.
//
// Once you know where an app starts, pass a fiber.ListenConfig to its Listen
// call yourself, remove the calls of this package, and delete the package.

package listenshim

import (
	"net"
	"runtime"
	"sync"
	"weak"

	"github.com/gofiber/fiber/v3"
)

// listenSettings are the settings fiber.Config had in v2 that fiber.ListenConfig
// has in v3.
type listenSettings struct {
	network string
	prefork bool
	quiet   bool
	routes  bool
}

// listenApps holds the settings of the apps that NewApp created, by weak pointer
// to the app: it must not keep an app alive.
var listenApps sync.Map // weak.Pointer[fiber.App] -> listenSettings

// listenOf reads the settings from a fiber.ListenConfig.
func listenOf(lc fiber.ListenConfig) listenSettings {
	return listenSettings{
		network: lc.ListenerNetwork,
		prefork: lc.EnablePrefork,
		quiet:   lc.DisableStartupMessage,
		routes:  lc.EnablePrintRoutes,
	}
}

// listenMerge adds the settings to the config of a call: a setting that the config
// makes keeps its value, and a true setting is never turned off, because v2 had no
// way to say otherwise.
func listenMerge(cfg fiber.ListenConfig, s listenSettings) fiber.ListenConfig {
	cfg.EnablePrefork = cfg.EnablePrefork || s.prefork
	cfg.DisableStartupMessage = cfg.DisableStartupMessage || s.quiet
	cfg.EnablePrintRoutes = cfg.EnablePrintRoutes || s.routes
	if cfg.ListenerNetwork == "" {
		cfg.ListenerNetwork = s.network
	}
	return cfg
}

// listenRecorded returns the settings that NewApp recorded for the app.
func listenRecorded(app *fiber.App) listenSettings {
	if app == nil {
		return listenSettings{}
	}
	if value, found := listenApps.Load(weak.Make(app)); found {
		if recorded, ok := value.(listenSettings); ok {
			return recorded
		}
	}
	return listenSettings{}
}

// NewApp is fiber.New(cfg) for an app whose listener settings are lc: they are
// recorded for the app, as v2 kept them in its config, and ListenConfig finds them
// again where the app is started.
func NewApp(cfg fiber.Config, lc fiber.ListenConfig) *fiber.App {
	app := fiber.New(cfg)

	if settings := listenOf(lc); settings != (listenSettings{}) {
		key := weak.Make(app)
		listenApps.Store(key, settings)
		runtime.AddCleanup(app, func(key weak.Pointer[fiber.App]) { listenApps.Delete(key) }, key)
	}
	return app
}

// Config is the fiber.Config of Fiber v2: the Config of v3 and the four settings
// that v3 moved to the fiber.ListenConfig of Listen. The fields of the embedded
// Config are promoted, so reading and assigning them works as before; a composite
// literal names the embedded field: Config{Config: fiber.Config{AppName: "x"},
// Prefork: true}.
type Config struct {
	fiber.Config
	// Network is "tcp", "tcp4" or "tcp6".
	Network string `json:"network"`
	// Prefork starts the app in one process per CPU.
	Prefork bool `json:"prefork"`
	// DisableStartupMessage hides the banner Listen prints.
	DisableStartupMessage bool `json:"disable_startup_message"`
	// EnablePrintRoutes prints the routes after the banner.
	EnablePrintRoutes bool `json:"enable_print_routes"`
}

// Settings returns the four settings of the config as a fiber.ListenConfig.
func (c Config) Settings() fiber.ListenConfig {
	return fiber.ListenConfig{
		EnablePrefork:         c.Prefork,
		ListenerNetwork:       c.Network,
		DisableStartupMessage: c.DisableStartupMessage,
		EnablePrintRoutes:     c.EnablePrintRoutes,
	}
}

// New is fiber.New(config) for the Config of v2. As in v2, only the first config is
// used.
func New(config ...Config) *fiber.App {
	if len(config) == 0 {
		return fiber.New()
	}
	return NewApp(config[0].Config, config[0].Settings())
}

// ConfigOf is app.Config() with the settings that v2 kept in it: the ones that
// NewApp recorded for the app, and the network v2 filled in when there was none.
func ConfigOf(app *fiber.App) Config {
	settings := listenRecorded(app)
	if settings.network == "" {
		settings.network = fiber.NetworkTCP4
	}
	return Config{
		Config:                app.Config(),
		Network:               settings.network,
		Prefork:               settings.prefork,
		DisableStartupMessage: settings.quiet,
		EnablePrintRoutes:     settings.routes,
	}
}

// ListenConfig returns the config to start app with: lc, if there is one, plus the
// settings that NewApp recorded for the app. Only the first lc counts, as for
// Listen.
func ListenConfig(app *fiber.App, lc ...fiber.ListenConfig) fiber.ListenConfig {
	var cfg fiber.ListenConfig
	if len(lc) > 0 {
		cfg = lc[0]
	}
	return listenMerge(cfg, listenRecorded(app))
}

// Listen is app.Listen(addr, lc...) with the settings recorded for app. It stands
// where the receiver is an expression, so that it is evaluated once.
func Listen(app *fiber.App, addr string, lc ...fiber.ListenConfig) error {
	return app.Listen(addr, ListenConfig(app, lc...))
}

// Listener is app.Listener(ln, lc...) with the settings recorded for app.
func Listener(app *fiber.App, ln net.Listener, lc ...fiber.ListenConfig) error {
	return app.Listener(ln, ListenConfig(app, lc...))
}

// ListenFunc is the method value app.Listen of Fiber v2, a function of the
// address, with the settings recorded for app.
func ListenFunc(app *fiber.App) func(addr string) error {
	return func(addr string) error { return Listen(app, addr) }
}

// ListenerFunc is the method value app.Listener of Fiber v2, a function of the
// listener, with the settings recorded for app.
func ListenerFunc(app *fiber.App) func(ln net.Listener) error {
	return func(ln net.Listener) error { return Listener(app, ln) }
}
