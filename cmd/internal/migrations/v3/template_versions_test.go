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

// A requirement that is already newer than the table's minimum stays: the
// migration used to put django/v4 v4.0.9 back to v4.0.0.
func Test_MigrateTemplateVersions_NeverLowersAVersion(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/template/django/v4"
	"github.com/gofiber/template/html/v2"
)

var _, _ = django.New, html.New
`,
		"go.mod": `module example

go 1.22

require (
	github.com/gofiber/template/django/v4 v4.0.9
	github.com/gofiber/template/html/v2 v2.1.3
	github.com/gofiber/template/jet/v3 v3.0.11 // indirect
	github.com/gofiber/template/mustache v1.0.0
)
`,
	})

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateTemplateVersions(newCmd(&buf), dir, nil, nil))
	assert.Equal(t, "Migrated template package versions\n", buf.String())

	assert.Equal(t, `module example

go 1.22

require (
	github.com/gofiber/template/django/v4 v4.0.9
	github.com/gofiber/template/html/v3 v3.0.0
	github.com/gofiber/template/jet/v3 v3.0.11 // indirect
	github.com/gofiber/template/mustache/v3 v3.0.0
)
`, importsRead(t, dir, "go.mod"))
	assert.Contains(t, importsRead(t, dir, "main.go"), `"github.com/gofiber/template/django/v4"`)
	assert.Contains(t, importsRead(t, dir, "main.go"), `"github.com/gofiber/template/html/v3"`)
}

func Test_MigrateTemplateVersions_RaisesAVersionBelowTheMinimum(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"go.mod": "module example\n\nrequire github.com/gofiber/template/html/v3 v3.0.0-beta.1\n",
	})

	require.NoError(t, v3.MigrateTemplateVersions(newCmd(&bytes.Buffer{}), dir, nil, nil))
	assert.Equal(t, "module example\n\nrequire github.com/gofiber/template/html/v3 v3.0.0\n", importsRead(t, dir, "go.mod"))
}

func Test_MigrateTemplateVersions_LeavesNewerLinesAndOtherModulesAlone(t *testing.T) {
	t.Parallel()

	src := `module example

require (
	github.com/gofiber/template v1.8.3 // indirect
	github.com/gofiber/template/v2 v2.1.1 // indirect
	github.com/gofiber/template/html/v4 v4.0.1
	github.com/gofiber/template/unknown/v2 v2.0.0
)
`
	dir := importsTree(t, map[string]string{
		"go.mod": src,
		"main.go": `package main

import (
	"github.com/gofiber/template/html/v4"
	"github.com/gofiber/template/unknown/v2"
)

var _, _ = html.New, unknown.New
`,
	})

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateTemplateVersions(newCmd(&buf), dir, nil, nil))

	assert.Empty(t, buf.String(), "an engine on a line newer than the table's is not ours to lower")
	assert.Equal(t, src, importsRead(t, dir, "go.mod"))
	assert.Contains(t, importsRead(t, dir, "main.go"), `"github.com/gofiber/template/html/v4"`)
	assert.Contains(t, importsRead(t, dir, "main.go"), `"github.com/gofiber/template/unknown/v2"`)
}

func Test_MigrateTemplateVersions_EveryModule(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"go.mod": "module example\r\n\r\nrequire (\r\n\tgithub.com/gofiber/template/html/v2 v2.1.3\r\n\tgithub.com/gofiber/template/html/v3 v3.0.11\r\n)\r\n\r\nreplace (\r\n\tgithub.com/gofiber/template/html/v2 => ../html\r\n\tgithub.com/gofiber/template/django/v3 v3.0.1 => ../django\r\n)\r\n",
		"svc/go.mod": `module example/svc

require github.com/gofiber/template/amber v1.8.3 // indirect

replace github.com/gofiber/template/amber => ../amber
`,
		"vendor/go.mod": "module vendor\n\nrequire github.com/gofiber/template/html/v2 v2.0.0\n",
	})

	require.NoError(t, v3.MigrateTemplateVersions(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, "module example\r\n\r\nrequire (\r\n\tgithub.com/gofiber/template/html/v3 v3.0.11\r\n)\r\n\r\nreplace (\r\n\tgithub.com/gofiber/template/html/v3 => ../html\r\n\tgithub.com/gofiber/template/django/v4 v4.0.0 => ../django\r\n)\r\n",
		importsRead(t, dir, "go.mod"), "the highest version wins when both majors are there; block replaces follow")
	assert.Equal(t, `module example/svc

require github.com/gofiber/template/amber/v3 v3.0.0 // indirect

replace github.com/gofiber/template/amber/v3 => ../amber
`, importsRead(t, dir, "svc/go.mod"))
	assert.Equal(t, "module vendor\n\nrequire github.com/gofiber/template/html/v2 v2.0.0\n", importsRead(t, dir, "vendor/go.mod"))
}

func Test_MigrateTemplateVersions_OnlyImportDeclarationsAreRewritten(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"main.go": `package main

import "github.com/gofiber/template/html/v2"

// see "github.com/gofiber/template/html/v2" for the options
const path = "github.com/gofiber/template/html/v2"

var _ = html.New
`,
	})

	require.NoError(t, v3.MigrateTemplateVersions(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, `package main

import "github.com/gofiber/template/html/v3"

// see "github.com/gofiber/template/html/v2" for the options
const path = "github.com/gofiber/template/html/v2"

var _ = html.New
`, importsRead(t, dir, "main.go"))
}

func Test_MigrateTemplateVersions_ReplaceForAVersionFollowsTheRequirement(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"go.mod": `module example

require (
	github.com/gofiber/template/html/v2 v2.1.3
	github.com/gofiber/template/html/v3 v3.0.11
)

replace github.com/gofiber/template/html/v2 v2.1.3 => ../html
`,
	})

	require.NoError(t, v3.MigrateTemplateVersions(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, `module example

require (
	github.com/gofiber/template/html/v3 v3.0.11
)

replace github.com/gofiber/template/html/v3 v3.0.11 => ../html
`, importsRead(t, dir, "go.mod"), "the replace applies to the version that is required, not to the table's minimum")
}
