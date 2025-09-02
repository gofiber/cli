package migrations_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations"
)

func Test_MigrateGoPkgs(t *testing.T) {
	dir, err := os.MkdirTemp("", "mgpkgs")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	mainContent := `package main
import (
    fiber "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/middleware/adaptor"
)
func main() {
    _, _ = fiber.New(), adaptor.New()
}`
	file := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(file, []byte(mainContent), 0o600))

	modContent := `module example

go 1.22

require github.com/gofiber/fiber/v2 v2.0.0`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	target := semver.MustParse("3.0.0")
	require.NoError(t, migrations.MigrateGoPkgs(cmd, dir, nil, target))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/fiber/v3")
	assert.NotContains(t, content, "github.com/gofiber/fiber/v2")

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/fiber/v3 v3.0.0")
	assert.Contains(t, buf.String(), "Migrating Go packages")
}

func Test_MigrateGoPkgs_WithHash(t *testing.T) {
	dir, err := os.MkdirTemp("", "mgpkgs_hash")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package main"), 0o600))

	modContent := `module example

go 1.22

require github.com/gofiber/fiber/v2 v2.0.0`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	target := semver.MustParse("3.0.1-0.20200102030405-abcdef123456")
	require.NoError(t, migrations.MigrateGoPkgs(cmd, dir, nil, target))

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/fiber/v3 v3.0.1-0.20200102030405-abcdef123456")
	assert.Contains(t, buf.String(), "Migrating Go packages")
}
