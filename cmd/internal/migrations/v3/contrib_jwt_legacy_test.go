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

			contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

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
			assert.Contains(t, notes[0].Message, "golang-jwt/jwt v4")
		})
	}
}

func Test_MigrateContribPackages_StandaloneJWTKeySets(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import jwtware "github.com/gofiber/jwt/v3"

var _ = jwtware.New(jwtware.Config{
	KeySetURLs: []string{"https://example.com/jwks.json"},
	SigningKey: []byte("secret"),
})
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor)

	content := readFile(t, file)
	assert.Contains(t, content, `jwtware "github.com/gofiber/contrib/v3/jwt"`)
	assert.Contains(t, content, `SigningKey: []byte("secret")`, "left for the user, who is told")
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, 6, notes[0].Line)
	assert.Contains(t, notes[0].Message, "KeySetURLs of the standalone gofiber/jwt module has no counterpart")
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

// A file that never imports the standalone module keeps its golang-jwt import.
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
