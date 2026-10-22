package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An interface that asks for the methods of the v2 session store gets the context
// the methods of the v3 store take, and so do the types that stand in for the
// store and the calls through them.
func Test_SessionMigrations_StoreBehindAnInterface(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"store/store.go": `package store

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

// Sessions is what the application needs from a session store.
type Sessions interface {
	Get(c fiber.Ctx) (*session.Session, error)
	Delete(id string) error
	Reset() error
}

// Registry asks for one more method.
type Registry interface {
	Sessions
	RegisterType(i interface{})
}

type Service struct {
	Store Sessions
}

func (s *Service) Logout(c fiber.Ctx, id string) error {
	sess, err := s.Store.Get(c)
	if err != nil {
		return err
	}
	_ = sess
	return s.Store.Delete(id)
}

func (s *Service) Purge() error { return s.Store.Reset() }

// Counting wraps a store and counts the calls.
type Counting struct {
	Inner Sessions
	N     int
}

func (c *Counting) Get(ctx fiber.Ctx) (*session.Session, error) { return c.Inner.Get(ctx) }

func (c *Counting) Delete(id string) error {
	c.N++
	return c.Inner.Delete(id)
}

func (c *Counting) Reset() error {
	c.N++
	return c.Inner.Reset()
}
`,
		"store/fake_test.go": `package store

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

type fake struct{ resets int }

func (f *fake) Get(fiber.Ctx) (*session.Session, error) { return nil, nil }
func (f *fake) Delete(string) error                      { return nil }
func (f *fake) Reset() error                             { f.resets++; return nil }

func use() {
	f := &fake{}
	_ = f.Reset()
	_ = f.Delete("x")
	var s Sessions = f
	_ = s.Reset()
}
`,
		"main.go": `package main

import (
	"example.com/app/store"
	"github.com/gofiber/fiber/v3/middleware/session"
)

func main() {
	st := session.New()
	svc := &store.Service{Store: st}
	_ = svc.Purge()
	c := &store.Counting{Inner: st}
	_ = c.Reset()
	_ = c.Delete("x")
}
`,
	}, mw3SessionChain...)

	got := res.files["store/store.go"]
	assert.Contains(t, got, "\tDelete(ctx context.Context, id string) error\n\tReset(ctx context.Context) error\n")
	assert.Contains(t, got, "\tRegisterType(i interface{})\n")
	assert.Contains(t, got, "return s.Store.Delete(c, id)", "the context of the request")
	assert.Contains(t, got, "return s.Store.Reset(context.Background())")
	assert.Contains(t, got, "defer sess.Release()")
	assert.Contains(t, got, "func (c *Counting) Delete(ctx context.Context, id string) error {")
	assert.Contains(t, got, "return c.Inner.Delete(ctx, id)", "a wrapper passes on the context it is given")
	assert.Contains(t, got, "func (c *Counting) Reset(ctx context.Context) error {")
	assert.Contains(t, got, "return c.Inner.Reset(ctx)")

	fake := res.files["store/fake_test.go"]
	assert.Contains(t, fake, "func (f *fake) Delete(context.Context, string) error")
	assert.Contains(t, fake, "func (f *fake) Reset(ctx context.Context) error")
	assert.Contains(t, fake, "_ = f.Reset(context.Background())")
	assert.Contains(t, fake, `_ = f.Delete(context.Background(), "x")`)
	assert.Contains(t, fake, "_ = s.Reset(context.Background())")

	main := res.files["main.go"]
	assert.Contains(t, main, "_ = c.Reset(context.Background())")
	assert.Contains(t, main, `_ = c.Delete(context.Background(), "x")`)

	again := mw3RunModule(t, res.files, mw3SessionChain...)
	assert.Equal(t, res.files, again.files)
	assert.Empty(t, again.out)
}

// What does not ask for the session store is left alone: a cache has the same
// names, and an interface needs Get with the context of a request to be the
// store's.
func Test_SessionMigrations_OtherInterfacesAreNotTheStore(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

// Resetter could be anything.
type Resetter interface {
	Reset() error
	Delete(id string) error
}

// Cache is a cache.
type Cache interface {
	Get(key string) any
	Delete(key string) error
	Reset() error
}

// Mixed asks for the store and for more.
type Mixed interface {
	Get(c fiber.Ctx) (*session.Session, error)
	Reset() error
	Close() error
}

type memory struct{}

func (memory) Get(key string) any       { return nil }
func (memory) Delete(key string) error  { return nil }
func (memory) Reset() error             { return nil }
func (memory) Close() error             { return nil }

func use(r Resetter, c Cache, m Mixed) {
	_ = r.Reset()
	_ = r.Delete("a")
	_ = c.Reset()
	_ = c.Delete("b")
	_ = m.Reset()
	_ = memory{}.Reset()
}
`
	res := mw3RunModule(t, map[string]string{"main.go": src}, mw3SessionChain...)
	got := res.files["main.go"]
	assert.Contains(t, got, "\tReset() error\n\tDelete(id string) error\n")
	assert.Contains(t, got, "\tGet(key string) any\n\tDelete(key string) error\n\tReset() error\n")
	assert.Contains(t, got, "\tReset() error\n\tClose() error\n")
	assert.Contains(t, got, "_ = r.Reset()")
	assert.Contains(t, got, `_ = c.Delete("b")`)
	assert.Contains(t, got, "_ = m.Reset()")
	assert.Contains(t, got, "_ = memory{}.Reset()")
	assert.NotContains(t, got, "context")
	require.Empty(t, res.notes)
}

// A type that embeds the store and declares a method of its own stands in for
// the store with the rest.
func Test_SessionMigrations_AStoreThatOverridesAMethod(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

type Sessions interface {
	Get(c fiber.Ctx) (*session.Session, error)
	Delete(id string) error
	Reset() error
}

type Counter struct {
	*session.Store
	resets int
}

func (c *Counter) Reset() error {
	c.resets++
	return c.Store.Reset()
}

func use() Sessions {
	c := &Counter{Store: session.New()}
	_ = c.Reset()
	_ = c.Delete("x")
	return c
}
`,
	}, mw3SessionChain...)

	got := res.files["main.go"]
	assert.Contains(t, got, "func (c *Counter) Reset(ctx context.Context) error {")
	assert.Contains(t, got, "return c.Store.Reset(ctx)")
	assert.Contains(t, got, "_ = c.Reset(context.Background())")
	assert.Contains(t, got, `_ = c.Delete(context.Background(), "x")`)
}

// The name of the package context may be taken, and a parameter called ctx too.
func Test_SessionMigrations_StoreInterfaceNames(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var context = "taken"

type Sessions interface {
	Get(c fiber.Ctx) (*session.Session, error)
	Delete(id string) error
	Reset() error
}

type wrapper struct{ inner Sessions }

func (w wrapper) Get(ctx fiber.Ctx) (*session.Session, error) { return w.inner.Get(ctx) }
func (w wrapper) Delete(id string) error                       { return w.inner.Delete(id) }
func (w wrapper) Reset() error                                 { return w.inner.Reset() }

var _ = context
`,
	}, mw3SessionChain...)

	got := res.files["main.go"]
	assert.Contains(t, got, "stdcontext \"context\"")
	assert.Contains(t, got, "Delete(ctx stdcontext.Context, id string) error")
	assert.Contains(t, got, "func (w wrapper) Delete(ctx stdcontext.Context, id string) error")
	assert.Contains(t, got, "{ return w.inner.Delete(ctx, id) }")
	assert.Contains(t, got, "func (w wrapper) Reset(ctx stdcontext.Context) error")
	assert.Contains(t, got, "{ return w.inner.Reset(ctx) }")
}
