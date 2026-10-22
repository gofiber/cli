// This file was added by "fiber migrate": Fiber v3 moved the listener settings of
// fiber.Config (Prefork, Network, DisableStartupMessage and EnablePrintRoutes) to
// the fiber.ListenConfig that Listen takes. The settings of some apps are set by
// code away from the call that starts them, a condition or a value that only a
// function knows, so they travel with the app instead: fiberMigrateNew creates the
// app and records them, and fiberMigrateListenConfig adds them to the config of the
// call that starts it.
//
// Once you know where an app starts, pass a fiber.ListenConfig to its Listen or
// Listener call and delete the calls of this file together with the file.

package listenshim

import (
	"runtime"
	"sync"
	"weak"

	"github.com/gofiber/fiber/v3"
)

// fiberMigrateListenSettings are the settings fiber.Config had in v2 that
// fiber.ListenConfig has in v3.
type fiberMigrateListenSettings struct {
	network string
	prefork bool
	quiet   bool
	routes  bool
}

// fiberMigrateListenApps holds the settings of the apps fiberMigrateNew created,
// by weak pointer to the app: it must not keep an app alive.
var fiberMigrateListenApps sync.Map // weak.Pointer[fiber.App] -> fiberMigrateListenSettings

// fiberMigrateNew is fiber.New(cfg) for an app whose listener settings are lc:
// they are recorded for the app, as v2 kept them in its config, and
// fiberMigrateListenConfig finds them again where the app is started.
func fiberMigrateNew(cfg fiber.Config, lc fiber.ListenConfig) *fiber.App {
	app := fiber.New(cfg)

	key := weak.Make(app)
	fiberMigrateListenApps.Store(key, fiberMigrateListenSettings{
		network: lc.ListenerNetwork,
		prefork: lc.EnablePrefork,
		quiet:   lc.DisableStartupMessage,
		routes:  lc.EnablePrintRoutes,
	})
	runtime.AddCleanup(app, func(key weak.Pointer[fiber.App]) { fiberMigrateListenApps.Delete(key) }, key)

	return app
}

// fiberMigrateListenConfig returns the config to start app with: lc, if there is
// one, plus the settings recorded for the app. A setting that lc makes keeps its
// value; a true setting is never turned off, because v2 had no way to say
// otherwise.
func fiberMigrateListenConfig(app *fiber.App, lc ...fiber.ListenConfig) fiber.ListenConfig {
	var cfg fiber.ListenConfig
	if len(lc) > 0 {
		cfg = lc[0]
	}
	if app == nil {
		return cfg
	}

	value, found := fiberMigrateListenApps.Load(weak.Make(app))
	if !found {
		return cfg
	}
	recorded, ok := value.(fiberMigrateListenSettings)
	if !ok {
		return cfg
	}

	cfg.EnablePrefork = cfg.EnablePrefork || recorded.prefork
	cfg.DisableStartupMessage = cfg.DisableStartupMessage || recorded.quiet
	cfg.EnablePrintRoutes = cfg.EnablePrintRoutes || recorded.routes
	if cfg.ListenerNetwork == "" {
		cfg.ListenerNetwork = recorded.network
	}
	return cfg
}
