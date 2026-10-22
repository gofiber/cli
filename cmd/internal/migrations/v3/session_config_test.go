package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSessionConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msession")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/session"
    "time"
)
var _ = session.New(session.Config{
    Expiration: 5 * time.Minute,
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "IdleTimeout:")
	assert.NotContains(t, content, "Expiration:")
	assert.Contains(t, buf.String(), "Migrating session middleware configs")
}

// v2 sessions lasted 24 hours, v3 sessions idle out after 30 minutes unless
// IdleTimeout says otherwise, so a config that never set the lifetime has to
// say it now.
func Test_MigrateSessionConfig_KeepsTheV2Lifetime(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, in, want string }{
		{
			name: "a store without a config",
			in: `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var store = session.New()
`,
			want: `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{IdleTimeout: 24 * time.Hour})
`,
		},
		{
			name: "configs that do not set the lifetime",
			in: `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var _ = time.Second

var oneLine = session.New(session.Config{CookieSecure: true})

var empty = session.New(session.Config{})

var multiLine = session.New(session.Config{
	CookieSecure: true,
	Storage:      nil, // in memory
})
`,
			want: `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var _ = time.Second

var oneLine = session.New(session.Config{CookieSecure: true, IdleTimeout: 24 * time.Hour})

var empty = session.New(session.Config{IdleTimeout: 24 * time.Hour})

var multiLine = session.New(session.Config{
	CookieSecure: true,
	Storage:      nil, // in memory
	IdleTimeout:  24 * time.Hour,
})
`,
		},
		{
			name: "a config that sets the lifetime only renames it",
			in: `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{Expiration: time.Hour})
`,
			want: `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{IdleTimeout: time.Hour})
`,
		},
		{
			name: "a config written for v3 keeps its lifetime",
			in: `package main

import "github.com/gofiber/fiber/v3/middleware/session"

var store = session.NewStore(session.Config{CookieSecure: true, AbsoluteTimeout: 0})
`,
			want: `package main

import "github.com/gofiber/fiber/v3/middleware/session"

var store = session.NewStore(session.Config{CookieSecure: true, AbsoluteTimeout: 0})
`,
		},
		{
			name: "time imported under another name, the package under an alias",
			in: `package main

import (
	t "time"

	sess "github.com/gofiber/fiber/v2/middleware/session"
)

var d = 5 * t.Second

var store = sess.New(sess.Config{CookieSecure: true})

var plain = sess.New()
`,
			want: `package main

import (
	t "time"

	sess "github.com/gofiber/fiber/v2/middleware/session"
)

var d = 5 * t.Second

var store = sess.New(sess.Config{CookieSecure: true, IdleTimeout: 24 * t.Hour})

var plain = sess.New(sess.Config{IdleTimeout: 24 * t.Hour})
`,
		},
		{
			name: "stores in struct fields, returns and arguments",
			in: `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

type server struct{ store *session.Store }

func newServer() *server {
	return &server{store: session.New(session.Config{Expiration: time.Hour})}
}

func newStore() *session.Store {
	return session.New(session.Config{CookieHTTPOnly: true})
}

func use(*session.Store) {}

func main() {
	use(session.New())
}
`,
			want: `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

type server struct{ store *session.Store }

func newServer() *server {
	return &server{store: session.New(session.Config{IdleTimeout: time.Hour})}
}

func newStore() *session.Store {
	return session.New(session.Config{CookieHTTPOnly: true, IdleTimeout: 24 * time.Hour})
}

func use(*session.Store) {}

func main() {
	use(session.New(session.Config{IdleTimeout: 24 * time.Hour}))
}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, res := mw3RunOne(t, tc.in, v3.MigrateSessionConfig)
			assert.Equal(t, tc.want, got)
			assert.Empty(t, res.notes)

			// A second run finds nothing left to do.
			again, second := mw3RunOne(t, got, v3.MigrateSessionConfig)
			assert.Equal(t, got, again)
			assert.Empty(t, second.out)
		})
	}
}

func Test_MigrateSessionConfig_ConfigDefault(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

func copyDefault(ttl time.Duration) *session.Store {
	cfg := session.ConfigDefault
	cfg.Expiration = ttl
	return session.New(cfg)
}

func keepsDefault() *session.Store {
	cfg := session.ConfigDefault
	cfg.CookieSecure = true
	return session.New(cfg)
}

func direct() *session.Store {
	return session.New(session.ConfigDefault)
}

func zeroValue() *session.Store {
	var cfg session.Config
	cfg.CookieSecure = true
	return session.New(cfg)
}

func zeroValueWithLifetime() *session.Store {
	var cfg session.Config
	cfg.Expiration = time.Hour
	return session.New(cfg)
}
`, v3.MigrateSessionConfig)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

func copyDefault(ttl time.Duration) *session.Store {
	cfg := session.ConfigDefault
	cfg.IdleTimeout = ttl
	return session.New(cfg)
}

func keepsDefault() *session.Store {
	cfg := session.ConfigDefault
	cfg.IdleTimeout = 24 * time.Hour
	cfg.CookieSecure = true
	return session.New(cfg)
}

func direct() *session.Store {
	return session.New(session.Config{IdleTimeout: 24 * time.Hour})
}

func zeroValue() *session.Store {
	var cfg session.Config
	cfg.IdleTimeout = 24 * time.Hour
	cfg.CookieSecure = true
	return session.New(cfg)
}

func zeroValueWithLifetime() *session.Store {
	var cfg session.Config
	cfg.IdleTimeout = time.Hour
	return session.New(cfg)
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionConfig_FieldAccess(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

type holder struct{ cfg session.Config }

func tune(cfg session.Config, store *session.Store) time.Duration {
	cfg.Expiration = time.Hour
	store.Expiration = 2 * time.Hour
	return store.Expiration + cfg.Expiration
}

func unrelated(h holder, other struct{ Expiration time.Duration }) time.Duration {
	return other.Expiration
}
`, v3.MigrateSessionConfig)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

type holder struct{ cfg session.Config }

func tune(cfg session.Config, store *session.Store) time.Duration {
	cfg.IdleTimeout = time.Hour
	store.IdleTimeout = 2 * time.Hour
	return store.IdleTimeout + cfg.IdleTimeout
}

func unrelated(h holder, other struct{ Expiration time.Duration }) time.Duration {
	return other.Expiration
}
`, got)
}

func Test_MigrateSessionConfig_OnlyTheFiberSessionPackage(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, src string }{
		{
			name: "the AWS SDK session package",
			src: `package main

import "github.com/aws/aws-sdk-go/aws/session"

var s = session.New(&aws.Config{})
var c = session.Config{Expiration: 5}
`,
		},
		{
			name: "a package of the project",
			src: `package main

import "example.com/project/session"

var s = session.New("user")
var c = session.Config{Expiration: 5}
`,
		},
		{
			name: "text and other structs",
			src: `package main

import "github.com/gofiber/fiber/v2/middleware/cache"

// session.New() and session.Config{Expiration: 1}
var msg = "session.New() session.Config{Expiration: 1}"

var c = cache.New(cache.Config{Expiration: 5})
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, res := mw3RunOne(t, tc.src, v3.MigrateSessionConfig)
			assert.Equal(t, tc.src, got)
			assert.Empty(t, res.out)
			assert.Empty(t, res.notes)
		})
	}
}

func Test_MigrateSessionConfig_ReportsWhatItCannotSee(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"example.com/sessioncfg"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var base = session.ConfigDefault

func fromLibrary() *session.Store {
	return session.New(sessioncfg.Build())
}
`, v3.MigrateSessionConfig)

	assert.Contains(t, got, "session.New(sessioncfg.Build())")
	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0], "session.ConfigDefault lasts 30 minutes in v3")
	assert.Contains(t, msgs[1], "sessioncfg.Build(): v2 sessions lasted 24 hours")
	assert.Equal(t, 9, res.notes[0].Line)
	assert.Equal(t, 12, res.notes[1].Line)
}
