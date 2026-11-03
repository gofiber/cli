package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateMount(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mmtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New()
    api := fiber.New()
    app.Mount("/api", api)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateMount(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, ".Use(\"/api\", api)")
	assert.Contains(t, buf.String(), "Migrating Mount usages")
}
