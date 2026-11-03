package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateListenerCallbacks(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mlistener")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2"
    "log"
)
func main() {
    app := fiber.New()
    app.Listen(":3000", fiber.ListenerConfig{
        OnShutdownError: func(err error) {
            log.Print(err)
        },
        OnShutdownSuccess: func() {
            log.Print("ok")
        },
    })
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateListenerCallbacks(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "OnShutdownError")
	assert.NotContains(t, content, "OnShutdownSuccess")
	assert.Contains(t, buf.String(), "Migrating listener callbacks")
}
