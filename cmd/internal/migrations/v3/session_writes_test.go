package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// v2 never used the KeyLookup and CookieName of a store to look anything up: they
// only held what the store reported, and the lookup was fixed when the store was
// made. The assignments to them are dropped, as v3 has no such fields.
func Test_MigrateSessionExtractor_StoreWritesAreDropped(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New()

func suffix() string { return os.Getenv("SUFFIX") }

func write(other *session.Store) {
	store.KeyLookup = "header:X-Session" // never changed where the store looks
	store.CookieName += suffix()
	other.KeyLookup, _ = "a", 1
	if store.KeyLookup = "query:sid"; len(os.Args) > 1 {
		fmt.Println("args")
	}
	store.Config.CookieName = "sid"
}
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New()

func suffix() string { return os.Getenv("SUFFIX") }

func write(other *session.Store) {
	_ = suffix()
	_, _ = "a", 1
	if _ = "query:sid"; len(os.Args) > 1 {
		fmt.Println("args")
	}
}
`, got)

	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 5)
	assert.Contains(t, msgs[0], `store.KeyLookup = "header:X-Session"`)
	assert.Contains(t, msgs[0], "the assignment was removed")
	assert.Contains(t, msgs[0], "v2 only changed what the store reports with it")
	assert.Contains(t, msgs[1], "store.CookieName += ...")
	assert.Contains(t, msgs[1], "the target was replaced by _, so the value is still evaluated")
	assert.Contains(t, msgs[2], "other.KeyLookup = ...")
	assert.Contains(t, msgs[3], "store.KeyLookup = ...")
	assert.Contains(t, msgs[4], `store.Config.CookieName = "sid"`)

	again, second := mw3RunOne(t, got, v3.MigrateSessionExtractor)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

// The address of the field of a store is the address of a copy of what the store
// reports: nothing written through it ever reached the store.
func Test_MigrateSessionReads_AddressOfAStoreField(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"flag"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{KeyLookup: "header:X-ID"})

func main() {
	flag.StringVar(&store.KeyLookup, "lookup", "", "the lookup")
	p := &(store.CookieName)
	_ = p
}
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"flag"

	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

var store = session.New(session.Config{Extractor: extractors.FromHeader("X-ID")})

func main() {
	flag.StringVar(func() *string {
		v := func(e extractors.Extractor) string {
			if n := len(e.Chain); n > 0 {
				e = e.Chain[n-1]
			}
			switch e.Source {
			case extractors.SourceHeader:
				return "header:" + e.Key
			case extractors.SourceQuery:
				return "query:" + e.Key
			}
			return "cookie:" + e.Key
		}(store.Extractor)
		return &v
	}(), "lookup", "", "the lookup")
	p := func() *string {
		v := func(e extractors.Extractor) string {
			if e.Source == extractors.SourceCookie {
				return e.Key
			}
			return ""
		}(store.Extractor)
		return &v
	}()
	_ = p
}
`, got)

	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0], "&store.KeyLookup is the address of a copy of the KeyLookup that the store reports now")
	assert.Contains(t, msgs[0], "writing through the pointer never changed where the store looks")
	assert.Contains(t, msgs[1], "&(store.CookieName) is the address of a copy of the CookieName")

	again, second := mw3RunOne(t, got, v3.MigrateSessionExtractor)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

// A variable that the dropped assignment was the last use of stays used, or the
// function would not compile.
func Test_MigrateSessionExtractor_StoreWritesKeepVariablesUsed(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var stores []*session.Store

func configure(own *session.Store) {
	own.KeyLookup = "cookie:own"
	for _, s := range stores {
		s.KeyLookup = "header:each"
	}
	alias := &own.Config
	alias.CookieName = "sid"
	used := session.New()
	used.CookieName = "sid"
	used.Reset()
}
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var stores []*session.Store

func configure(own *session.Store) {
	for _, s := range stores {
		_ = s
	}
	alias := &own.Config
	_ = alias
	used := session.New()
	used.Reset()
}
`, got)
}

// The init and the post statement of a for, the init of a switch, the body of a
// closure that is called, started or deferred, and a tuple assignment are places
// where an assignment is no statement of a list: the target is replaced by _, or
// the body is left empty, and the program compiles and looks where it did.
func Test_MigrateSessionExtractor_StoreWritesInLoopsAndClosures(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New()

func odd() {
	for store.CookieName = "a"; len(os.Args) > 5; store.KeyLookup = "b" {
		fmt.Println("loop")
	}
	func() { store.KeyLookup = "x" }()
	done := make(chan struct{})
	go func() {
		store.KeyLookup = "y"
		close(done)
	}()
	<-done
	defer func() { store.CookieName = "z" }()
	store.KeyLookup, store.CookieName = "a", "b"
	switch store.KeyLookup = "q"; {
	case len(os.Args) > 3:
		fmt.Println("three")
	}
}
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New()

func odd() {
	for _ = "a"; len(os.Args) > 5; _ = "b" {
		fmt.Println("loop")
	}
	func() {}()
	done := make(chan struct{})
	go func() {
		close(done)
	}()
	<-done
	defer func() {}()
	_, _ = "a", "b"
	switch _ = "q"; {
	case len(os.Args) > 3:
		fmt.Println("three")
	}
}
`, got)
	assert.Len(t, mw3Messages(res.notes), 8)

	again, second := mw3RunOne(t, got, v3.MigrateSessionExtractor)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

// A statement that shares its line with other code, and one that is not in a
// list of statements, keep what is around them.
func Test_MigrateSessionExtractor_StoreWritesInOddPlaces(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var store = session.New()

func odd(cond bool) {
	if cond {
		store.KeyLookup = "cookie:a"
	}
	switch {
	case cond:
		store.CookieName = "b"
	default:
		store.KeyLookup = "header:c"
	}
	store.KeyLookup = "cookie:d"; _ = cond
}
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var store = session.New()

func odd(cond bool) {
	if cond {
	}
	switch {
	case cond:
	default:
	}
	_ = cond
}
`, got)
}
