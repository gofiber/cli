package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// mw3AuthModule is a module whose main package does not import the session
// middleware: the stores live in the packages it imports.
func mw3AuthModule() map[string]string {
	return map[string]string{
		"internal/auth/auth.go": `package auth

import "github.com/gofiber/fiber/v3/middleware/session"

var Store = session.New()

func NewStore() *session.Store {
	return session.New(session.Config{KeyLookup: "header:X-Auth"})
}

type Base struct{ *session.Store }

type Wrapper struct {
	Base
	Name string
}

func Default() Wrapper { return Wrapper{} }
`,
		"internal/handlers/handlers.go": `package handlers

import "github.com/gofiber/fiber/v3/middleware/session"

type Handlers struct {
	Sessions *session.Store
	All      []*session.Store
}

func New(s *session.Store) *Handlers { return &Handlers{Sessions: s} }
`,
	}
}

// A variable, a function, a type that embeds a store or a field of another
// package of the module is a store, in a file that does not import the session
// middleware either.
func Test_SessionMigrations_StoresOfOtherPackages(t *testing.T) {
	t.Parallel()

	files := mw3AuthModule()
	files["main.go"] = `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"

	"example.com/app/internal/auth"
	"example.com/app/internal/handlers"
)

func main() {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		sess, err := auth.Store.Get(c)
		if err != nil {
			return err
		}
		_ = sess.Keys()
		auth.Store.Reset()
		st := auth.NewStore()
		st.Delete("x")
		w := auth.Default()
		w.Reset()
		fmt.Println(st.KeyLookup, w.CookieName)
		h := handlers.New(st)
		h.Sessions.Reset()
		for _, s := range h.All {
			s.Delete("y")
		}
		sess.SetExpiry(time.Hour)
		return nil
	})
}
`
	res := mw3RunModule(t, files, mw3SessionChain...)

	got := res.files["main.go"]
	assert.Contains(t, got, "defer sess.Release() // Important: Manual cleanup required")
	assert.Contains(t, got, "auth.Store.Reset(c)")
	assert.Contains(t, got, `st.Delete(c, "x")`)
	assert.Contains(t, got, "w.Reset(c)")
	assert.Contains(t, got, "h.Sessions.Reset(c)")
	assert.Contains(t, got, `s.Delete(c, "y")`)
	assert.Contains(t, got, "sess.SetIdleTimeout(time.Hour)")
	assert.Contains(t, got, "}(st.Extractor), func(e extractors.Extractor) string {")
	assert.Contains(t, got, "}(w.Extractor))")
	assert.Contains(t, got, "fmt.Sprint(key)")
	assert.NotContains(t, got, "KeyLookup")
	assert.NotContains(t, got, "session\"")

	// The packages that declare them are migrated as before.
	assert.Contains(t, res.files["internal/auth/auth.go"], "session.NewStore(session.Config{Extractor: extractors.FromHeader(\"X-Auth\"), IdleTimeout: 24 * time.Hour})")

	again := mw3RunModule(t, res.files, mw3SessionChain...)
	assert.Equal(t, res.files, again.files)
}

// Without a module there are no other packages to look at.
func Test_SessionMigrations_NoModuleNoOtherPackages(t *testing.T) {
	t.Parallel()

	files := mw3AuthModule()
	files["main.go"] = `package main

import "example.com/app/internal/auth"

func main() {
	auth.Store.Reset()
}
`
	res := mw3Run(t, files, mw3SessionChain...)
	assert.Contains(t, res.files["main.go"], "auth.Store.Reset()")
}

// A package that is called like a variable is the variable where one hides it.
func Test_SessionMigrations_ALocalHidesThePackage(t *testing.T) {
	t.Parallel()

	files := mw3AuthModule()
	files["main.go"] = `package main

import "example.com/app/internal/auth"

type local struct{ Store *plain }

type plain struct{}

func (*plain) Reset() error { return nil }

func main() {
	_ = auth.Store.Reset
	{
		auth := local{Store: &plain{}}
		_ = auth.Store.Reset()
	}
}
`
	res := mw3RunModule(t, files, mw3SessionChain...)
	assert.Contains(t, res.files["main.go"], "_ = auth.Store.Reset()")
	assert.Contains(t, res.files["main.go"], "_ = auth.Store.Reset\n")
}

// A package that gives the session types other names, and a generic type that
// holds a store.
func Test_SessionMigrations_ReExportsAndGenericTypes(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"internal/sess/sess.go": `package sess

import "github.com/gofiber/fiber/v3/middleware/session"

type Store = session.Store
type Session = session.Session
type Config = session.Config

func New(cfg ...Config) *Store { return session.New(cfg...) }

var Default = New()
`,
		"internal/repo/repo.go": `package repo

import "example.com/app/internal/sess"

type Cache[T any] struct {
	store *sess.Store
	items map[string]T
}

func NewCache[T any]() *Cache[T] { return &Cache[T]{store: sess.New()} }

func (c *Cache[T]) Store() *sess.Store { return c.store }

func (c *Cache[T]) Flush() error { return c.store.Reset() }
`,
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"

	"example.com/app/internal/repo"
	"example.com/app/internal/sess"
)

func main() {
	cache := repo.NewCache[int]()
	cache.Store().Delete("x")
	_ = cache.Flush()
	sess.Default.Reset()
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		s, _ := sess.Default.Get(c)
		_ = s.Keys()
		return nil
	})
}
`,
	}, mw3SessionChain...)

	assert.Contains(t, res.files["internal/sess/sess.go"], "session.NewStore(cfg...)")
	assert.Contains(t, res.files["internal/repo/repo.go"], "c.store.Reset(context.Background())")
	main := res.files["main.go"]
	assert.Contains(t, main, `cache.Store().Delete(context.Background(), "x")`)
	assert.Contains(t, main, "sess.Default.Reset(context.Background())")
	assert.Contains(t, main, "defer s.Release()")
	assert.Contains(t, main, "fmt.Sprint(key)")
}

// A session that an interface of the program hands out is released like one that
// comes from the store.
func Test_SessionMigrations_SessionsOfAnInterface(t *testing.T) {
	t.Parallel()

	res := mw3RunModule(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

type Sessions interface {
	Get(c fiber.Ctx) (*session.Session, error)
	Name() string
}

func handler(c fiber.Ctx, s Sessions) error {
	ss, err := s.Get(c)
	if err != nil {
		return err
	}
	_ = ss.ID()
	return nil
}
`,
	}, mw3SessionChain...)

	assert.Contains(t, res.files["main.go"], "defer ss.Release() // Important: Manual cleanup required")
}

// The test files of a package see its declarations, and so do the external test
// packages of the module.
func Test_SessionMigrations_TestFilesOfOtherPackages(t *testing.T) {
	t.Parallel()

	files := mw3AuthModule()
	files["internal/auth/auth_test.go"] = `package auth

func reset() { Store.Reset() }
`
	files["internal/auth/auth_ext_test.go"] = `package auth_test

import "example.com/app/internal/auth"

func reset() { auth.Store.Reset() }
`
	res := mw3RunModule(t, files, mw3SessionChain...)

	assert.Contains(t, res.files["internal/auth/auth_test.go"], "Store.Reset(context.Background())")
	assert.Contains(t, res.files["internal/auth/auth_ext_test.go"], "auth.Store.Reset(context.Background())")
}

// Reads and writes of the lookup of a store of another package.
func Test_SessionMigrations_LookupOfAStoreOfAnotherPackage(t *testing.T) {
	t.Parallel()

	files := mw3AuthModule()
	files["main.go"] = `package main

import (
	"fmt"

	"example.com/app/internal/auth"
	"example.com/app/internal/handlers"
)

func main() {
	auth.Store.KeyLookup = "header:X"
	h := handlers.New(auth.NewStore())
	h.Sessions.CookieName = "sid"
	fmt.Println(auth.Store.KeyLookup, h.Sessions.CookieName)
}
`
	res := mw3RunModule(t, files, v3.MigrateSessionConfig, v3.MigrateSessionExtractor)

	got := res.files["main.go"]
	assert.NotContains(t, got, "= \"header:X\"")
	assert.NotContains(t, got, "sid\"\n")
	assert.Contains(t, got, "}(auth.Store.Extractor), func(e extractors.Extractor) string {")
	assert.Contains(t, got, "}(h.Sessions.Extractor))")
	msgs := mw3Messages(res.notes)
	require.NotEmpty(t, msgs)
}
