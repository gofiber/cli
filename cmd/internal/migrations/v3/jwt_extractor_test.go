package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateJWTExtractor(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v3"
    jwtware "github.com/gofiber/contrib/jwt"
)
var _ = jwtware.New(jwtware.Config{
    TokenLookup: "header:Authorization, cookie:jwt",
    AuthScheme: "Token",
    Filter: func(c fiber.Ctx) bool { return false },
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.NotContains(t, content, "AuthScheme")
	assert.Regexp(t, `Extractor:\s*extractors.Chain\(extractors.FromAuthHeader\("Token"\),\s*extractors.FromCookie\("jwt"\)\)`, content)
	assert.Contains(t, content, "Next:")
	assert.Contains(t, content, "func(c fiber.Ctx) bool { return false },")
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_TokenLookupExpr(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_expr")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    jwtware "github.com/gofiber/contrib/jwt"
    "strings"
)
var _ = jwtware.New(jwtware.Config{
    TokenLookup: strings.Join([]string{"header:Authorization"}, ","),
    AuthScheme: "Bearer",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "// TODO: migrate TokenLookup: strings.Join([]string{\"header:Authorization\"}, \",\")")
	assert.NotContains(t, content, "AuthScheme")
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_CustomAlias(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_alias")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import authjwt "github.com/gofiber/contrib/jwt/v3"
var _ = authjwt.New(authjwt.Config{
    TokenLookup: "cookie:session",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Regexp(t, `Extractor:\s*extractors.FromCookie\("session"\)`, content)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_ContribV3Import(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_v3import")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import jwtware "github.com/gofiber/contrib/v3/jwt"
var _ = jwtware.New(jwtware.Config{
    TokenLookup: "cookie:jwt",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Regexp(t, `Extractor:\s*extractors.FromCookie\("jwt"\)`, content)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_InlineConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_inline")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import jwtware "github.com/gofiber/contrib/jwt"

var _ = jwtware.New(jwtware.Config{TokenLookup: "cookie:jwt"})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Regexp(t, `Extractor:\s*extractors.FromCookie\("jwt"\)`, content)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_ImportWithComment(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_comment")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    jwtware "github.com/gofiber/contrib/jwt" // jwt middleware
    "github.com/gofiber/fiber/v2"
)

func JWTMiddleware() fiber.Handler {
    return jwtware.New(jwtware.Config{
        TokenLookup: "cookie:jwt",
    })
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Regexp(t, `Extractor:\s*extractors.FromCookie\("jwt"\)`, content)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_FiberV2Middleware(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_fiber_v2")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package auth

import (
    "os"
    "strconv"

    jwtware "github.com/gofiber/contrib/jwt"
    "github.com/gofiber/fiber/v2"
    "github.com/golang-jwt/jwt/v5"
)

// JWT error message.
func jwtError(c *fiber.Ctx, err error) error {
    if err.Error() == "Missing or malformed JWT" {
        return c.Status(fiber.StatusBadRequest).JSON(&fiber.Map{
            "status":  "error",
            "message": "Missing or malformed JWT!",
        })
    }

    return c.Status(fiber.StatusUnauthorized).JSON(&fiber.Map{
        "status":  "error",
        "message": "Invalid or expired JWT!",
    })
}

// Guards a specific endpoint in the API.
func JWTMiddleware() fiber.Handler {
    return jwtware.New(jwtware.Config{
        ErrorHandler: jwtError,
        SigningKey:   jwtware.SigningKey{Key: []byte(os.Getenv("JWT_SECRET"))},
        TokenLookup:  "cookie:jwt",
    })
}

// Gets user data (their ID) from the JWT middleware. Should be executed after calling 'JWTMiddleware()'.
func GetDataFromJWT(c *fiber.Ctx) error {
    jwtData := c.Locals("user").(*jwt.Token)
    claims := jwtData.Claims.(jwt.MapClaims)
    parsedUserID := claims["uid"].(string)
    userID, err := strconv.Atoi(parsedUserID)
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(&fiber.Map{
            "status":  "fail",
            "message": err.Error(),
        })
    }

    c.Locals("currentUser", userID)
    return c.Next()
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Regexp(t, `Extractor:\s*extractors.FromCookie\("jwt"\)`, content)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_LegacyImportPath(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_legacy")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    jwtware "github.com/gofiber/jwt/v2"
    "github.com/gofiber/fiber/v2"
)

func JWTMiddleware() fiber.Handler {
    return jwtware.New(jwtware.Config{
        ErrorHandler: jwtError,
        SigningKey:   jwtware.SigningKey{Key: []byte(os.Getenv("JWT_SECRET"))},
        TokenLookup:  "cookie:jwt",
    })
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Regexp(t, `Extractor:\s*extractors.FromCookie\("jwt"\)`, content)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_PointerConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_pointer")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    jwtware "github.com/gofiber/contrib/jwt"
    "github.com/gofiber/fiber/v2"
)

func JWTMiddleware() fiber.Handler {
    return jwtware.New(&jwtware.Config{
        TokenLookup: "cookie:jwt",
    })
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Regexp(t, `Extractor:\s*extractors.FromCookie\("jwt"\)`, content)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating jwt middleware configs")
}

func Test_MigrateJWTExtractor_SkipUnrelatedPackage(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mjwt_skip")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	original := `package main
import jwtware "example.com/jwtware"
var _ = jwtware.Config{
    TokenLookup: "header:Authorization",
}`
	file := writeTempFile(t, dir, original)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateJWTExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Equal(t, original, content)
	assert.Empty(t, buf.String())
}
