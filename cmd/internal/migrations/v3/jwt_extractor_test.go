package v3_test

import (
	"bytes"
	"strings"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// contribCompact collapses every run of white space to one space, which is what
// gofmt's alignment of values leaves of a comparison.
func contribCompact(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// contribMigration is the shape of every migration function.
type contribMigration = func(*cobra.Command, string, *semver.Version, *semver.Version) error

// contribMigrate runs the migrations over dir, one after the other, the way the
// list does, and returns what they printed.
func contribMigrate(t *testing.T, dir string, migrations ...contribMigration) string {
	t.Helper()

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	for _, m := range migrations {
		require.NoError(t, m(cmd, dir, nil, nil))
	}
	return buf.String()
}

// jwtConfigSource wraps the body of a function into a program that imports the
// jwt middleware under the name jwtware.
func jwtConfigSource(body string) string {
	return `package main

import (
	"os"

	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/contrib/jwt"
)

var key = []byte("secret")

` + body + `
`
}

func Test_MigrateJWTExtractor(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, jwtConfigSource(`var _ = jwtware.New(jwtware.Config{
	SigningKey: jwtware.SigningKey{Key: key},
	TokenLookup: "header:Authorization, cookie:jwt",
	AuthScheme: "Token",
	Filter: func(c *fiber.Ctx) bool { return false },
})`))

	out := contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.NotContains(t, content, "TokenLookup")
	assert.NotContains(t, content, "AuthScheme")
	assert.NotContains(t, content, "Filter")
	assert.Contains(t, contribCompact(content), contribCompact(`Extractor: extractors.Chain(extractors.FromAuthHeader("Token"), extractors.FromCookie("jwt")),`))
	assert.Contains(t, contribCompact(content), contribCompact("Next: func(c fiber.Ctx) bool { return false },"))
	assert.Contains(t, content, "\t\"github.com/gofiber/fiber/v3/extractors\"\n")
	assert.Contains(t, out, "Migrating jwt middleware configs")
	assert.Empty(t, internal.DrainNotes(dir))
}

// v2 applied the AuthScheme to the header lookups and defaulted it to Bearer
// only when there was no TokenLookup, so the credential a client has to send
// depends on both fields. Each case below is a different credential.
func Test_MigrateJWTExtractor_KeepsTheAcceptedCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   string // the extractor, "" when none is needed
	}{
		{
			name:   "scheme alone: Token <jwt>",
			config: `AuthScheme: "Token",`,
			want:   `Extractor: extractors.FromAuthHeader("Token"),`,
		},
		{
			name:   "scheme alone, the default one",
			config: `AuthScheme: "Bearer",`,
		},
		{
			name:   "scheme alone, spelled differently",
			config: `AuthScheme: "bearer",`,
		},
		{
			name:   "empty scheme alone is Bearer",
			config: `AuthScheme: "",`,
		},
		{
			name:   "Authorization header without a scheme is the bare token",
			config: `TokenLookup: "header:Authorization",`,
			want:   `Extractor: extractors.FromAuthHeader(""),`,
		},
		{
			name:   "Authorization header with the default scheme",
			config: "TokenLookup: \"header:Authorization\",\n\tAuthScheme: \"Bearer\",",
			want:   `Extractor: extractors.FromAuthHeader("Bearer"),`,
		},
		{
			name:   "header name is not case sensitive",
			config: "TokenLookup: \"header:authorization\",\n\tAuthScheme: \"Token\",",
			want:   `Extractor: extractors.FromAuthHeader("Token"),`,
		},
		{
			name:   "another header without a scheme",
			config: `TokenLookup: "header:X-Auth-Token",`,
			want:   `Extractor: extractors.FromHeader("X-Auth-Token"),`,
		},
		{
			name:   "cookie ignores the scheme",
			config: "TokenLookup: \"cookie:jwt\",\n\tAuthScheme: \"Bearer\",",
			want:   `Extractor: extractors.FromCookie("jwt"),`,
		},
		{
			name:   "query and param",
			config: `TokenLookup: "query:token, param:jwt",`,
			want:   `Extractor: extractors.Chain(extractors.FromQuery("token"), extractors.FromParam("jwt")),`,
		},
		{
			name:   "form",
			config: `TokenLookup: "form:token",`,
			want:   `Extractor: extractors.FromForm("token"),`,
		},
		{
			name:   "sources v2 did not know are skipped",
			config: `TokenLookup: "bogus:x, cookie:jwt",`,
			want:   `Extractor: extractors.FromCookie("jwt"),`,
		},
		{
			name:   "lookup built from the header constant",
			config: "TokenLookup: \"header:\" + fiber.HeaderAuthorization,\n\tAuthScheme: \"Token\",",
			want:   `Extractor: extractors.FromAuthHeader("Token"),`,
		},
		{
			name:   "scheme in a constant",
			config: "TokenLookup: \"header:Authorization\",\n\tAuthScheme: schemeConst,",
			want:   `Extractor: extractors.FromAuthHeader(schemeConst),`,
		},
		{
			name:   "scheme that is computed, on the Authorization header",
			config: "TokenLookup: \"header:Authorization\",\n\tAuthScheme: scheme,",
			want:   `Extractor: extractors.FromAuthHeader(scheme),`,
		},
		{
			name:   "scheme in a constant, alone",
			config: `AuthScheme: schemeConst,`,
			want:   `Extractor: extractors.FromAuthHeader(schemeConst),`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, jwtConfigSource("const schemeConst = \"Token\"\nvar scheme = os.Getenv(\"SCHEME\")\n"+
				"var _ = jwtware.New(jwtware.Config{\n\tSigningKey: jwtware.SigningKey{Key: key},\n\t"+tt.config+"\n})"))

			out := contribMigrate(t, dir, v3.MigrateJWTExtractor)

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.NotContains(t, content, "TokenLookup")
			assert.NotContains(t, content, "AuthScheme")
			if tt.want == "" {
				assert.NotContains(t, content, "Extractor")
				assert.NotContains(t, content, "extractors")
				assert.Contains(t, out, "Migrating jwt middleware configs")
			} else {
				assert.Contains(t, contribCompact(content), contribCompact(tt.want))
				assert.Contains(t, content, "\t\"github.com/gofiber/fiber/v3/extractors\"\n")
			}
			assert.Contains(t, content, "SigningKey: jwtware.SigningKey{Key: key},")
			assert.Empty(t, internal.DrainNotes(dir))
		})
	}
}

// No extractor of v3 strips a scheme from a header other than Authorization, and
// a lookup that is computed cannot be read: both stay as they are, which does not
// compile, and are reported.
func Test_MigrateJWTExtractor_LeavesWhatItCannotTranslate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		message string
	}{
		{
			name:    "scheme on another header",
			config:  "TokenLookup: \"header:X-Auth-Token\",\n\tAuthScheme: \"Bearer\",",
			message: `the AuthScheme ("Bearer") in front of the token in the X-Auth-Token header`,
		},
		{
			name:    "computed scheme on another header",
			config:  "TokenLookup: \"header:X-Auth-Token\",\n\tAuthScheme: scheme,",
			message: "the AuthScheme (scheme) in front of the token in the X-Auth-Token header",
		},
		{
			name:    "computed lookup",
			config:  "TokenLookup: strings.Join([]string{\"header:Authorization\"}, \",\"),\n\tAuthScheme: \"Bearer\",",
			message: "TokenLookup is not a string constant",
		},
		{
			name:    "computed scheme alone",
			config:  `AuthScheme: scheme,`,
			message: "AuthScheme is not a string constant",
		},
		{
			name:    "no part of the lookup is one v2 reads",
			config:  `TokenLookup: "bogus:x",`,
			message: "names no source that v2 reads",
		},
		{
			name:    "part without a name",
			config:  `TokenLookup: "cookie",`,
			message: "not made of <source>:<name> parts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			source := jwtConfigSource("var scheme = os.Getenv(\"SCHEME\")\nvar _ = jwtware.New(jwtware.Config{\n\tSigningKey: jwtware.SigningKey{Key: key},\n\t" + tt.config + "\n})")
			file := writeTempFile(t, dir, source)

			out := contribMigrate(t, dir, v3.MigrateJWTExtractor)

			assert.Equal(t, source, readFile(t, file))
			assert.Empty(t, out, "nothing changed")
			notes := internal.DrainNotes(dir)
			require.Len(t, notes, 1)
			assert.Equal(t, "main.go", notes[0].File)
			assert.Contains(t, notes[0].Message, tt.message)
			assert.Contains(t, notes[0].Message, "Config.Extractor")
		})
	}
}

func Test_MigrateJWTExtractor_InlineConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, jwtConfigSource(`var _ = jwtware.New(jwtware.Config{SigningKey: jwtware.SigningKey{Key: key}, TokenLookup: "cookie:jwt", AuthScheme: "Bearer", ContextKey: "jwt"})`))

	out := contribMigrate(t, dir, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, contribCompact(content), contribCompact(`jwtware.Config{SigningKey: jwtware.SigningKey{Key: key}, Extractor: extractors.FromCookie("jwt")})`))
	assert.Contains(t, out, "Migrating jwt middleware configs")
}

// The fields after a handler that nests braces (an if, a switch, a closure) were
// out of reach of the regular expression the migration used to be.
func Test_MigrateJWTExtractor_HandlersWithNestedBraces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, jwtConfigSource(`var _ = jwtware.New(jwtware.Config{
	SigningKey: jwtware.SigningKey{Key: key},
	SuccessHandler: func(c *fiber.Ctx) error {
		if c.Path() == "/" {
			for _, h := range []string{"a"} {
				if h == "a" {
					return c.SendStatus(fiber.StatusOK)
				}
			}
		}
		return c.Next()
	},
	ErrorHandler: func(c *fiber.Ctx, err error) error {
		switch {
		case err != nil:
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": map[string]string{"message": err.Error()}})
		}
		return nil
	},
	KeyFunc: func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, nil
		}
		return key, nil
	},
	TokenLookup: "header:Authorization",
	AuthScheme:  "Token",
	ContextKey:  "token",
})`))

	contribMigrate(t, dir, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.NotContains(t, content, "TokenLookup")
	assert.NotContains(t, content, "AuthScheme")
	assert.NotContains(t, content, "ContextKey")
	assert.Contains(t, contribCompact(content), contribCompact(`Extractor: extractors.FromAuthHeader("Token"),`))
	assert.Contains(t, contribCompact(content), "return key, nil }, Extractor:")
}

func Test_MigrateJWTExtractor_UnaliasedImportAfterTheContribMigration(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/contrib/jwt"
	"github.com/gofiber/fiber/v2"
)

var _ = jwtware.New(jwtware.Config{
	SigningKey: jwtware.SigningKey{Key: []byte("secret")},
	TokenLookup: "cookie:jwt",
})

var _ = fiber.New
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "\tjwtware \"github.com/gofiber/contrib/v3/jwt\"\n")
	assert.Contains(t, contribCompact(content), contribCompact(`Extractor: extractors.FromCookie("jwt"),`))
	assert.NotContains(t, content, "TokenLookup")
}

func Test_MigrateJWTExtractor_CustomAlias(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"github.com/gofiber/contrib/jwt", "github.com/gofiber/contrib/v3/jwt", "github.com/gofiber/jwt/v3"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, `package main

import authjwt "`+path+`"

var _ = authjwt.New(authjwt.Config{
	TokenLookup: "cookie:session",
})
`)

			contribMigrate(t, dir, v3.MigrateJWTExtractor)

			content := readFile(t, file)
			assert.NotContains(t, content, "TokenLookup")
			assert.Contains(t, contribCompact(content), contribCompact(`Extractor: extractors.FromCookie("session"),`))
		})
	}
}

func Test_MigrateJWTExtractor_PointerConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, jwtConfigSource(`func middleware() fiber.Handler {
	return jwtware.New(jwtware.Config{
		SigningKey:  jwtware.SigningKey{Key: key},
		TokenLookup: "cookie:jwt",
	})
}

var cfg = &jwtware.Config{TokenLookup: "query:jwt"}`))

	contribMigrate(t, dir, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, contribCompact(content), contribCompact(`Extractor: extractors.FromCookie("jwt"),`))
	assert.Contains(t, contribCompact(content), contribCompact(`&jwtware.Config{Extractor: extractors.FromQuery("jwt")}`))
}

// The config of the module's README, built in steps, and the one the migration
// used to miss: the fields are assigned after the literal.
func Test_MigrateJWTExtractor_AssignedFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, jwtConfigSource(`func config() jwtware.Config {
	cfg := jwtware.Config{
		SigningKey: jwtware.SigningKey{Key: key},
	}
	cfg.TokenLookup = "cookie:jwt"
	cfg.ContextKey = "jwt"
	cfg.Filter = func(c *fiber.Ctx) bool {
		return c.Path() == "/login"
	}
	return cfg
}

func scheme() jwtware.Config {
	var cfg jwtware.Config
	cfg.SigningKey = jwtware.SigningKey{Key: key}
	cfg.AuthScheme = "Token"
	return cfg
}`))

	out := contribMigrate(t, dir, v3.MigrateHandlerSignatures, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, contribCompact(content), contribCompact(`cfg.Extractor = extractors.FromCookie("jwt")`))
	assert.Contains(t, content, "cfg.Next = func(c fiber.Ctx) bool {")
	assert.NotContains(t, content, "cfg.ContextKey")
	assert.NotContains(t, content, "cfg.TokenLookup")
	assert.Contains(t, contribCompact(content), contribCompact(`cfg.Extractor = extractors.FromAuthHeader("Token")`))
	assert.NotContains(t, content, "cfg.AuthScheme")
	assert.Contains(t, out, "Migrating jwt middleware configs")
	assert.Empty(t, internal.DrainNotes(dir))
}

// A field assigned inside an if is only part of the config on some paths, which
// an extractor cannot reproduce.
func Test_MigrateJWTExtractor_ConditionalAssignmentIsReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := jwtConfigSource(`func config(useCookie bool) jwtware.Config {
	cfg := jwtware.Config{SigningKey: jwtware.SigningKey{Key: key}, AuthScheme: "Token"}
	if useCookie {
		cfg.TokenLookup = "cookie:jwt"
	}
	return cfg
}`)
	file := writeTempFile(t, dir, source)

	contribMigrate(t, dir, v3.MigrateJWTExtractor)

	assert.Equal(t, source, readFile(t, file))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "set conditionally")
}

func Test_MigrateJWTExtractor_Errors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/contrib/jwt"
)

func handler(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, jwtware.ErrJWTMissingOrMalformed):
		return c.Status(fiber.StatusBadRequest).SendString(jwtware.ErrJWTMissingOrMalformed.Error())
	case err == jwtware.ErrJWTMissingOrMalformed:
		return nil
	case err.Error() == jwtware.ErrJWTMissingOrMalformed.Error():
		return nil
	case err.Error() == "missing or malformed JWT":
		return c.SendStatus(fiber.StatusBadRequest)
	case err.Error() != "missing or malformed JWT":
		return nil
	case err.Error() == "Missing or malformed JWT": // not the text of this module
		return nil
	}
	return c.SendStatus(fiber.StatusUnauthorized)
}

func tag(err error) bool {
	switch err {
	case jwtware.ErrJWTMissingOrMalformed:
		return true
	}
	return false
}
`)

	out := contribMigrate(t, dir, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "case errors.Is(err, extractors.ErrNotFound):")
	assert.Contains(t, content, "SendString(jwtware.ErrMissingToken.Error())", "the text of the error stays the one clients see")
	assert.Contains(t, content, "case err == extractors.ErrNotFound:")
	assert.Contains(t, content, "case err.Error() == extractors.ErrNotFound.Error():")
	assert.Contains(t, content, "case errors.Is(err, extractors.ErrNotFound):\n\t\treturn c.SendStatus(fiber.StatusBadRequest)")
	assert.Contains(t, content, "case !errors.Is(err, extractors.ErrNotFound):")
	assert.Contains(t, content, `case err.Error() == "Missing or malformed JWT": // not the text of this module`)
	assert.Contains(t, content, "\tcase extractors.ErrNotFound:\n")
	assert.NotContains(t, content, "ErrJWTMissingOrMalformed")
	assert.Contains(t, out, "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_ErrorsOfTheStandaloneModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/jwt/v3"
)

func jwtError(c *fiber.Ctx, err error) error {
	if err.Error() == "Missing or malformed JWT" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"message": "Missing or malformed JWT"})
	}
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"message": "Invalid or expired JWT"})
}

var _ = jwtware.New
`)

	contribMigrate(t, dir, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "if errors.Is(err, extractors.ErrNotFound) {")
	assert.Contains(t, content, "\t\"errors\"\n")
	assert.Contains(t, content, `{"message": "Missing or malformed JWT"}`, "the response text is not a comparison")
}

// A file that uses the name errors for something else keeps its comparison and
// is told about it.
func Test_MigrateJWTExtractor_ErrorsPackageShadowed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import jwtware "github.com/gofiber/contrib/jwt"

var errors = []error{}

func isMissing(err error) bool {
	return err.Error() == "missing or malformed JWT"
}

var _ = jwtware.New
`)

	contribMigrate(t, dir, v3.MigrateJWTExtractor)

	assert.Contains(t, readFile(t, file), `return err.Error() == "missing or malformed JWT"`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "extractors.ErrNotFound")
}

func Test_MigrateJWTExtractor_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, jwtConfigSource(`func config() jwtware.Config {
	cfg := jwtware.Config{TokenLookup: "cookie:jwt", AuthScheme: "Bearer", Filter: func(c *fiber.Ctx) bool { return false }}
	cfg.ContextKey = "jwt"
	return cfg
}

var _ = jwtware.New(jwtware.Config{AuthScheme: "Token"})

func handler(err error) bool { return err == jwtware.ErrJWTMissingOrMalformed }`))

	contribMigrate(t, dir, v3.MigrateJWTExtractor)
	migrated := readFile(t, file)
	internal.DrainNotes(dir)

	out := contribMigrate(t, dir, v3.MigrateJWTExtractor)

	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateJWTExtractor_LeavesV3ConfigsAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	jwtware "github.com/gofiber/contrib/v3/jwt"
)

var _ = jwtware.New(jwtware.Config{
	SigningKey: jwtware.SigningKey{Key: []byte("secret")},
	Extractor:  extractors.FromCookie("jwt"),
	Next:       func(c fiber.Ctx) bool { return false },
})

var _ = jwtware.New(jwtware.Config{SigningKey: jwtware.SigningKey{Key: []byte("secret")}})
`
	file := writeTempFile(t, dir, source)

	out := contribMigrate(t, dir, v3.MigrateJWTExtractor)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
}

func Test_MigrateJWTExtractor_SkipUnrelatedPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	original := `package main

import jwtware "example.com/jwtware"

var _ = jwtware.Config{
	TokenLookup: "header:Authorization",
	ContextKey:  "user",
}

func is(err error) bool { return err == jwtware.ErrJWTMissingOrMalformed }
`
	file := writeTempFile(t, dir, original)

	out := contribMigrate(t, dir, v3.MigrateJWTExtractor)

	assert.Equal(t, original, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateJWTExtractor_SkipsBrokenFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import jwtware "github.com/gofiber/contrib/jwt"

var _ = jwtware.Config{TokenLookup: "cookie:jwt"
`
	file := writeTempFile(t, dir, source)

	out := contribMigrate(t, dir, v3.MigrateJWTExtractor)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "does not parse")
}
