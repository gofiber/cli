package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// Files with Windows line endings give the result of the same file with Unix
// ones: a field or a statement that is removed takes its whole line, not just
// the text of it.
func Test_Migrations_CRLF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		migrate mw1Migration
		name    string
		src     string
	}{
		{v3.MigrateCORSConfig, "cors", `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func handler() fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins: "",
		AllowMethods: "GET, POST", // the methods
		AllowHeaders: "",
	})
}
`},
		{v3.MigrateEnvVarConfig, "envvar", `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/envvar"
)

func handler() fiber.Handler {
	return envvar.New(envvar.Config{
		ExportVars: map[string]string{"A": ""},
		ExcludeVars: map[string]string{"B": ""},
	})
}
`},
		{v3.MigrateBasicauthConfig, "basicauth", `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
)

func handler() fiber.Handler {
	cfg := basicauth.Config{
		ContextUsername: "user",
		Realm:           "Restricted",
	}
	cfg.ContextUsername = "other"
	return basicauth.New(cfg)
}
`},
		{v3.MigrateCSRFConfig, "csrf", `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
)

func handler() fiber.Handler {
	return csrf.New(csrf.Config{
		SessionKey: "csrf", // the key of the session
		CookieName: "csrf_",
		HandlerContextKey: "handler",
	})
}
`},
		{v3.MigrateEncryptcookieConfig, "encryptcookie", `package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/encryptcookie"
)

func handler() fiber.Handler {
	return encryptcookie.New(encryptcookie.Config{
		Key: encryptcookie.GenerateKey(),
		Encryptor: func(decryptedString, key string) (string, error) {
			return decryptedString, nil
		},
	})
}
`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			unix := mw1Run(t, tt.migrate, tt.src)
			windows := mw1Run(t, tt.migrate, strings.ReplaceAll(tt.src, "\n", "\r\n"))

			assert.NotEqual(t, tt.src, unix.file(), "the migration changes the file")
			assert.Equal(t, unix.file(), strings.ReplaceAll(windows.file(), "\r", ""))
			assert.Equal(t, unix.notes, windows.notes)
		})
	}
}
