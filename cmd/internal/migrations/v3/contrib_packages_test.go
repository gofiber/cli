package v3_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateContribPackages(t *testing.T) {
	dir := t.TempDir()

	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		if module != "session" {
			return "", fmt.Errorf("unexpected module %s", module)
		}
		return "v3.1.0", nil
	})
	t.Cleanup(restore)

	file := writeTempFile(t, dir, `package main
import (
    session "github.com/gofiber/contrib/session"
)

func main() {
    _ = session.NewStore
}`)

	modContent := `module example

go 1.22

require (
    github.com/gofiber/fiber/v2 v2.0.0
    github.com/gofiber/contrib/session v1.2.3
)`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContribPackages(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/contrib/v3/session")
	assert.NotContains(t, content, "github.com/gofiber/contrib/session")

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/contrib/v3/session v3.1.0")
	assert.NotContains(t, mod, "github.com/gofiber/contrib/session v1.2.3")

	assert.Contains(t, buf.String(), "Migrating contrib packages")
}

func Test_MigrateContribPackages_Replace(t *testing.T) {
	dir := t.TempDir()

	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		if module != "websocket" {
			return "", fmt.Errorf("unexpected module %s", module)
		}
		return "v3.0.0", nil
	})
	t.Cleanup(restore)

	file := writeTempFile(t, dir, `package main
import (
    websocket "github.com/gofiber/contrib/websocket"
)

var _ = websocket.New`) // keep import

	modContent := `module example

go 1.22

require github.com/gofiber/contrib/websocket v1.0.0

replace github.com/gofiber/contrib/websocket => ../local`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContribPackages(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/contrib/v3/websocket")

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/contrib/v3/websocket v3.0.0")
	assert.Contains(t, mod, "replace github.com/gofiber/contrib/v3/websocket => ../local")

	assert.Contains(t, buf.String(), "Migrating contrib packages")
}

func Test_MigrateContribPackages_Rename(t *testing.T) {
	dir := t.TempDir()

	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		if module != "otel" {
			return "", fmt.Errorf("unexpected module %s", module)
		}
		return "v1.2.0", nil
	})
	t.Cleanup(restore)

	file := writeTempFile(t, dir, `package main
import (
    otel "github.com/gofiber/contrib/otelfiber/v2"
)

var _ = otel.Middleware`)

	modContent := `module example

go 1.22

require github.com/gofiber/contrib/otelfiber/v2 v2.0.0

replace github.com/gofiber/contrib/otelfiber/v2 => ../local`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContribPackages(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/contrib/v3/otel")
	assert.NotContains(t, content, "github.com/gofiber/contrib/otelfiber/v2")

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/contrib/v3/otel v1.2.0")
	assert.Contains(t, mod, "replace github.com/gofiber/contrib/v3/otel => ../local")
	assert.NotContains(t, mod, "github.com/gofiber/contrib/otelfiber/v2")

	assert.Contains(t, buf.String(), "Migrating contrib packages")
}

func Test_MigrateContribPackages_Idempotent(t *testing.T) {
	dir := t.TempDir()

	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		return "v3.3.3", nil
	})
	t.Cleanup(restore)

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/contrib/v3/session"
)

func main() {
    _ = session.NewStore
}`)

	modContent := `module example

go 1.22

require github.com/gofiber/contrib/v3/session v1.2.3`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContribPackages(cmd, dir, nil, nil))
	first := readFile(t, file)
	firstMod := readFile(t, filepath.Join(dir, "go.mod"))

	require.NoError(t, v3.MigrateContribPackages(cmd, dir, nil, nil))
	second := readFile(t, file)
	secondMod := readFile(t, filepath.Join(dir, "go.mod"))

	assert.Equal(t, first, second)
	assert.Equal(t, firstMod, secondMod)
	assert.Empty(t, buf.String())
}
