package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateTemplateVersions(t *testing.T) {
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main
import (
    amber "github.com/gofiber/template/amber"
    django "github.com/gofiber/template/django/v3"
    html "github.com/gofiber/template/html/v2"
)

var (
    _ = html.New
    _ = django.New
    _ = amber.New
)
`)

	modContent := `module example

go 1.22

require (
    github.com/gofiber/template/html/v2 v2.0.0
    github.com/gofiber/template/django/v3 v3.0.1
    github.com/gofiber/template/amber v1.8.3
)

replace github.com/gofiber/template/html/v2 => ../html
replace github.com/gofiber/template/django/v3 => ../django`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateTemplateVersions(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/template/html/v3")
	assert.Contains(t, content, "github.com/gofiber/template/django/v4")
	assert.Contains(t, content, "github.com/gofiber/template/amber/v3")
	assert.NotContains(t, content, "github.com/gofiber/template/html/v2")
	assert.NotContains(t, content, "github.com/gofiber/template/django/v3")

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/template/html/v3 v3.0.0")
	assert.Contains(t, mod, "github.com/gofiber/template/django/v4 v4.0.0")
	assert.Contains(t, mod, "github.com/gofiber/template/amber/v3 v3.0.0")
	assert.Contains(t, mod, "replace github.com/gofiber/template/html/v3 => ../html")
	assert.Contains(t, mod, "replace github.com/gofiber/template/django/v4 => ../django")

	assert.Contains(t, buf.String(), "Migrated template package versions")
}

func Test_MigrateTemplateVersions_ReplaceWithVersion(t *testing.T) {
	dir := t.TempDir()

	_ = writeTempFile(t, dir, `package main
import _ "github.com/gofiber/template/html/v2"
`)

	modContent := `module example

go 1.22

require github.com/gofiber/template/html/v2 v2.0.0

replace github.com/gofiber/template/html/v2 v2.0.0 => ../html`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateTemplateVersions(cmd, dir, nil, nil))

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "replace github.com/gofiber/template/html/v3 v3.0.0 => ../html")
	assert.NotContains(t, mod, "replace github.com/gofiber/template/html/v3 v2.0.0 => ../html")
}

func Test_MigrateTemplateVersions_Idempotent(t *testing.T) {
	dir := t.TempDir()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/template/html/v3"
`)

	modContent := `module example

go 1.22

require github.com/gofiber/template/html/v3 v3.0.0`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o600))

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateTemplateVersions(cmd, dir, nil, nil))
	firstContent := readFile(t, file)
	firstMod := readFile(t, filepath.Join(dir, "go.mod"))

	require.NoError(t, v3.MigrateTemplateVersions(cmd, dir, nil, nil))
	secondContent := readFile(t, file)
	secondMod := readFile(t, filepath.Join(dir, "go.mod"))

	assert.Equal(t, firstContent, secondContent)
	assert.Equal(t, firstMod, secondMod)
	assert.Empty(t, buf.String())
}
