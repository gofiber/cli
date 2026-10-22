package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// pasetoConfigSource wraps the body of a function into a program that imports
// the paseto middleware under the name pasetoware.
func pasetoConfigSource(body string) string {
	return `package main

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	pasetoware "github.com/gofiber/contrib/paseto"
)

var key = []byte("secret-key-of-32-bytes-length!!!")

const header = "Authorization"

const prefixConst = "Token"

var prefix = os.Getenv("PREFIX")

var lookup = [2]string{"header", "Authorization"}

var _ = strings.ToUpper

var _ = fiber.New

` + body + `
`
}

func Test_MigratePasetoExtractor_HeaderPrefix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, pasetoConfigSource(`var _ = pasetoware.New(pasetoware.Config{
	SymmetricKey: key,
	TokenLookup: [2]string{"header", "Authorization"},
	TokenPrefix: "Bearer",
})`))

	out := contribMigrate(t, dir, v3.MigratePasetoExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.NotContains(t, content, "TokenLookup")
	assert.NotContains(t, content, "TokenPrefix")
	assert.Contains(t, contribCompact(content), contribCompact(`Extractor: extractors.FromAuthHeader("Bearer"),`))
	assert.Contains(t, content, "\t\"github.com/gofiber/fiber/v3/extractors\"\n")
	assert.Contains(t, out, "Migrating paseto middleware configs")
	assert.Empty(t, internal.DrainNotes(dir))
}

// v2 asked for a prefix wherever the token was read, and for none by default.
func Test_MigratePasetoExtractor_KeepsTheAcceptedCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "no lookup and no prefix: the bare token of the Authorization header",
			config: ``,
			want:   `Extractor: extractors.FromAuthHeader(""),`,
		},
		{
			name:   "prefix alone: Token <paseto>",
			config: `TokenPrefix: "Token",`,
			want:   `Extractor: extractors.FromAuthHeader("Token"),`,
		},
		{
			name:   "the default lookup spelled out, with its prefix (what ConfigDefault of v2 is)",
			config: "TokenLookup: [2]string{pasetoware.LookupHeader, fiber.HeaderAuthorization},\n\tTokenPrefix: \"Bearer\",",
			want:   `Extractor: extractors.FromAuthHeader("Bearer"),`,
		},
		{
			name:   "header without a prefix",
			config: `TokenLookup: [2]string{"header", "Authorization"},`,
			want:   `Extractor: extractors.FromAuthHeader(""),`,
		},
		{
			name:   "header named by a constant",
			config: "TokenLookup: [2]string{pasetoware.LookupHeader, header},\n\tTokenPrefix: \"Bearer\",",
			want:   `Extractor: extractors.FromAuthHeader("Bearer"),`,
		},
		{
			name:   "an empty lookup means the defaults",
			config: `TokenLookup: [2]string{"", ""},`,
			want:   `Extractor: extractors.FromAuthHeader(""),`,
		},
		{
			name:   "a source v2 did not know was read from a header",
			config: `TokenLookup: [2]string{"headers", "X-Token"},`,
			want:   `Extractor: extractors.FromHeader("X-Token"),`,
		},
		{
			name:   "another header",
			config: `TokenLookup: [2]string{pasetoware.LookupHeader, "X-Token"},`,
			want:   `Extractor: extractors.FromHeader("X-Token"),`,
		},
		{
			name:   "prefix in a constant",
			config: `TokenPrefix: prefixConst,`,
			want:   `Extractor: extractors.FromAuthHeader(prefixConst),`,
		},
		{
			name:   "prefix that is computed, on the Authorization header",
			config: "TokenLookup: [2]string{\"header\", header},\n\tTokenPrefix: prefix,",
			want:   `Extractor: extractors.FromAuthHeader(prefix),`,
		},
		{
			name:   "header named by a concatenation",
			config: `TokenLookup: [2]string{"header", "X-" + "Token"},`,
			want:   `Extractor: extractors.FromHeader("X-Token"),`,
		},
		{
			name:   "query",
			config: `TokenLookup: [2]string{pasetoware.LookupQuery, "token"},`,
			want:   `Extractor: extractors.FromQuery("token"),`,
		},
		{
			name:   "param",
			config: `TokenLookup: [2]string{pasetoware.LookupParam, "token"},`,
			want:   `Extractor: extractors.FromParam("token"),`,
		},
		{
			name:   "cookie",
			config: `TokenLookup: [2]string{"cookie", "session"},`,
			want:   `Extractor: extractors.FromCookie("session"),`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, pasetoConfigSource("var _ = pasetoware.New(pasetoware.Config{\n\tSymmetricKey: key,\n\t"+tt.config+"\n})"))

			out := contribMigrate(t, dir, v3.MigratePasetoExtractor)

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.NotContains(t, content, "TokenLookup")
			assert.NotContains(t, content, "TokenPrefix")
			assert.Contains(t, contribCompact(content), contribCompact(tt.want))
			assert.Contains(t, content, "SymmetricKey: key,")
			assert.Contains(t, out, "Migrating paseto middleware configs")
			assert.Empty(t, internal.DrainNotes(dir))
		})
	}
}

// Nothing in the extractors package reads a prefix from a place other than the
// Authorization header: those configs stay as they are, which does not compile,
// and are reported.
func Test_MigratePasetoExtractor_LeavesWhatItCannotTranslate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		message string
	}{
		{
			name:    "prefix on another header",
			config:  "TokenLookup: [2]string{\"header\", \"X-Token\"},\n\tTokenPrefix: \"Bearer\",",
			message: `the TokenPrefix ("Bearer") in front of the token in the X-Token header`,
		},
		{
			name:    "prefix in a cookie",
			config:  "TokenLookup: [2]string{\"cookie\", \"session\"},\n\tTokenPrefix: \"v2\",",
			message: `the TokenPrefix ("v2") in front of the token in the cookie session`,
		},
		{
			name:    "computed lookup",
			config:  `TokenLookup: lookup,`,
			message: "TokenLookup is not an array of string constants",
		},
		{
			name:    "computed element",
			config:  `TokenLookup: [2]string{"header", strings.ToUpper("authorization")},`,
			message: "TokenLookup is not an array of string constants",
		},
		{
			name:    "computed prefix in a cookie",
			config:  "TokenLookup: [2]string{\"cookie\", \"session\"},\n\tTokenPrefix: prefix,",
			message: "the TokenPrefix (prefix) in front of the token in the cookie session",
		},
		{
			name:    "computed prefix on another header",
			config:  "TokenLookup: [2]string{\"header\", \"X-Token\"},\n\tTokenPrefix: prefix,",
			message: "the TokenPrefix (prefix) in front of the token in the X-Token header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			source := pasetoConfigSource("var _ = pasetoware.New(pasetoware.Config{\n\tSymmetricKey: key,\n\t" + tt.config + "\n})")
			file := writeTempFile(t, dir, source)

			out := contribMigrate(t, dir, v3.MigratePasetoExtractor)

			assert.Equal(t, source, readFile(t, file))
			assert.Empty(t, out)
			notes := internal.DrainNotes(dir)
			require.Len(t, notes, 1)
			assert.Contains(t, notes[0].Message, tt.message)
			assert.Contains(t, notes[0].Message, "Config.Extractor")
		})
	}
}

func Test_MigratePasetoExtractor_RemovesTheContextKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, pasetoConfigSource(`var _ = pasetoware.New(pasetoware.Config{
	SymmetricKey: key,
	ContextKey:   "paseto-user",
	TokenPrefix:  "Bearer",
})

var _ = pasetoware.New(pasetoware.Config{
	SymmetricKey: key,
	ContextKey:   "paseto-user",
})

var _ = pasetoware.New(pasetoware.Config{SymmetricKey: key, ContextKey: pasetoware.DefaultContextKey, Next: nil})`))

	contribMigrate(t, dir, v3.MigratePasetoExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.NotContains(t, content, "ContextKey")
	assert.NotContains(t, content, "TokenPrefix")
	assert.Contains(t, contribCompact(content), contribCompact(`Extractor:   extractors.FromAuthHeader("Bearer"),`))
	assert.Equal(t, 3, countOccurrences(content, `Extractor:`))
	assert.Contains(t, contribCompact(content), contribCompact(`pasetoware.Config{SymmetricKey: key, Extractor: extractors.FromAuthHeader(""), Next: nil}`))
}

func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

func Test_MigratePasetoExtractor_InlineConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, pasetoConfigSource(`var _ = pasetoware.New(pasetoware.Config{SymmetricKey: key, TokenLookup: [2]string{"query", "token"}})
var _ = pasetoware.New(pasetoware.Config{SymmetricKey: key})`))

	contribMigrate(t, dir, v3.MigratePasetoExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, contribCompact(content), contribCompact(`pasetoware.Config{SymmetricKey: key, Extractor: extractors.FromQuery("token")}`))
	assert.Contains(t, contribCompact(content), contribCompact(`pasetoware.Config{SymmetricKey: key, Extractor: extractors.FromAuthHeader("")}`))
}

func Test_MigratePasetoExtractor_AssignedFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, pasetoConfigSource(`func config() pasetoware.Config {
	cfg := pasetoware.Config{SymmetricKey: key}
	cfg.TokenLookup = [2]string{pasetoware.LookupHeader, "Authorization"}
	cfg.TokenPrefix = "Bearer"
	cfg.ContextKey = "paseto"
	return cfg
}

func bare() pasetoware.Config {
	var cfg = pasetoware.Config{SymmetricKey: key}
	return cfg
}`))

	out := contribMigrate(t, dir, v3.MigratePasetoExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, contribCompact(content), contribCompact(`cfg.Extractor = extractors.FromAuthHeader("Bearer")`))
	assert.NotContains(t, content, "cfg.TokenLookup")
	assert.NotContains(t, content, "cfg.TokenPrefix")
	assert.NotContains(t, content, "cfg.ContextKey")
	assert.Contains(t, contribCompact(content), contribCompact(`cfg = pasetoware.Config{SymmetricKey: key, Extractor: extractors.FromAuthHeader("")}`))
	assert.Contains(t, out, "Migrating paseto middleware configs")
}

func Test_MigratePasetoExtractor_ConditionalAssignmentIsReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := pasetoConfigSource(`func config(cookie bool) pasetoware.Config {
	cfg := pasetoware.Config{SymmetricKey: key}
	if cookie {
		cfg.TokenLookup = [2]string{"cookie", "session"}
	}
	return cfg
}`)
	file := writeTempFile(t, dir, source)

	contribMigrate(t, dir, v3.MigratePasetoExtractor)

	assert.Equal(t, source, readFile(t, file))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "set conditionally")
}

func Test_MigratePasetoExtractor_UnaliasedImportAfterTheContribMigration(t *testing.T) {
	contribVersions(t, map[string]string{"paseto": "v1.1.12"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/contrib/paseto"

var _ = pasetoware.New(pasetoware.Config{
	SymmetricKey: []byte("secret-key-of-32-bytes-length!!!"),
	TokenLookup:  [2]string{pasetoware.LookupQuery, "token"},
})
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigratePasetoExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "\tpasetoware \"github.com/gofiber/contrib/v3/paseto\"\n")
	assert.Contains(t, contribCompact(content), contribCompact(`Extractor:    extractors.FromQuery("token"),`))
}

func Test_MigratePasetoExtractor_CustomAlias(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"github.com/gofiber/contrib/paseto", "github.com/gofiber/contrib/paseto/v3", "github.com/gofiber/contrib/v3/paseto"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, `package main

import authpaseto "`+path+`"

var _ = authpaseto.New(authpaseto.Config{
	TokenLookup: [2]string{"cookie", "session"},
})
`)

			contribMigrate(t, dir, v3.MigratePasetoExtractor)

			content := readFile(t, file)
			assert.NotContains(t, content, "TokenLookup")
			assert.Contains(t, contribCompact(content), contribCompact(`Extractor: extractors.FromCookie("session"),`))
		})
	}
}

func Test_MigratePasetoExtractor_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, pasetoConfigSource(`var _ = pasetoware.New(pasetoware.Config{SymmetricKey: key})

func config() pasetoware.Config {
	cfg := pasetoware.Config{SymmetricKey: key, ContextKey: "x"}
	cfg.TokenPrefix = "Token"
	return cfg
}`))

	contribMigrate(t, dir, v3.MigratePasetoExtractor)
	migrated := readFile(t, file)
	internal.DrainNotes(dir)

	out := contribMigrate(t, dir, v3.MigratePasetoExtractor)

	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigratePasetoExtractor_LeavesV3ConfigsAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package main

import (
	"github.com/gofiber/fiber/v3/extractors"
	pasetoware "github.com/gofiber/contrib/v3/paseto"
)

var _ = pasetoware.New(pasetoware.Config{
	SymmetricKey: []byte("secret-key-of-32-bytes-length!!!"),
	Extractor:    extractors.FromCookie("session"),
})
`
	file := writeTempFile(t, dir, source)

	out := contribMigrate(t, dir, v3.MigratePasetoExtractor)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
}

func Test_MigratePasetoExtractor_SkipUnrelatedPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	original := `package main

import pasetoware "example.com/paseto"

var _ = pasetoware.Config{
	TokenLookup: [2]string{"header", "Authorization"},
}
`
	file := writeTempFile(t, dir, original)

	out := contribMigrate(t, dir, v3.MigratePasetoExtractor)

	assert.Equal(t, original, readFile(t, file))
	assert.Empty(t, out)
}
