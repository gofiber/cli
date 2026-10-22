package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A type that embeds a store has its methods and its config: what is done with
// the type is done with the store.
func Test_SessionMigrations_PromotedFromAnEmbeddedStore(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"main.go": `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

type Handlers struct {
	*session.Store
	name string
}

type Server struct {
	Handlers
	addr string
}

func (h *Handlers) logout(c fiber.Ctx, id string) error {
	sess, err := h.Get(c)
	if err != nil {
		return err
	}
	fmt.Println(sess.Keys())
	sess.SetExpiry(time.Minute)
	return h.Delete(id)
}

func purge(s *Server, h Handlers) error {
	h.Expiration = time.Hour
	s.Expiration = time.Hour
	fmt.Println(s.KeyLookup, h.CookieName)
	if err := h.Reset(); err != nil {
		return err
	}
	return s.Reset()
}
`,
	}, mw3SessionChain...)

	got := res.files["main.go"]
	assert.Contains(t, got, "sess, err := h.Get(c)\n\tif err != nil {\n\t\treturn err\n\t}\n\tdefer sess.Release()")
	assert.Contains(t, got, "sess.SetIdleTimeout(time.Minute)")
	assert.Contains(t, got, "return h.Delete(c, id)")
	assert.Contains(t, got, "h.IdleTimeout = time.Hour")
	assert.Contains(t, got, "s.IdleTimeout = time.Hour")
	assert.Contains(t, got, "if err := h.Reset(context.Background()); err != nil {")
	assert.Contains(t, got, "return s.Reset(context.Background())")
	assert.Contains(t, got, "}(s.Extractor), func(e extractors.Extractor) string {")
	assert.Contains(t, got, "}(h.Extractor))")
	assert.Contains(t, got, "fmt.Sprint(key)")
	assert.NotContains(t, got, "KeyLookup")
	assert.NotContains(t, got, "Expiration")

	again := mw3RunModule(t, res.files, mw3SessionChain...)
	assert.Equal(t, res.files, again.files)
}

// The members the type declares itself, a method or a field, hide the ones it
// would get from the store.
func Test_SessionMigrations_OwnMembersHideThePromotedOnes(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

type Handlers struct {
	*session.Store
	KeyLookup string
}

func (h *Handlers) Reset() error { return nil }

func (h *Handlers) Delete(id string) error { return nil }

type Wrapper struct {
	Handlers
}

func use(c fiber.Ctx, h *Handlers, w Wrapper) {
	_ = h.Reset()
	_ = h.Delete("a")
	_ = w.Reset()
	_ = w.Delete("b")
	fmt.Println(h.KeyLookup, w.KeyLookup)
	sess, _ := h.Get(c)
	_ = sess
}
`
	res := mw3RunModule(t, map[string]string{"main.go": src}, mw3SessionChain...)
	got := res.files["main.go"]

	// Only Get is the store's.
	assert.Contains(t, got, "_ = h.Reset()")
	assert.Contains(t, got, `_ = h.Delete("a")`)
	assert.Contains(t, got, "_ = w.Reset()")
	assert.Contains(t, got, `_ = w.Delete("b")`)
	assert.Contains(t, got, "fmt.Println(h.KeyLookup, w.KeyLookup)")
	assert.Contains(t, got, "defer sess.Release()")
	assert.Empty(t, res.notes)
}

// A type that embeds a session has the methods of a session.
func Test_SessionMigrations_PromotedFromAnEmbeddedSession(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"main.go": `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3/middleware/session"
)

type Current struct {
	*session.Session
	user string
}

func show(cur Current, list []Current) {
	cur.SetExpiry(time.Minute)
	fmt.Println(cur.Keys(), len(list[0].Keys()))
	cur.Delete("key")
	cur.Reset()
}
`,
	}, mw3SessionChain...)

	got := res.files["main.go"]
	assert.Contains(t, got, "cur.SetIdleTimeout(time.Minute)")
	assert.Contains(t, got, "}(cur.Keys()), len(list[0].Keys()))")
	assert.Contains(t, got, `cur.Delete("key")`)
	assert.Contains(t, got, "cur.Reset()")
	assert.NotContains(t, got, "context")
}

// A config a type embeds by value is a config, and one that is built by hand.
func Test_SessionMigrations_PromotedFromAnEmbeddedConfig(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"main.go": `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3/middleware/session"
)

type Options struct {
	session.Config
	Name string
}

func build() *session.Store {
	o := Options{Name: "app"}
	o.KeyLookup = "header:X-ID"
	o.Expiration = time.Hour
	fmt.Println(o.KeyLookup)
	return session.New(o.Config)
}
`,
	}, mw3SessionChain...)

	got := res.files["main.go"]
	assert.Contains(t, got, `o.Extractor = extractors.FromHeader("X-ID")`)
	assert.Contains(t, got, "o.IdleTimeout = time.Hour")
	assert.Contains(t, got, "if e.Extract == nil {")
	assert.Contains(t, got, "}(o.Extractor))")
	assert.NotContains(t, got, "KeyLookup")
}

// A type the source does not show, embedded above the store, may declare the
// name: the call is reported and left, unless the type is a lock.
func Test_SessionMigrations_AHiddenTypeAboveTheStore(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"main.go": `package main

import (
	"database/sql"
	"sync"

	"github.com/gofiber/fiber/v3/middleware/session"
)

type Inner struct {
	*session.Store
}

type Locked struct {
	Inner
	sync.Mutex
}

type Shared struct {
	Inner
	*sql.DB
}

func use(l *Locked, s *Shared) {
	_ = l.Reset()
	_ = s.Reset()
}
`,
	}, mw3SessionChain...)

	got := res.files["main.go"]
	assert.Contains(t, got, "_ = l.Reset(context.Background())")
	assert.Contains(t, got, "_ = s.Reset()")
	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "s.Reset: if this is the Reset of the session type that its receiver embeds, Reset takes a context in v3")
}

// Embedding is followed through the files of the package, and a type of the
// package that embeds something else is not a store.
func Test_SessionMigrations_EmbeddingAcrossFiles(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"types.go": `package main

import "github.com/gofiber/fiber/v3/middleware/session"

type Base struct {
	*session.Store
}

type Service struct {
	Base
}

type Plain struct {
	name string
}

type Cache struct {
	Plain
}
`,
		"use.go": `package main

func reset(s *Service, c *Cache) {
	_ = s.Reset()
	_ = c.Reset()
}
`,
	}, mw3SessionChain...)

	assert.Contains(t, res.files["use.go"], "_ = s.Reset(context.Background())")
	assert.Contains(t, res.files["use.go"], "_ = c.Reset()")
	assert.Empty(t, res.notes)
}
