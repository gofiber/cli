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

// opafiber is called opa in v3. Without the rename the version of "opafiber/v2" was looked up
// and the lookup failed, which stopped the rewrite of every contrib module of the go.mod.
func Test_MigrateContribPackages_Opafiber(t *testing.T) {
	dir := t.TempDir()

	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		versions := map[string]string{"opa": "v3.0.0", "newrelic": "v3.2.0"}
		if v, ok := versions[module]; ok {
			return v, nil
		}
		return "", fmt.Errorf("unexpected module %s", module)
	})
	t.Cleanup(restore)

	file := writeTempFile(t, dir, `package main
import (
    fibernewrelic "github.com/gofiber/contrib/fibernewrelic"
    opafiber "github.com/gofiber/contrib/opafiber/v2"
)

func main() {
    _, _ = fibernewrelic.New, opafiber.New
}`)

	modContent := `module example

go 1.22

require (
    github.com/gofiber/fiber/v2 v2.0.0
    github.com/gofiber/contrib/opafiber/v2 v2.0.6
    github.com/gofiber/contrib/fibernewrelic v1.0.0
)`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContribPackages(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `opafiber "github.com/gofiber/contrib/v3/opa"`)
	assert.Contains(t, content, `fibernewrelic "github.com/gofiber/contrib/v3/newrelic"`)
	assert.NotContains(t, content, "opafiber/v2")

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/contrib/v3/opa v3.0.0")
	assert.Contains(t, mod, "github.com/gofiber/contrib/v3/newrelic v3.2.0")
	assert.NotContains(t, mod, "opafiber")
}

// The standalone jwt module of the v2 era (github.com/gofiber/jwt/v3, where the v3 is the major
// version of that module) was folded into contrib. Its import stayed, so the config the jwt
// migration wrote for v3 did not belong to the package.
func Test_MigrateContribPackages_LegacyJWT(t *testing.T) {
	dir := t.TempDir()

	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		if module != "jwt" {
			return "", fmt.Errorf("unexpected module %s", module)
		}
		return "v3.0.0", nil
	})
	t.Cleanup(restore)

	named := writeTempFile(t, dir, `package main

import (
	"github.com/gofiber/fiber/v2"
	jwt "github.com/gofiber/jwt/v3"
)

var _ = jwt.New(jwt.Config{})
var _ = fiber.New
`)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	unnamed := writeTempFile(t, sub, `package sub

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/jwt/v2"
)

var _ = jwtware.New(jwtware.Config{})
var _ = fiber.New
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(`module example

go 1.22

require (
    github.com/gofiber/fiber/v2 v2.0.0
    github.com/gofiber/jwt/v3 v3.3.10
)

replace github.com/gofiber/jwt/v3 v3.3.10 => ../jwt

replace (
    github.com/gofiber/jwt/v3 => ../jwt
    github.com/gofiber/jwt/v3 v3.3.9 => ../jwt
)

exclude (
    github.com/gofiber/jwt/v3 v3.3.8
)
`), 0o600))

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	assert.Contains(t, readFile(t, named), `jwt "github.com/gofiber/contrib/v3/jwt"`, "the name the file chose stays")
	assert.Contains(t, readFile(t, unnamed), `jwtware "github.com/gofiber/contrib/v3/jwt"`, "an import without a name gets the one of the package")
	assert.NotContains(t, readFile(t, named)+readFile(t, unnamed), "gofiber/jwt")

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/contrib/v3/jwt v")
	assert.NotContains(t, mod, "require github.com/gofiber/jwt")
	assert.NotContains(t, mod, "gofiber/jwt/v3 v3.3.10\n)", "the requirement moved")
	// a replace of the old module points at a copy of the old module, which cannot stand in for the
	// new one: it stays as it was, whatever its shape, and so does every other directive but the
	// requirement
	assert.Contains(t, mod, "replace github.com/gofiber/jwt/v3 v3.3.10 => ../jwt")
	assert.Contains(t, mod, "replace (\n    github.com/gofiber/jwt/v3 => ../jwt\n    github.com/gofiber/jwt/v3 v3.3.9 => ../jwt\n)")
	assert.Contains(t, mod, "exclude (\n    github.com/gofiber/jwt/v3 v3.3.8\n)")
	assert.NotContains(t, mod, "replace github.com/gofiber/contrib")
	assert.Contains(t, buf.String(), "Migrating contrib packages")
}

// The requirement moves where it stands, with the comment it has, in a block and on a line of its own.
func Test_MigrateContribPackages_LegacyJWTRequirementShapes(t *testing.T) {
	tests := []struct {
		name string
		mod  string
		want string
	}{
		{
			name: "line of its own",
			mod:  "require github.com/gofiber/jwt/v3 v3.3.10 // indirect\n",
			want: "require github.com/gofiber/contrib/v3/jwt v3.0.0 // indirect\n",
		},
		{
			name: "block",
			mod:  "require (\n\tgithub.com/gofiber/fiber/v2 v2.0.0\n\tgithub.com/gofiber/jwt/v2 v2.1.0 // indirect\n)\n",
			want: "require (\n\tgithub.com/gofiber/fiber/v2 v2.0.0\n\tgithub.com/gofiber/contrib/v3/jwt v3.0.0 // indirect\n)\n",
		},
		{
			name: "windows line endings",
			mod:  "require (\r\n\tgithub.com/gofiber/jwt/v3 v3.3.10\r\n)\r\n",
			want: "require (\r\n\tgithub.com/gofiber/contrib/v3/jwt v3.0.0\r\n)\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			restore := v3.SetContribV3VersionFetcher(func(string) (string, error) { return "v3.0.0", nil })
			t.Cleanup(restore)

			goMod := filepath.Join(dir, "go.mod")
			require.NoError(t, os.WriteFile(goMod, []byte("module example\n\ngo 1.22\n\n"+tt.mod), 0o600))

			var buf bytes.Buffer
			require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

			assert.Equal(t, "module example\n\ngo 1.22\n\n"+tt.want, readFile(t, goMod))
		})
	}
}

func Test_MigrateContribPackages_LegacyJWTIdempotent(t *testing.T) {
	dir := t.TempDir()

	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		if module != "jwt" {
			return "", fmt.Errorf("unexpected module %s", module)
		}
		return "v3.0.0", nil
	})
	t.Cleanup(restore)

	file := writeTempFile(t, dir, `package main

import jwtware "github.com/gofiber/jwt/v3"

var _ = jwtware.New
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.22\n\nrequire github.com/gofiber/jwt/v3 v3.3.10\n"), 0o600))

	var first bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&first), dir, nil, nil))
	migrated := readFile(t, file)
	mod := readFile(t, filepath.Join(dir, "go.mod"))

	var second bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&second), dir, nil, nil))
	assert.Equal(t, migrated, readFile(t, file))
	assert.Equal(t, mod, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Empty(t, second.String())
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
	tests := []struct {
		name         string
		oldPath      string
		newModule    string
		newVersion   string
		importAlias  string
		importSymbol string
		oldVersion   string
	}{
		{
			name:         "otel",
			oldPath:      "github.com/gofiber/contrib/otelfiber/v2",
			newModule:    "otel",
			newVersion:   "v1.2.0",
			importAlias:  "otel",
			importSymbol: "Middleware",
			oldVersion:   "v2.0.0",
		},
		{
			name:         "newrelic",
			oldPath:      "github.com/gofiber/contrib/fibernewrelic",
			newModule:    "newrelic",
			newVersion:   "v1.0.0",
			importAlias:  "newrelic",
			importSymbol: "New",
			oldVersion:   "v1.3.1",
		},
		{
			name:         "i18n",
			oldPath:      "github.com/gofiber/contrib/fiberi18n",
			newModule:    "i18n",
			newVersion:   "v1.0.0",
			importAlias:  "i18n",
			importSymbol: "New",
			oldVersion:   "v1.0.0",
		},
		{
			name:         "sentry",
			oldPath:      "github.com/gofiber/contrib/fibersentry",
			newModule:    "sentry",
			newVersion:   "v1.0.8",
			importAlias:  "sentry",
			importSymbol: "New",
			oldVersion:   "v1.0.8",
		},
		{
			name:         "zap",
			oldPath:      "github.com/gofiber/contrib/fiberzap",
			newModule:    "zap",
			newVersion:   "v1.0.0",
			importAlias:  "zap",
			importSymbol: "New",
			oldVersion:   "v1.0.2",
		},
		{
			name:         "zerolog",
			oldPath:      "github.com/gofiber/contrib/fiberzerolog",
			newModule:    "zerolog",
			newVersion:   "v1.0.0",
			importAlias:  "zerolog",
			importSymbol: "NewLogger",
			oldVersion:   "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
				if module != tt.newModule {
					return "", fmt.Errorf("unexpected module %s", module)
				}
				return tt.newVersion, nil
			})
			t.Cleanup(restore)

			file := writeTempFile(t, dir, fmt.Sprintf(`package main
import (
    %s %q
)

var _ = %s.%s`, tt.importAlias, tt.oldPath, tt.importAlias, tt.importSymbol))

			modContent := fmt.Sprintf(`module example

go 1.22

require %s %s

replace %s => ../local`, tt.oldPath, tt.oldVersion, tt.oldPath)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

			var buf bytes.Buffer
			cmd := newCmd(&buf)
			require.NoError(t, v3.MigrateContribPackages(cmd, dir, nil, nil))

			content := readFile(t, file)
			assert.Contains(t, content, "github.com/gofiber/contrib/v3/"+tt.newModule)
			assert.NotContains(t, content, tt.oldPath)

			mod := readFile(t, filepath.Join(dir, "go.mod"))
			assert.Contains(t, mod, "github.com/gofiber/contrib/v3/"+tt.newModule+" "+tt.newVersion)
			assert.Contains(t, mod, "replace github.com/gofiber/contrib/v3/"+tt.newModule+" => ../local")
			assert.NotContains(t, mod, tt.oldPath)

			assert.Contains(t, buf.String(), "Migrating contrib packages")
		})
	}
}

func Test_MigrateContribPackages_RenameSubpackage(t *testing.T) {
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
    middleware "github.com/gofiber/contrib/otelfiber/v2/middleware"
)

var _ = middleware.Middleware`)

	modContent := `module example

go 1.22

require github.com/gofiber/contrib/otelfiber/v2 v2.0.0

replace github.com/gofiber/contrib/otelfiber/v2 => ../local`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateContribPackages(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/contrib/v3/otel/middleware")
	assert.NotContains(t, content, "github.com/gofiber/contrib/otelfiber/v2/middleware")

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
