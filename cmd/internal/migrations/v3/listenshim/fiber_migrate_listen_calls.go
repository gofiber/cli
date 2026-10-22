// This file was added by "fiber migrate", next to fiber_migrate_listen.go, which
// explains why. Some of the calls that start an app there have an expression for a
// receiver, such as newApp().Listen(addr): the functions below take the app as an
// argument, so that the expression is evaluated once.
//
// Assign the app to a variable and pass a fiber.ListenConfig to Listen yourself to
// do without them.

package listenshim

import (
	"net"

	"github.com/gofiber/fiber/v3"
)

// fiberMigrateListen is app.Listen(addr, lc...) with the settings recorded for app.
func fiberMigrateListen(app *fiber.App, addr string, lc ...fiber.ListenConfig) error {
	return app.Listen(addr, fiberMigrateListenConfig(app, lc...))
}

// fiberMigrateListener is app.Listener(ln, lc...) with the settings recorded for app.
func fiberMigrateListener(app *fiber.App, ln net.Listener, lc ...fiber.ListenConfig) error {
	return app.Listener(ln, fiberMigrateListenConfig(app, lc...))
}
