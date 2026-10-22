package v3_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The v2 store embeds its defaulted config, so a program can read the lookup back
// from it. v3 keeps the lookup in the Extractor: the strings v2 gave are computed
// from it.
func Test_MigrateSessionReads_Store(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store *session.Store

func init() { store = session.New(session.Config{KeyLookup: "header:X-SID"}) }

func lookup() string {
	return store.KeyLookup
}

func cookie() string { return store.CookieName }

var _ = fmt.Sprint
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

var store *session.Store

func init() { store = session.New(session.Config{Extractor: extractors.FromHeader("X-SID")}) }

func lookup() string {
	return func(e extractors.Extractor) string {
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
}

func cookie() string {
	return func(e extractors.Extractor) string {
		if e.Source == extractors.SourceCookie {
			return e.Key
		}
		return ""
	}(store.Extractor)
}

var _ = fmt.Sprint
`, got)
	assert.Contains(t, res.out, "Migrating session KeyLookup config")

	require.Len(t, res.notes, 2)
	assert.Equal(t, 14, res.notes[0].Line)
	assert.Contains(t, res.notes[0].Message, "store.KeyLookup is rebuilt from the Extractor")
	assert.Contains(t, res.notes[0].Message, "an extractor that you wrote yourself")
	assert.Equal(t, 17, res.notes[1].Line)
	assert.Contains(t, res.notes[1].Message, "store.CookieName is the name of the cookie the Extractor reads")
	assert.Contains(t, res.notes[1].Message, "the value differs in that case")
}

// The read stays where it is: a function literal can stand in a condition, a
// switch, an argument list, behind parentheses or an index.
func Test_MigrateSessionReads_WhereTheyAre(t *testing.T) {
	t.Parallel()

	const header = `package main

import (
	"fmt"
	"strings"

	fsession "github.com/gofiber/fiber/v2/middleware/session"
)

var store *fsession.Store

func init() { store = fsession.New() }

type app struct {
	sessions *fsession.Store
}

var _ = strings.Cut
`
	bodies := []string{
		"func f() {\n\tif kind, name, _ := strings.Cut(store.KeyLookup, \":\"); kind == \"cookie\" {\n\t\tfmt.Println(name)\n\t}\n}",
		"func f() {\n\tswitch store.KeyLookup {\n\tcase \"cookie:session_id\":\n\t\tfmt.Println(1)\n\t}\n}",
		"func f() {\n\tfmt.Println(\n\t\tstore.KeyLookup,\n\t\t(store.CookieName),\n\t)\n}",
		"func f() byte { return store.KeyLookup[0] }",
		"func (a *app) f() string { return a.sessions.KeyLookup + a.sessions.CookieName }",
		"func f(a *app) string { return a.sessions.KeyLookup }",
		"func f() { defer fmt.Println(store.KeyLookup) }",
		"func f() { go func() { fmt.Println(store.CookieName) }() }",
		"func f() []string { return []string{store.KeyLookup, store.CookieName} }",
		"func f() map[string]bool { return map[string]bool{store.KeyLookup: true} }",
		"func f() { for _, part := range strings.Split(store.KeyLookup, \":\") { fmt.Println(part) } }",
	}

	for i, body := range bodies {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()

			got, res := mw3RunOne(t, header+"\n"+body+"\n", v3.MigrateSessionExtractor)
			_, err := parser.ParseFile(token.NewFileSet(), "", got, parser.ParseComments)
			require.NoError(t, err, got)
			assert.NotContains(t, got, ".KeyLookup")
			assert.NotContains(t, got, ".CookieName")
			assert.Contains(t, got, ".Extractor)")
			assert.NotEmpty(t, res.notes)
		})
	}
}

func Test_MigrateSessionReads_ConfigsAndTheDefault(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
)

func main(custom session.Config) {
	var zero session.Config
	other := &custom
	fmt.Println(custom.KeyLookup, zero.KeyLookup, other.KeyLookup)
}
`, v3.MigrateSessionExtractor)

	// A config that was built by hand may have no extractor, which was the empty
	// lookup in v2; a store, and the default, always have one.
	assert.Equal(t, 3, strings.Count(got, "if e.Extract == nil {"), got)
	assert.Contains(t, got, "}(custom.Extractor), func")
	assert.Contains(t, got, "}(zero.Extractor), func")
	assert.Contains(t, got, "}(other.Extractor))")
	assert.NotContains(t, got, ".KeyLookup")
	assert.Len(t, res.notes, 3)
}

// A config that the program makes in view has the strings it was given, and a
// read is that string.
func Test_MigrateSessionReads_ReadsOfAConfigMadeInView(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

const cookie = "sid"

func newConfig(ttl time.Duration) session.Config {
	cfg := session.ConfigDefault
	cfg.Expiration = ttl
	return cfg
}

func main() {
	byCookie := session.Config{CookieName: cookie}
	byLookup := session.Config{KeyLookup: "header:" + "X-ID"}
	both := session.Config{KeyLookup: "query:q", CookieName: "c"}
	none := session.Config{Expiration: time.Hour}
	copied := byCookie
	fmt.Printf("%q %q %q %q\n", byCookie.KeyLookup, byCookie.CookieName, byLookup.KeyLookup, byLookup.CookieName)
	fmt.Printf("%q %q %q %q\n", both.KeyLookup, both.CookieName, none.KeyLookup, none.CookieName)
	fmt.Printf("%q %q %q\n", copied.KeyLookup, session.ConfigDefault.KeyLookup, session.ConfigDefault.CookieName)
	derived := newConfig(time.Minute)
	fmt.Printf("%q %q\n", derived.KeyLookup, derived.CookieName)
	_, _, _, _, _ = session.New(byLookup), session.New(both), session.New(none), session.New(copied), session.New(derived)
}
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `fmt.Printf("%q %q %q %q\n", "", "sid", "header:X-ID", "")`)
	assert.Contains(t, got, `fmt.Printf("%q %q %q %q\n", "query:q", "c", "", "")`)
	assert.Contains(t, got, `fmt.Printf("%q %q %q\n", "", "cookie:session_id", "")`)
	assert.Contains(t, got, `fmt.Printf("%q %q\n", "cookie:session_id", "")`)
	assert.NotContains(t, got, "func(e")
	assert.NotContains(t, got, "KeyLookup")
	assert.Empty(t, res.notes, "nothing is lost where the strings are known")
}

// A variable that the program changes, or that a function hands over, has to be
// read from the extractor.
func Test_MigrateSessionReads_ReadsOfAConfigThatIsNotKnown(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

func load() session.Config {
	if os.Getenv("X") != "" {
		return session.Config{CookieName: "a"}
	}
	return session.Config{CookieName: "b"}
}

func param(p session.Config) { fmt.Println(p.KeyLookup) }

func changed() {
	cfg := session.Config{CookieName: "a"}
	cfg.KeyLookup = os.Getenv("LOOKUP")
	fmt.Println(cfg.KeyLookup)

	again := session.Config{CookieName: "a"}
	again = load()
	fmt.Println(again.KeyLookup)

	addr := session.Config{CookieName: "a"}
	set(&addr)
	fmt.Println(addr.CookieName)

	both := load()
	fmt.Println(both.CookieName)
}

func set(*session.Config) {}
`, v3.MigrateSessionExtractor)

	assert.NotContains(t, got, ".KeyLookup")
	assert.Contains(t, got, "}(p.Extractor))")
	assert.Contains(t, got, "}(cfg.Extractor))")
	assert.Contains(t, got, "}(again.Extractor))")
	assert.Contains(t, got, "}(addr.Extractor))")
	assert.Contains(t, got, "}(both.Extractor))")
	assert.Len(t, res.notes, 5+1)
}

// A store the program makes in view has the CookieName it was given, which is the
// empty one where it used KeyLookup, and the lookup v2 made of them.
func Test_MigrateSessionReads_ReadsOfAStoreMadeInView(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

func newConfig(ttl time.Duration) session.Config {
	cfg := session.ConfigDefault
	cfg.Expiration = ttl
	cfg.CookieHTTPOnly = true
	return cfg
}

var shared = session.New(session.Config{KeyLookup: "header:X-SID"})

func main() {
	store := session.New(newConfig(2 * time.Hour))
	byCookie := session.New(session.Config{CookieName: "sid"})
	plain := session.New()
	fmt.Println(store.Expiration, store.KeyLookup, store.CookieName)
	fmt.Println(byCookie.KeyLookup, byCookie.CookieName, plain.KeyLookup, plain.CookieName)
	fmt.Println(shared.KeyLookup, shared.CookieName, shared.Config.CookieName)
	byCookie.Reset()
	plain.Reset()
}
`, v3.MigrateSessionConfig, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `fmt.Println(store.IdleTimeout, "cookie:session_id", "")`)
	assert.Contains(t, got, `fmt.Println("cookie:sid", "sid", "cookie:session_id", "")`)
	assert.Contains(t, got, `fmt.Println("header:X-SID", "", "")`)
	assert.NotContains(t, got, "func(e")
	assert.Empty(t, res.notes)
}

// The variable that the read was the last use of stays used.
func Test_MigrateSessionReads_AFoldedReadKeepsTheVariableUsed(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
)

func main() {
	store := session.New()
	fmt.Println(store.KeyLookup)
	used := session.New()
	fmt.Println(used.KeyLookup)
	used.Reset()
}
`, v3.MigrateSessionExtractor)

	// The first store has nothing but the read: it is read from the extractor.
	assert.Contains(t, got, "}(store.Extractor))")
	assert.Contains(t, got, `fmt.Println("cookie:session_id")`)
	assert.NotEmpty(t, res.notes)
}

// A lookup that is copied from one config to another needs no string, and is the
// extractor of the other one.
func Test_MigrateSessionReads_CopiedLookups(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

func derive(from *session.Store, base session.Config) (*session.Store, *session.Store, *session.Store) {
	literal := session.New(session.Config{KeyLookup: from.KeyLookup, CookieHTTPOnly: true})

	cfg := session.ConfigDefault
	cfg.KeyLookup = from.KeyLookup
	assigned := session.New(cfg)

	again := session.New(session.Config{
		KeyLookup: base.KeyLookup,
	})
	return literal, assigned, again
}
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, "session.Config{Extractor: from.Extractor, CookieHTTPOnly: true}")
	assert.Contains(t, got, "cfg.Extractor = from.Extractor")
	assert.Contains(t, got, "\t\tExtractor: base.Extractor,\n")
	assert.NotContains(t, got, "KeyLookup")
	assert.Empty(t, res.notes, "copying the extractor is what v2 did with the string")
}

// A copy of a config, of a store's config or of the config a pointer points to,
// holds a config.
func Test_MigrateSessionReads_CopiesOfConfigs(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

func derive(from *session.Store, base session.Config, ptr *session.Config) {
	a := base
	a.KeyLookup = from.KeyLookup
	b := *ptr
	b.KeyLookup = "header:X-B"
	c := from.Config
	c.CookieName = "c"
	_, _, _ = a, b, c
}
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, "a.Extractor = from.Extractor")
	assert.Contains(t, got, `b.Extractor = extractors.FromHeader("X-B")`)
	assert.Contains(t, got, `c.Extractor = extractors.FromCookie("c")`)
	assert.NotContains(t, got, "KeyLookup")
	assert.NotContains(t, got, "CookieName")
	assert.Empty(t, res.notes)
}

// CookieName won over KeyLookup in v2, so a literal that sets it as well is
// read the long way.
func Test_MigrateSessionReads_CopiedLookupNextToCookieName(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

func derive(from *session.Store) *session.Store {
	return session.New(session.Config{KeyLookup: from.KeyLookup, CookieName: "winner"})
}
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `session.Config{Extractor: extractors.FromCookie("winner")}`)
	assert.NotContains(t, got, "KeyLookup")
}

// An assignment to the field of a config that is not a plain "=", or its address,
// is not a read: the code stays, the build fails on it, and it is reported.
func Test_MigrateSessionReads_LeavesWhatIsNotARead(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2/middleware/session"

func write(cfg session.Config) {
	cfg.KeyLookup += "x"
	p := &cfg.CookieName
	_ = p
	for cfg.KeyLookup = range []string{"a"} {
	}
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, src, got)

	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 3)
	assert.Contains(t, msgs[0], "cfg.KeyLookup no longer exists")
	assert.Contains(t, msgs[1], "&cfg.CookieName: the session Config has no KeyLookup or CookieName in v3")
	assert.Contains(t, msgs[2], "cfg.KeyLookup no longer exists")
}

// Other things that have a KeyLookup are not sessions.
func Test_MigrateSessionReads_OnlySessions(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"example.com/project/session"

	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/keyauth"
)

var cfg = keyauth.Config{KeyLookup: "header:X"}

var cookie = csrf.Config{CookieName: "c"}

var own = session.New()

func read() []string {
	return []string{cfg.KeyLookup, cookie.CookieName, own.KeyLookup, csrf.ConfigDefault.CookieName}
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)
	assert.Empty(t, res.notes)
}

// A handler file does not have to import the session package to use a store that
// another file of the package declares.
func Test_MigrateSessionReads_StoreOfAnotherFile(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"store.go": `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var Store *session.Store

func init() { Store = session.New(session.Config{KeyLookup: "cookie:app_sid"}) }

type App struct{ Sessions *session.Store }

func newStore() *session.Store { return Store }
`,
		"handlers.go": `package main

import "fmt"

func show(app *App) {
	fmt.Println(Store.KeyLookup, app.Sessions.CookieName, newStore().KeyLookup)
}
`,
		"other.go": `package main

type other struct{ KeyLookup string }

func show2(o other) string { return o.KeyLookup }
`,
	}, v3.MigrateSessionExtractor)

	assert.Contains(t, res.files["handlers.go"], `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, res.files["handlers.go"], "}(Store.Extractor)")
	assert.Contains(t, res.files["handlers.go"], "}(app.Sessions.Extractor)")
	assert.Contains(t, res.files["handlers.go"], "}(newStore().Extractor)")
	assert.NotContains(t, res.files["handlers.go"], ".KeyLookup")
	assert.Contains(t, res.files["other.go"], "return o.KeyLookup")
	assert.Len(t, res.notes, 3)
}

func Test_MigrateSessionReads_ExtractorsImportOfTheFile(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import (
	ext "github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{Extractor: ext.FromCookie("sid")})

func lookup() string { return store.KeyLookup }
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, "func(e ext.Extractor) string {")
	assert.Contains(t, got, "case ext.SourceHeader:")
	assert.Equal(t, 1, strings.Count(got, "fiber/v3/extractors"))
}

func Test_MigrateSessionReads_ANameThatIsTakenGetsAnAlias(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var extractors = []string{"unrelated"}

func lookup(store *session.Store) string { return store.KeyLookup }
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `fiberextractors "github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, got, "func(e fiberextractors.Extractor) string {")
}

// Where a declaration hides the extractors package the read cannot be written:
// it stays, and is reported.
func Test_MigrateSessionReads_ExtractorsHiddenByAVariable(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v3/extractors"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{Extractor: extractors.FromCookie("sid")})

func lookup() string {
	extractors := []string{"unrelated"}
	_ = extractors
	return store.KeyLookup
}

func plain() string {
	return store.KeyLookup
}
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, `package main

import (
	"github.com/gofiber/fiber/v3/extractors"
	fiberextractors "github.com/gofiber/fiber/v3/extractors"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{Extractor: extractors.FromCookie("sid")})

func lookup() string {
	extractors := []string{"unrelated"}
	_ = extractors
	return func(e fiberextractors.Extractor) string {
		if n := len(e.Chain); n > 0 {
			e = e.Chain[n-1]
		}
		switch e.Source {
		case fiberextractors.SourceHeader:
			return "header:" + e.Key
		case fiberextractors.SourceQuery:
			return "query:" + e.Key
		}
		return "cookie:" + e.Key
	}(store.Extractor)
}

func plain() string {
	return func(e extractors.Extractor) string {
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
}
`, got)
	assert.Len(t, res.notes, 2)

	again, second := mw3RunOne(t, got, v3.MigrateSessionExtractor)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
}

// The name for the second import is free in the whole package, and the names
// the function literals declare themselves are not used for a package.
func Test_MigrateSessionReads_AliasOfTheSecondImportIsFree(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3/extractors"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{Extractor: extractors.FromCookie("sid")})

func lookup() string {
	extractors := []string{"unrelated"}
	_ = extractors
	return store.KeyLookup
}
`,
		"other.go": `package main

var fiberextractors = "a package level name of another file"
`,
		"e.go": `package main

import (
	e "github.com/gofiber/fiber/v3/extractors"
)

func name() string {
	return store.CookieName
}
`,
	}, v3.MigrateSessionExtractor)
	assert.Contains(t, res.files["main.go"], "\tfiberextractors2 \"github.com/gofiber/fiber/v3/extractors\"\n")
	assert.Contains(t, res.files["main.go"], "return func(e fiberextractors2.Extractor) string {")
	// The file that names the package e keeps a literal whose parameter is e.
	assert.Contains(t, res.files["e.go"], "\textractors \"github.com/gofiber/fiber/v3/extractors\"\n")
	assert.Contains(t, res.files["e.go"], "func(e extractors.Extractor) string {")
	assert.Contains(t, res.files["e.go"], "if e.Source == extractors.SourceCookie {")
}

func Test_MigrateSessionReads_Idempotent(t *testing.T) {
	t.Parallel()

	got, first := mw3RunOne(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{KeyLookup: "query:sid"})

func main() {
	cfg := session.ConfigDefault
	cfg.KeyLookup = store.KeyLookup
	fmt.Println(store.KeyLookup, store.CookieName, cfg.KeyLookup)
}
`, v3.MigrateSessionExtractor)
	require.NotEmpty(t, first.out)

	again, second := mw3RunOne(t, got, v3.MigrateSessionExtractor)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}
