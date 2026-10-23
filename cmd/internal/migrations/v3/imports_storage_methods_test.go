package v3_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateStorageInterface(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"store/store.go": `package store

import (
	"sync"
	"time"
)

// Store is a storage for Fiber v2.
type Store struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *Store) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key], nil
}

func (s *Store) Set(key string, val []byte, exp time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = val
	return nil
}

func (s *Store) Delete(key string) error { delete(s.m, key); return nil }

func (s *Store) Reset() error { clear(s.m); return nil }

func (s *Store) Close() error { return nil }
`,
	})

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateStorageInterface(newCmd(&buf), dir, nil, nil))
	assert.Equal(t, "Adding the context methods to fiber.Storage implementations\n", buf.String())

	assert.Equal(t, `package store

import (
	"context"
	"sync"
	"time"
)

// Store is a storage for Fiber v2.
type Store struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *Store) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key], nil
}

func (s *Store) Set(key string, val []byte, exp time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = val
	return nil
}

func (s *Store) Delete(key string) error { delete(s.m, key); return nil }

func (s *Store) Reset() error { clear(s.m); return nil }

func (s *Store) Close() error { return nil }

// The methods below make the type a fiber.Storage of Fiber v3. They call the ones above, so a
// canceled context is not honored: TODO(fiber migrate): pass ctx on to the backing store.

// GetWithContext calls Get.
func (s *Store) GetWithContext(_ context.Context, key string) ([]byte, error) {
	return s.Get(key)
}

// SetWithContext calls Set.
func (s *Store) SetWithContext(_ context.Context, key string, val []byte, exp time.Duration) error {
	return s.Set(key, val, exp)
}

// DeleteWithContext calls Delete.
func (s *Store) DeleteWithContext(_ context.Context, key string) error {
	return s.Delete(key)
}

// ResetWithContext calls Reset.
func (s *Store) ResetWithContext(_ context.Context) error {
	return s.Reset()
}
`, importsRead(t, dir, "store/store.go"))
	assert.Empty(t, importsNotes(dir))

	var again bytes.Buffer
	require.NoError(t, v3.MigrateStorageInterface(newCmd(&again), dir, nil, nil))
	assert.Empty(t, again.String(), "idempotent")
}

func Test_MigrateStorageInterface_Receivers(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"value.go": `package a

import (
	"context"
	t "time"
)

type Value struct{}

func (v Value) Get(string) ([]byte, error) { return nil, nil }
func (v Value) Set(string, []byte, t.Duration) error { return nil }
func (v Value) Delete(string) error { return nil }
func (Value) Reset() error { return nil }
func (Value) Close() error { return nil }

var _ context.Context
`,
		"generic.go": `package a

import "time"

type Generic[V any] struct{ m map[string]V }

func (g *Generic[V]) Get(key string) ([]byte, error)                      { return nil, nil }
func (g *Generic[V]) Set(key string, val []byte, exp time.Duration) error { return nil }
func (g *Generic[V]) Delete(key string) error                             { return nil }
func (g *Generic[V]) Reset() error                                        { return nil }
func (g *Generic[V]) Close() error                                        { return nil }
`,
		"unnamed.go": `package a

import "time"

type Unnamed struct{}

func (*Unnamed) Get(key string) (value []byte, err error)                       { return }
func (*Unnamed) Set(key string, val []byte, exp time.Duration) (err error)      { return }
func (*Unnamed) Delete(key string) error                                        { return nil }
func (*Unnamed) Reset() error                                                   { return nil }
func (*Unnamed) Close() error                                                   { return nil }
`,
		"clash.go": `package a

import "time"

type Clash struct{}

func (val *Clash) Get(key string) ([]byte, error)                      { return nil, nil }
func (val *Clash) Set(key string, v []byte, exp time.Duration) error { return nil }
func (val *Clash) Delete(key string) error                             { return nil }
func (val *Clash) Reset() error                                        { return nil }
func (val *Clash) Close() error                                        { return nil }
`,
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	value := importsRead(t, dir, "value.go")
	assert.Contains(t, value, "func (v Value) GetWithContext(_ context.Context, key string) ([]byte, error) {\n\treturn v.Get(key)\n}")
	assert.Contains(t, value, "func (v Value) SetWithContext(_ context.Context, key string, val []byte, exp t.Duration) error {\n\treturn v.Set(key, val, exp)\n}",
		"the time package keeps the name the file gives it")
	assert.Equal(t, 1, strings.Count(value, `"context"`), "context was imported already")

	generic := importsRead(t, dir, "generic.go")
	assert.Contains(t, generic, "func (g *Generic[V]) DeleteWithContext(_ context.Context, key string) error {\n\treturn g.Delete(key)\n}")

	unnamed := importsRead(t, dir, "unnamed.go")
	assert.Contains(t, unnamed, "func (s *Unnamed) ResetWithContext(_ context.Context) error {\n\treturn s.Reset()\n}", "a receiver without a name gets one")

	clash := importsRead(t, dir, "clash.go")
	assert.Contains(t, clash, "func (s *Clash) SetWithContext(_ context.Context, key string, val []byte, exp time.Duration) error {\n\treturn s.Set(key, val, exp)\n}",
		"a receiver named like a parameter of the new methods is renamed")
}

func Test_MigrateStorageInterface_TheMethodsOfATypeCanBeInSeveralFiles(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"a_type.go": `package store

type Store struct{}

func (s *Store) Close() error { return nil }
`,
		"b_set.go": `package store

import "time"

func (s *Store) Set(key string, val []byte, exp time.Duration) error { return nil }

func (s *Store) Delete(key string) error { return nil }
`,
		"c_get.go": `package store

func (s *Store) Get(key string) ([]byte, error) { return nil, nil }

func (s *Store) Reset() error { return nil }
`,
		// Another package with a type of the same name is a different type.
		"other/other.go": `package store

import "time"

type Store struct{}

func (s *Store) Set(key string, val []byte, exp time.Duration) error { return nil }
`,
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, "package store\n\ntype Store struct{}\n\nfunc (s *Store) Close() error { return nil }\n", importsRead(t, dir, "a_type.go"))
	assert.Equal(t, "package store\n\nfunc (s *Store) Get(key string) ([]byte, error) { return nil, nil }\n\nfunc (s *Store) Reset() error { return nil }\n", importsRead(t, dir, "c_get.go"))

	set := importsRead(t, dir, "b_set.go")
	assert.Contains(t, set, "func (s *Store) GetWithContext(_ context.Context, key string) ([]byte, error)")
	assert.Contains(t, set, "func (s *Store) SetWithContext(_ context.Context, key string, val []byte, exp time.Duration) error")
	assert.Contains(t, set, `"context"`)
	assert.Less(t, strings.Index(set, "func (s *Store) Delete"), strings.Index(set, "TODO(fiber migrate)"), "after the last method of the type in the file")

	assert.NotContains(t, importsRead(t, dir, "other/other.go"), "WithContext", "it has no Get, Delete, Reset or Close")
}

func Test_MigrateStorageInterface_OnlyWhatIsMissing(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"store.go": `package store

import (
	"context"
	"time"
)

type Store struct{}

func (s *Store) Get(key string) ([]byte, error)                      { return nil, nil }
func (s *Store) Set(key string, val []byte, exp time.Duration) error { return nil }
func (s *Store) Delete(key string) error                             { return nil }
func (s *Store) Reset() error                                        { return nil }
func (s *Store) Close() error                                        { return nil }

func (s *Store) GetWithContext(ctx context.Context, key string) ([]byte, error) { return nil, ctx.Err() }
func (s *Store) ResetWithContext(ctx context.Context) error                     { return ctx.Err() }
`,
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	content := importsRead(t, dir, "store.go")
	assert.Equal(t, 1, strings.Count(content, "GetWithContext("), "the existing one is kept, not duplicated")
	assert.Equal(t, 1, strings.Count(content, "ResetWithContext("))
	assert.Contains(t, content, "func (s *Store) SetWithContext(_ context.Context, key string, val []byte, exp time.Duration) error")
	assert.Contains(t, content, "func (s *Store) DeleteWithContext(_ context.Context, key string) error")
	assert.Contains(t, content, "return nil, ctx.Err()")
}

func Test_MigrateStorageInterface_LeavesWhatItDoesNotRecognize(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"nolcose.go": `package a

import "time"

type NoClose struct{}

func (NoClose) Get(key string) ([]byte, error)                      { return nil, nil }
func (NoClose) Set(key string, val []byte, exp time.Duration) error { return nil }
func (NoClose) Delete(key string) error                             { return nil }
func (NoClose) Reset() error                                        { return nil }
`,
		"other.go": `package a

import "time"

type Other struct{}

func (Other) Get(key string) (string, error)                         { return "", nil }
func (Other) Set(key string, val []byte, exp time.Duration) error    { return nil }
func (Other) Delete(key string) error                                { return nil }
func (Other) Reset() error                                           { return nil }
func (Other) Close() error                                           { return nil }
`,
		"cache.go": `package a

type Cache struct{}

func (Cache) Get(key string) int                { return 0 }
func (Cache) Set(key string, val int)           {}
func (Cache) Delete(key string)                 {}
func (Cache) Reset()                            {}
func (Cache) Close()                            {}
`,
	}
	dir := importsTree(t, files)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateStorageInterface(newCmd(&buf), dir, nil, nil))

	assert.Empty(t, buf.String())
	for name := range files {
		assert.NotContains(t, importsRead(t, dir, name), "WithContext", name)
	}
}

// importsEmbeddingMethods are the five methods of Fiber v2's Storage on a type
// named Wrapped.
const importsEmbeddingMethods = `
func (w *Wrapped) Get(key string) ([]byte, error)                      { return nil, nil }
func (w *Wrapped) Set(key string, val []byte, exp time.Duration) error { return nil }
func (w *Wrapped) Delete(key string) error                             { return nil }
func (w *Wrapped) Reset() error                                        { return nil }
func (w *Wrapped) Close() error                                        { return nil }
`

// A type that embeds a storage and has the methods of Fiber v2's Storage gets the
// context methods all the same: the middleware of v3 call them, and the ones the
// embedded type brings would skip the type's own Get, Set, Delete and Reset.
func Test_MigrateStorageInterface_EmbeddingATypeThatBringsTheMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		decl string
		more string
	}{
		{
			name: "a type of another module",
			decl: "type Wrapped struct {\n\t*memory.Storage\n}\n",
			more: "import \"github.com/gofiber/storage/memory/v2\"\n\nvar _ = memory.New\n",
		},
		{
			name: "an interface of another module",
			decl: "type Wrapped struct {\n\tfiber.Storage\n}\n",
			more: "import \"github.com/gofiber/fiber/v3\"\n\nvar _ fiber.Storage\n",
		},
		{
			name: "a type of the package with one of the methods",
			decl: "type Base struct{}\n\nfunc (Base) GetWithContext(_ context.Context, key string) ([]byte, error) { return nil, nil }\n\ntype Wrapped struct {\n\tBase\n}\n",
			more: "import \"context\"\n\nvar _ context.Context\n",
		},
		{
			name: "a type that embeds one with the method",
			decl: "type Base struct{}\n\nfunc (Base) ResetWithContext(_ context.Context) error { return nil }\n\ntype Middle struct {\n\t*Base\n}\n\ntype Wrapped struct {\n\tMiddle\n}\n",
			more: "import \"context\"\n\nvar _ context.Context\n",
		},
		{
			name: "an interface of the package with one of the methods",
			decl: "type Backend interface {\n\tSetWithContext(context.Context, string, []byte, time.Duration) error\n}\n\ntype Wrapped struct {\n\tBackend\n}\n",
			more: "import \"context\"\n\nvar _ context.Context\n",
		},
		{
			name: "an alias of a type of another module",
			decl: "type Base = memory.Storage\n\ntype Wrapped struct {\n\tBase\n}\n",
			more: "import \"github.com/gofiber/storage/memory/v2\"\n\nvar _ memory.Storage\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := "package a\n\nimport \"time\"\n\n" + tt.more + "\n" + tt.decl + importsEmbeddingMethods
			dir := importsTree(t, map[string]string{"wrapped.go": src})

			var buf bytes.Buffer
			require.NoError(t, v3.MigrateStorageInterface(newCmd(&buf), dir, nil, nil))

			assert.Equal(t, "Adding the context methods to fiber.Storage implementations\n", buf.String())
			assert.Empty(t, importsNotes(dir))
			out := importsRead(t, dir, "wrapped.go")
			for _, m := range []string{"GetWithContext", "SetWithContext", "DeleteWithContext", "ResetWithContext"} {
				assert.Contains(t, out, "func (w *Wrapped) "+m+"(", m)
			}
		})
	}
}

// storageWrapper is a wrapper that counts: it embeds the storage of Fiber and
// overrides some of the methods.
const storageWrapper = `package store

import (
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Counting wraps a storage and counts the calls.
type Counting struct {
	fiber.Storage
	Gets, Sets, Deletes atomic.Int64
}

func (c *Counting) Get(key string) ([]byte, error) {
	c.Gets.Add(1)
	return c.Storage.Get(key)
}

func (c *Counting) Set(key string, val []byte, exp time.Duration) error {
	c.Sets.Add(1)
	return c.Storage.Set(key, val, exp)
}

func (c *Counting) Delete(key string) error {
	c.Deletes.Add(1)
	return c.Storage.Delete(key)
}
`

// A wrapper that embeds fiber.Storage and overrides Get, Set and Delete is
// bypassed by the middleware of v3 (which call GetWithContext of the embedded
// storage) unless it has the twins of the methods it overrides, and only those.
func Test_MigrateStorageInterface_WrapperThatOverridesSomeMethods(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{"store/counting.go": storageWrapper})

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateStorageInterface(newCmd(&buf), dir, nil, nil))
	assert.Equal(t, "Adding the context methods to fiber.Storage implementations\n", buf.String())

	got := importsRead(t, dir, "store/counting.go")
	contribRequireParses(t, got)
	assert.Contains(t, got, "func (c *Counting) GetWithContext(_ context.Context, key string) ([]byte, error) {\n\treturn c.Get(key)\n}")
	assert.Contains(t, got, "func (c *Counting) SetWithContext(_ context.Context, key string, val []byte, exp time.Duration) error {\n\treturn c.Set(key, val, exp)\n}")
	assert.Contains(t, got, "func (c *Counting) DeleteWithContext(_ context.Context, key string) error {\n\treturn c.Delete(key)\n}")
	assert.NotContains(t, got, "ResetWithContext", "Reset is the embedded one, which is what v3 should call")
	assert.Contains(t, got, "\t\"context\"\n")
	assert.Contains(t, got, "without these the calls skip this type")
	assert.Empty(t, importsNotes(dir))

	var again bytes.Buffer
	require.NoError(t, v3.MigrateStorageInterface(newCmd(&again), dir, nil, nil))
	assert.Empty(t, again.String(), "idempotent")
	assert.Equal(t, got, importsRead(t, dir, "store/counting.go"))
}

// The methods of a wrapper can be in several files, and the twin one of them
// declares already stays.
func Test_MigrateStorageInterface_WrapperMethodsInSeveralFiles(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"a.go": `package store

import "github.com/gofiber/fiber/v3"

type Tenant struct {
	fiber.Storage
	Prefix string
}
`,
		"b.go": `package store

func (t *Tenant) Get(key string) ([]byte, error) { return t.Storage.Get(t.Prefix + key) }
`,
		"c.go": `package store

import (
	"context"
	"time"
)

func (t *Tenant) Set(key string, val []byte, exp time.Duration) error {
	return t.Storage.Set(t.Prefix+key, val, exp)
}

func (t *Tenant) SetWithContext(ctx context.Context, key string, val []byte, exp time.Duration) error {
	return t.Storage.SetWithContext(ctx, t.Prefix+key, val, exp)
}
`,
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Contains(t, importsRead(t, dir, "b.go"), "func (t *Tenant) GetWithContext(_ context.Context, key string) ([]byte, error) {\n\treturn t.Get(key)\n}")
	assert.Equal(t, 1, strings.Count(importsRead(t, dir, "c.go"), "func (t *Tenant) SetWithContext("), "the twin that exists is kept")
	assert.NotContains(t, importsRead(t, dir, "a.go"), "WithContext")
}

// A type that embeds something the scan knows nothing about is a wrapper when it
// declares two of the methods; one method does not make it a storage.
func Test_MigrateStorageInterface_WrapperOfAnUnknownType(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"two.go": `package a

import (
	"time"

	"example.com/kv"
)

type Two struct {
	*kv.Store
}

func (t *Two) Get(key string) ([]byte, error)                      { return t.Store.Get(key) }
func (t *Two) Set(key string, val []byte, exp time.Duration) error { return t.Store.Set(key, val, exp) }
`,
		"one.go": `package a

import "example.com/kv"

type One struct {
	*kv.Store
}

func (o *One) Get(key string) ([]byte, error) { return o.Store.Get(key) }
`,
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	two := importsRead(t, dir, "two.go")
	assert.Contains(t, two, "func (t *Two) GetWithContext(")
	assert.Contains(t, two, "func (t *Two) SetWithContext(")
	assert.NotContains(t, two, "DeleteWithContext")
	assert.NotContains(t, importsRead(t, dir, "one.go"), "WithContext")

	// One method does not make it a storage, but it may wrap one: the note says what to do.
	notes := importsNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "one.go:9: ")
	assert.Contains(t, notes[0], "One declares Get with the signature of Fiber v2's Storage and embeds Store of example.com/kv")
	assert.Contains(t, notes[0], "call GetWithContext, which it brings, and skip Get")
}

// Delete(string) error and Reset() error are everywhere: they do not make a type that
// embeds something unreadable a storage wrapper, and a type of Fiber itself, such as
// the session store, is no storage.
func Test_MigrateStorageInterface_WrapperRuleNeedsAMethodOfAStorage(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"session.go": `package a

import "github.com/gofiber/fiber/v3/middleware/session"

type Counter struct {
	*session.Store
	resets int
}

func (c *Counter) Reset() error {
	c.resets++
	return nil
}

func (c *Counter) Delete(id string) error { return nil }

func (c *Counter) Get(key string) ([]byte, error) { return nil, nil }
`,
		"unknown.go": `package a

import "example.com/kv"

type Cleaner struct {
	*kv.Store
}

func (c *Cleaner) Delete(key string) error { return c.Store.Delete(key) }

func (c *Cleaner) Reset() error { return c.Store.Reset() }
`,
		"valued.go": `package a

import "example.com/kv"

type Cache struct {
	*kv.Store
}

func (c *Cache) Get(key string) ([]byte, error) { return c.Store.Get(key) }

func (c *Cache) Delete(key string) error { return c.Store.Delete(key) }
`,
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.NotContains(t, importsRead(t, dir, "session.go"), "WithContext")
	assert.NotContains(t, importsRead(t, dir, "unknown.go"), "WithContext")

	valued := importsRead(t, dir, "valued.go")
	assert.Contains(t, valued, "func (c *Cache) GetWithContext(")
	assert.Contains(t, valued, "func (c *Cache) DeleteWithContext(")

	assert.Empty(t, importsNotes(dir))
}

// The type a wrapper embeds can be a storage of another package of the module.
func Test_MigrateStorageInterface_WrapperOfAStorageOfAnotherPackage(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.22\n",
		"store/store.go": `package store

import "time"

type Map struct{}

func (Map) Get(key string) ([]byte, error)                      { return nil, nil }
func (Map) Set(key string, val []byte, exp time.Duration) error { return nil }
func (Map) Delete(key string) error                             { return nil }
func (Map) Reset() error                                        { return nil }
func (Map) Close() error                                        { return nil }
`,
		"metrics/metrics.go": `package metrics

import "example.com/app/store"

type Timed struct {
	*store.Map
	Calls int
}

func (t *Timed) Get(key string) ([]byte, error) {
	t.Calls++
	return t.Map.Get(key)
}
`,
		"other/other.go": `package other

import "example.com/app/store"

type Plain struct {
	*store.Map
}

func (p *Plain) Name() string { return "plain" }
`,
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Contains(t, importsRead(t, dir, "store/store.go"), "func (s Map) GetWithContext(")
	assert.Contains(t, importsRead(t, dir, "metrics/metrics.go"), "func (t *Timed) GetWithContext(_ context.Context, key string) ([]byte, error) {\n\treturn t.Get(key)\n}")
	assert.NotContains(t, importsRead(t, dir, "other/other.go"), "WithContext", "it overrides nothing")
}

func Test_MigrateStorageInterface_EmbeddingAnythingElseKeepsTheMethodsComing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		decl string
	}{
		{name: "sync.Mutex", decl: "type Wrapped struct {\n\tsync.Mutex\n\tdata map[string][]byte\n}\n"},
		{name: "a pointer to sync.RWMutex", decl: "type Wrapped struct {\n\t*sync.RWMutex\n\tdata map[string][]byte\n}\n"},
		{name: "a type of the package without those methods", decl: "type Base struct{ data map[string][]byte }\n\nfunc (Base) Len() int { return 0 }\n\ntype Wrapped struct {\n\tBase\n}\n"},
		{name: "a field that is not embedded", decl: "type Wrapped struct {\n\tbase memory.Storage\n}\n"},
		{name: "error", decl: "type Wrapped struct {\n\terror\n}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := "package a\n\nimport (\n\t\"sync\"\n\t\"time\"\n\n\t\"github.com/gofiber/storage/memory/v2\"\n)\n\nvar (\n\t_ sync.Mutex\n\t_ = memory.New\n)\n\n" + tt.decl + importsEmbeddingMethods
			dir := importsTree(t, map[string]string{"wrapped.go": src})

			var buf bytes.Buffer
			require.NoError(t, v3.MigrateStorageInterface(newCmd(&buf), dir, nil, nil))

			assert.Equal(t, "Adding the context methods to fiber.Storage implementations\n", buf.String())
			assert.Empty(t, importsNotes(dir))
			out := importsRead(t, dir, "wrapped.go")
			for _, m := range []string{"GetWithContext", "SetWithContext", "DeleteWithContext", "ResetWithContext"} {
				assert.Contains(t, out, "func (w *Wrapped) "+m+"(", m)
			}
		})
	}
}

func Test_MigrateStorageInterface_ContextImport(t *testing.T) {
	t.Parallel()

	storage := `
type Store struct{}

func (s *Store) Get(key string) ([]byte, error)                      { return nil, nil }
func (s *Store) Set(key string, val []byte, exp time.Duration) error { return nil }
func (s *Store) Delete(key string) error                             { return nil }
func (s *Store) Reset() error                                        { return nil }
func (s *Store) Close() error                                        { return nil }
`
	dir := importsTree(t, map[string]string{
		"aliased.go": "package a\n\nimport (\n\tstdctx \"context\"\n\t\"time\"\n)\n" + storage + "\nvar _ stdctx.Context\n",
		"taken.go":   "package b\n\nimport (\n\t\"time\"\n\n\t\"example.com/context\"\n)\n" + storage + "\nvar _ = context.Background\n",
		"single.go":  "package c\n\nimport \"time\"\n" + storage,
		"none.go":    "package d\n\nimport \"time\"\n" + storage,
		"groups.go":  "package e\n\nimport (\n\t\"sync\"\n\t\"time\"\n\n\t\"example.com/other\"\n)\n" + storage + "\nvar _ sync.Mutex\nvar _ other.Thing\n",
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	aliased := importsRead(t, dir, "aliased.go")
	assert.Contains(t, aliased, "func (s *Store) GetWithContext(_ stdctx.Context, key string)")
	assert.Equal(t, 1, strings.Count(aliased, `"context"`))

	taken := importsRead(t, dir, "taken.go")
	assert.Contains(t, taken, `stdcontext "context"`)
	assert.Contains(t, taken, "func (s *Store) GetWithContext(_ stdcontext.Context, key string)")

	single := importsRead(t, dir, "single.go")
	assert.Contains(t, single, "import (\n\t\"context\"\n\t\"time\"\n)")
	assert.Contains(t, single, "func (s *Store) GetWithContext(_ context.Context, key string)")

	// The package of the standard library joins the group of the standard library.
	groups := importsRead(t, dir, "groups.go")
	assert.Contains(t, groups, "import (\n\t\"context\"\n\t\"sync\"\n\t\"time\"\n\n\t\"example.com/other\"\n)")
	assert.Contains(t, groups, "func (s *Store) GetWithContext(_ context.Context, key string)")
}

func Test_MigrateStorageInterface_TestFilesAndExternalTestPackages(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"store_test.go": `package store_test

import "time"

type mock struct{}

func (mock) Get(key string) ([]byte, error)                      { return nil, nil }
func (mock) Set(key string, val []byte, exp time.Duration) error { return nil }
func (mock) Delete(key string) error                             { return nil }
func (mock) Reset() error                                        { return nil }
func (mock) Close() error                                        { return nil }
`,
		"store.go": "package store\n\ntype mock struct{}\n",
	})

	require.NoError(t, v3.MigrateStorageInterface(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Contains(t, importsRead(t, dir, "store_test.go"), "func (s mock) GetWithContext(_ context.Context, key string) ([]byte, error)")
	assert.Equal(t, "package store\n\ntype mock struct{}\n", importsRead(t, dir, "store.go"))
}
