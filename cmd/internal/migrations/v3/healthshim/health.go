// Package healthshim is the source of the file that the migration adds to
// <module>/internal/fibermigrate: the health check middleware of Fiber v2, for
// the configs that cannot be split into one route for each probe.
package healthshim

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// HealthConfig is the config of the health check middleware of Fiber v2
// (healthcheck.Config): both probes, each with its endpoint, in one value.
type HealthConfig struct {
	// Next defines a function to skip this middleware when returned true.
	Next func(c fiber.Ctx) bool

	// LivenessProbe tells whether the application is running. Default: always true.
	LivenessProbe func(c fiber.Ctx) bool

	// LivenessEndpoint is the path the liveness probe is served on. Default: "/livez".
	LivenessEndpoint string

	// ReadinessProbe tells whether the application is ready for requests. Default:
	// always true.
	ReadinessProbe func(c fiber.Ctx) bool

	// ReadinessEndpoint is the path the readiness probe is served on. Default: "/readyz".
	ReadinessEndpoint string
}

// healthDefaultLiveness and healthDefaultReadiness are the endpoints of the
// probes that a config does not name.
const (
	healthDefaultLiveness  = "/livez"
	healthDefaultReadiness = "/readyz"
)

func healthDefaultProbe(fiber.Ctx) bool { return true }

// HealthConfigDefault is the default config of the middleware
// (healthcheck.ConfigDefault of Fiber v2).
var HealthConfigDefault = HealthConfig{
	LivenessProbe:     healthDefaultProbe,
	LivenessEndpoint:  healthDefaultLiveness,
	ReadinessProbe:    healthDefaultProbe,
	ReadinessEndpoint: healthDefaultReadiness,
}

// healthDefaults fills in what the config leaves out.
func healthDefaults(config ...HealthConfig) HealthConfig {
	if len(config) < 1 {
		return HealthConfigDefault
	}

	cfg := config[0]
	if cfg.LivenessProbe == nil {
		cfg.LivenessProbe = healthDefaultProbe
	}
	if cfg.ReadinessProbe == nil {
		cfg.ReadinessProbe = healthDefaultProbe
	}
	if cfg.LivenessEndpoint == "" {
		cfg.LivenessEndpoint = healthDefaultLiveness
	}
	if cfg.ReadinessEndpoint == "" {
		cfg.ReadinessEndpoint = healthDefaultReadiness
	}
	return cfg
}

// healthProbeHandler answers with the result of a probe.
func healthProbeHandler(probe func(fiber.Ctx) bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		if probe == nil {
			return c.Next()
		}
		if probe(c) {
			return c.SendStatus(fiber.StatusOK)
		}
		return c.SendStatus(fiber.StatusServiceUnavailable)
	}
}

// Health is healthcheck.New of Fiber v2: a middleware that answers a GET request
// for the endpoint of either probe with 200 or 503, whatever the path it is mounted
// on, and hands every other request to the next handler. In Fiber v3 a handler of
// the healthcheck package answers every request that reaches it, which is why the
// migration registers one route for each probe where it can; this is for the
// configs that it cannot split, a config held in a variable that is used elsewhere,
// returned by a function or kept in a struct. Once the probes are registered one by
// one (app.Get(healthcheck.LivenessEndpoint, healthcheck.New(healthcheck.Config{Probe: ...})))
// this file can go.
func Health(config ...HealthConfig) fiber.Handler {
	cfg := healthDefaults(config...)
	isLive := healthProbeHandler(cfg.LivenessProbe)
	isReady := healthProbeHandler(cfg.ReadinessProbe)

	return func(c fiber.Ctx) error {
		if cfg.Next != nil && cfg.Next(c) {
			return c.Next()
		}
		if c.Method() != fiber.MethodGet {
			return c.Next()
		}

		// the path below the one the middleware is mounted on
		prefix := len(strings.TrimRight(c.Route().Path, "/"))
		if len(c.Path()) >= prefix {
			path := c.Path()[prefix:]
			trimmed := path
			if !c.App().Config().StrictRouting {
				trimmed = strings.TrimRight(path, "/")
			}
			switch {
			case path == cfg.ReadinessEndpoint || trimmed == cfg.ReadinessEndpoint:
				return isReady(c)
			case path == cfg.LivenessEndpoint || trimmed == cfg.LivenessEndpoint:
				return isLive(c)
			}
		}

		return c.Next()
	}
}
