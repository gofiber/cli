package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The migrations find a store, a session or a config by what the code says they
// are. A store is reset with the context of the request now: that tells whether
// the receiver was found.
func mw3ResetsOf(t *testing.T, files map[string]string) map[string]string {
	t.Helper()

	return mw3Run(t, files, v3.MigrateSessionMethods).files
}

func Test_MigrateSessionMethods_StoresBehindFieldsOfFields(t *testing.T) {
	t.Parallel()

	files := mw3ResetsOf(t, map[string]string{
		"types.go": `package main

import "github.com/gofiber/fiber/v3/middleware/session"

type deps struct {
	sessions *session.Store
}

type server struct {
	*deps
	name string
}

type app struct {
	srv    *server
	direct *session.Store
	list   []*session.Store
}
`,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v3"

func purge(c fiber.Ctx, a *app) error {
	_ = a.direct.Reset()
	_ = a.srv.sessions.Reset()
	_ = a.srv.deps.sessions.Reset()
	_ = a.list[0].Reset()
	return a.srv.sessions.Delete("x")
}

func unrelated(c fiber.Ctx, a *app) error {
	return a.srv.name.Reset()
}
`,
	})

	handlers := files["handlers.go"]
	assert.Equal(t, 4, strings.Count(handlers, ".Reset(c)"), handlers)
	assert.Contains(t, handlers, `a.srv.sessions.Delete(c, "x")`)
	assert.Contains(t, handlers, "a.srv.name.Reset()", "a field that holds something else is left alone")
}

func Test_MigrateSessionMethods_StoresOfCollections(t *testing.T) {
	t.Parallel()

	files := mw3ResetsOf(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var stores = []*session.Store{session.NewStore()}

type registry map[string]*session.Store

func purge(c fiber.Ctx, extra ...*session.Store) error {
	for _, s := range extra {
		_ = s.Reset()
	}
	for _, s := range stores {
		_ = s.Reset()
	}
	for i := range stores {
		_ = stores[i].Reset()
	}
	byName := map[string]*session.Store{}
	_ = byName["x"].Reset()
	for name, s := range byName {
		_ = s.Delete(name)
	}
	var reg registry
	_ = reg["y"].Reset()
	made := make([]*session.Store, 2)
	_ = made[1].Reset()
	return extra[0].Reset()
}
`,
	})

	got := files["main.go"]
	assert.Equal(t, 7, strings.Count(got, ".Reset(c)"), got)
	assert.Contains(t, got, "s.Delete(c, name)")
	assert.NotContains(t, got, ".Reset()")
}

func Test_MigrateSessionMethods_StoresFromFunctionsAndMethods(t *testing.T) {
	t.Parallel()

	files := mw3ResetsOf(t, map[string]string{
		"main.go": `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

type app struct{ store *session.Store }

func (a *app) Store() *session.Store { return a.store }

func (a *app) session(c fiber.Ctx) (*session.Session, error) { return a.store.Get(c) }

func newApp() *app { return &app{store: session.NewStore()} }

var global = newApp()

func work(c fiber.Ctx, get func() *session.Store) error {
	a := newApp()
	_ = a.Store().Reset()
	_ = newApp().Store().Reset()
	_ = global.Store().Reset()
	_ = get().Reset()
	make := func() *session.Store { return a.store }
	_ = make().Reset()
	sess, err := a.session(c)
	if err != nil {
		return err
	}
	sess.SetExpiry(time.Hour)
	return nil
}
`,
	})

	got := files["main.go"]
	assert.Equal(t, 5, strings.Count(got, ".Reset(c)"), got)
	assert.Contains(t, got, "sess.SetIdleTimeout(time.Hour)")
}

func Test_MigrateSessionMethods_StoresFromTypesAndAssertions(t *testing.T) {
	t.Parallel()

	files := mw3ResetsOf(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

type SessionStore = session.Store

type Mine session.Store

type Stores []*session.Store

func work(c fiber.Ctx, a *SessionStore, m *Mine, all Stores, v any) error {
	_ = a.Reset()
	_ = m.Reset()
	_ = all[0].Reset()
	_ = c.Locals("store").(*session.Store).Reset()
	switch s := v.(type) {
	case *session.Store:
		return s.Reset()
	case *Mine:
		return s.Reset()
	}
	return nil
}
`,
	})

	got := files["main.go"]
	// The type that is defined from the store has none of its methods.
	assert.Contains(t, got, "_ = m.Reset()")
	assert.Contains(t, got, "case *Mine:\n\t\treturn s.Reset()\n")
	assert.Contains(t, got, "_ = a.Reset(c)")
	assert.Contains(t, got, "_ = all[0].Reset(c)")
	assert.Contains(t, got, `_ = c.Locals("store").(*session.Store).Reset(c)`)
	assert.Equal(t, 1, strings.Count(got, "return s.Reset(c)"), got)
	assert.Equal(t, 1, strings.Count(got, "return s.Reset()"), got)
}

// A declaration of the function hides the package level name, and what is
// declared as something else is not a store.
func Test_MigrateSessionMethods_ShadowedNamesAreNotStores(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

type cache struct{}

func (cache) Reset() error { return nil }

func work(c fiber.Ctx, others []cache) error {
	for _, store := range others {
		_ = store.Reset()
	}
	if store := (cache{}); true {
		_ = store.Reset()
	}
	var sessions []cache
	for _, s := range sessions {
		_ = s.Reset()
	}
	return nil
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionMethods)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)
}

func Test_MigrateSessionMethods_OtherPackagesNamedSession(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"example.com/aws/session"

	"github.com/gofiber/fiber/v3"
)

type app struct {
	sessions *session.Session
	pool     []*session.Session
}

func work(c fiber.Ctx, a *app, list []*session.Session) {
	a.sessions.Keys()
	a.pool[0].SetExpiry(1)
	for _, s := range list {
		s.Keys()
		s.SetExpiry(2)
	}
	session.New().Reset()
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionMethods)
	assert.Equal(t, src, got, "the session package of another module is not Fiber's")
	assert.Empty(t, res.out)
	assert.Empty(t, res.notes)
}

// Sessions are found in the same ways as stores are.
func Test_MigrateSessionMethods_SessionsOfFieldsCollectionsAndAssertions(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"holder.go": `package main

import "github.com/gofiber/fiber/v3/middleware/session"

type holder struct {
	sess  *session.Session
	other string
	all   map[string]*session.Session
}
`,
		"use.go": `package main

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

func (h *holder) keys() []string { return h.sess.Keys() }

func (h *holder) touch() {
	h.sess.SetExpiry(time.Hour)
	for _, s := range h.all {
		s.SetExpiry(time.Minute)
	}
}

func fromLocals(c fiber.Ctx) []string {
	sess := c.Locals("session").(*session.Session)
	asserted := c.Locals("session").(*session.Session).Keys()
	_ = asserted
	return sess.Keys()
}

func unrelated(h *holder) []string { return h.other.Keys() }
`,
	}, v3.MigrateSessionMethods)

	use := res.files["use.go"]
	assert.Equal(t, 3, strings.Count(use, "func(keys []any) []string {"), use)
	assert.Contains(t, use, "}(h.sess.Keys())")
	assert.Contains(t, use, "}(sess.Keys())")
	assert.Contains(t, use, "}(c.Locals(\"session\").(*session.Session).Keys())")
	assert.Contains(t, use, "h.sess.SetIdleTimeout(time.Hour)")
	assert.Contains(t, use, "s.SetIdleTimeout(time.Minute)")
	assert.Contains(t, use, "return h.other.Keys()")
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionRelease_SessionsOfStoresBehindFieldsAndCollections(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"types.go": `package main

import "github.com/gofiber/fiber/v3/middleware/session"

type deps struct{ sessions *session.Store }

type server struct{ *deps }

var stores []*session.Store
`,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v3"

func nested(c fiber.Ctx, s *server) error {
	sess, err := s.sessions.Get(c)
	if err != nil {
		return err
	}
	return sess.Save()
}

func listed(c fiber.Ctx) error {
	sess, err := stores[0].Get(c)
	if err != nil {
		return err
	}
	return sess.Save()
}

func ranged(c fiber.Ctx, extra ...*session.Store) {
	for _, st := range extra {
		_ = st
	}
}
`,
	}, v3.MigrateSessionRelease)

	handlers := res.files["handlers.go"]
	assert.Equal(t, 2, strings.Count(handlers, "defer sess.Release()"), handlers)

	again := mw3Run(t, res.files, v3.MigrateSessionRelease)
	assert.Equal(t, res.files, again.files)
	assert.Empty(t, again.out)
}

func Test_MigrateSessionConfig_ExpirationOfStoresBehindFields(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
)

type app struct{ sessions *session.Store }

type env struct{ app *app }

func show(e *env, cfgs []session.Config) {
	fmt.Println(e.app.sessions.Expiration)
	for _, c := range cfgs {
		fmt.Println(c.Expiration)
	}
}
`, v3.MigrateSessionConfig)

	assert.Contains(t, got, "e.app.sessions.IdleTimeout")
	assert.Contains(t, got, "c.IdleTimeout")
	assert.NotContains(t, got, "Expiration")
}

// The reads of the lookup follow the stores the same way.
func Test_MigrateSessionReads_StoresBehindFieldsAndCollections(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"types.go": `package main

import "github.com/gofiber/fiber/v2/middleware/session"

type deps struct {
	sessions *session.Store
	config   session.Config
}

type server struct{ *deps }
`,
		"use.go": `package main

import "github.com/gofiber/fiber/v2/middleware/session"

func show(s *server, stores ...*session.Store) []string {
	out := []string{s.sessions.KeyLookup, s.config.KeyLookup, s.deps.config.CookieName}
	for _, st := range stores {
		out = append(out, st.KeyLookup)
	}
	return out
}
`,
	}, v3.MigrateSessionExtractor)

	use := res.files["use.go"]
	assert.NotContains(t, use, ".KeyLookup")
	assert.NotContains(t, use, ".CookieName")
	assert.Contains(t, use, "}(s.sessions.Extractor)")
	assert.Contains(t, use, "}(s.config.Extractor)")
	assert.Contains(t, use, "}(st.Extractor)")
	assert.Len(t, res.notes, 4)
	require.NotEmpty(t, res.notes)
}

func Test_MigrateSessionExtractor_AssignmentsToFieldsOfConfigs(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

type options struct {
	Session session.Config
}

func configure(o *options) {
	o.Session.KeyLookup = "header:X-SID"
	o.Session.CookieName = "sid"
}
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `o.Session.Extractor = extractors.FromHeader("X-SID")`)
	assert.Contains(t, got, `o.Session.Extractor = extractors.FromCookie("sid")`)
	assert.Empty(t, res.notes)
}
