package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const cacheImports = `import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cache"
)
`

func Test_MigrateCacheConfig(t *testing.T) {
	t.Parallel()

	// the example of the v2 documentation: CacheControl comes after a function literal
	res := mw1Run(t, v3.MigrateCacheConfig, `package main

`+cacheImports+`
func docs() fiber.Handler {
	return cache.New(cache.Config{
		Next: func(c *fiber.Ctx) bool {
			return c.Query("noCache") == "true"
		},
		Expiration:   30 * time.Minute,
		CacheControl: true,
	})
}

func keyGen(st fiber.Storage) fiber.Handler {
	return cache.New(cache.Config{
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.Path()
		},
		Store:        st,
		CacheControl: true,
	})
}

func deprecated(st fiber.Storage) fiber.Handler {
	return cache.New(cache.Config{
		Expiration: time.Minute,
		Store:      st,
		Key: func(c *fiber.Ctx) string {
			return c.OriginalURL()
		},
		Methods:      []string{fiber.MethodGet},
		CacheControl: false,
	})
}
`)

	assert.Equal(t, `package main

`+cacheImports+`
func docs() fiber.Handler {
	return cache.New(cache.Config{
		Next: func(c *fiber.Ctx) bool {
			return c.Query("noCache") == "true"
		},
		Expiration:          30 * time.Minute,
		DisableCacheControl: false,
	})
}

func keyGen(st fiber.Storage) fiber.Handler {
	return cache.New(cache.Config{
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.Path()
		},
		Storage:             st,
		DisableCacheControl: false,
		Expiration:          time.Minute,
	})
}

func deprecated(st fiber.Storage) fiber.Handler {
	return cache.New(cache.Config{
		Expiration: time.Minute,
		Storage:    st,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.OriginalURL()
		},
		Methods:             []string{fiber.MethodGet},
		DisableCacheControl: true,
	})
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating cache middleware configs")
	assert.Empty(t, res.notes)
}

func Test_MigrateCacheConfig_CacheControlIsInverted(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCacheConfig, `package main

`+cacheImports+`
var cfg struct {
	EnableCache         bool
	DisableCacheHeaders bool
}

func enabled() bool { return true }

var (
	a = cache.Config{Expiration: time.Hour, CacheControl: true}
	b = cache.Config{Expiration: time.Hour, CacheControl: false}
	c = cache.Config{Expiration: time.Hour, CacheControl: cfg.EnableCache}
	d = cache.Config{Expiration: time.Hour, CacheControl: !cfg.DisableCacheHeaders}
	e = cache.Config{Expiration: time.Hour, CacheControl: enabled()}
	f = cache.Config{Expiration: time.Hour, CacheControl: cfg.EnableCache && enabled()}
	g = cache.Config{Expiration: time.Hour, CacheControl: !(cfg.DisableCacheHeaders || !enabled())}
	h = cache.Config{
		Expiration:   time.Hour,
		CacheControl: true, // with comment
	}
	i = cache.Config{Expiration: time.Hour, CacheControl: cfg.EnableCache /* block comment */}
)
`)

	file := res.file()
	for _, want := range []string{
		`a = cache.Config{Expiration: time.Hour, DisableCacheControl: false}`,
		`b = cache.Config{Expiration: time.Hour, DisableCacheControl: true}`,
		`c = cache.Config{Expiration: time.Hour, DisableCacheControl: !cfg.EnableCache}`,
		`d = cache.Config{Expiration: time.Hour, DisableCacheControl: cfg.DisableCacheHeaders}`,
		`e = cache.Config{Expiration: time.Hour, DisableCacheControl: !enabled()}`,
		`f = cache.Config{Expiration: time.Hour, DisableCacheControl: !(cfg.EnableCache && enabled())}`,
		`g = cache.Config{Expiration: time.Hour, DisableCacheControl: cfg.DisableCacheHeaders || !enabled()}`,
		"DisableCacheControl: false, // with comment",
		`i = cache.Config{Expiration: time.Hour, DisableCacheControl: !cfg.EnableCache /* block comment */}`,
	} {
		assert.Contains(t, file, want)
	}
	assert.NotRegexp(t, `[^e]CacheControl:`, file)
}

func Test_MigrateCacheConfig_RestoresTheDefaultsOfV2(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCacheConfig, `package main

`+cacheImports+`
func configs(st fiber.Storage) []fiber.Handler {
	return []fiber.Handler{
		cache.New(),
		cache.New(cache.Config{}),
		cache.New(cache.Config{Expiration: 10 * time.Minute}),
		cache.New(cache.Config{Storage: st}),
		cache.New(cache.Config{
			Storage: st,
		}),
		cache.New(cache.Config{
			Storage: st, // the store
		}),
		cache.New(cache.Config{
			Expiration:           10 * time.Minute,
			StoreResponseHeaders: true,
		}),
	}
}
`)

	assert.Equal(t, `package main

`+cacheImports+`
func configs(st fiber.Storage) []fiber.Handler {
	return []fiber.Handler{
		cache.New(cache.Config{DisableCacheControl: true, Expiration: time.Minute}),
		cache.New(cache.Config{DisableCacheControl: true, Expiration: time.Minute}),
		cache.New(cache.Config{Expiration: 10 * time.Minute, DisableCacheControl: true}),
		cache.New(cache.Config{Storage: st, DisableCacheControl: true, Expiration: time.Minute}),
		cache.New(cache.Config{
			Storage:             st,
			DisableCacheControl: true,
			Expiration:          time.Minute,
		}),
		cache.New(cache.Config{
			Storage:             st, // the store
			DisableCacheControl: true,
			Expiration:          time.Minute,
		}),
		cache.New(cache.Config{
			Expiration:           10 * time.Minute,
			StoreResponseHeaders: true,
			DisableCacheControl:  true,
		}),
	}
}
`, res.file())
}

func Test_MigrateCacheConfig_Assignments(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCacheConfig, `package main

`+cacheImports+`
func config(st fiber.Storage, enabled bool) cache.Config {
	cfg := cache.ConfigDefault
	cfg.Expiration = 2 * time.Minute
	cfg.CacheControl = true
	cfg.Store = st
	cfg.Key = func(c *fiber.Ctx) string { return c.Path() }

	var other cache.Config
	other.CacheControl = enabled
	return cfg
}
`)

	file := res.file()
	assert.Contains(t, file, `cfg.DisableCacheControl = false`)
	assert.Contains(t, file, `cfg.Storage = st`)
	assert.Contains(t, file, `cfg.KeyGenerator = func(c *fiber.Ctx) string { return c.Path() }`)
	assert.Contains(t, file, `other.DisableCacheControl = !enabled`)
}

func Test_MigrateCacheConfig_ConfigOfV3IsKept(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cache"
)

var _ fiber.Handler

var a = cache.Config{DisableCacheControl: true}
var b = cache.Config{KeyHeaders: []string{"Accept"}, Expiration: time.Hour}
var c = cache.Config{DisableCacheControl: false, Expiration: time.Minute}
`
	res := mw1Run(t, v3.MigrateCacheConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}

func Test_MigrateCacheConfig_IgnoresOtherTypes(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/cache"
	"example.com/app/store"
)

type httpCache struct {
	CacheControl bool
	Store        string
	Key          string
}

var h = httpCache{CacheControl: true, Store: "memory", Key: "k"}

var s = store.Config{CacheControl: true, Expiration: time.Hour}

var _ = cache.New
`
	res := mw1Run(t, v3.MigrateCacheConfig, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)

	// a package of the project that is called cache
	own := `package main

import "example.com/app/cache"

var c = cache.Config{CacheControl: true}
var d = cache.New()
`
	res = mw1Run(t, v3.MigrateCacheConfig, own)
	assert.Equal(t, own, res.file())
}

func Test_MigrateCacheConfig_Alias(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCacheConfig, `package main

import (
	"time"

	ca "github.com/gofiber/fiber/v2/middleware/cache"
)

var a = ca.Config{Expiration: time.Hour, CacheControl: true}
var b = ca.New()
`)

	assert.Contains(t, res.file(), `ca.Config{Expiration: time.Hour, DisableCacheControl: false}`)
	assert.Contains(t, res.file(), `ca.New(ca.Config{DisableCacheControl: true, Expiration: time.Minute})`)
}

func Test_MigrateCacheConfig_StoreAndStorage(t *testing.T) {
	t.Parallel()

	// v2 let the deprecated Store win over Storage
	res := mw1Run(t, v3.MigrateCacheConfig, `package main

`+cacheImports+`
func handler(a, b fiber.Storage) fiber.Handler {
	return cache.New(cache.Config{Expiration: time.Hour, Storage: a, Store: b})
}
`)

	assert.Contains(t, res.file(), `cache.Config{Expiration: time.Hour, Storage: b, DisableCacheControl: true}`)
}

func Test_MigrateCacheConfig_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateCacheConfig, `package main

`+cacheImports+`
func handlers(st fiber.Storage) []fiber.Handler {
	cfg := cache.ConfigDefault
	cfg.CacheControl = true
	return []fiber.Handler{
		cache.New(),
		cache.New(cache.Config{CacheControl: true, Store: st}),
		cache.New(cache.Config{Expiration: time.Hour}),
		cache.New(cfg),
	}
}
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateCacheConfig, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
}
