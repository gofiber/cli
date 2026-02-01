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

const (
	swaggoModule    = "swaggo"
	swaggerUIModule = "swaggerui"
)

func Test_MigrateSwaggerPackages(t *testing.T) {
	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		switch module {
		case swaggoModule:
			return "v3.1.0", nil
		case swaggerUIModule:
			return "v3.0.1", nil
		default:
			return "", fmt.Errorf("unexpected module %s", module)
		}
	})
	t.Cleanup(restore)

	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/contrib/swagger"

func main() {
    _ = swagger.Config{}
}`)

	modContent := `module example

go 1.22

require github.com/gofiber/contrib/swagger v1.0.0

replace github.com/gofiber/contrib/swagger => ../local`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSwaggerPackages(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `swagger "github.com/gofiber/contrib/v3/swaggerui"`)

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/contrib/v3/swaggerui v3.0.1")
	assert.Contains(t, mod, "replace github.com/gofiber/contrib/v3/swaggerui => ../local")

	assert.Contains(t, buf.String(), "Migrating swagger packages")
}

func Test_MigrateSwaggerPackages_FiberSwagger(t *testing.T) {
	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		switch module {
		case swaggoModule:
			return "v3.2.0", nil
		case swaggerUIModule:
			return "v3.0.0", nil
		default:
			return "", fmt.Errorf("unexpected module %s", module)
		}
	})
	t.Cleanup(restore)

	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main
import (
    "fmt"
    "github.com/gofiber/swagger"
)

func main() {
    fmt.Println(swagger.Config{})
}`)

	modContent := `module example

go 1.22

require github.com/gofiber/swagger v1.1.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSwaggerPackages(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `swagger "github.com/gofiber/contrib/v3/swaggo"`)

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/contrib/v3/swaggo v3.2.0")

	assert.Contains(t, buf.String(), "Migrating swagger packages")
}

func Test_MigrateSwaggerPackages_Idempotent(t *testing.T) {
	calls := 0
	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		calls++
		switch module {
		case swaggoModule:
			return "v3.0.1", nil
		case swaggerUIModule:
			return "v3.2.1", nil
		default:
			return "", fmt.Errorf("unexpected module %s", module)
		}
	})
	t.Cleanup(restore)

	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main
import swagger "github.com/gofiber/contrib/v3/swaggo"

func main() {
    _ = swagger.Config{}
}`)

	modContent := `module example

go 1.22

require (
    github.com/gofiber/contrib/v3/swaggo v3.0.1
    github.com/gofiber/contrib/v3/swaggerui v3.2.1
)`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSwaggerPackages(cmd, dir, nil, nil))
	firstContent := readFile(t, file)
	firstMod := readFile(t, filepath.Join(dir, "go.mod"))

	require.NoError(t, v3.MigrateSwaggerPackages(cmd, dir, nil, nil))
	secondContent := readFile(t, file)
	secondMod := readFile(t, filepath.Join(dir, "go.mod"))

	assert.Equal(t, firstContent, secondContent)
	assert.Equal(t, firstMod, secondMod)
	assert.Empty(t, buf.String())
	assert.Equal(t, 2, calls)
}

func Test_MigrateSwaggerPackages_PreservesExistingImports(t *testing.T) {
	restore := v3.SetContribV3VersionFetcher(func(module string) (string, error) {
		switch module {
		case swaggoModule:
			return "v3.4.0", nil
		case swaggerUIModule:
			return "v3.2.1", nil
		default:
			return "", fmt.Errorf("unexpected module %s", module)
		}
	})
	t.Cleanup(restore)

	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/contrib/v3/swaggo"

func main() {
    _ = swaggo.Config{}
}`)

	modContent := `module example

go 1.22

require github.com/gofiber/contrib/v3/swaggo v3.4.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSwaggerPackages(cmd, dir, nil, nil))

	assert.Equal(t, `package main
import "github.com/gofiber/contrib/v3/swaggo"

func main() {
    _ = swaggo.Config{}
}`, readFile(t, file))

	assert.Equal(t, modContent, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Empty(t, buf.String())
}
