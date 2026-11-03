package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSessionStore(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionstore")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/session"
)

var (
    store        = session.New()
    sessionStore = session.New(session.Config{})
)

func main() {
    app := fiber.New()
    custom := session.New()
    anotherStore := session.New(session.Config{})
    if temp := session.New(); temp != nil {
        _ = temp
    }
    _ = store
    _ = sessionStore
    _ = custom
    _ = anotherStore
    app.Use(session.New())
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionStore(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Regexp(t, `store\s*=\s*session.NewStore\(\)`, content)
	assert.Regexp(t, `sessionStore\s*=\s*session.NewStore\(session.Config\{\}\)`, content)
	assert.Contains(t, content, "custom := session.NewStore()")
	assert.Contains(t, content, "anotherStore := session.NewStore(session.Config{})")
	assert.Contains(t, content, "temp := session.NewStore()")
	assert.Contains(t, content, "app.Use(session.New())")
	assert.Contains(t, buf.String(), "Migrating session store")
}
