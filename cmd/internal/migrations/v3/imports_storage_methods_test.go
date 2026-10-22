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

func Test_MigrateStorageInterface_EmbeddingMayBringTheMethods(t *testing.T) {
	t.Parallel()

	src := `package a

import (
	"sync"
	"time"
)

type Wrapped struct {
	sync.Mutex
	inner map[string][]byte
}

func (w *Wrapped) Get(key string) ([]byte, error)                      { return nil, nil }
func (w *Wrapped) Set(key string, val []byte, exp time.Duration) error { return nil }
func (w *Wrapped) Delete(key string) error                             { return nil }
func (w *Wrapped) Reset() error                                        { return nil }
func (w *Wrapped) Close() error                                        { return nil }
`
	dir := importsTree(t, map[string]string{"wrapped.go": src})

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateStorageInterface(newCmd(&buf), dir, nil, nil))

	assert.Empty(t, buf.String())
	assert.Equal(t, src, importsRead(t, dir, "wrapped.go"))
	notes := importsNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "wrapped.go:14: ")
	assert.Contains(t, notes[0], "Wrapped has the methods of Fiber v2's Storage but embeds another type")
	assert.Contains(t, notes[0], "GetWithContext, SetWithContext, DeleteWithContext, ResetWithContext")
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
