package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_refreshContrib(t *testing.T) {
	dir := t.TempDir()
	mainSrc := `package main

import _ "github.com/gofiber/contrib/v3/monitor"

func main(){}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o600))
	modSrc := `module test

require github.com/gofiber/contrib/v3/monitor v1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modSrc), 0o600))

	old := latestContribVersionFn
	latestContribVersionFn = func(string) string { return "v1.2.3" }
	defer func() { latestContribVersionFn = old }()

	c := &cobra.Command{}
	c.SetIn(bytes.NewBufferString("\n"))
	var buf bytes.Buffer
	c.SetOut(&buf)

	changed, err := refreshContrib(c, dir, "")
	require.NoError(t, err)
	assert.True(t, changed)

	content, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(content), "github.com/gofiber/contrib/v3/monitor")

	gm, err := os.ReadFile(filepath.Join(dir, "go.mod")) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(gm), "github.com/gofiber/contrib/v3/monitor v1.2.3")
}

func Test_refreshContrib_RenamedModule(t *testing.T) {
	tests := []struct {
		name         string
		oldImport    string
		oldRequire   string
		newModule    string
		newVersion   string
		notContains  string
	}{
		{
			name:        "zap",
			oldImport:   "github.com/gofiber/contrib/fiberzap",
			oldRequire:  "github.com/gofiber/contrib/fiberzap",
			newModule:   "zap",
			newVersion:  "v1.1.0",
			notContains: "github.com/gofiber/contrib/fiberzap",
		},
		{
			name:        "i18n",
			oldImport:   "github.com/gofiber/contrib/fiberi18n",
			oldRequire:  "github.com/gofiber/contrib/fiberi18n",
			newModule:   "i18n",
			newVersion:  "v1.0.0",
			notContains: "github.com/gofiber/contrib/fiberi18n",
		},
		{
			name:        "zerolog",
			oldImport:   "github.com/gofiber/contrib/fiberzerolog",
			oldRequire:  "github.com/gofiber/contrib/fiberzerolog",
			newModule:   "zerolog",
			newVersion:  "v1.0.0",
			notContains: "github.com/gofiber/contrib/fiberzerolog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mainSrc := "package main\n\nimport _ " + `"` + tt.oldImport + `"` + "\n\nfunc main(){}"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o600))
			modSrc := "module test\n\nrequire " + tt.oldRequire + " v1.0.0\n"
			if tt.name == "zap" {
				modSrc = "module test\n\nrequire " + tt.oldRequire + " v1.0.2\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modSrc), 0o600))

			old := latestContribVersionFn
			latestContribVersionFn = func(module string) string {
				if module != tt.newModule {
					t.Fatalf("unexpected module %s", module)
				}
				return tt.newVersion
			}
			defer func() { latestContribVersionFn = old }()

			c := &cobra.Command{}
			c.SetIn(bytes.NewBufferString("\n"))
			var buf bytes.Buffer
			c.SetOut(&buf)

			changed, err := refreshContrib(c, dir, "")
			require.NoError(t, err)
			assert.True(t, changed)

			content, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
			require.NoError(t, err)
			assert.Contains(t, string(content), "github.com/gofiber/contrib/v3/"+tt.newModule)
			assert.NotContains(t, string(content), tt.notContains)

			gm, err := os.ReadFile(filepath.Join(dir, "go.mod")) // #nosec G304
			require.NoError(t, err)
			assert.Contains(t, string(gm), "github.com/gofiber/contrib/v3/"+tt.newModule+" "+tt.newVersion)
			assert.NotContains(t, string(gm), tt.notContains)
		})
	}
}

func Test_refreshStorage(t *testing.T) {
	dir := t.TempDir()
	mainSrc := `package main

import _ "github.com/gofiber/storage/redis"

func main(){}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o600))
	modSrc := `module test

require github.com/gofiber/storage/redis v1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modSrc), 0o600))

	old := latestStorageVersionFn
	latestStorageVersionFn = func(string, string) string { return "v2.3.4" }
	defer func() { latestStorageVersionFn = old }()

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	changed, err := refreshStorage(c, dir, "")
	require.NoError(t, err)
	assert.True(t, changed)

	content, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(content), "github.com/gofiber/storage/redis/v2")

	gm, err := os.ReadFile(filepath.Join(dir, "go.mod")) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(gm), "github.com/gofiber/storage/redis/v2 v2.3.4")
}

func Test_refreshTemplates(t *testing.T) {
	dir := t.TempDir()
	mainSrc := `package main

import _ "github.com/gofiber/template/html/v2"

func main(){}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o600))
	modSrc := `module test

require github.com/gofiber/template/html/v2 v2.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modSrc), 0o600))

	old := latestTemplateVersionFn
	latestTemplateVersionFn = func(string, string) string { return "v3.2.1" }
	defer func() { latestTemplateVersionFn = old }()

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	changed, err := refreshTemplates(c, dir, "")
	require.NoError(t, err)
	assert.True(t, changed)

	content, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(content), "github.com/gofiber/template/html/v3")

	gm, err := os.ReadFile(filepath.Join(dir, "go.mod")) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(gm), "github.com/gofiber/template/html/v3 v3.2.1")
}
