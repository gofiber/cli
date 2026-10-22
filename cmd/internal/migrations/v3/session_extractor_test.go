package v3_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func mw3MigrateSessionKeyLookup(t *testing.T, lookup string) (content, out string) {
	t.Helper()

	dir, err := os.MkdirTemp("", "msessionkl")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/session"
var _ = session.New(session.Config{
    KeyLookup: `+lookup+`, // comment
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionExtractor(cmd, dir, nil, nil))
	return readFile(t, file), buf.String()
}

func Test_MigrateSessionExtractor_Literals(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, lookup, extractor string }{
		{name: "a cookie", lookup: `"cookie:__Host-session"`, extractor: `extractors.FromCookie("__Host-session")`},
		{name: "a header", lookup: `"header:X-Session-ID"`, extractor: `extractors.FromHeader("X-Session-ID")`},
		{name: "a raw string", lookup: "`cookie:raw_sid`", extractor: `extractors.FromCookie("raw_sid")`},
		{
			// v2 read the cookie first and answered with a cookie whatever the source.
			name: "a query", lookup: `"query:session_id"`,
			extractor: `extractors.Chain(extractors.FromCookie("session_id"), extractors.FromQuery("session_id"))`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content, out := mw3MigrateSessionKeyLookup(t, tc.lookup)
			assert.NotContains(t, content, "KeyLookup")
			assert.Contains(t, content, "Extractor: "+tc.extractor+", // comment")
			assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
			assert.Contains(t, out, "Migrating session KeyLookup config")
		})
	}
}

func Test_MigrateSessionExtractor_QueryKeepsTheCookieAndSaysSo(t *testing.T) {
	t.Parallel()

	_, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var store = session.New(session.Config{KeyLookup: "query:sid"})
`, v3.MigrateSessionExtractor)

	require.Len(t, res.notes, 1)
	assert.Equal(t, 5, res.notes[0].Line)
	assert.Contains(t, res.notes[0].Message, "no longer copies a session ID that arrived in the query into the cookie")
}

func Test_MigrateSessionExtractor_Nested(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionkl_cookie_nested")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/session"
var _ = session.New(session.Config{
    Storage: storage.New(storage.Config{M: map[string]string{"a": "b"}}),
    KeyLookup: "cookie:__Host-session",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.Contains(t, content, `Extractor: extractors.FromCookie("__Host-session")`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating session KeyLookup config")
}

func Test_MigrateSessionExtractor_OneLine(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var a = session.New(session.Config{KeyLookup: "cookie:a", Expiration: time.Hour})

var b = session.New(session.Config{Expiration: time.Hour, KeyLookup: "header:X-B"})

var c = session.New(session.Config{KeyLookup: "cookie:c"})
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

var a = session.New(session.Config{Extractor: extractors.FromCookie("a"), Expiration: time.Hour})

var b = session.New(session.Config{Expiration: time.Hour, Extractor: extractors.FromHeader("X-B")})

var c = session.New(session.Config{Extractor: extractors.FromCookie("c")})
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionExtractor_Constants(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
)

const lookup = "header:X-Token"

const prefix = "cookie:"

var cookieName = "my_sid"

var a = session.New(session.Config{KeyLookup: lookup})

var b = session.New(session.Config{KeyLookup: prefix + cookieName})

var c = session.New(session.Config{KeyLookup: fmt.Sprintf("cookie:%s", cookieName)})

func local() *session.Store {
	const name = "local_sid"
	return session.New(session.Config{KeyLookup: "cookie:" + name})
}
`, v3.MigrateSessionExtractor)

	// fmt was only there for the Sprintf that is a constant now.
	assert.Equal(t, `package main

import (
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

const lookup = "header:X-Token"

const prefix = "cookie:"

var cookieName = "my_sid"

var a = session.New(session.Config{Extractor: extractors.FromHeader("X-Token")})

var b = session.New(session.Config{Extractor: extractors.FromCookie("my_sid")})

var c = session.New(session.Config{Extractor: extractors.FromCookie("my_sid")})

func local() *session.Store {
	const name = "local_sid"
	return session.New(session.Config{Extractor: extractors.FromCookie("local_sid")})
}
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionExtractor_ConstantsOfSiblingFiles(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"a.go": `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var store = session.New(session.Config{KeyLookup: lookup, CookieName: ""})
`,
		"b.go": `package main

const lookup = "cookie:" + name

const name = "shared_sid"
`,
		// A variable that is assigned is no constant, so the program reads it, and a
		// name of another package is not resolved.
		"c.go": `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var moving = "cookie:first"

func init() { moving = "cookie:second" }

var other = session.New(session.Config{KeyLookup: moving})
`,
	}, v3.MigrateSessionExtractor)

	assert.Contains(t, res.files["a.go"], `Extractor: extractors.FromCookie("shared_sid")`)
	assert.NotContains(t, res.files["a.go"], "CookieName")
	assert.Contains(t, res.files["c.go"], "}(moving)})")
	assert.NotContains(t, res.files["c.go"], "KeyLookup:")

	require.Len(t, res.notes, 1)
	assert.Equal(t, "c.go", res.notes[0].File)
	assert.Contains(t, res.notes[0].Message, "KeyLookup is computed")
}

func Test_MigrateSessionExtractor_CookieName(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var legacy = session.New(session.Config{
	CookieName: "legacy_sid",
	Expiration: 30 * time.Minute,
})

// v2 let CookieName win over KeyLookup.
var both = session.New(session.Config{
	KeyLookup:  "header:X-Ignored",
	CookieName: "winner",
})

var empty = session.New(session.Config{
	CookieName: "",
	KeyLookup:  "header:X-Kept",
})
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

var legacy = session.New(session.Config{
	Extractor:  extractors.FromCookie("legacy_sid"),
	Expiration: 30 * time.Minute,
})

// v2 let CookieName win over KeyLookup.
var both = session.New(session.Config{
	Extractor: extractors.FromCookie("winner"),
})

var empty = session.New(session.Config{
	Extractor: extractors.FromHeader("X-Kept"),
})
`, got)
	assert.Empty(t, res.notes)
}

// A lookup that v2 itself would have refused stays, so that the build fails on
// it, and is reported: dropping it would leave the application on the default
// cookie without saying so.
func Test_MigrateSessionExtractor_LeavesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, lookup, note string }{
		{name: "an unknown source", lookup: `"unknown:session_id"`, note: `KeyLookup "unknown:session_id" is not a "cookie:name"`},
		{name: "a source without a name", lookup: `"cookie:"`, note: `KeyLookup "cookie:" is not a "cookie:name"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := `package main

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var _ = os.Getenv
var _ = strings.Join

var store = session.New(session.Config{
	KeyLookup: ` + tc.lookup + `,
})
`
			got, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
			assert.Equal(t, src, got)
			assert.Empty(t, res.out, "nothing was changed")
			require.Len(t, res.notes, 1)
			assert.Equal(t, 14, res.notes[0].Line)
			assert.Contains(t, res.notes[0].Message, tc.note)
		})
	}
}

func Test_MigrateSessionExtractor_EmptyLookupIsTheDefault(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var store = session.New(session.Config{
	KeyLookup: "",
	CookieSecure: true,
})
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var store = session.New(session.Config{
	CookieSecure: true,
})
`, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionExtractor_FieldAssignments(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

func build() *session.Store {
	cfg := session.Config{}
	cfg.KeyLookup = "header:X-SID"
	cfg.CookieName = "sid"
	return session.New(cfg)
}

func fromEnv(cfg session.Config) *session.Store {
	cfg.KeyLookup = "cookie:" + os.Getenv("LOOKUP")
	return session.New(cfg)
}

func read(store *session.Store) (string, string) {
	return store.KeyLookup, store.CookieName
}
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

func build() *session.Store {
	cfg := session.Config{}
	cfg.Extractor = extractors.FromHeader("X-SID")
	cfg.Extractor = extractors.FromCookie("sid")
	return session.New(cfg)
}

func fromEnv(cfg session.Config) *session.Store {
	cfg.Extractor = func(lookup string) extractors.Extractor {
		if lookup == "" {
			lookup = "cookie:session_id"
		}
		source, name, found := strings.Cut(lookup, ":")
		if !found || strings.Contains(name, ":") {
			panic("[session] KeyLookup must in the form of <source>:<name>")
		}
		switch source {
		case "cookie":
			return extractors.FromCookie(name)
		case "header":
			return extractors.FromHeader(name)
		case "query":
			return extractors.Chain(extractors.FromCookie(name), extractors.FromQuery(name))
		}
		panic("[session] source is not supported")
	}("cookie:" + os.Getenv("LOOKUP"))
	return session.New(cfg)
}

func read(store *session.Store) (string, string) {
	return store.KeyLookup, store.CookieName
}
`, got)

	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 3)
	assert.Contains(t, msgs[0], "KeyLookup is computed")
	assert.Contains(t, msgs[1], "store.KeyLookup no longer exists")
	assert.Contains(t, msgs[2], "store.CookieName no longer exists")
}

func Test_MigrateSessionExtractor_ReportsTheRemovedSource(t *testing.T) {
	t.Parallel()

	_, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

func lookup(src session.Source) string { return string(src) + ":sid" }

var cookie = lookup(session.SourceCookie)
`, v3.MigrateSessionExtractor)

	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0], "session.Source was removed")
	assert.Contains(t, msgs[1], "session.SourceCookie was removed")
}

func Test_MigrateSessionExtractor_ExistingImports(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import (
	ext "github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{KeyLookup: "cookie:sid"})
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `Extractor: ext.FromCookie("sid")`)
	assert.Equal(t, 1, strings.Count(got, "fiber/v3/extractors"))
}

func Test_MigrateSessionExtractor_ANameThatIsTakenGetsAnAlias(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var extractors = []string{"unrelated"}

var store = session.New(session.Config{KeyLookup: "cookie:sid"})
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `fiberextractors "github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, got, `Extractor: fiberextractors.FromCookie("sid")`)
}

func Test_MigrateSessionExtractor_IgnoresOtherConfigs(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msession_ignore")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/keyauth"
var _ = keyauth.New(keyauth.Config{
    KeyLookup: "header:Authorization",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `KeyLookup: "header:Authorization"`)
	assert.NotContains(t, content, "Extractor")
	assert.NotContains(t, buf.String(), "Migrating session KeyLookup config")
}

func Test_MigrateSessionExtractor_OnlyTheFiberSessionPackage(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"example.com/project/session"

	"github.com/gofiber/fiber/v2/middleware/keyauth"
)

var a = session.New(session.Config{KeyLookup: "cookie:sid", CookieName: "x"})

var b = keyauth.New(keyauth.Config{KeyLookup: "header:X"})
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, src, got)
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionExtractor_AliasedImportAndIdempotency(t *testing.T) {
	t.Parallel()

	src := `package main

import fsession "github.com/gofiber/fiber/v2/middleware/session"

var store = fsession.New(fsession.Config{KeyLookup: "cookie:alias_sid"})
`
	got, _ := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Contains(t, got, `fsession.New(fsession.Config{Extractor: extractors.FromCookie("alias_sid")})`)

	again, second := mw3RunOne(t, got, v3.MigrateSessionExtractor)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

// A lookup that is only known when the program runs is read by a function that
// does what the v2 constructor did with the string: an empty value is the default
// cookie, anything but <source>:<name> panics, and the source picks the extractor.
func Test_MigrateSessionExtractor_ComputedLookup(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var store = session.New(session.Config{
	KeyLookup: os.Getenv("SESSION_LOOKUP"),
	CookieSecure: true,
})
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

var store = session.New(session.Config{
	Extractor: func(lookup string) extractors.Extractor {
		if lookup == "" {
			lookup = "cookie:session_id"
		}
		source, name, found := strings.Cut(lookup, ":")
		if !found || strings.Contains(name, ":") {
			panic("[session] KeyLookup must in the form of <source>:<name>")
		}
		switch source {
		case "cookie":
			return extractors.FromCookie(name)
		case "header":
			return extractors.FromHeader(name)
		case "query":
			return extractors.Chain(extractors.FromCookie(name), extractors.FromQuery(name))
		}
		panic("[session] source is not supported")
	}(os.Getenv("SESSION_LOOKUP")),
	CookieSecure: true,
})
`, got)
	assert.Contains(t, res.out, "Migrating session KeyLookup config")

	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "KeyLookup is computed")
	assert.Contains(t, msgs[0], "a `query:` lookup")

	// The Extractor is in place: a second run finds nothing to do.
	again, second := mw3RunOne(t, got, v3.MigrateSessionExtractor)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

func Test_MigrateSessionExtractor_ComputedLookupUsesTheStringsImportOfTheFile(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, `package main

import (
	"os"
	str "strings"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var _ = str.ToUpper

var store = session.New(session.Config{KeyLookup: os.Getenv("SESSION_LOOKUP")})
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `str.Cut(lookup, ":")`)
	assert.Contains(t, got, `str.Contains(name, ":")`)
	assert.NotContains(t, got, "\t\"strings\"\n")
}

// A file that uses the name strings for something else cannot have the function:
// the config stays and is reported.
func Test_MigrateSessionExtractor_ComputedLookupNeedsTheStringsName(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var strings = []string{"a"}

var store = session.New(session.Config{KeyLookup: os.Getenv("SESSION_LOOKUP")})
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)
	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "KeyLookup is not a constant string")
}

// A CookieName that is computed alone falls back to the default cookie when it
// turns out empty, like in v2. Next to a KeyLookup, which of the two applies
// depends on the value, so that stays as it is and is reported.
func Test_MigrateSessionExtractor_ComputedCookieName(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var alone = session.New(session.Config{CookieName: os.Getenv("COOKIE")})
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

var alone = session.New(session.Config{Extractor: func(name string) extractors.Extractor {
	if name == "" {
		name = "session_id"
	}
	return extractors.FromCookie(name)
}(os.Getenv("COOKIE"))})
`, got)
	assert.Empty(t, res.notes)

	src := `package main

import (
	"os"

	"github.com/gofiber/fiber/v2/middleware/session"
)

var both = session.New(session.Config{CookieName: os.Getenv("COOKIE"), KeyLookup: "header:X-SID"})
`
	kept, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, src, kept)
	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "CookieName is not a constant string")
}
