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

	restore := stubFiberDownload(t, fiberGoMod)
	defer restore()

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	target := semver.MustParse("3.0.0")
	curr := map[string]map[string]*semver.Version{
		dir: {
			"github.com/valyala/fasthttp":   semver.MustParse("1.0.0"),
			"github.com/andybalholm/brotli": semver.MustParse("1.2.0"),
		},
	}
	require.NoError(t, migrations.MigrateDependencies(cmd, dir, curr, target))

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

	restore := stubFiberDownload(t, fiberGoMod)
	defer restore()

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	target := semver.MustParse("3.0.0")
	curr := map[string]map[string]*semver.Version{
		dir: {
			"github.com/valyala/fasthttp":   semver.MustParse("1.10.0"),
			"github.com/andybalholm/brotli": semver.MustParse("1.2.0"),
		},
	}
	require.NoError(t, migrations.MigrateDependencies(cmd, dir, curr, target))

	content := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, content, "github.com/valyala/fasthttp v1.10.0")
	assert.Contains(t, content, "github.com/andybalholm/brotli v1.2.0")
	assert.Empty(t, buf.String())
}

func Test_MigrateDependencies_RaisesTheGoVersion(t *testing.T) {
	// Not parallel: the stub replaces a package-level variable.
	fiberMod := `module github.com/gofiber/fiber/v3

go 1.27.0

require github.com/valyala/fasthttp v1.10.0
`
	tests := []struct {
		name       string
		projectGo  string
		wantGo     string
		wantOutput string
	}{
		{
			name:       "older than the release",
			projectGo:  "1.26.0",
			wantGo:     "go 1.27.0",
			wantOutput: "Ensuring go version >= 1.27.0\n",
		},
		{
			name:      "newer than the release",
			projectGo: "1.28.2",
			wantGo:    "go 1.28.2",
		},
		{
			name:      "already the release's",
			projectGo: "1.27.0",
			wantGo:    "go 1.27.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := importsWriteTree(t, map[string]string{
				"go.mod":    "module example\n\ngo " + tc.projectGo + "\n\nrequire (\n\tgithub.com/gofiber/fiber/v3 v3.7.0\n\tgithub.com/valyala/fasthttp v1.10.0\n)\n",
				"fiber.mod": fiberMod,
			})
			restore := stubFiberDownload(t, filepath.Join(dir, "fiber.mod"))
			defer restore()

			var buf bytes.Buffer
			curr := map[string]map[string]*semver.Version{dir: {"github.com/valyala/fasthttp": semver.MustParse("1.10.0")}}
			require.NoError(t, migrations.MigrateDependencies(newCmd(&buf), dir, curr, semver.MustParse("3.7.0")))

			assert.Contains(t, readFile(t, filepath.Join(dir, "go.mod")), tc.wantGo)
			assert.Equal(t, tc.wantOutput, buf.String())
		})
	}
}
