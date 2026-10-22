package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSessionMethods_SetExpiry(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

func handler(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	sess.SetExpiry(30 * time.Minute)
	return sess.Save()
}

func byID(ctx context.Context, id string) error {
	sess, err := store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	sess.SetExpiry(time.Hour)
	return sess.Save()
}

func param(sess *session.Session) {
	sess.SetExpiry(time.Minute)
}
`, v3.MigrateSessionMethods)

	assert.Equal(t, `package main

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

func handler(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	sess.SetIdleTimeout(30 * time.Minute)
	return sess.Save()
}

func byID(ctx context.Context, id string) error {
	sess, err := store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	sess.SetIdleTimeout(time.Hour)
	return sess.Save()
}

func param(sess *session.Session) {
	sess.SetIdleTimeout(time.Minute)
}
`, got)
	assert.Empty(t, res.notes)
	assert.Contains(t, res.out, "Migrating session methods")
}

func Test_MigrateSessionMethods_SetExpiryOfSomethingElse(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"time"

	"example.com/jwtlib"

	"github.com/gofiber/fiber/v3/middleware/session"
)

type token struct{}

func (token) SetExpiry(time.Duration) {}

var _ = session.NewStore

func issue(t token, claims *jwtlib.Claims) {
	t.SetExpiry(time.Hour)
	claims.SetExpiry(time.Hour)
	token{}.SetExpiry(time.Hour)
}

func lookup() any { return nil }

func unknown() {
	lookup().(interface{ SetExpiry(time.Duration) }).SetExpiry(time.Hour)
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionMethods)
	assert.Equal(t, src, got, "a receiver that is not a session is not renamed")

	// Only the receiver that cannot be told is reported.
	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "if lookup().(interface{ SetExpiry(time.Duration) }) is a session, SetExpiry is called SetIdleTimeout in v3")
}

func Test_MigrateSessionMethods_StoreResetAndDelete(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

type server struct{ sessions *session.Store }

func purge(c fiber.Ctx) error {
	if err := store.Reset(); err != nil {
		return err
	}
	return store.Delete(c.Params("id"))
}

func named(ctx fiber.Ctx, s *server) error {
	return s.sessions.Delete(ctx.Query("id"))
}

func background(id string) error {
	if err := store.Delete(id); err != nil {
		return err
	}
	return store.Reset()
}

func closure(c fiber.Ctx) {
	go func() {
		// The request may be over by the time this runs.
		_ = store.Delete("old")
	}()
}

func getsAStore() *session.Store { return store }

func viaFunction() error {
	return getsAStore().Reset()
}
`, v3.MigrateSessionMethods)

	assert.Equal(t, `package main

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

type server struct{ sessions *session.Store }

func purge(c fiber.Ctx) error {
	if err := store.Reset(c); err != nil {
		return err
	}
	return store.Delete(c, c.Params("id"))
}

func named(ctx fiber.Ctx, s *server) error {
	return s.sessions.Delete(ctx, ctx.Query("id"))
}

func background(id string) error {
	if err := store.Delete(context.Background(), id); err != nil {
		return err
	}
	return store.Reset(context.Background())
}

func closure(c fiber.Ctx) {
	go func() {
		// The request may be over by the time this runs.
		_ = store.Delete(context.Background(), "old")
	}()
}

func getsAStore() *session.Store { return store }

func viaFunction() error {
	return getsAStore().Reset(context.Background())
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionMethods_OnlyStores(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

type cache struct{}

func (cache) Reset() error           { return nil }
func (cache) Delete(key string) error { return nil }

var c cache

func handler(ctx fiber.Ctx, store *session.Store) error {
	sess, err := store.Get(ctx)
	if err != nil {
		return err
	}
	sess.Delete("key")
	sess.Reset()
	_ = c.Reset()
	return c.Delete("key")
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionMethods)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)
}

func Test_MigrateSessionMethods_ReportsKeys(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"sort"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

func keys(c fiber.Ctx) []string {
	sess, _ := store.Get(c)
	var names []string
	for _, k := range sess.Keys() {
		names = append(names, k.(string))
	}
	sort.Strings(names)
	return names
}
`, v3.MigrateSessionMethods)

	assert.Contains(t, got, "for _, k := range sess.Keys()")
	require.Len(t, res.notes, 1)
	assert.Equal(t, 15, res.notes[0].Line)
	assert.Contains(t, res.notes[0].Message, "Session.Keys() returns []any in v3")
	assert.Empty(t, res.out, "a note is no change")
}

func Test_MigrateSessionMethods_PackageLevelStoreOfAnotherFile(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"store.go": `package main

import sess "github.com/gofiber/fiber/v3/middleware/session"

var Store = sess.NewStore()

type App struct{ Sessions *sess.Store }

func newStore() *sess.Store { return Store }
`,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v3"

func purge(c fiber.Ctx, app *App) error {
	if err := Store.Reset(); err != nil {
		return err
	}
	if err := newStore().Delete("x"); err != nil {
		return err
	}
	return app.Sessions.Delete(c.Params("id"))
}
`,
	}, v3.MigrateSessionMethods)

	// handlers.go does not import the session package, so it is out of reach
	// of a migration keyed by that import; store.go has nothing to change.
	assert.Contains(t, res.files["handlers.go"], "Store.Reset()")
	assert.Empty(t, res.out)
}

func Test_MigrateSessionMethods_Idempotent(t *testing.T) {
	t.Parallel()

	got, first := mw3RunOne(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

func purge(c fiber.Ctx) error {
	sess, _ := store.Get(c)
	sess.SetExpiry(time.Hour)
	_ = store.Delete("x")
	return store.Reset()
}
`, v3.MigrateSessionMethods)
	require.NotEmpty(t, first.out)

	again, second := mw3RunOne(t, got, v3.MigrateSessionMethods)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}
