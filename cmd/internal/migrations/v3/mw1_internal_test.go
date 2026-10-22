package v3

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// These tests look at the text a migration produces before goimports has had
// its say: on a first run the packages of v3 cannot be resolved from the module
// cache, so every import the output needs has to be written by the migration.

func mw1FileInfo(t *testing.T) internal.FileInfo {
	t.Helper()
	dir := t.TempDir()
	return internal.FileInfo{Root: dir, Path: dir + "/main.go", Rel: "main.go"}
}

func Test_migrateCORSContent_AddsTheStringsImport(t *testing.T) {
	t.Parallel()

	out := migrateCORSContent(`package main

import (
	"os"

	"github.com/gofiber/fiber/v3/middleware/cors"
)

var _ = cors.Config{AllowOrigins: os.Getenv("ORIGINS")}
`, mw1FileInfo(t))

	assert.Contains(t, out, "\t\"strings\"\n")
	assert.Contains(t, out, `strings.FieldsFunc(os.Getenv("ORIGINS")`)
}

func Test_migrateCORSContent_UsesTheAliasOfTheStringsImport(t *testing.T) {
	t.Parallel()

	out := migrateCORSContent(`package main

import (
	"os"
	str "strings"

	"github.com/gofiber/fiber/v3/middleware/cors"
)

var _ = cors.Config{AllowOrigins: os.Getenv("ORIGINS")}
`, mw1FileInfo(t))

	assert.Contains(t, out, `str.FieldsFunc(os.Getenv("ORIGINS")`)
	assert.Equal(t, 1, strings.Count(out, `"strings"`))
}

func Test_migrateCORSContent_NoImportForLiterals(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v3/middleware/cors"

var _ = cors.Config{AllowOrigins: "https://a.com"}
`
	out := migrateCORSContent(src, mw1FileInfo(t))

	assert.NotContains(t, out, `"strings"`)
	assert.Contains(t, out, `[]string{"https://a.com"}`)
}

func Test_migrateCORSContent_SkipsAFileThatDoesNotParse(t *testing.T) {
	t.Parallel()

	fi := mw1FileInfo(t)
	src := `package main

import "github.com/gofiber/fiber/v3/middleware/cors"

var _ = cors.Config{AllowOrigins: "a"
`
	assert.Equal(t, src, migrateCORSContent(src, fi))
	notes := internal.DrainNotes(fi.Root)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "does not parse")
}
