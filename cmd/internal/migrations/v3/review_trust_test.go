package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trustNotes returns the notes of the review about the trusted proxy settings.
func trustNotes(t *testing.T, files map[string]string) []string {
	t.Helper()
	return reviewNotesWith(reviewNotes(t, files), "v2 trusted the X-Forwarded-* headers of every client unless EnableTrustedProxyCheck was on")
}

func Test_Review_TrustedProxyNote(t *testing.T) {
	t.Parallel()

	reads := `
func scalars(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"host": c.Hostname(), "secure": c.Secure(), "base": c.BaseURL(), "scheme": c.Scheme()})
}
`
	tests := []struct {
		name  string
		files map[string]string
		// want says where the note stands and what it contains, nothing when empty
		at       string
		contains []string
		absent   []string
	}{
		{
			name: "a default app that reads the scheme and host",
			files: map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() {
	app := fiber.New()
	app.Get("/", scalars)
}
` + reads},
			at: "main.go:6: ",
			contains: []string{
				"No fiber.Config of this project sets TrustProxy.",
				"c.Scheme(), c.Secure(), c.Host(), c.Hostname(), c.BaseURL() and c.Subdomains() (first read at main.go:11)",
				"X-Forwarded-Proto and X-Forwarded-Host are ignored",
			},
			absent: []string{"earlydata"},
		},
		{
			name: "the note stands at the first config literal",
			files: map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() {
	_ = fiber.New(fiber.Config{
		AppName: "x",
	})
}
` + reads},
			at:       "main.go:6: ",
			contains: []string{"No fiber.Config of this project sets TrustProxy."},
		},
		{
			name: "an alias of the fiber package",
			files: map[string]string{"main.go": `package main

import f "github.com/gofiber/fiber/v3"

func main() { _ = f.New() }

func h(c f.Ctx) string { return c.Host() }
`},
			at:       "main.go:5: ",
			contains: []string{"first read at main.go:7"},
		},
		{
			name: "the trust is configured",
			files: map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() {
	_ = fiber.New(fiber.Config{
		TrustProxy:       true,
		TrustProxyConfig: fiber.TrustProxyConfig{Proxies: []string{"10.0.0.0/8"}},
	})
}
` + reads},
		},
		{
			name: "the trust is configured by assignments",
			files: map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() {
	cfg := fiber.Config{}
	cfg.TrustProxy = true
	cfg.TrustProxyConfig.Loopback = true
	_ = fiber.New(cfg)
}
` + reads},
		},
		{
			name: "TrustProxy is true and no proxy is listed",
			files: map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() {
	_ = fiber.New(fiber.Config{TrustProxy: true})
}
` + reads},
			at:       "main.go:6: ",
			contains: []string{"TrustProxy is true, but no TrustProxyConfig is set, so v3 trusts no peer."},
			absent:   []string{"No fiber.Config of this project sets TrustProxy."},
		},
		{
			name: "TrustProxy is set from an expression",
			files: map[string]string{"main.go": `package main

import (
	"os"

	"github.com/gofiber/fiber/v3"
)

func main() {
	_ = fiber.New(fiber.Config{TrustProxy: os.Getenv("BEHIND_PROXY") == "1"})
}
` + reads},
			at:       "main.go:10: ",
			contains: []string{"TrustProxy is set from an expression here: whenever it is false, v2 trusted every client and v3 trusts none."},
		},
		{
			name: "TrustProxy is switched off",
			files: map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() {
	_ = fiber.New(fiber.Config{TrustProxy: false})
}
` + reads},
			at:       "main.go:6: ",
			contains: []string{"No fiber.Config of this project sets TrustProxy."},
		},
		{
			name: "a config with a ProxyHeader that does not trust is noted by the config migration",
			files: map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() {
	_ = fiber.New(fiber.Config{ProxyHeader: fiber.HeaderXForwardedFor})
}
` + reads},
		},
		{
			name: "earlydata without a trusted proxy",
			files: map[string]string{"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/earlydata"
)

func main() {
	app := fiber.New()
	app.Use(earlydata.New())
}
`},
			at: "main.go:9: ",
			contains: []string{
				"The earlydata middleware (imported at main.go:5) answers 425 Too Early to every request with Early-Data: 1 unless the peer is trusted.",
			},
			absent: []string{"Behind a reverse proxy"},
		},
		{
			name: "earlydata with a trusted proxy",
			files: map[string]string{"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/earlydata"
)

func main() {
	app := fiber.New(fiber.Config{TrustProxy: true, TrustProxyConfig: fiber.TrustProxyConfig{Private: true}})
	app.Use(earlydata.New())
}
`},
		},
		{
			name: "reads and earlydata in one note",
			files: map[string]string{
				"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() { _ = fiber.New() }
`,
				"mw.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/earlydata"
)

var _ = earlydata.New

func redirect(c fiber.Ctx) string { return c.BaseURL() }
`,
			},
			at:       "main.go:5: ",
			contains: []string{"(first read at mw.go:10)", "(imported at mw.go:5)"},
		},
		{
			name: "no read, no earlydata",
			files: map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

func main() { _ = fiber.New() }
`},
		},
		{
			name: "methods of the same names on other values",
			files: map[string]string{"main.go": `package main

import (
	"net/url"

	"github.com/gofiber/fiber/v3"
)

type remote struct{}

func (remote) Host() string { return "x" }

func main() {
	_ = fiber.New()
	u, _ := url.Parse("http://example.com")
	_ = u.Hostname()
	_ = remote{}.Host()
}
`},
		},
		{
			name: "only the first read is named, the library has no app",
			files: map[string]string{"lib.go": `package lib

import "github.com/gofiber/fiber/v3"

func Base(c fiber.Ctx) string { return c.BaseURL() }

func Sub(c fiber.Ctx) []string { return c.Subdomains() }
`},
			at:       "lib.go:5: ",
			contains: []string{"(first read at lib.go:5)"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			notes := trustNotes(t, tc.files)
			if tc.at == "" {
				assert.Empty(t, notes)
				return
			}
			require.Len(t, notes, 1, "%v", notes)
			assert.Contains(t, notes[0], tc.at+"v2 trusted the X-Forwarded-* headers of every client unless EnableTrustedProxyCheck was on")
			assert.Contains(t, notes[0], "v3 ignores them unless TrustProxy is true and the peer is listed in TrustProxyConfig (Proxies, Loopback, Private, LinkLocal or UnixSocket).")
			for _, part := range tc.contains {
				assert.Contains(t, notes[0], part)
			}
			for _, part := range tc.absent {
				assert.NotContains(t, notes[0], part)
			}
		})
	}
}
