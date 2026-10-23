package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The standalone gofiber/jwt module took the key as it was and the algorithm in a
// field of its own, HS256 unless said otherwise. contrib/v3/jwt takes a
// SigningKey that holds both. Only the import can tell the two configs apart, so
// the key is moved while the import is.
func Test_MigrateContribPackages_StandaloneJWTKey(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "a key and nothing else",
			config: `SigningKey: []byte("secret"),`,
			want:   `SigningKey: jwtware.SigningKey{Key: []byte("secret"), JWTAlg: "HS256"},`,
		},
		{
			name:   "an algorithm constant of the module",
			config: "SigningKey: publicKey,\n\t\tSigningMethod: jwtware.RS256,",
			want:   `SigningKey: jwtware.SigningKey{Key: publicKey, JWTAlg: "RS256"},`,
		},
		{
			name:   "an algorithm string",
			config: "SigningMethod: \"ES384\",\n\t\tSigningKey: publicKey,",
			want:   `SigningKey: jwtware.SigningKey{Key: publicKey, JWTAlg: "ES384"},`,
		},
		{
			name:   "a key that is one of v3 already",
			config: `SigningKey: jwtware.SigningKey{Key: publicKey, JWTAlg: "RS256"},`,
			want:   `SigningKey: jwtware.SigningKey{Key: publicKey, JWTAlg: "RS256"},`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contribVersions(t, map[string]string{"jwt": "v1.2.6"})
			dir := t.TempDir()
			file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/jwt/v3"
	"github.com/golang-jwt/jwt/v4"
)

var publicKey any

func main() {
	app := fiber.New()
	app.Use(jwtware.New(jwtware.Config{
		`+tt.config+`
		TokenLookup: "header:Authorization",
		AuthScheme:  "Bearer",
	}))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(c.Locals("user").(*jwt.Token).Raw)
	})
}
`)

			contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor, v3.MigrateJWTClaims)

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.Contains(t, content, "\tjwtware \"github.com/gofiber/contrib/v3/jwt\"\n")
			assert.Contains(t, contribCompact(content), contribCompact(tt.want))
			assert.NotContains(t, content, "SigningMethod")
			assert.Contains(t, contribCompact(content), `Extractor: extractors.FromAuthHeader("Bearer")`)
			assert.Contains(t, content, "\t\"github.com/golang-jwt/jwt/v5\"\n")
			assert.NotContains(t, content, "golang-jwt/jwt/v4")

			notes := internal.DrainNotes(dir)
			require.Len(t, notes, 1)
			assert.Contains(t, notes[0].Message, "github.com/golang-jwt/jwt/v4")
		})
	}
}

// The refresh of the key sets is configurable in the standalone module, and
// contrib/v3/jwt refreshes them in one way: the settings that go with them have no
// counterpart, and the config is left for the user.
func Test_MigrateContribPackages_StandaloneJWTKeySetsWithoutCounterpart(t *testing.T) {
	tests := []struct {
		name   string
		config string
		field  string
	}{
		{"refresh interval", "KeySetURLs: []string{\"https://example.com/jwks.json\"},\n\tKeyRefreshInterval: &interval,", "KeyRefreshInterval"},
		{"refresh on an unknown kid", "KeySetURL: \"https://example.com/jwks.json\",\n\tKeyRefreshUnknownKID: &yes,", "KeyRefreshUnknownKID"},
		{"given keys next to the key set", "KeySetURLs: []string{\"https://example.com/jwks.json\"},\n\tSigningKeys: map[string]interface{}{\"a\": []byte(\"secret\")},", "SigningKeys"},
		{"both fields", "KeySetURL: \"https://example.com/a.json\",\n\tKeySetURLs: []string{\"https://example.com/b.json\"},", "KeySetURL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contribVersions(t, map[string]string{"jwt": "v1.2.6"})
			dir := t.TempDir()
			file := writeTempFile(t, dir, `package main

import (
	"time"

	jwtware "github.com/gofiber/jwt/v3"
)

var (
	interval = time.Hour
	yes      = true
)

var _ = jwtware.New(jwtware.Config{
	`+tt.config+`
	SigningKey: []byte("secret"),
})
`)

			contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

			content := readFile(t, file)
			assert.Contains(t, content, `jwtware "github.com/gofiber/contrib/v3/jwt"`)
			assert.NotContains(t, content, "JWKSetURLs")
			assert.Contains(t, contribCompact(content), `SigningKey: []byte("secret")`, "left for the user, who is told")
			notes := internal.DrainNotes(dir)
			require.Len(t, notes, 1)
			assert.Contains(t, notes[0].Message, tt.field+" of the standalone gofiber/jwt module has no counterpart")
		})
	}
}

// KeySetURLs is the list of key sets under the name it has in contrib/v3/jwt, and
// KeySetURL is the same list with one URL: the module appended it to KeySetURLs.
func Test_MigrateContribPackages_StandaloneJWTKeySets(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
		field  string
	}{
		{
			name:   "a list of URLs",
			config: `KeySetURLs: []string{"https://example.com/a.json", "https://example.com/b.json"},`,
			want:   `JWKSetURLs: []string{"https://example.com/a.json", "https://example.com/b.json"},`,
			field:  "KeySetURLs",
		},
		{
			name:   "a list that is a variable",
			config: `KeySetURLs: urls,`,
			want:   `JWKSetURLs: urls,`,
			field:  "KeySetURLs",
		},
		{
			name:   "one URL",
			config: `KeySetURL: "https://example.com/jwks.json",`,
			want:   `JWKSetURLs: []string{"https://example.com/jwks.json"},`,
			field:  "KeySetURL",
		},
		{
			name:   "one URL that is a concatenation",
			config: `KeySetURL: base + "/jwks.json",`,
			want:   `JWKSetURLs: []string{base + "/jwks.json"},`,
			field:  "KeySetURL",
		},
		{
			name:   "an http URL",
			config: `KeySetURL: "http://127.0.0.1:8080/jwks.json",`,
			want:   `JWKSetURLs: []string{"http://127.0.0.1:8080/jwks.json"},`,
			field:  "KeySetURL",
		},
		{
			name:   "a list next to a key that stays the fallback",
			config: "KeySetURLs: urls,\n\tSigningKey: []byte(\"secret\"),\n\tSigningMethod: \"HS512\",",
			want:   "JWKSetURLs: urls,\n\tSigningKey: jwtware.SigningKey{Key: []byte(\"secret\"), JWTAlg: \"HS512\"},",
			field:  "KeySetURLs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contribVersions(t, map[string]string{"jwt": "v1.2.6"})
			dir := t.TempDir()
			file := writeTempFile(t, dir, `package main

import jwtware "github.com/gofiber/jwt/v3"

var (
	base = "https://example.com"
	urls = []string{"https://example.com/jwks.json"}
)

var _ = jwtware.New(jwtware.Config{
	`+tt.config+`
})
`)

			contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

			content := readFile(t, file)
			contribRequireParses(t, content)
			assert.Contains(t, content, `jwtware "github.com/gofiber/contrib/v3/jwt"`)
			assert.Contains(t, contribCompact(content), contribCompact(tt.want))
			assert.NotContains(t, content, "KeySetURL")
			notes := internal.DrainNotes(dir)
			require.Len(t, notes, 1)
			assert.Equal(t, 11, notes[0].Line)
			assert.Contains(t, notes[0].Message, tt.field+" is JWKSetURLs in contrib/v3/jwt")
			assert.Contains(t, notes[0].Message, "downloads the key sets when the middleware is created")

			// the file is a v3 file now: a second run has nothing to do
			out := contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)
			assert.Equal(t, content, readFile(t, file))
			assert.Empty(t, out)
			assert.Empty(t, internal.DrainNotes(dir))
		})
	}
}

// A URL that the module took and v3 does not stays with the user: an empty
// KeySetURL meant "no key set" and the key given next to it was used, where
// JWKSetURLs refuses an empty URL at startup, and v3 only takes absolute http and
// https URLs.
func Test_MigrateContribPackages_StandaloneJWTKeySetsThatStay(t *testing.T) {
	tests := []struct {
		name string
		// config is what the literal in the config of the program holds, below what follows the declaration.
		config, below string
		field         string
	}{
		{name: "a URL that may be empty next to a key", config: "KeySetURL: os.Getenv(\"JWKS_URL\"),\n\tSigningKey: []byte(\"secret\"),", field: "KeySetURL"},
		{name: "a URL that may be empty next to a function", config: "KeySetURL: os.Getenv(\"JWKS_URL\"),\n\tKeyFunc: nil,", field: "KeySetURL"},
		{name: "a relative URL", config: `KeySetURL: "/jwks.json",`, field: "KeySetURL"},
		{name: "another scheme", config: `KeySetURL: "file:///etc/jwks.json",`, field: "KeySetURL"},
		{name: "an empty URL", config: `KeySetURL: "",`, field: "KeySetURL"},
		{name: "a relative URL in the list", config: `KeySetURLs: []string{"https://example.com/a.json", "b.json"},`, field: "KeySetURLs"},
		{
			name:   "set in a branch",
			config: `SigningKey: []byte("secret"),`,
			below: "\nfunc init() {\n\tcfg := jwtware.Config{SigningKey: []byte(\"secret\")}\n\tif len(urls) > 0 {\n\t\tcfg.KeySetURLs = urls\n\t}\n" +
				"\t_ = jwtware.New(cfg)\n}\n",
			field: "KeySetURLs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contribVersions(t, map[string]string{"jwt": "v1.2.6"})
			dir := t.TempDir()
			file := writeTempFile(t, dir, `package main

import (
	"os"

	jwtware "github.com/gofiber/jwt/v3"
)

var urls = []string{"https://example.com/jwks.json"}

var _ = os.Getenv

var _ = jwtware.New(jwtware.Config{
	`+tt.config+`
})
`+tt.below)

			contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

			content := readFile(t, file)
			assert.NotContains(t, content, "JWKSetURLs")
			notes := internal.DrainNotes(dir)
			require.Len(t, notes, 1)
			assert.Contains(t, notes[0].Message, tt.field+" of the standalone gofiber/jwt module has no counterpart")
		})
	}
}

// The message of the standalone module started with a capital letter.
func Test_MigrateContribPackages_StandaloneJWTErrors(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/jwt/v3"
)

func jwtError(c *fiber.Ctx, err error) error {
	if err.Error() == "Missing or malformed JWT" {
		return c.Status(fiber.StatusBadRequest).SendString("Missing or malformed JWT")
	}
	return c.Status(fiber.StatusUnauthorized).SendString("Invalid or expired JWT")
}

var _ = jwtware.New
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	contribRequireParses(t, content)
	assert.Contains(t, content, "if errors.Is(err, extractors.ErrNotFound) {")
	assert.Contains(t, content, `SendString("Missing or malformed JWT")`)
}

func Test_MigrateContribPackages_StandaloneJWTIdempotent(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	jwtware "github.com/gofiber/jwt/v3"
	"github.com/golang-jwt/jwt/v4"
)

var _ = jwtware.New(jwtware.Config{SigningKey: []byte("secret")})
var _ jwt.Claims
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)
	migrated := readFile(t, file)
	internal.DrainNotes(dir)

	out := contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

// MigrateContribPackages moves the imports of the middleware and nothing else: the
// golang-jwt import of a file moves with MigrateJWTClaims.
func Test_MigrateContribPackages_GolangJWTVersionOfOtherFiles(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	source := `package main

import (
	jwtware "github.com/gofiber/contrib/v3/jwt"
	"github.com/golang-jwt/jwt/v4"
)

var _ = jwtware.New
var _ jwt.Claims
`
	file := writeTempFile(t, dir, source)

	out := contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
}

// The contrib module of jwt is not the standalone one: its SigningKey is the
// struct already, and nothing is wrapped or renamed.
func Test_MigrateContribPackages_ContribJWTKeyIsLeftAlone(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	jwtware "github.com/gofiber/contrib/jwt"
	"github.com/golang-jwt/jwt/v5"
)

var _ = jwtware.New(jwtware.Config{SigningKey: jwtware.SigningKey{Key: []byte("secret")}})
var _ jwt.Claims
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	assert.Contains(t, content, `jwtware.SigningKey{Key: []byte("secret")}`)
	assert.NotContains(t, content, "JWTAlg")
	assert.Contains(t, content, `"github.com/golang-jwt/jwt/v5"`)
}
