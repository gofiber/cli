package v3_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// otelSource wraps the body of a program that imports the otel middleware the way
// its README does.
func otelSource(imports, body string) string {
	return `package main

import (
	"os"

	` + imports + `
	"github.com/gofiber/fiber/v3"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	tp   = sdktrace.NewTracerProvider()
	name = os.Getenv("SERVICE")
)

type settings struct{ Service string }

var cfg = settings{Service: "checkout"}

const serviceName = "billing"

` + body + `
`
}

const otelFiberImport = `otelfiber "github.com/gofiber/contrib/v3/otel"`

// otelServerNameOptions are the two options that stand for WithServerName(value).
func otelServerNameOptions(pkg, value string) string {
	option := func(member string) string {
		return pkg + "." + member + "(func(fiber.Ctx) []attribute.KeyValue {\n" +
			"\t\t\treturn []attribute.KeyValue{attribute.String(\"http.server_name\", " + value + ")}\n" +
			"\t\t})"
	}
	return option("WithCustomAttributes") + ",\n\t\t" + option("WithCustomMetricAttributes")
}

func Test_MigrateOtelOptions(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateOtelOptions, otelSource(otelFiberImport, `func setup(app *fiber.App) {
	app.Use(otelfiber.Middleware(
		otelfiber.WithTracerProvider(tp),
		otelfiber.WithServerName("checkout"),
		otelfiber.WithPropagators(nil),
	))
}`))

	file := res.file()
	assert.Contains(t, file, "otelfiber.WithTracerProvider(tp),\n\t\t"+otelServerNameOptions("otelfiber", `"checkout"`)+",\n\t\totelfiber.WithPropagators(nil),")
	assert.NotContains(t, file, "WithServerName")
	assert.Contains(t, file, "\t\"go.opentelemetry.io/otel/attribute\"\n")
	assert.Contains(t, res.stdout, "Migrating otel options")
	assert.Empty(t, res.notes)

	// a second run has nothing left to do
	again := mw1Run(t, v3.MigrateOtelOptions, file)
	assert.Equal(t, file, again.file())
	assert.Empty(t, again.stdout)
	assert.Empty(t, again.notes)
}

func Test_MigrateOtelOptions_ImportSpellings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		imports string
		pkg     string
	}{
		{"v2 path", `"github.com/gofiber/contrib/otelfiber"`, "otelfiber"},
		{"v2 path with the major version", `"github.com/gofiber/contrib/otelfiber/v2"`, "otelfiber"},
		{"v3 path under the old name", `otelfiber "github.com/gofiber/contrib/v3/otel"`, "otelfiber"},
		{"v3 path", `"github.com/gofiber/contrib/v3/otel"`, "otel"},
		{"own name", `tracing "github.com/gofiber/contrib/otelfiber"`, "tracing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := mw1Run(t, v3.MigrateOtelOptions, otelSource(tt.imports, fmt.Sprintf(`func setup(app *fiber.App) {
	app.Use(%[1]s.Middleware(%[1]s.WithServerName(name)))
}`, tt.pkg)))

			file := res.file()
			assert.NotContains(t, file, "WithServerName")
			assert.Contains(t, file, fmt.Sprintf("%s.WithCustomAttributes(func(fiber.Ctx) []attribute.KeyValue {", tt.pkg))
			assert.Contains(t, file, fmt.Sprintf("%s.WithCustomMetricAttributes(func(fiber.Ctx) []attribute.KeyValue {", tt.pkg))
			assert.Contains(t, file, `attribute.String("http.server_name", name)`)
			assert.Empty(t, res.notes)
		})
	}
}

func Test_MigrateOtelOptions_Names(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		// want is the value as gofmt writes it in the arguments of a call
		want string
		note bool
	}{
		{"literal", `"checkout"`, `"checkout"`, false},
		{"constant", `serviceName`, `serviceName`, false},
		{"variable", `name`, `name`, false},
		{"field", `cfg.Service`, `cfg.Service`, false},
		{"sum", `"svc-" + cfg.Service`, `"svc-"+cfg.Service`, false},
		{"in parentheses", `("svc-" + name)`, `("svc-" + name)`, false},
		{"a call", `os.Getenv("SERVICE")`, "", true},
		{"a conversion", `string(name)`, "", true},
		{"a call in a sum", `"svc-" + os.Getenv("SERVICE")`, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := otelSource(otelFiberImport, fmt.Sprintf(`func setup(app *fiber.App) {
	app.Use(otelfiber.Middleware(
		otelfiber.WithServerName(%s),
	))
}`, tt.value))
			res := mw1Run(t, v3.MigrateOtelOptions, src)

			if !tt.note {
				assert.NotContains(t, res.file(), "WithServerName")
				assert.Contains(t, res.file(), fmt.Sprintf(`attribute.String("http.server_name", %s)`, tt.want))
				assert.Empty(t, res.notes)
				return
			}

			// the call cannot be moved into a function that runs for every request: it stays, with a note
			assert.Equal(t, src, res.file())
			assert.Empty(t, res.stdout)
			if assert.Len(t, res.notes, 1) {
				assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: otel: WithServerName is not in the v3 module", mw1LineOf(t, src, "WithServerName")))
				assert.Contains(t, res.notes[0], "the name is the result of a call")
				assert.Contains(t, res.notes[0], `attribute.String("http.server_name", name)`)
			}
		})
	}
}

func Test_MigrateOtelOptions_OptionLiteral(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateOtelOptions, otelSource(otelFiberImport, `func options() []otelfiber.Option {
	return []otelfiber.Option{
		otelfiber.WithTracerProvider(tp),
		otelfiber.WithServerName(serviceName),
	}
}`))

	file := res.file()
	assert.NotContains(t, file, "WithServerName")
	assert.Contains(t, file, `attribute.String("http.server_name", serviceName)`)
	assert.Contains(t, file, "otelfiber.WithCustomMetricAttributes(func(fiber.Ctx) []attribute.KeyValue {")
	assert.Empty(t, res.notes)
}

func Test_MigrateOtelOptions_SingleLine(t *testing.T) {
	t.Parallel()

	res := mw1Run(t, v3.MigrateOtelOptions, otelSource(otelFiberImport, `func setup(app *fiber.App) {
	app.Use(otelfiber.Middleware(otelfiber.WithTracerProvider(tp), otelfiber.WithServerName("checkout")))
}`))

	file := res.file()
	assert.NotContains(t, file, "WithServerName")
	assert.Contains(t, file, `attribute.String("http.server_name", "checkout")`)
	assert.Contains(t, file, "otelfiber.WithTracerProvider(tp), otelfiber.WithCustomAttributes(")
	assert.Empty(t, res.notes)
	contribRequireParses(t, file)
}

// The two options hold one function each and the last one given counts, so the
// name goes into the options of the project, or stays where it is.
func Test_MigrateOtelOptions_KeepsTheCustomAttributesOfTheProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		reason string
	}{
		{
			name: "attributes next to it",
			body: `func setup(app *fiber.App) {
	app.Use(otelfiber.Middleware(
		otelfiber.WithServerName("checkout"),
		otelfiber.WithCustomAttributes(func(c fiber.Ctx) []attribute.KeyValue { return nil }),
	))
}`,
			reason: "the options next to it set custom attributes already",
		},
		{
			name: "metric attributes next to it",
			body: `func setup(app *fiber.App) {
	app.Use(otelfiber.Middleware(
		otelfiber.WithCustomMetricAttributes(func(c fiber.Ctx) []attribute.KeyValue { return nil }),
		otelfiber.WithServerName("checkout"),
	))
}`,
			reason: "the options next to it set custom attributes already",
		},
		{
			name: "options in a variable",
			body: `func setup(app *fiber.App, extra ...otelfiber.Option) {
	app.Use(otelfiber.Middleware(append(extra, otelfiber.WithServerName("checkout"))...))
}`,
			reason: "neither an argument of Middleware or New nor an element of a []Option literal",
		},
		{
			name: "an option the file does not show",
			body: `func setup(app *fiber.App, extra otelfiber.Option) {
	app.Use(otelfiber.Middleware(extra, otelfiber.WithServerName("checkout")))
}`,
			reason: "an option next to it is not written here",
		},
		{
			name: "the option in a variable",
			body: `func setup(app *fiber.App) {
	server := otelfiber.WithServerName("checkout")
	app.Use(otelfiber.Middleware(server))
}`,
			reason: "neither an argument of Middleware or New nor an element of a []Option literal",
		},
		{
			name: "a literal and attributes elsewhere in the file",
			body: `func options() []otelfiber.Option {
	return []otelfiber.Option{otelfiber.WithServerName("checkout")}
}

func more() otelfiber.Option {
	return otelfiber.WithCustomAttributes(func(c fiber.Ctx) []attribute.KeyValue { return nil })
}`,
			reason: "this file sets custom attributes elsewhere",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := otelSource(otelFiberImport+"\n\t\"go.opentelemetry.io/otel/attribute\"", tt.body)
			res := mw1Run(t, v3.MigrateOtelOptions, src)

			assert.Equal(t, src, res.file())
			assert.Empty(t, res.stdout)
			if assert.Len(t, res.notes, 1) {
				assert.Contains(t, res.notes[0], fmt.Sprintf("main.go:%d: otel: WithServerName is not in the v3 module", mw1LineOf(t, src, "WithServerName")))
				assert.Contains(t, res.notes[0], tt.reason)
			}
		})
	}
}

func Test_MigrateOtelOptions_ImportsOfItsOwn(t *testing.T) {
	t.Parallel()

	// the file imports neither fiber nor attribute, and uses attribute for another name
	res := mw1Run(t, v3.MigrateOtelOptions, `package main

import (
	otelfiber "github.com/gofiber/contrib/v3/otel"
)

var attribute = "mine"

var options = []otelfiber.Option{otelfiber.WithServerName(attribute)}
`)

	file := res.file()
	assert.Contains(t, file, `otelattribute "go.opentelemetry.io/otel/attribute"`)
	assert.Contains(t, file, `"github.com/gofiber/fiber/v3"`)
	assert.Contains(t, file, `otelattribute.String("http.server_name", attribute)`)
	assert.Contains(t, file, "func(fiber.Ctx) []otelattribute.KeyValue")
	assert.NotContains(t, file, "WithServerName")
	assert.Empty(t, res.notes)
}

func Test_MigrateOtelOptions_LeavesOtherCodeAlone(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	otelfiber "github.com/gofiber/contrib/v3/otel"
	"github.com/gofiber/fiber/v3"
)

type server struct{}

func (server) WithServerName(string) fiber.Handler { return nil }

var _ = otelfiber.Middleware

func setup(app *fiber.App, s server) {
	// otelfiber.WithServerName("x") in a comment
	app.Use(s.WithServerName("checkout"))
	_ = "otelfiber.WithServerName(\"y\")"
}

func shadowed(app *fiber.App) {
	otelfiber := server{}
	app.Use(otelfiber.WithServerName("checkout"))
}
`
	res := mw1Run(t, v3.MigrateOtelOptions, src)

	assert.Equal(t, src, res.file())
	assert.Empty(t, res.stdout)
	assert.Empty(t, res.notes)

	// a project without the middleware, with a function of that name
	other := `package main

import "example.com/other"

var _ = other.WithServerName("x")
`
	res = mw1Run(t, v3.MigrateOtelOptions, other)
	assert.Equal(t, other, res.file())
	assert.Empty(t, res.notes)
}

func Test_MigrateOtelOptions_CRLF(t *testing.T) {
	t.Parallel()

	src := otelSource(otelFiberImport, `func setup(app *fiber.App) {
	app.Use(otelfiber.Middleware(
		otelfiber.WithServerName("checkout"),
	))
}`)
	unix := mw1Run(t, v3.MigrateOtelOptions, src)
	windows := mw1Run(t, v3.MigrateOtelOptions, strings.ReplaceAll(src, "\n", "\r\n"))

	assert.NotEqual(t, src, unix.file())
	assert.Equal(t, unix.file(), strings.ReplaceAll(windows.file(), "\r", ""))
}
