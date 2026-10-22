package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const corsHead = `package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

var _ = fmt.Sprint
var _ = os.Getenv
var _ = strings.Join
`

func Test_MigrateCORSConfig(t *testing.T) {
	t.Parallel()

	// the example of the v2 documentation
	res := mw1Run(t, v3.MigrateCORSConfig, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func main() {
	app := fiber.New()
	app.Use(cors.New(cors.Config{
		AllowOrigins:  "https://gofiber.io, https://gofiber.net",
		AllowMethods:  "GET,POST,HEAD,PUT,DELETE,PATCH",
		AllowHeaders:  "Origin, Content-Type, Accept",
		ExposeHeaders: "Content-Length", // exposed
		MaxAge:        300,
	}))
}
`)

	assert.Equal(t, `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func main() {
	app := fiber.New()
	app.Use(cors.New(cors.Config{
		AllowOrigins:  []string{"https://gofiber.io", "https://gofiber.net"},
		AllowMethods:  []string{"GET", "POST", "HEAD", "PUT", "DELETE", "PATCH"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Accept"},
		ExposeHeaders: []string{"Content-Length"}, // exposed
		MaxAge:        300,
	}))
}
`, res.file())
	assert.Contains(t, res.stdout, "Migrating CORS middleware configs")
	assert.Empty(t, res.notes)
}

func Test_MigrateCORSConfig_Expressions(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCORSConfig, corsHead+`
const allowedOrigins = "https://example.com,https://api.example.com"

var allowedMethods = "GET,POST,PUT"

func constants() fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins: allowedOrigins,
		AllowMethods: allowedMethods,
	})
}

func joined(headers []string) fiber.Handler {
	return cors.New(cors.Config{
		AllowMethods: strings.Join([]string{fiber.MethodGet, fiber.MethodPost}, ","),
		AllowHeaders: strings.Join(headers, ", "),
	})
}

func computed(host string) fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins:  fmt.Sprintf("https://%s,https://www.%s", host, host),
		AllowMethods:  os.Getenv("CORS_METHODS"),
		ExposeHeaders: "X-A," + "X-B",
		AllowHeaders: "Origin, Content-Type, " +
			"Authorization",
	})
}

func raw() fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins: `+"`https://example.com,https://example2.com`"+`,
	})
}
`)

	assert.Equal(t, corsHead+`
const allowedOrigins = "https://example.com,https://api.example.com"

var allowedMethods = "GET,POST,PUT"

func constants() fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins: strings.FieldsFunc(allowedOrigins, func(r rune) bool { return r == ',' || r == ' ' }),
		AllowMethods: strings.FieldsFunc(allowedMethods, func(r rune) bool { return r == ',' || r == ' ' }),
	})
}

func joined(headers []string) fiber.Handler {
	return cors.New(cors.Config{
		AllowMethods: []string{fiber.MethodGet, fiber.MethodPost},
		AllowHeaders: headers,
	})
}

func computed(host string) fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins:  strings.FieldsFunc(fmt.Sprintf("https://%s,https://www.%s", host, host), func(r rune) bool { return r == ',' || r == ' ' }),
		AllowMethods:  strings.FieldsFunc(os.Getenv("CORS_METHODS"), func(r rune) bool { return r == ',' || r == ' ' }),
		ExposeHeaders: []string{"X-A", "X-B"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Authorization"},
	})
}

func raw() fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins: []string{"https://example.com", "https://example2.com"},
	})
}
`, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateCORSConfig_EmptyStringsMeanTheDefault(t *testing.T) {
	t.Parallel()

	// "" is the default in v2 (all origins, the default methods, the headers the
	// request asks for); []string{""} would panic or send empty headers in v3
	res := mw1Run(t, v3.MigrateCORSConfig, `package main

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func configs() []fiber.Handler {
	cfg := cors.Config{AllowOrigins: " , ", AllowMethods: ""}
	cfg.AllowHeaders = ""
	cfg.ExposeHeaders = "X-Request-Id"
	return []fiber.Handler{
		cors.New(cors.Config{
			AllowOrigins:     "",
			AllowOriginsFunc: func(origin string) bool { return strings.HasSuffix(origin, ".example.com") },
			AllowHeaders:     "", // echo the headers of the request
			ExposeHeaders:    "",
		}),
		cors.New(cors.Config{AllowOrigins: "https://a.com", AllowHeaders: ""}),
		cors.New(cfg),
	}
}
`)

	assert.Equal(t, `package main

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func configs() []fiber.Handler {
	cfg := cors.Config{}
	cfg.AllowHeaders = nil
	cfg.ExposeHeaders = []string{"X-Request-Id"}
	return []fiber.Handler{
		cors.New(cors.Config{
			AllowOriginsFunc: func(origin string) bool { return strings.HasSuffix(origin, ".example.com") },
		}),
		cors.New(cors.Config{AllowOrigins: []string{"https://a.com"}}),
		cors.New(cfg),
	}
}
`, res.file())
}

func Test_MigrateCORSConfig_Assignments(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCORSConfig, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func configs() fiber.Handler {
	cfg := cors.ConfigDefault
	cfg.AllowOrigins = "https://example.com,https://other.com"
	cfg.AllowHeaders = os.Getenv("HEADERS")

	var typed cors.Config
	typed.AllowMethods = "GET"
	other := struct{ AllowOrigins string }{}
	other.AllowOrigins = "https://example.com"
	_ = other
	return cors.New(cfg, typed)
}
`)

	file := res.file()
	assert.Contains(t, file, `cfg.AllowOrigins = []string{"https://example.com", "https://other.com"}`)
	assert.Contains(t, file, `cfg.AllowHeaders = strings.FieldsFunc(os.Getenv("HEADERS"), func(r rune) bool { return r == ',' || r == ' ' })`)
	assert.Contains(t, file, `typed.AllowMethods = []string{"GET"}`)
	assert.Contains(t, file, `other.AllowOrigins = "https://example.com"`)
}

func Test_MigrateCORSConfig_IgnoresOtherStructs(t *testing.T) {
	t.Parallel()

	// the application's own settings with the field names of cors.Config
	src := `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

type corsSettings struct {
	AllowOrigins string
	AllowMethods string
}

func handler() fiber.Handler {
	s := corsSettings{AllowOrigins: "https://example.com", AllowMethods: "GET"}
	return cors.New(cors.Config{AllowOrigins: []string{s.AllowOrigins}})
}
`
	res := mw1Run(t, v3.MigrateCORSConfig, src)
	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)

	// a file that does not import the middleware at all
	unrelated := `package main

type settings struct{ AllowOrigins string }

var s = settings{AllowOrigins: "https://example.com"}
`
	res = mw1Run(t, v3.MigrateCORSConfig, unrelated)
	assert.Equal(t, unrelated, res.file())

	// a package of the project that is called cors
	own := `package main

import "example.com/app/cors"

var c = cors.Config{AllowOrigins: "https://example.com"}
`
	res = mw1Run(t, v3.MigrateCORSConfig, own)
	assert.Equal(t, own, res.file())
}

func Test_MigrateCORSConfig_Alias(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateCORSConfig, `package main

import (
	f "github.com/gofiber/fiber/v2"
	c "github.com/gofiber/fiber/v2/middleware/cors"
)

func handler() f.Handler {
	return c.New(c.Config{AllowOrigins: "https://a.com, https://b.com", AllowMethods: "GET"})
}
`)

	assert.Contains(t, res.file(), `c.New(c.Config{AllowOrigins: []string{"https://a.com", "https://b.com"}, AllowMethods: []string{"GET"}})`)
}

func Test_MigrateCORSConfig_Idempotent(t *testing.T) {
	t.Parallel()

	first := mw1Run(t, v3.MigrateCORSConfig, corsHead+`
func configs(headers []string, origins string) []fiber.Handler {
	cfg := cors.ConfigDefault
	cfg.AllowOrigins = origins
	return []fiber.Handler{
		cors.New(cors.Config{
			AllowOrigins: origins,
			AllowMethods: strings.Join(headers, ","),
			AllowHeaders: "X-A, X-B",
			ExposeHeaders: "",
		}),
		cors.New(cfg),
	}
}
`)
	assert.NotEmpty(t, first.stdout)

	second := mw1Run(t, v3.MigrateCORSConfig, first.file())
	assert.Equal(t, first.file(), second.file())
	assert.Empty(t, second.stdout)
}

func Test_MigrateCORSConfig_V3CodeIsLeftAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
)

func handler(origins []string) fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins: origins,
		AllowMethods: []string{fiber.MethodGet},
	})
}
`
	res := mw1Run(t, v3.MigrateCORSConfig, src)
	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
}
