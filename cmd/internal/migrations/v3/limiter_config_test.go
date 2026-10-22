package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// limiterSource wraps the body of a Setup function in a valid v2 file.
func limiterSource(body string) string {
	return `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

var store fiber.Storage

func Setup(app *fiber.App, perMinute int) {
` + body + `
}
`
}

func Test_MigrateLimiterConfig(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLimiterConfig, limiterSource(`	app.Use(limiter.New(limiter.Config{Max: 10, Duration: time.Minute}))

	app.Use("/api", limiter.New(limiter.Config{
		Max:      20,
		Duration: 30 * time.Second,
		Store:    store,
		Key: func(c *fiber.Ctx) string {
			return c.Get("x-forwarded-for")
		},
	}))`))

	assert.Contains(t, content, "limiter.Config{Max: 10, Expiration: time.Minute}")
	assert.Contains(t, content, "Expiration: 30 * time.Second,")
	assert.Contains(t, content, "Storage:    store,")
	assert.Contains(t, content, "KeyGenerator: func(c *fiber.Ctx) string {")
	assert.NotContains(t, content, "Duration:")
	assert.NotContains(t, content, "Store:")
	assert.Contains(t, run.out, "Migrating limiter middleware configs")
	assert.Empty(t, run.notes)
}

func Test_MigrateLimiterConfig_BracesBeforeTheDeprecatedFields(t *testing.T) {
	t.Parallel()

	// The old regexp stopped at the first closing brace, so everything after a
	// function literal or a nested composite literal kept its v2 name.
	content, _ := mw2Single(t, v3.MigrateLimiterConfig, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

type opts struct{ Name string }

func mk(o opts) fiber.Storage { return nil }

func Setup(app *fiber.App) {
	app.Use(limiter.New(limiter.Config{
		Max: 20,
		Key: func(c *fiber.Ctx) string {
			return c.IP()
		},
		Duration: 30 * time.Second,
		Store:    mk(opts{Name: "limiter"}),
	}))
	app.Use(limiter.New(limiter.Config{
		Store:    mk(opts{Name: "limiter"}),
		Duration: time.Minute,
		Next:     func(c *fiber.Ctx) bool { return c.IP() == "127.0.0.1" },
		Max:      5,
	}))
}
`)

	assert.Contains(t, content, "KeyGenerator: func(c *fiber.Ctx) string {")
	assert.Contains(t, content, "Expiration: 30 * time.Second,")
	assert.Contains(t, content, `Storage:    mk(opts{Name: "limiter"}),`)
	assert.Contains(t, content, "Expiration: time.Minute,")
	assert.NotContains(t, content, "Duration:")
	assert.NotContains(t, content, "Store:")
	assert.NotContains(t, content, "\tKey:")
}

func Test_MigrateLimiterConfig_Alias(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateLimiterConfig, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	rl "github.com/gofiber/fiber/v2/middleware/limiter"
)

func Setup(app *fiber.App) {
	app.Use(rl.New(rl.Config{
		Max:      10,
		Duration: time.Minute,
		Key:      func(c *fiber.Ctx) string { return c.IP() },
	}))
}
`)

	assert.Contains(t, content, "Expiration:   time.Minute,")
	assert.Contains(t, content, "KeyGenerator: func(c *fiber.Ctx) string { return c.IP() },")
	assert.NotContains(t, content, "Duration")
}

func Test_MigrateLimiterConfig_Assignments(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLimiterConfig, limiterSource(`	cfg := limiter.ConfigDefault
	cfg.Max = perMinute
	cfg.Duration = time.Minute
	cfg.Store = store
	cfg.Key = func(c *fiber.Ctx) string { return c.IP() }
	app.Use(limiter.New(cfg))

	var cfg2 limiter.Config
	cfg2.Duration = 10 * time.Second
	app.Use("/login", limiter.New(cfg2))

	window := limiter.ConfigDefault.Duration
	_ = window`))

	assert.Contains(t, content, "cfg.Expiration = time.Minute")
	assert.Contains(t, content, "cfg.Storage = store")
	assert.Contains(t, content, "cfg.KeyGenerator = func(c *fiber.Ctx) string { return c.IP() }")
	assert.Contains(t, content, "cfg2.Expiration = 10 * time.Second")
	assert.Contains(t, content, "window := limiter.ConfigDefault.Expiration")
	assert.NotContains(t, content, "Duration")
	assert.Empty(t, run.notes)
}

func Test_MigrateLimiterConfig_LeavesStringsAndComments(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateLimiterConfig, limiterSource(`	app.Use(limiter.New(limiter.Config{
		Max:      5,
		Duration: time.Minute, // Duration: the window
		LimitReached: func(c *fiber.Ctx) error {
			// Key: not touched, Store: neither
			return c.Status(fiber.StatusTooManyRequests).SendString("Key: rate limit reached; Duration: 1m; Store: none")
		},
	}))`))

	assert.Contains(t, content, `"Key: rate limit reached; Duration: 1m; Store: none"`)
	assert.Contains(t, content, "// Key: not touched, Store: neither")
	assert.Contains(t, content, "Expiration: time.Minute, // Duration: the window")
}

func Test_MigrateLimiterConfig_IgnoresOtherTypes(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

type cacheConfig struct {
	Duration time.Duration
	Store    map[string]string
	Key      string
}

func Setup(app *fiber.App) {
	c := cacheConfig{Duration: time.Hour, Store: nil, Key: "k"}
	c.Duration = time.Minute
	_ = c.Key
	app.Use(limiter.New())
}
`
	content, run := mw2Single(t, v3.MigrateLimiterConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateLimiterConfig_OtherLimiterPackage(t *testing.T) {
	t.Parallel()

	src := `package main

import "example.com/limiter"

var _ = limiter.Config{Duration: 1, Store: nil, Key: nil}
`
	content, run := mw2Single(t, v3.MigrateLimiterConfig, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateLimiterConfig_BothNamesAreLeftAlone(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLimiterConfig, limiterSource(`	app.Use(limiter.New(limiter.Config{
		Duration:   time.Minute,
		Expiration: time.Hour,
	}))`))

	assert.Contains(t, content, "Duration:   time.Minute,")
	require.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0].Message, "sets both Duration and Expiration")
}

func Test_MigrateLimiterConfig_CustomAlgorithm(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLimiterConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

type customLimiter struct{}

func (customLimiter) New(cfg limiter.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if cfg.Next != nil && cfg.Next(c) {
			return c.Next()
		}
		return c.Next()
	}
}

type passThrough struct{}

func (passThrough) New(cfg limiter.Config) fiber.Handler {
	return limiter.FixedWindow{}.New(cfg)
}

var _ limiter.LimiterHandler = customLimiter{}

type unrelated struct{}

func (unrelated) New(cfg limiter.Config) error { return nil }

func Setup(app *fiber.App) {
	app.Use(limiter.New(limiter.Config{Max: 20, LimiterMiddleware: customLimiter{}}))
}
`)

	assert.Contains(t, content, "func (customLimiter) New(cfg *limiter.Config) fiber.Handler {")
	assert.Contains(t, content, "var _ limiter.Handler = customLimiter{}")
	assert.Contains(t, content, "func (passThrough) New(cfg *limiter.Config) fiber.Handler {")
	assert.Contains(t, content, "return limiter.FixedWindow{}.New(cfg)", "cfg is already a pointer in the rewritten method")
	assert.Contains(t, content, "func (unrelated) New(cfg limiter.Config) error", "a New that does not return a handler is not a limiter algorithm")
	assert.NotContains(t, content, "LimiterHandler")
	assert.Empty(t, run.notes)
}

func Test_MigrateLimiterConfig_CustomAlgorithmUsingTheConfigAsValue(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLimiterConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

type logging struct{}

func report(cfg limiter.Config) {}

func (logging) New(cfg limiter.Config) fiber.Handler {
	report(cfg)
	return func(c *fiber.Ctx) error { return c.Next() }
}
`)

	assert.Contains(t, content, "func (logging) New(cfg *limiter.Config) fiber.Handler {")
	require.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0].Message, "cfg is a *limiter.Config in v3")
	assert.Equal(t, 12, run.notes[0].Line)
}

func Test_MigrateLimiterConfig_WindowNew(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLimiterConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func build() limiter.Config { return limiter.ConfigDefault }

func Setup(app *fiber.App) {
	cfg := limiter.ConfigDefault
	app.Use(limiter.FixedWindow{}.New(cfg))
	app.Use(limiter.SlidingWindow{}.New(limiter.Config{Max: 5}))
	app.Use(limiter.SlidingWindow{}.New(&cfg))
	app.Use(limiter.FixedWindow{}.New(build()))
}
`)

	assert.Contains(t, content, "limiter.FixedWindow{}.New(&cfg)")
	assert.Contains(t, content, "limiter.SlidingWindow{}.New(&limiter.Config{Max: 5})")
	assert.Contains(t, content, "limiter.SlidingWindow{}.New(&cfg)")
	assert.Contains(t, content, "limiter.FixedWindow{}.New(build())")
	require.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0].Message, "build() cannot be addressed")
}

func Test_MigrateLimiterConfig_Idempotent(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLimiterConfig, map[string]string{"main.go": limiterSource(`	cfg := limiter.ConfigDefault
	cfg.Duration = time.Minute
	app.Use(limiter.FixedWindow{}.New(cfg))
	app.Use(limiter.New(limiter.Config{Duration: time.Minute, Store: store}))`)})
	first := run.file(t, "main.go")
	require.Contains(t, run.out, "Migrating limiter middleware configs")

	second := mw2Again(t, v3.MigrateLimiterConfig, run.dir)

	assert.Equal(t, first, second.file(t, "main.go"))
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}
