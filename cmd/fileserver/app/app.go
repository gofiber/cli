package fileserver

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"
)

// Options holds the settings for the file server.
type Options struct {
	Dir       string
	Path      string
	Logger    bool
	Cors      bool
	Health    bool
	Browse    bool
	Download  bool
	Compress  bool
	Cache     time.Duration
	MaxAge    int
	Index     string
	ByteRange bool
}

// NewApp creates a Fiber application using the provided options.
func NewApp(o Options) *fiber.App {
	app := fiber.New()

	// Recover should be registered first to handle panics from later middleware.
	app.Use(recover.New())

	if o.Logger {
		app.Use(logger.New())
	}
	if o.Cors {
		app.Use(cors.New())
	}

	if o.Health {
		app.Get(healthcheck.DefaultLivenessEndpoint, healthcheck.NewHealthChecker())
		app.Get(healthcheck.DefaultReadinessEndpoint, healthcheck.NewHealthChecker())
		app.Get(healthcheck.DefaultStartupEndpoint, healthcheck.NewHealthChecker())
	}

	cfgStatic := static.Config{
		Browse:        o.Browse,
		Download:      o.Download,
		Compress:      o.Compress,
		ByteRange:     o.ByteRange,
		CacheDuration: o.Cache,
		MaxAge:        o.MaxAge,
	}
	if o.Index != "" {
		cfgStatic.IndexNames = strings.Split(o.Index, ",")
	}
	app.Use(o.Path, static.New(o.Dir, cfgStatic))

	return app
}
