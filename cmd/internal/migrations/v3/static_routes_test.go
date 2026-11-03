package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateStaticRoutes(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msrtest")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"
func main() {
    app := fiber.New()
    app.Static("/", "./public")
    app.Static("/prefix", "./public", Static{Index: "index.htm"})
    app.Static("/fiber", "./public", fiber.Static{Compress: true})
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateStaticRoutes(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "\"github.com/gofiber/fiber/v3/middleware/static\"")
	assert.Contains(t, content, `.Get("/*", static.New("./public"))`)
	assert.Contains(t, content, `static.New("./public", static.Config{IndexNames: []string{"index.htm"}})`)
	assert.Contains(t, content, `static.New("./public", static.Config{Compress: true})`)
	assert.NotContains(t, content, "fiber.static.Config")
	assert.Contains(t, buf.String(), "Migrating app.Static usage")
}
