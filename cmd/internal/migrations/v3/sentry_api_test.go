package v3_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const sentryImports = `import (
	"github.com/getsentry/sentry-go"
	fibersentry "github.com/gofiber/contrib/fibersentry"
	"github.com/gofiber/fiber/v2"
)
`

// sentryExtrasNote is the start of the note that a project gets once for the
// extras that became contexts.
const sentryExtrasNote = "sentry-go 0.46 removed Scope.SetExtra, SetExtras, RemoveExtra and Event.Extra: each extra is now a context named after its key"

func Test_MigrateSentryAPI_ScopeExtras(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateSentryAPI, `package main

`+sentryImports+`
func handler(c *fiber.Ctx) error {
	if hub := fibersentry.GetHubFromContext(c); hub != nil {
		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetExtra("unwantedQuery", "someQueryDataMaybe")
			hub.CaptureMessage("unwanted query")
		})
		hub.Scope().SetExtra("request", c.Path())
		hub.ConfigureScope(func(s *sentry.Scope) {
			s.RemoveExtra("request")
		})
	}
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetExtra("user", c.Get("X-User"))
	})
	sentry.CurrentHub().Scope().SetExtra("pid", 1)
	return nil
}
`)

	file := res.file()
	assert.Contains(t, file, `scope.SetContext("unwantedQuery", sentry.Context{"value": "someQueryDataMaybe"})`)
	assert.Contains(t, file, `hub.Scope().SetContext("request", sentry.Context{"value": c.Path()})`)
	assert.Contains(t, file, `s.RemoveContext("request")`)
	assert.Contains(t, file, `scope.SetContext("user", sentry.Context{"value": c.Get("X-User")})`)
	assert.Contains(t, file, `sentry.CurrentHub().Scope().SetContext("pid", sentry.Context{"value": 1})`)
	assert.NotContains(t, file, "Extra")
	assert.Contains(t, res.stdout, "Migrating the sentry-go API")

	// the project is told once, at the first of them
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: %s", mw1LineOf(t, file, `scope.SetContext("unwantedQuery"`), sentryExtrasNote))
		assert.Contains(t, res.notes[0], `holds the value under "value"`)
		assert.Contains(t, res.notes[0], "(4 more in this project)")
	}

	// a second run has nothing left to do
	again := mw1Run(t, v3.MigrateSentryAPI, file)
	assert.Equal(t, file, again.file())
	assert.Empty(t, again.stdout)
	assert.Empty(t, again.notes)
}

func Test_MigrateSentryAPI_AliasedImports(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateSentryAPI, `package main

import (
	sg "github.com/getsentry/sentry-go"
	"github.com/gofiber/contrib/v3/sentry"
	"github.com/gofiber/fiber/v3"
)

func handler(c fiber.Ctx) error {
	hub := sentry.MustGetHubFromContext(c)
	hub.WithScope(func(scope *sg.Scope) {
		scope.SetExtra("k", "v")
	})
	return nil
}
`)

	assert.Contains(t, res.file(), `scope.SetContext("k", sg.Context{"value": "v"})`)
	assert.NotContains(t, res.file(), "SetExtra")
}

func Test_MigrateSentryAPI_ContextTypeWhenTheFileCannotNameThePackage(t *testing.T) {
	t.Parallel()

	// the file reaches the hub through the middleware only, and a local called
	// sentry hides the package where the call is
	res := mw1Run(t, v3.MigrateSentryAPI, `package main

import (
	fibersentry "github.com/gofiber/contrib/fibersentry"
	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	fibersentry.GetHubFromContext(c).Scope().SetExtra("k", c.Path())
	return nil
}
`)

	assert.Contains(t, res.file(), `.SetContext("k", map[string]any{"value": c.Path()})`)
	assert.Empty(t, mw1MissingImports(res.file()))
}

func Test_MigrateSentryAPI_SetExtras(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateSentryAPI, `package main

`+sentryImports+`
var shared = map[string]interface{}{"a": 1}

type extras map[string]interface{}

func handler(c *fiber.Ctx, scope *sentry.Scope, others extras) {
	scope.SetExtras(map[string]interface{}{"path": c.Path(), "method": c.Method()})
	scope.SetExtras(map[string]any{
		"id": c.Params("id"), // the id
		"q":  c.Query("q"),
	})
	scope.SetExtras(shared)
	scope.SetExtras(others)
}
`)

	file := res.file()
	assert.Contains(t, file, `scope.SetContexts(map[string]sentry.Context{"path": {"value": c.Path()}, "method": {"value": c.Method()}})`)
	assert.Contains(t, file, "\"id\": {\"value\": c.Params(\"id\")}, // the id")
	assert.Contains(t, file, `"q":  {"value": c.Query("q")},`)
	assert.Contains(t, file, `map[string]sentry.Context{`)
	assert.Equal(t, 2, strings.Count(file, "func(extras map[string]any) map[string]sentry.Context {"))
	assert.Contains(t, file, "}(shared))")
	assert.Contains(t, file, "}(others))")
	assert.NotContains(t, file, "SetExtras")
}

func Test_MigrateSentryAPI_ReservedContextsAndUnknownReceivers(t *testing.T) {
	t.Parallel()

	src := `package main

` + sentryImports + `
type recorder struct{}

func (recorder) SetExtra(key string, value any) {}

func handler(scope *sentry.Scope, r recorder, thing interface{ SetExtra(string, any) }) {
	scope.SetExtra("trace", "x")
	scope.SetExtra("device", "x")
	scope.SetExtra(key(), "x", "y")
	r.SetExtra("kept", 1)
	thing.SetExtra("unknown", 1)
	unknownScope().SetExtra("unknown", 1)
}
`
	res := mw1Run(t, v3.MigrateSentryAPI, src)

	// nothing changed: the compiler points at what is left
	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	// thing is declared as an interface of its own, which is no scope: no note for it
	if assert.Len(t, res.notes, 4, "%v", res.notes) {
		assert.Contains(t, res.notes[0], `the key "trace" is the name of a context that the SDK sets on every event`)
		assert.Contains(t, res.notes[1], `the key "device"`)
		assert.Contains(t, res.notes[2], "use SetContext, SetContexts or RemoveContext (left as it is)")
		assert.Contains(t, res.notes[3], "this file does not say what unknownScope() is")
	}
}

func Test_MigrateSentryAPI_VariablesOfTheScope(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateSentryAPI, `package main

`+sentryImports+`
var hub = sentry.CurrentHub()

type reporter struct {
	hub   *sentry.Hub
	scope *sentry.Scope
}

func (r *reporter) Hub() *sentry.Hub { return r.hub }

func use(r *reporter, c *fiber.Ctx) {
	scope := hub.Scope()
	scope.SetExtra("a", 1)

	pushed := sentry.CurrentHub().PushScope()
	pushed.SetExtra("b", 2)

	cloned := scope.Clone()
	cloned.SetExtra("c", 3)

	r.hub.Scope().SetExtra("d", 4)
	r.scope.SetExtra("e", 5)
	r.Hub().Scope().SetExtra("f", 6)

	var typed *sentry.Scope = scope
	typed.SetExtra("g", 7)

	for _, s := range []*sentry.Scope{scope} {
		s.SetExtra("not followed", 8)
	}
}
`)

	file := res.file()
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		assert.Contains(t, file, fmt.Sprintf(`.SetContext(%q, sentry.Context{"value": `, key))
	}
	// the range variable says nothing about its type
	assert.Contains(t, file, `s.SetExtra("not followed", 8)`)
	if assert.Len(t, res.notes, 2) {
		assert.Contains(t, res.notes[0], "this file does not say what s is")
		assert.Contains(t, res.notes[1], sentryExtrasNote)
	}
}

func Test_MigrateSentryAPI_EventExtras(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateSentryAPI, `package main

`+sentryImports+`
func init() {
	_ = sentry.Init(sentry.ClientOptions{
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			event.Extra["handler"] = "fiber"
			event.Extra["status"] = hint
			if v, ok := event.Extra["seen"]; ok && v != nil {
				delete(event.Extra, "seen")
			}
			note := event.Extra["note"]
			_ = note
			return event
		},
	})

	e := sentry.NewEvent()
	e.Extra["x"] = 1
	_ = &sentry.Event{Message: "m", Extra: map[string]interface{}{"a": 1, "b": "two"}}
}
`)

	file := res.file()
	assert.Contains(t, file, `event.Contexts["handler"] = sentry.Context{"value": "fiber"}`)
	assert.Contains(t, file, `event.Contexts["status"] = sentry.Context{"value": hint}`)
	assert.Contains(t, file, `if v, ok := event.Contexts["seen"]["value"]; ok && v != nil {`)
	assert.Contains(t, file, `delete(event.Contexts, "seen")`)
	assert.Contains(t, file, `note := event.Contexts["note"]["value"]`)
	assert.Contains(t, file, `e.Contexts["x"] = sentry.Context{"value": 1}`)
	assert.Contains(t, file, `sentry.Event{Message: "m", Contexts: map[string]sentry.Context{"a": {"value": 1}, "b": {"value": "two"}}}`)
	assert.NotContains(t, file, "Extra")
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], sentryExtrasNote)
	}
}

func Test_MigrateSentryAPI_EventExtraThatCannotBeRewritten(t *testing.T) {
	t.Parallel()

	src := `package main

` + sentryImports + `
func report(event *sentry.Event, other map[string]interface{}) {
	for key := range event.Extra {
		_ = key
	}
	event.Extra = other
	event.Extra["n"]++
	_ = len(event.Extra)
	_ = &sentry.Event{Extra: other, Contexts: map[string]sentry.Context{}}
}
`
	res := mw1Run(t, v3.MigrateSentryAPI, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	if assert.Len(t, res.notes, 5, "%v", res.notes) {
		for _, note := range res.notes[:4] {
			assert.Contains(t, note, "Event.Extra was removed in sentry-go 0.46")
		}
		assert.Contains(t, res.notes[4], "move the extras into the Contexts of this literal")
	}
}

func Test_MigrateSentryAPI_Logger(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateSentryAPI, `package main

import (
	"log"
	"os"

	"github.com/getsentry/sentry-go"
)

var mine = log.New(os.Stdout, "[mine] ", 0)

func setup(logger sentry.Logger, typed []sentry.Logger) {
	sentry.Logger.SetOutput(os.Stderr)
	sentry.Logger.Printf("hello %d", 1)
	w := sentry.Logger.Writer()
	_ = w
	sentry.Logger = log.New(os.Stderr, "[sentry] ", log.Lshortfile)
	sentry.Logger = mine
	_ = new(sentry.Logger)
}
`)

	file := res.file()
	assert.Contains(t, file, "sentry.DebugLogger.SetOutput(os.Stderr)\n")
	assert.Contains(t, file, `sentry.DebugLogger.Printf("hello %d", 1)`)
	assert.Contains(t, file, "w := sentry.DebugLogger.Writer()")
	// the logger of the SDK is not replaced any more: the new one gives its settings to it
	assert.Contains(t, file, "sentry.DebugLogger.SetOutput(os.Stderr)\n\tsentry.DebugLogger.SetPrefix(\"[sentry] \")\n\tsentry.DebugLogger.SetFlags(log.Lshortfile)")
	assert.Contains(t, file, "sentry.DebugLogger.SetOutput(mine.Writer())\n\tsentry.DebugLogger.SetPrefix(mine.Prefix())\n\tsentry.DebugLogger.SetFlags(mine.Flags())")
	// the type of the logging API is not the variable
	assert.Contains(t, file, "func setup(logger sentry.Logger, typed []sentry.Logger)")
	assert.Contains(t, file, "_ = new(sentry.Logger)")
	assert.Empty(t, res.notes)

	again := mw1Run(t, v3.MigrateSentryAPI, file)
	assert.Equal(t, file, again.file())
}

func Test_MigrateSentryAPI_ClientNilChecks(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateSentryAPI, `package main

`+sentryImports+`
func check(c *fiber.Ctx) bool {
	hub := fibersentry.GetHubFromContext(c)
	if hub.Client() == nil {
		return false
	}
	client := sentry.CurrentHub().Client()
	if client != nil && hub.Scope() != nil {
		return true
	}
	if nil == fibersentry.MustGetHubFromContext(c).Client() {
		return false
	}
	var configured *sentry.Client
	return configured == nil || client == configured
}
`)

	file := res.file()
	assert.Contains(t, file, "if !hub.Client().IsEnabled() {")
	assert.Contains(t, file, "if client.IsEnabled() && hub.Scope() != nil {")
	assert.Contains(t, file, "if !fibersentry.MustGetHubFromContext(c).Client().IsEnabled() {")
	assert.Contains(t, file, "return !configured.IsEnabled() || client == configured")
	assert.Empty(t, res.notes)
}

func Test_MigrateSentryAPI_ClientOptionsAndUsers(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateSentryAPI, `package main

`+sentryImports+`
func options(logs bool) []sentry.ClientOptions {
	opts := sentry.ClientOptions{Dsn: "x"}
	opts.EnableLogs = true
	opts.ProfilesSampleRate = 1
	return []sentry.ClientOptions{
		{Dsn: "a", EnableLogs: true, TracesSampleRate: 1},
		{
			Dsn:                "b",
			ProfilesSampleRate: 0.5, // profiles
			DisableMetrics:     false,
		},
		{Dsn: "c", EnableLogs: logs},
		{Dsn: "d", DisableLogs: true},
		opts,
		sentry.ClientOptions{DisableLogs: false, DisableMetrics: true},
	}
}

func users() []sentry.User {
	return []sentry.User{
		{ID: "1", Segment: "paid"},
		{ID: "2"},
	}
}
`)

	file := res.file()
	assert.Contains(t, file, `{Dsn: "a", TracesSampleRate: 1},`)
	assert.Contains(t, file, "{\n\t\t\tDsn: \"b\",\n\t\t},")
	assert.Contains(t, file, `{Dsn: "c", EnableLogs: logs},`)
	assert.Contains(t, file, `{Dsn: "d", DisableLogs: true},`)
	assert.Contains(t, file, `sentry.ClientOptions{DisableMetrics: true},`)
	assert.Contains(t, file, `opts := sentry.ClientOptions{Dsn: "x"}`+"\n\treturn")
	assert.Contains(t, file, `{ID: "1"},`)
	assert.NotContains(t, file, "ProfilesSampleRate")
	assert.NotContains(t, file, "Segment")
	if assert.Len(t, res.notes, 4, "%v", res.notes) {
		assert.Contains(t, res.notes[0], "ClientOptions.EnableLogs was removed in sentry-go 0.47")
		assert.Contains(t, res.notes[1], "ClientOptions.DisableLogs was removed in sentry-go 0.49")
		assert.Contains(t, res.notes[2], "ClientOptions.DisableMetrics was removed in sentry-go 0.49")
		assert.Contains(t, res.notes[3], "sentry-go 0.31 dropped profiling and the segment of a user")
		assert.Contains(t, res.notes[3], "(2 more in this project)")
	}
}

func Test_MigrateSentryAPI_MetricsAreReported(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/getsentry/sentry-go"

func metrics() {
	_ = sentry.NewCounterMetric("hits", sentry.Second(), nil, 0, 1)
	_ = sentry.Percent()
}
`
	res := mw1Run(t, v3.MigrateSentryAPI, src)

	assert.Equal(t, src, res.file())
	if assert.Len(t, res.notes, 3, "%v", res.notes) {
		assert.Contains(t, res.notes[0], "sentry.NewCounterMetric was removed in sentry-go 0.31")
		assert.Contains(t, res.notes[0], "sentry.NewMeter(ctx)")
		assert.Contains(t, res.notes[1], "sentry.Second was removed")
		assert.Contains(t, res.notes[2], "sentry.Percent was removed")
	}
}

func Test_MigrateSentryAPI_LeavesTheCodeOfOtherPackagesAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"example.com/other/sentry"
	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx, scope *sentry.Scope, event *sentry.Event) {
	scope.SetExtra("k", 1)
	event.Extra["k"] = 1
	sentry.Logger.Println("x")
	if event.Client() == nil {
		return
	}
}
`
	res := mw1Run(t, v3.MigrateSentryAPI, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)
}

func Test_MigrateSentryAPI_ValuesOfOtherPackagesOfTheModule(t *testing.T) {
	t.Parallel()

	res := mw1RunModule(t, v3.MigrateSentryAPI, map[string]string{
		"obs/obs.go": `package obs

import "github.com/getsentry/sentry-go"

// Hub is the hub of the service.
var Hub = sentry.CurrentHub()

var Default = &Reporter{hub: sentry.CurrentHub()}

type Reporter struct {
	hub *sentry.Hub
}

func (r *Reporter) Scope() *sentry.Scope { return r.hub.Scope() }

func Scope() *sentry.Scope { return Hub.Scope() }

func Event() *sentry.Event { return sentry.NewEvent() }

func Settings() map[string]string { return nil }
`,
		"handlers/handlers.go": `package handlers

import (
	"github.com/gofiber/fiber/v2"

	"example.com/app/obs"
)

type server struct {
	reporter *obs.Reporter
}

func (s *server) index(c *fiber.Ctx) error {
	obs.Hub.Scope().SetExtra("hub", 1)
	obs.Scope().SetExtra("function", 2)
	obs.Default.Scope().SetExtra("variable", 3)
	s.reporter.Scope().SetExtra("field", 4)
	obs.Event().Extra["event"] = 5
	if obs.Hub.Client() == nil {
		return nil
	}
	obs.Settings().SetExtra("other", 6)
	return nil
}
`,
	})

	file := res.files["handlers/handlers.go"]
	for _, key := range []string{"hub", "function", "variable", "field"} {
		assert.Contains(t, file, fmt.Sprintf(`.SetContext(%q, map[string]any{"value": `, key), key)
	}
	assert.Contains(t, file, `obs.Event().Contexts["event"] = map[string]any{"value": 5}`)
	assert.Contains(t, file, "if !obs.Hub.Client().IsEnabled() {")
	// a value that is not one of sentry-go
	assert.Contains(t, file, `obs.Settings().SetExtra("other", 6)`)
	if assert.Len(t, res.notes, 1) {
		assert.Contains(t, res.notes[0], "handlers/handlers.go:14: "+sentryExtrasNote)
		assert.Contains(t, res.notes[0], "(4 more in this project)")
	}
	assert.Contains(t, res.stdout, "Migrating the sentry-go API")
}

func Test_MigrateSentryAPI_UsesTheFilesAsEarlierRewritesLeftThem(t *testing.T) {
	t.Parallel()

	// the scope is made in a file that the migration rewrites first, with a
	// different name for the package of sentry-go in each file
	res := mw1RunModule(t, v3.MigrateSentryAPI, map[string]string{
		"a_scope.go": `package main

import sg "github.com/getsentry/sentry-go"

func newScope() *sg.Scope {
	scope := sg.NewScope()
	scope.SetExtra("first", 1)
	return scope
}
`,
		"b_use.go": `package main

func use() {
	newScope().SetExtra("second", 2)
}
`,
	})

	assert.Contains(t, res.files["a_scope.go"], `scope.SetContext("first", sg.Context{"value": 1})`)
	assert.Contains(t, res.files["b_use.go"], `newScope().SetContext("second", map[string]any{"value": 2})`)
}

func Test_MigrateSentryAPI_CRLF(t *testing.T) {
	t.Parallel()

	src := `package main

` + sentryImports + `
func options() sentry.ClientOptions {
	return sentry.ClientOptions{
		Dsn:                "a",
		ProfilesSampleRate: 1, // profiles
		EnableLogs:         true,
	}
}

func extras(scope *sentry.Scope) {
	scope.SetExtra("k", 1)
	scope.SetExtras(map[string]interface{}{
		"a": 1,
	})
}
`
	unix := mw1Run(t, v3.MigrateSentryAPI, src)
	windows := mw1Run(t, v3.MigrateSentryAPI, strings.ReplaceAll(src, "\n", "\r\n"))

	assert.NotEqual(t, src, unix.file())
	assert.Equal(t, unix.file(), strings.ReplaceAll(windows.file(), "\r", ""))
	assert.Equal(t, unix.notes, windows.notes)
}
