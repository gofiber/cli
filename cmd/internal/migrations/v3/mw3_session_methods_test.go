package v3_test

import (
	"strings"
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

// The keys of a v2 session were strings, and v3 gives them as any: the call is
// wrapped so that the program gets the strings it was written for.
func Test_MigrateSessionMethods_KeysAreStrings(t *testing.T) {
	t.Parallel()

	src := `package main

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
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionMethods)

	assert.Equal(t, `package main

import (
	"fmt"
	"sort"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

func keys(c fiber.Ctx) []string {
	sess, _ := store.Get(c)
	var names []string
	for _, k := range func(keys []any) []string {
		names := make([]string, len(keys))
		for i, key := range keys {
			names[i] = fmt.Sprint(key)
		}
		return names
	}(sess.Keys()) {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
`, got)
	assert.Empty(t, res.notes)
	assert.Contains(t, res.out, "Migrating session methods")

	again, second := mw3RunOne(t, got, v3.MigrateSessionMethods)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

func Test_MigrateSessionMethods_KeysWhereTheyAreUsed(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v3"
	fsession "github.com/gofiber/fiber/v3/middleware/session"
)

var store = fsession.NewStore()

func handler(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	keys := sess.Keys()
	sort.Strings(keys)
	var all []string
	all = append(all, sess.Keys()...)
	joined := strings.Join(sess.Keys(), ",")
	raw, _ := json.Marshal(sess.Keys())
	if len(sess.Keys()) > 3 {
		return c.SendString(joined + string(raw))
	}
	return c.JSON(map[string]any{
		"keys": sess.Keys(),
		"all":  all,
	})
}
`, v3.MigrateSessionMethods)

	// len() does not care what the keys are, the rest does.
	assert.Contains(t, got, "if len(sess.Keys()) > 3 {")
	assert.Equal(t, 5, strings.Count(got, "func(keys []any) []string {"), got)
	assert.Contains(t, got, "}(sess.Keys())...)")
	assert.Contains(t, got, `strings.Join(func(keys []any) []string {`)
	assert.Contains(t, got, `"keys": func(keys []any) []string {`)
	assert.Contains(t, got, `import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
`)
	assert.Empty(t, res.notes)
}

// A function literal that takes []any does not need strings either, which is how
// the rewritten call looks, so a second run leaves it alone.
func Test_MigrateSessionMethods_KeysForAnyAreLeftAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

func handler(c fiber.Ctx) int {
	sess, _ := store.Get(c)
	count := func(keys []any) int { return len(keys) }(sess.Keys())
	empty := func(prefix string, keys []interface{}) int { return len(keys) }("x", sess.Keys())
	return count + empty + len(sess.Keys())
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionMethods)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)
	assert.Empty(t, res.notes)

	// A len that the program declares is a function like any other.
	got, _ = mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v3/middleware/session"

var store = session.NewStore()

func handler(sess *session.Session) int {
	len := func(keys []string) int { return 0 }
	return len(sess.Keys())
}
`, v3.MigrateSessionMethods)
	assert.Contains(t, got, "}(sess.Keys()))")
}

// fmt is imported when the file does not, and is not taken for another name.
func Test_MigrateSessionMethods_KeysNeedFmt(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import (
	f "fmt"

	"github.com/gofiber/fiber/v3/middleware/session"
)

func show(sess *session.Session) []string {
	f.Println("keys")
	return sess.Keys()
}
`, v3.MigrateSessionMethods)
	assert.Contains(t, got, "names[i] = f.Sprint(key)")
	assert.Equal(t, 1, strings.Count(got, `"fmt"`))

	got, _ = mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v3/middleware/session"

func show(sess *session.Session) []string {
	fmt := "a name the file uses for something else"
	_ = fmt
	return sess.Keys()
}
`, v3.MigrateSessionMethods)
	assert.Contains(t, got, `stdfmt "fmt"`)
	assert.Contains(t, got, "names[i] = stdfmt.Sprint(key)")

	// The import is there, and hidden where one call is: that call uses the
	// package under a second name, the other keeps the name of the file.
	got, res := mw3RunOne(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3/middleware/session"
)

var _ = fmt.Sprint

func show(sess *session.Session) []string {
	fmt := "hidden"
	_ = fmt
	return sess.Keys()
}

func plain(sess *session.Session) []string {
	return sess.Keys()
}
`, v3.MigrateSessionMethods)
	assert.Equal(t, `package main

import (
	"fmt"
	stdfmt "fmt"

	"github.com/gofiber/fiber/v3/middleware/session"
)

var _ = fmt.Sprint

func show(sess *session.Session) []string {
	fmt := "hidden"
	_ = fmt
	return func(keys []any) []string {
		names := make([]string, len(keys))
		for i, key := range keys {
			names[i] = stdfmt.Sprint(key)
		}
		return names
	}(sess.Keys())
}

func plain(sess *session.Session) []string {
	return func(keys []any) []string {
		names := make([]string, len(keys))
		for i, key := range keys {
			names[i] = fmt.Sprint(key)
		}
		return names
	}(sess.Keys())
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionMethods_KeysOnlyOfSessions(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3/middleware/session"
)

var _ = session.NewStore

type registry struct{ names map[string]bool }

func (r registry) Keys() []string {
	var keys []string
	for k := range r.names {
		keys = append(keys, k)
	}
	return keys
}

func list(r registry, other interface{ Keys() []string }) []string {
	return append(r.Keys(), other.Keys()...)
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionMethods)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)
	assert.Empty(t, res.notes)
}

// A handler file need not import the session package to use a store that
// another file of the package declares.
func Test_MigrateSessionMethods_PackageLevelStoreOfAnotherFile(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"store.go": `package main

import sess "github.com/gofiber/fiber/v3/middleware/session"

var Store = sess.NewStore()

type App struct{ Sessions *sess.Store }

func newStore() *sess.Store { return Store }

func get() *sess.Session { return nil }
`,
		"handlers.go": `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
)

func purge(c fiber.Ctx, app *App) error {
	if err := Store.Reset(); err != nil {
		return err
	}
	if err := newStore().Delete("x"); err != nil {
		return err
	}
	get().SetExpiry(time.Hour)
	return app.Sessions.Delete(c.Params("id"))
}
`,
		"other.go": `package main

type cache struct{}

func (cache) Reset() error { return nil }

func clear(c cache) error { return c.Reset() }
`,
	}, v3.MigrateSessionMethods)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
)

func purge(c fiber.Ctx, app *App) error {
	if err := Store.Reset(c); err != nil {
		return err
	}
	if err := newStore().Delete(c, "x"); err != nil {
		return err
	}
	get().SetIdleTimeout(time.Hour)
	return app.Sessions.Delete(c, c.Params("id"))
}
`, res.files["handlers.go"])
	assert.Contains(t, res.files["other.go"], "return c.Reset()")
	assert.Empty(t, res.notes)
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
