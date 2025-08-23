package migrations_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations"
)

func Test_MigrateDependencies(t *testing.T) {
	dir, err := os.MkdirTemp("", "mdeps")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	mod := `module example

go 1.22

require (
    github.com/gofiber/fiber/v3 v3.0.0
    github.com/valyala/fasthttp v1.0.0
    github.com/andybalholm/brotli v1.2.0
)`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600))

	fiberMod := `module github.com/gofiber/fiber/v3

go 1.22

require (
    github.com/valyala/fasthttp v1.10.0
    github.com/andybalholm/brotli v1.0.0
)`
	fiberGoMod := filepath.Join(dir, "fiber.mod")
	require.NoError(t, os.WriteFile(fiberGoMod, []byte(fiberMod), 0o600))

	origExec := migrations.ExecCommand
	migrations.ExecCommand = func(string, ...string) *exec.Cmd {
		return exec.Command("echo", fmt.Sprintf(`{"GoMod":%q}`, fiberGoMod)) // #nosec G204 -- testing stub
	}
	defer func() { migrations.ExecCommand = origExec }()

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	target := semver.MustParse("3.0.0")
	require.NoError(t, migrations.MigrateDependencies(cmd, dir, nil, target))

	content := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, content, "github.com/valyala/fasthttp v1.10.0")
	assert.Contains(t, content, "github.com/andybalholm/brotli v1.2.0")
	assert.Contains(t, buf.String(), "Updating dependency versions")
}

func Test_MigrateDependencies_NoChange(t *testing.T) {
	dir, err := os.MkdirTemp("", "mdeps_nc")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	mod := `module example

go 1.22

require (
    github.com/gofiber/fiber/v3 v3.0.0
    github.com/valyala/fasthttp v1.10.0
    github.com/andybalholm/brotli v1.2.0
)`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600))

	fiberMod := `module github.com/gofiber/fiber/v3

go 1.22

require (
    github.com/valyala/fasthttp v1.10.0
    github.com/andybalholm/brotli v1.0.0
)`
	fiberGoMod := filepath.Join(dir, "fiber.mod")
	require.NoError(t, os.WriteFile(fiberGoMod, []byte(fiberMod), 0o600))

	origExec := migrations.ExecCommand
	migrations.ExecCommand = func(string, ...string) *exec.Cmd {
		return exec.Command("echo", fmt.Sprintf(`{"GoMod":%q}`, fiberGoMod)) // #nosec G204 -- testing stub
	}
	defer func() { migrations.ExecCommand = origExec }()

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	target := semver.MustParse("3.0.0")
	require.NoError(t, migrations.MigrateDependencies(cmd, dir, nil, target))

	content := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, content, "github.com/valyala/fasthttp v1.10.0")
	assert.Contains(t, content, "github.com/andybalholm/brotli v1.2.0")
	assert.Empty(t, buf.String())
}
