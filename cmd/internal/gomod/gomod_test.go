package gomod_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/gomod"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path) // #nosec G304 -- the path is this test's own temp dir
	require.NoError(t, err)
	return string(b)
}

func Test_Paths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := write(t, dir, "go.mod", "module a\n")
	nested := write(t, dir, "svc/api/go.mod", "module a/svc/api\n")
	write(t, dir, "vendor/x/go.mod", "module x\n")
	write(t, dir, "web/node_modules/y/go.mod", "module y\n")
	write(t, dir, ".git/hooks/go.mod", "module z\n")

	paths, err := gomod.Paths(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{root, nested}, paths)
}

func Test_Edit_LeavesUntouchedFilesAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Odd spacing that Format would normalize: it must survive when nothing changes.
	src := "module a\n\ngo 1.21\n\nrequire   github.com/gofiber/fiber/v2   v2.52.15\n"
	path := write(t, dir, "go.mod", src)

	changed, err := gomod.Edit(dir, func(*gomod.File) error { return nil })
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, src, read(t, path))
}

func Test_File_SetRequire(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := write(t, dir, "go.mod", `module a

go 1.21

require github.com/gofiber/fiber/v2 v2.52.15 // pinned until the upgrade

require (
	github.com/gofiber/template/html/v2 v2.1.3 // indirect
	github.com/google/uuid v1.6.0
)
`)

	changed, err := gomod.Edit(dir, func(f *gomod.File) error {
		f.SetRequire(f.Requires("github.com/gofiber/fiber/v2"), "github.com/gofiber/fiber/v3", "v3.6.1")
		f.SetRequire(f.Requires("github.com/gofiber/template/html/v2"), "github.com/gofiber/template/html/v3", "v3.0.0")
		return nil
	})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, `module a

go 1.21

require github.com/gofiber/fiber/v3 v3.6.1 // pinned until the upgrade

require (
	github.com/gofiber/template/html/v3 v3.0.0 // indirect
	github.com/google/uuid v1.6.0
)
`, read(t, path))
}

func Test_File_SetReplaceOld(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := write(t, dir, "go.mod", `module a

go 1.21

require github.com/gofiber/template/html/v2 v2.1.3

replace github.com/gofiber/template/django/v3 v3.0.1 => ../django

replace (
	github.com/gofiber/template/html/v2 => ../html
	github.com/gofiber/template/amber v1.8.3 => github.com/me/amber v1.0.0
)
`)

	changed, err := gomod.Edit(dir, func(f *gomod.File) error {
		for _, r := range f.Replace {
			switch r.Old.Path {
			case "github.com/gofiber/template/django/v3":
				f.SetReplaceOld(r, "github.com/gofiber/template/django/v4", "v4.0.0")
			case "github.com/gofiber/template/html/v2":
				f.SetReplaceOld(r, "github.com/gofiber/template/html/v3", "")
			case "github.com/gofiber/template/amber":
				f.SetReplaceOld(r, "github.com/gofiber/template/amber/v3", "v3.0.0")
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, `module a

go 1.21

require github.com/gofiber/template/html/v2 v2.1.3

replace github.com/gofiber/template/django/v4 v4.0.0 => ../django

replace (
	github.com/gofiber/template/html/v3 => ../html
	github.com/gofiber/template/amber/v3 v3.0.0 => github.com/me/amber v1.0.0
)
`, read(t, path))
}

func Test_File_DedupeRequires(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := write(t, dir, "go.mod", `module a

go 1.21

require (
	github.com/gofiber/fiber/v2 v2.52.15
	github.com/gofiber/fiber/v3 v3.0.0-beta.4
	github.com/google/uuid v1.6.0
)
`)

	_, err := gomod.Edit(dir, func(f *gomod.File) error {
		f.SetRequire(f.Requires("github.com/gofiber/fiber/v2"), "github.com/gofiber/fiber/v3", "v3.6.1")
		f.DedupeRequires()
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, `module a

go 1.21

require (
	github.com/gofiber/fiber/v3 v3.6.1
	github.com/google/uuid v1.6.0
)
`, read(t, path))
}

func Test_File_DropRequire(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := write(t, dir, "go.mod", "module a\n\ngo 1.21\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n\nrequire github.com/google/uuid v1.6.0\n")

	_, err := gomod.Edit(dir, func(f *gomod.File) error {
		f.DropRequire(f.Requires("github.com/gofiber/fiber/v2"))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "module a\n\ngo 1.21\n\nrequire github.com/google/uuid v1.6.0\n", read(t, path))
}

func Test_File_RaiseGo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		want    string
		changed bool
	}{
		{
			name:    "lower language version",
			src:     "module a\n\ngo 1.21\n",
			want:    "module a\n\ngo 1.26.0\n",
			changed: true,
		},
		{
			name:    "trailing comment is kept",
			src:     "module a\n\ngo 1.21 // keep in sync with the Dockerfile\n",
			want:    "module a\n\ngo 1.26.0 // keep in sync with the Dockerfile\n",
			changed: true,
		},
		{
			name:    "newer is never lowered",
			src:     "module a\n\ngo 1.27.1\n",
			want:    "module a\n\ngo 1.27.1\n",
			changed: false,
		},
		{
			name:    "equal stays",
			src:     "module a\n\ngo 1.26.0\n",
			want:    "module a\n\ngo 1.26.0\n",
			changed: false,
		},
		{
			name:    "language version below the release of the same minor",
			src:     "module a\n\ngo 1.26\n",
			want:    "module a\n\ngo 1.26.0\n",
			changed: true,
		},
		{
			name:    "missing go line",
			src:     "module a\n\nrequire github.com/google/uuid v1.6.0\n",
			want:    "module a\n\ngo 1.26.0\n\nrequire github.com/google/uuid v1.6.0\n",
			changed: true,
		},
		{
			name:    "older toolchain line goes with the raise",
			src:     "module a\n\ngo 1.22.0\n\ntoolchain go1.22.5\n",
			want:    "module a\n\ngo 1.26.0\n",
			changed: true,
		},
		{
			name:    "newer toolchain line stays",
			src:     "module a\n\ngo 1.22.0\n\ntoolchain go1.28.0\n",
			want:    "module a\n\ngo 1.26.0\n\ntoolchain go1.28.0\n",
			changed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := write(t, dir, "go.mod", tc.src)

			var got bool
			changed, err := gomod.Edit(dir, func(f *gomod.File) error {
				got = f.RaiseGo("1.26.0")
				return nil
			})
			require.NoError(t, err)
			assert.Equal(t, tc.changed, got)
			assert.Equal(t, tc.changed, changed)
			assert.Equal(t, tc.want, read(t, path))
		})
	}
}

func Test_Edit_KeepsCRLF(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := write(t, dir, "go.mod", "module a\r\n\r\ngo 1.21\r\n\r\nrequire github.com/gofiber/fiber/v2 v2.52.15\r\n")

	_, err := gomod.Edit(dir, func(f *gomod.File) error {
		f.SetRequire(f.Requires("github.com/gofiber/fiber/v2"), "github.com/gofiber/fiber/v3", "v3.6.1")
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "module a\r\n\r\ngo 1.21\r\n\r\nrequire github.com/gofiber/fiber/v3 v3.6.1\r\n", read(t, path))
}

func Test_Edit_ReportsAGoModThatDoesNotParse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	good := write(t, dir, "go.mod", "module a\n\ngo 1.21\n")
	bad := write(t, dir, "testdata/tpl/go.mod", "module {{ .Name }}\n\ngo 1.21 1.22 1.23\n")

	changed, err := gomod.Edit(dir, func(f *gomod.File) error {
		f.RaiseGo("1.26.0")
		return nil
	})
	require.NoError(t, err, "a go.mod that does not parse must not stop the others")
	assert.True(t, changed)
	assert.Equal(t, "module a\n\ngo 1.26.0\n", read(t, good))
	assert.Equal(t, "module {{ .Name }}\n\ngo 1.21 1.22 1.23\n", read(t, bad))

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "testdata/tpl/go.mod", notes[0].File)
	assert.Contains(t, notes[0].Message, "does not parse")
}

func Test_Edit_KeepsDirectivesThisVersionDoesNotKnow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := write(t, dir, "go.mod", "module a\n\ngo 1.21\n\nfuture directive\n")

	_, err := gomod.Edit(dir, func(f *gomod.File) error {
		f.RaiseGo("1.26.0")
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "module a\n\ngo 1.26.0\n\nfuture directive\n", read(t, path))
}
