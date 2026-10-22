package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The Source type of v2 is a string, and its constants are the strings they
// stand for: wherever the program names them, the string does the same.
func Test_MigrateSessionSource_TypeAndConstants(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
)

type lookup struct {
	source session.Source
	name   string
}

type mode session.Source

const preferred = session.SourceHeader

const typed session.Source = session.SourceURLQuery

var known = map[session.Source]bool{session.SourceCookie: true, session.SourceURLQuery: false}

var order = []session.Source{session.SourceCookie, session.SourceHeader}

func where(src session.Source) string {
	switch src {
	case session.SourceCookie, session.SourceHeader:
		return "id"
	case session.SourceURLQuery:
		return "query"
	}
	return string(src)
}

func parse(s string) session.Source { return session.Source(s) }

func isCookie(src session.Source) bool { return src == session.SourceCookie }

func (l lookup) String() string { return fmt.Sprintf("%s:%s", l.source, l.name) }

func describe() string {
	return fmt.Sprint(string(session.SourceCookie), len(session.SourceURLQuery), mode(session.SourceHeader))
}

var store = session.New(session.Config{KeyLookup: string(session.SourceCookie) + ":sid"})
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/fiber/v3/extractors"
)

type lookup struct {
	source string
	name   string
}

type mode string

const preferred = "header"

const typed string = "query"

var known = map[string]bool{"cookie": true, "query": false}

var order = []string{"cookie", "header"}

func where(src string) string {
	switch src {
	case "cookie", "header":
		return "id"
	case "query":
		return "query"
	}
	return string(src)
}

func parse(s string) string { return string(s) }

func isCookie(src string) bool { return src == "cookie" }

func (l lookup) String() string { return fmt.Sprintf("%s:%s", l.source, l.name) }

func describe() string {
	return fmt.Sprint("cookie", len("query"), mode("header"))
}

var store = session.New(session.Config{Extractor: extractors.FromCookie("sid")})
`, got)
	assert.Empty(t, res.notes)
	assert.Contains(t, res.out, "Migrating session Source constants")
	assert.Contains(t, res.out, "Migrating session KeyLookup config")
}

// A lookup that is built from the constants is a string like any other once they
// are written out: a constant one becomes the extractor, one that calls the
// program's own function is computed when the program starts.
func Test_MigrateSessionSource_LookupsBuiltFromConstants(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2/middleware/session"
)

const prefix = string(session.SourceHeader) + ":"

func lookup(src session.Source, name string) string { return fmt.Sprintf("%s:%s", src, name) }

var _ = strings.ToUpper

var header = session.New(session.Config{KeyLookup: prefix + "X-SID"})

var formatted = session.New(session.Config{KeyLookup: fmt.Sprintf("%s:%s", session.SourceURLQuery, "q")})

var computed = session.New(session.Config{KeyLookup: lookup(session.SourceCookie, "sid")})
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, `var header = session.New(session.Config{Extractor: extractors.FromHeader("X-SID")})`)
	assert.Contains(t, got, `extractors.Chain(extractors.FromCookie("q"), extractors.FromQuery("q"))`)
	assert.Contains(t, got, `}(lookup("cookie", "sid"))})`)
	assert.Contains(t, got, `func lookup(src string, name string) string`)
	assert.NotContains(t, got, "session.Source")

	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0], `KeyLookup "query:q"`)
	assert.Contains(t, msgs[1], "KeyLookup is computed")
}

func Test_MigrateSessionSource_AliasedImportAndMultiLine(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import fsession "github.com/gofiber/fiber/v2/middleware/session"

type rule struct {
	Source fsession.Source
	Name   string
}

var rules = []rule{
	{
		Source: fsession.SourceCookie,
		Name:   "sid",
	},
	{
		Source: fsession.SourceURLQuery,
		Name:   "q",
	},
}

func check(
	src fsession.Source,
) bool {
	switch src {
	case fsession.SourceCookie,
		fsession.SourceHeader:
		return true
	}
	return false
}
`, v3.MigrateSessionExtractor)

	assert.Equal(t, `package main

type rule struct {
	Source string
	Name   string
}

var rules = []rule{
	{
		Source: "cookie",
		Name:   "sid",
	},
	{
		Source: "query",
		Name:   "q",
	},
}

func check(
	src string,
) bool {
	switch src {
	case "cookie",
		"header":
		return true
	}
	return false
}
`, got, "the import is gone with the last use of the package")
	assert.Empty(t, res.notes)
}

// What is not the session middleware of Fiber is not touched: another package of
// that name, a variable called session, a field with the name of the type.
func Test_MigrateSessionSource_LeavesOtherCodeAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"example.com/project/session"

	"github.com/gofiber/fiber/v2/middleware/keyauth"
)

var _ = keyauth.New

var a session.Source = session.SourceCookie

type options struct {
	Source string
}

func use(o options) string { return o.Source }
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, src, got)
	assert.Empty(t, res.out)
	assert.Empty(t, res.notes)

	src = `package main

import "github.com/gofiber/fiber/v2/middleware/session"

var _ = session.New

func shadowed(session struct {
	Source       string
	SourceCookie string
}) string {
	return session.Source + session.SourceCookie
}
`
	got, res = mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, src, got, "a variable that is called session is not the package")
	assert.Empty(t, res.out)
	assert.Empty(t, res.notes)
}

// A type that cannot be written as string where it stands is left, so that the
// build fails on it, and is reported.
func Test_MigrateSessionSource_ReportsWhatItCannotWrite(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

type embedded struct {
	session.Source
}

type embeddedPointer struct {
	*session.Source
}

var kind session.Source = session.SourceCookie
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, "\tsession.Source\n}")
	assert.Contains(t, got, "\t*session.Source\n}")
	assert.Contains(t, got, `var kind string = "cookie"`, "a use that can be written is written")
	msgs := mw3Messages(res.notes)
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0], "session.Source was removed")
	assert.Contains(t, msgs[1], "session.Source was removed")

	src := `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2/middleware/session"
)

func show(src session.Source) {
	string := "hidden"
	fmt.Println(string, src, session.SourceCookie)
}
`
	got, res = mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Equal(t, src, got, "a file that declares string cannot name it")
	msgs = mw3Messages(res.notes)
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0], "session.Source was removed")
	assert.Contains(t, msgs[1], "session.SourceCookie was removed")
}

// v2 told a Source from a string in a type switch, and the switch cannot have
// string twice: it is left, and reported, where it has both.
func Test_MigrateSessionSource_TypeSwitch(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

func both(v any) string {
	switch x := v.(type) {
	case session.Source:
		return "source " + string(x)
	case string:
		return "string " + x
	}
	return ""
}

func only(v any) string {
	switch x := v.(type) {
	case session.Source:
		return "source " + string(x)
	case int:
		return "int"
	}
	return ""
}

func assertion(v any) (session.Source, bool) {
	src, ok := v.(session.Source)
	return src, ok
}
`, v3.MigrateSessionExtractor)

	assert.Contains(t, got, "\tcase session.Source:\n\t\treturn \"source \" + string(x)\n\tcase string:")
	assert.Contains(t, got, "\tcase string:\n\t\treturn \"source \" + string(x)\n\tcase int:")
	assert.Contains(t, got, "func assertion(v any) (string, bool) {\n\tsrc, ok := v.(string)")

	// The switch that is left is reported, and so are the places where the type is
	// asked for and a plain string now matches as well.
	lines := map[int]string{}
	for _, n := range res.notes {
		lines[n.Line] = n.Message
	}
	require.Len(t, lines, 3)
	assert.Contains(t, lines[7], "a case of a type switch that has a case string already")
	assert.Contains(t, lines[17], "this also matches a plain string")
	assert.Contains(t, lines[26], "this also matches a plain string")
}

func Test_MigrateSessionSource_Idempotent(t *testing.T) {
	t.Parallel()

	got, first := mw3RunOne(t, `package main

import "github.com/gofiber/fiber/v2/middleware/session"

func where(src session.Source) bool { return src == session.SourceURLQuery }

var store = session.New(session.Config{KeyLookup: string(session.SourceHeader) + ":X-SID"})
`, v3.MigrateSessionExtractor)
	require.NotEmpty(t, first.out)

	again, second := mw3RunOne(t, got, v3.MigrateSessionExtractor)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
	assert.Empty(t, second.notes)
}

// The types of the project that are called Source are not the one of the session
// middleware, whatever file they are in.
func Test_MigrateSessionSource_NotTheTypesOfTheProject(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"sources.go": `package main

import "github.com/gofiber/fiber/v2/middleware/session"

type Source string

const SourceCookie Source = "cookie"

func lookup(src Source) string { return string(src) }

func fiberLookup(src session.Source) string { return string(src) }
`,
		"use.go": `package main

var cookie = lookup(SourceCookie)
`,
	}, v3.MigrateSessionExtractor)

	assert.Contains(t, res.files["sources.go"], "func lookup(src Source) string")
	assert.Contains(t, res.files["sources.go"], "func fiberLookup(src string) string")
	assert.Equal(t, `package main

var cookie = lookup(SourceCookie)
`, res.files["use.go"])
	assert.Empty(t, res.notes)
}

// A variable called like the session package hides it wherever it is declared:
// by a range clause, a type switch or a select case as well as by a var.
func Test_MigrateSessionSource_NamesHiddenByVariablesThatSayNothingOfTheirType(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"github.com/gofiber/fiber/v2/middleware/session"
)

type item struct{ Source string }

func use(items []item, v any, ch chan item) []string {
	var out []string
	for _, session := range items {
		out = append(out, session.Source)
	}
	switch session := v.(type) {
	case item:
		out = append(out, session.Source)
	}
	select {
	case session := <-ch:
		out = append(out, session.Source)
	}
	type session struct{ Source string }
	out = append(out, session{}.Source)
	return out
}

var _ session.Source = session.SourceCookie
`
	got, res := mw3RunOne(t, src, v3.MigrateSessionExtractor)
	assert.Contains(t, got, "out = append(out, session.Source)")
	assert.Equal(t, 3, strings.Count(got, "out = append(out, session.Source)"))
	assert.Contains(t, got, `out = append(out, session{}.Source)`)
	assert.Contains(t, got, `var _ string = "cookie"`)
	assert.Empty(t, res.notes)
}
