package migrations_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations"
)

func Test_MigrateGoPkgs(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mgpkgs")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	mainContent := `package main
import (
    fiber "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/adaptor"
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
	assert.Contains(t, content, `fiber "github.com/gofiber/fiber/v3"`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/middleware/adaptor"`)
	assert.NotContains(t, content, "github.com/gofiber/fiber/v2")

	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "github.com/gofiber/fiber/v3 v3.0.0")
	assert.Contains(t, buf.String(), "Migrating Go packages")
}

func Test_MigrateGoPkgs_WithHash(t *testing.T) {
	t.Parallel()

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

func Test_MigrateGoPkgs_ImportForms(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod": "module example\n\ngo 1.22\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n",
		"a_plain.go": `package main

import "github.com/gofiber/fiber/v2"

var _ = fiber.New
`,
		"b_aliased.go": `package main

import (
	f "github.com/gofiber/fiber/v2" // the framework
	_ "github.com/gofiber/fiber/v2/middleware/pprof"
	. "github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

var _, _ = f.New, logger.New
var _ = New
`,
		"c_raw.go": "package main\n\nimport fiber `github.com/gofiber/fiber/v2`\n\nvar _ = fiber.New\n",
		"d_test.go": `//go:build integration

package main_test

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

func Test(t *testing.T) { _ = fiber.New() }
`,
		"e_already.go": `package main

import "github.com/gofiber/fiber/v3"

var _ = fiber.New
`,
		"f_strings.go": `package main

import "github.com/gofiber/fiber/v2"

// Import path of the framework: "github.com/gofiber/fiber/v2" (see docs).
const frameworkPath = "github.com/gofiber/fiber/v2"

type Info struct {
	Pkg string ` + "`" + `json:"pkg" example:"github.com/gofiber/fiber/v2"` + "`" + `
}

const doc = ` + "`" + `see https://pkg.go.dev/github.com/gofiber/fiber/v2#App` + "`" + `

var _ = fiber.New
`,
	})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoPkgs(newCmd(&buf), dir, nil, semver.MustParse("3.6.1")))
	assert.Equal(t, "Migrating Go packages\n", buf.String())

	assert.Contains(t, importsRead(t, dir, "a_plain.go"), `import "github.com/gofiber/fiber/v3"`)
	b := importsRead(t, dir, "b_aliased.go")
	assert.Contains(t, b, `f "github.com/gofiber/fiber/v3" // the framework`)
	assert.Contains(t, b, `_ "github.com/gofiber/fiber/v3/middleware/pprof"`)
	assert.Contains(t, b, `. "github.com/gofiber/fiber/v3/middleware/helmet"`)
	assert.Contains(t, b, `"github.com/gofiber/fiber/v3/middleware/logger"`)
	assert.NotContains(t, b, "fiber/v2")
	assert.Contains(t, importsRead(t, dir, "c_raw.go"), `fiber "github.com/gofiber/fiber/v3"`)
	d := importsRead(t, dir, "d_test.go")
	assert.Contains(t, d, "//go:build integration")
	assert.Contains(t, d, `"github.com/gofiber/fiber/v3"`)
	assert.Contains(t, importsRead(t, dir, "e_already.go"), `"github.com/gofiber/fiber/v3"`)

	f := importsRead(t, dir, "f_strings.go")
	assert.Contains(t, f, `import "github.com/gofiber/fiber/v3"`)
	assert.Contains(t, f, `// Import path of the framework: "github.com/gofiber/fiber/v2" (see docs).`, "comments are not imports")
	assert.Contains(t, f, `const frameworkPath = "github.com/gofiber/fiber/v2"`, "strings are not imports")
	assert.Contains(t, f, `example:"github.com/gofiber/fiber/v2"`, "struct tags are not imports")
	assert.Contains(t, f, "pkg.go.dev/github.com/gofiber/fiber/v2#App")
}

func Test_MigrateGoPkgs_Idempotent(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod":  "module example\n\ngo 1.22\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n",
		"main.go": "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ = fiber.New\n",
	})
	target := semver.MustParse("3.6.1")

	var first bytes.Buffer
	require.NoError(t, migrations.MigrateGoPkgs(newCmd(&first), dir, nil, target))
	assert.Equal(t, "Migrating Go packages\n", first.String())
	mod, src := readFile(t, filepath.Join(dir, "go.mod")), readFile(t, filepath.Join(dir, "main.go"))

	var second bytes.Buffer
	require.NoError(t, migrations.MigrateGoPkgs(newCmd(&second), dir, nil, target))
	assert.Empty(t, second.String())
	assert.Equal(t, mod, readFile(t, filepath.Join(dir, "go.mod")))
	assert.Equal(t, src, readFile(t, filepath.Join(dir, "main.go")))
}

func Test_MigrateGoPkgs_GoModShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "indirect comment",
			src:  "module a\n\ngo 1.21\n\nrequire (\n\tgithub.com/gofiber/fiber/v2 v2.52.15 // indirect\n\tgithub.com/google/uuid v1.6.0\n)\n",
			want: "module a\n\ngo 1.21\n\nrequire (\n\tgithub.com/gofiber/fiber/v3 v3.6.1 // indirect\n\tgithub.com/google/uuid v1.6.0\n)\n",
		},
		{
			name: "trailing comment",
			src:  "module a\n\ngo 1.21\n\nrequire github.com/gofiber/fiber/v2 v2.52.15 // pinned until the v3 upgrade\n",
			want: "module a\n\ngo 1.21\n\nrequire github.com/gofiber/fiber/v3 v3.6.1 // pinned until the v3 upgrade\n",
		},
		{
			name: "no trailing newline",
			src:  "module a\n\ngo 1.21\n\nrequire github.com/gofiber/fiber/v2 v2.52.15",
			want: "module a\n\ngo 1.21\n\nrequire github.com/gofiber/fiber/v3 v3.6.1",
		},
		{
			name: "crlf",
			src:  "module a\r\n\r\ngo 1.21\r\n\r\nrequire github.com/gofiber/fiber/v2 v2.52.15\r\n",
			want: "module a\r\n\r\ngo 1.21\r\n\r\nrequire github.com/gofiber/fiber/v3 v3.6.1\r\n",
		},
		{
			name: "tab between module and version",
			src:  "module a\n\nrequire github.com/gofiber/fiber/v2\tv2.52.15\n",
			want: "module a\n\nrequire github.com/gofiber/fiber/v3\tv3.6.1\n",
		},
		{
			name: "replace before require",
			src:  "module a\n\nreplace github.com/gofiber/fiber/v2 => ../fiber-fork\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n",
			want: "module a\n\nreplace github.com/gofiber/fiber/v2 => ../fiber-fork\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		},
		{
			name: "pseudo version",
			src:  "module a\n\nrequire github.com/gofiber/fiber/v2 v2.52.16-0.20250101000000-0123456789ab\n",
			want: "module a\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n",
		},
		{
			name: "both majors",
			src:  "module a\n\nrequire (\n\tgithub.com/gofiber/fiber/v2 v2.52.15\n\tgithub.com/gofiber/fiber/v3 v3.0.0-beta.4\n)\n",
			want: "module a\n\nrequire (\n\tgithub.com/gofiber/fiber/v3 v3.6.1\n)\n",
		},
		{
			name: "other gofiber modules are not fiber",
			src:  "module a\n\nrequire (\n\tgithub.com/gofiber/fiber/v2 v2.52.15\n\tgithub.com/gofiber/schema v1.0.0\n\tgithub.com/gofiber/template/html/v2 v2.1.3\n)\n",
			want: "module a\n\nrequire (\n\tgithub.com/gofiber/fiber/v3 v3.6.1\n\tgithub.com/gofiber/schema v1.0.0\n\tgithub.com/gofiber/template/html/v2 v2.1.3\n)\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := importsWriteTree(t, map[string]string{"go.mod": tc.src})
			var buf bytes.Buffer
			require.NoError(t, migrations.MigrateGoPkgs(newCmd(&buf), dir, nil, semver.MustParse("3.6.1")))

			assert.Equal(t, tc.want, readFile(t, filepath.Join(dir, "go.mod")))
			assert.Equal(t, "Migrating Go packages\n", buf.String())
		})
	}
}

func Test_MigrateGoPkgs_EveryModuleOfARepository(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		// No go.mod at the root: a workspace with the modules below it.
		"go.work":              "go 1.22\n\nuse (\n\t./svc/api\n\t./tools/gen\n)\n",
		"svc/api/go.mod":       "module example.com/mono/svc/api\n\ngo 1.21\n\nrequire (\n\tgithub.com/gofiber/fiber/v2 v2.52.15\n)\n",
		"svc/api/main.go":      "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ = fiber.New\n",
		"tools/gen/go.mod":     "module example.com/mono/tools/gen\n\ngo 1.21\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n",
		"tools/gen/main.go":    "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ = fiber.New\n",
		"vendor/x/go.mod":      "module x\n\nrequire github.com/gofiber/fiber/v2 v2.0.0\n",
		"other/go.mod":         "module example.com/other\n\nrequire github.com/google/uuid v1.6.0\n",
		"testdata/bad/go.mod":  "module {{ .Name }}\n",
		"svc/api/svc/go.mod":   "module example.com/mono/svc/api/svc\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n",
		"svc/api/svc/main.go":  "package main\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ = fiber.New\n",
		"tools/gen/Makefile":   "build:\n\tgo build ./...\n",
		"svc/api/.hidden/a.go": "package a\n",
	})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateGoPkgs(newCmd(&buf), dir, nil, semver.MustParse("3.5.0")))
	assert.Equal(t, "Migrating Go packages\n", buf.String())

	for _, name := range []string{"svc/api/go.mod", "tools/gen/go.mod", "svc/api/svc/go.mod"} {
		assert.Contains(t, importsRead(t, dir, name), "github.com/gofiber/fiber/v3 v3.5.0", name)
	}
	assert.Contains(t, importsRead(t, dir, "vendor/x/go.mod"), "fiber/v2 v2.0.0", "vendor is skipped")
	assert.Contains(t, importsRead(t, dir, "other/go.mod"), "uuid v1.6.0")
	for _, name := range []string{"svc/api/main.go", "tools/gen/main.go", "svc/api/svc/main.go"} {
		assert.Contains(t, importsRead(t, dir, name), `"github.com/gofiber/fiber/v3"`, name)
	}

	notes := importsNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "testdata/bad/go.mod")
}

func Test_MigrateGoPkgs_ReplaceOfTheOldModule(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod": "module a\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n\nreplace (\n\tgithub.com/gofiber/fiber/v2 => ../fiber-fork\n)\n",
	})

	require.NoError(t, migrations.MigrateGoPkgs(newCmd(&bytes.Buffer{}), dir, nil, semver.MustParse("3.6.1")))

	assert.Contains(t, readFile(t, filepath.Join(dir, "go.mod")), "github.com/gofiber/fiber/v2 => ../fiber-fork", "a fork of v2 cannot stand in for v3")
	notes := importsNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "go.mod:6: ")
	assert.Contains(t, notes[0], "replace directive for github.com/gofiber/fiber/v2 does not apply to github.com/gofiber/fiber/v3")
}

func Test_MigrateGoPkgs_NotesWhatItDoesNotRewrite(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod": "module a\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n",
		"main.go": `package main

//go:generate go run github.com/gofiber/fiber/v2/internal/gen -o out.go
//go:generate go run github.com/gofiber/fiber/v3/internal/gen -o other.go

import "github.com/gofiber/fiber/v2"

var _ = fiber.New
`,
		"Dockerfile":                   "FROM golang:1.22\nRUN go install github.com/gofiber/fiber/v2/cmd/example@latest\n",
		"Makefile":                     "build:\n\tgo build -ldflags \"-X github.com/gofiber/fiber/v2.Version=custom\" ./...\n",
		".github/workflows/ci.yml":     "jobs:\n  x:\n    steps:\n      - run: go get github.com/gofiber/fiber/v2@v2.52.15\n      - run: go get github.com/gofiber/fiber/v3@v3.6.1\n",
		"README.md":                    "import \"github.com/gofiber/fiber/v2\"\n",
		"node_modules/pkg/build.sh":    "go get github.com/gofiber/fiber/v2\n",
		"vendor/ci.yml":                "go get github.com/gofiber/fiber/v2\n",
		"scripts/release.sh":           "#!/bin/sh\ngo list github.com/gofiber/fiber/v2/...\n",
		"docs/assets/image.dockerfile": "RUN go install github.com/gofiber/fiber/v2/cmd/x@latest\n",
	})

	require.NoError(t, migrations.MigrateGoPkgs(newCmd(&bytes.Buffer{}), dir, nil, semver.MustParse("3.6.1")))

	notes := importsNotes(dir)
	assert.Equal(t, []string{
		".github/workflows/ci.yml:4: mentions github.com/gofiber/fiber/v2; it is github.com/gofiber/fiber/v3 now. The CLI does not edit this kind of file, update it by hand.",
		"Dockerfile:2: mentions github.com/gofiber/fiber/v2; it is github.com/gofiber/fiber/v3 now. The CLI does not edit this kind of file, update it by hand.",
		"Makefile:2: mentions github.com/gofiber/fiber/v2; it is github.com/gofiber/fiber/v3 now. The CLI does not edit this kind of file, update it by hand.",
		"docs/assets/image.dockerfile:1: mentions github.com/gofiber/fiber/v2; it is github.com/gofiber/fiber/v3 now. The CLI does not edit this kind of file, update it by hand.",
		"main.go:3: `//go:generate` runs github.com/gofiber/fiber/v2, which is not the migrated Fiber module; update the directive by hand.",
		"scripts/release.sh:2: mentions github.com/gofiber/fiber/v2; it is github.com/gofiber/fiber/v3 now. The CLI does not edit this kind of file, update it by hand.",
	}, slices.Sorted(slices.Values(notes)))

	assert.Contains(t, importsRead(t, dir, "Dockerfile"), "fiber/v2/cmd/example", "build files are not rewritten")
}

func Test_MigrateGoPkgs_BrokenFileDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod":        "module a\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n",
		"a/a.go":        "package a\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ = fiber.New\n",
		"testdata/b.go": "package {{ .Name }}\n\nimport \"github.com/gofiber/fiber/v2\"\n",
		"z/z.go":        "package z\n\nimport \"github.com/gofiber/fiber/v2\"\n\nvar _ = fiber.New\n",
	})

	require.NoError(t, migrations.MigrateGoPkgs(newCmd(&bytes.Buffer{}), dir, nil, semver.MustParse("3.6.1")))

	assert.Contains(t, importsRead(t, dir, "a/a.go"), "fiber/v3")
	assert.Contains(t, importsRead(t, dir, "z/z.go"), "fiber/v3")
	assert.Contains(t, importsRead(t, dir, "testdata/b.go"), "fiber/v2", "the broken file is left as it was")
	assert.Contains(t, importsRead(t, dir, "go.mod"), "fiber/v3 v3.6.1", "and the go.mod is still migrated")

	notes := importsNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "testdata/b.go")
	assert.Contains(t, notes[0], "does not parse")
}

// importsRead and importsNotes are small helpers of the tests above.
func importsRead(t *testing.T, dir, name string) string {
	t.Helper()

	return readFile(t, filepath.Join(dir, filepath.FromSlash(name)))
}

func importsNotes(dir string) []string {
	var out []string
	for _, n := range internal.DrainNotes(dir) {
		out = append(out, n.String())
	}
	return out
}
