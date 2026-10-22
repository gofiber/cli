package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSessionStore(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionstore")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/session"
)

var (
    store        = session.New()
    sessionStore = session.New(session.Config{})
)

func main() {
    app := fiber.New()
    custom := session.New()
    anotherStore := session.New(session.Config{})
    if temp := session.New(); temp != nil {
        _ = temp
    }
    _ = store
    _ = sessionStore
    _ = custom
    _ = anotherStore
    app.Use(session.New())
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionStore(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Regexp(t, `store\s*=\s*session.NewStore\(\)`, content)
	assert.Regexp(t, `sessionStore\s*=\s*session.NewStore\(session.Config\{\}\)`, content)
	assert.Contains(t, content, "custom := session.NewStore()")
	assert.Contains(t, content, "anotherStore := session.NewStore(session.Config{})")
	assert.Contains(t, content, "temp := session.NewStore()")
	assert.Contains(t, content, "app.Use(session.New())")
	assert.Contains(t, buf.String(), "Migrating session store")
}

// session.New is the store in v2 wherever it is used, not just on the right of
// an assignment.
func Test_MigrateSessionStore_EveryPlaceAStoreIsMade(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

type server struct {
	app   *fiber.App
	store *session.Store
}

func newServer() *server {
	return &server{app: fiber.New(), store: session.New(session.Config{Expiration: time.Hour})}
}

func newStore() *session.Store {
	return session.New(session.Config{
		Expiration: 2 * time.Hour,
	})
}

func register(app *fiber.App, store *session.Store) {}

var typed *session.Store = session.New()

var assignedLater *session.Store

func init() {
	assignedLater = session.New()
}

func main() {
	srv := newServer()
	register(srv.app, session.New())
	stores := []*session.Store{session.New(), session.New()}
	_, _ = stores, typed
}
`, v3.MigrateSessionStore)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

type server struct {
	app   *fiber.App
	store *session.Store
}

func newServer() *server {
	return &server{app: fiber.New(), store: session.NewStore(session.Config{Expiration: time.Hour})}
}

func newStore() *session.Store {
	return session.NewStore(session.Config{
		Expiration: 2 * time.Hour,
	})
}

func register(app *fiber.App, store *session.Store) {}

var typed *session.Store = session.NewStore()

var assignedLater *session.Store

func init() {
	assignedLater = session.NewStore()
}

func main() {
	srv := newServer()
	register(srv.app, session.NewStore())
	stores := []*session.Store{session.NewStore(), session.NewStore()}
	_, _ = stores, typed
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionStore_ImportAlias(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import sess "github.com/gofiber/fiber/v2/middleware/session"

var store = sess.New(sess.Config{})
`, v3.MigrateSessionStore)

	assert.Contains(t, got, "var store = sess.NewStore(sess.Config{})")
}

// Another package named session is not Fiber's: it keeps its New.
func Test_MigrateSessionStore_OnlyTheFiberSessionPackage(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, src string }{
		{
			name: "the AWS SDK",
			src: `package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/gofiber/fiber/v2"
)

func newS3() *session.Session {
	sess := session.New(&aws.Config{Region: aws.String("us-east-1")})
	_ = fiber.New
	return sess
}
`,
		},
		{
			name: "a package of the project",
			src: `package main

import (
	"example.com/project/session"

	"github.com/gofiber/fiber/v2"
)

func main() {
	s := session.New("user-42")
	_, _ = s, fiber.New()
}
`,
		},
		{
			name: "a variable that is called session",
			src: `package main

import "github.com/gofiber/fiber/v2/middleware/session"

type factory struct{}

func (factory) New() int { return 1 }

func use(session factory) int {
	return session.New()
}

var _ = session.Config{}
`,
		},
		{
			name: "text",
			src: `package main

import "github.com/gofiber/fiber/v2/middleware/session"

// session.New() makes the store
var doc = "store := session.New()"

var _ = session.Config{}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, res := mw3RunOne(t, tc.src, v3.MigrateSessionStore)
			assert.Equal(t, tc.src, got)
			assert.Empty(t, res.out)
		})
	}
}

// A call that is handed to Use is the v3 middleware, which has the name New.
func Test_MigrateSessionStore_LeavesTheMiddleware(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

func main() {
	app := fiber.New()
	app.Use(session.New())
	app.Group("/api").Use("/x", session.New(session.Config{CookieSecure: true}))
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionStore)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)

	// The second run has nothing to do either.
	again, second := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var store = session.New()
`, v3.MigrateSessionStore)
	twice, third := mw3RunOne(t, again, v3.MigrateSessionStore)
	assert.Equal(t, again, twice)
	assert.NotEmpty(t, second.out)
	assert.Empty(t, third.out)
}
