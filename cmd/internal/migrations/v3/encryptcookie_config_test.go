package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateEncryptcookieConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mencryptcookie")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/encryptcookie"
)
var _ = encryptcookie.New(encryptcookie.Config{
    Encryptor: func(value, key string) (string, error) { return "", nil },
    Decryptor: func(value string, key string) (string, error) { return "", nil },
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateEncryptcookieConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "Encryptor: func(_ string, value, key string) (string, error)")
	assert.Contains(t, content, "Decryptor: func(_ string, value string, key string) (string, error)")
	assert.Contains(t, buf.String(), "Migrating encryptcookie middleware configs")
}

func Test_MigrateEncryptcookieConfig_MultiLine(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mencryptcookieml")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/encryptcookie"
)
var _ = encryptcookie.New(encryptcookie.Config{
    Encryptor: func(
        value string,
        key string,
    ) (string, error) { return "", nil },
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateEncryptcookieConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "\n\tEncryptor: func(\n\t\t_ string,\n\t\tvalue string,\n\t\tkey string,")
	assert.Contains(t, buf.String(), "Migrating encryptcookie middleware configs")
}

func Test_MigrateEncryptcookieConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mencryptcookieidem")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/encryptcookie"
)
var _ = encryptcookie.New(encryptcookie.Config{
    Encryptor: func(value, key string) (string, error) { return "", nil },
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateEncryptcookieConfig(cmd, dir, nil, nil))
	first := readFile(t, file)
	require.NoError(t, v3.MigrateEncryptcookieConfig(cmd, dir, nil, nil))
	second := readFile(t, file)
	assert.Equal(t, first, second)
}
