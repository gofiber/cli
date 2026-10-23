package internal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func readGoFile(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path) // #nosec G304 -- the path is this test's own temp dir
	require.NoError(t, err)
	return string(b)
}

func Test_ChangeFileContent_RewritesGoFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	a := writeGoFile(t, dir, "a.go", "package a\n\nvar x = 1\n")
	sub := writeGoFile(t, dir, "sub/b.go", "package b\n\nvar x = 1\n")
	vendored := writeGoFile(t, dir, "vendor/c.go", "package c\n\nvar x = 1\n")
	text := writeGoFile(t, dir, "notes.txt", "var x = 1\n")

	changed, err := internal.ChangeFileContent(dir, func(content string) string {
		return strings.ReplaceAll(content, "x = 1", "x = 2")
	})
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Contains(t, readGoFile(t, a), "x = 2")
	assert.Contains(t, readGoFile(t, sub), "x = 2")
	assert.Contains(t, readGoFile(t, vendored), "x = 1", "vendor is skipped")
	assert.Contains(t, readGoFile(t, text), "x = 1", "only .go files are touched")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_ChangeFileContent_SkipsBrokenFilesAndContinues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Walk order is lexical, so the broken file sits between two good ones.
	first := writeGoFile(t, dir, "a_first.go", "package a\n\nvar x = 1\n")
	broken := writeGoFile(t, dir, "b_broken.go", "package a\n\nvar y = (1\nvar x = 1\n")
	last := writeGoFile(t, dir, "c_last.go", "package a\n\nvar z = 1\nvar x = 1\n")

	changed, err := internal.ChangeFileContent(dir, func(content string) string {
		return strings.ReplaceAll(content, "x = 1", "x = 2")
	})
	require.NoError(t, err, "a file that does not parse must not fail the run")
	assert.True(t, changed)

	assert.Contains(t, readGoFile(t, first), "x = 2")
	assert.Contains(t, readGoFile(t, last), "x = 2", "files after the broken one are still migrated")
	assert.Equal(t, "package a\n\nvar y = (1\nvar x = 1\n", readGoFile(t, broken), "the broken file is left as it was")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "b_broken.go", notes[0].File)
	assert.Positive(t, notes[0].Line)
	assert.Contains(t, notes[0].Message, "does not parse")
	assert.Empty(t, internal.DrainNotes(dir), "draining forgets the notes")
}

func Test_ChangeFileContent_RewriteThatBreaksTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeGoFile(t, dir, "a.go", "package a\n\nvar x = 1\n")

	changed, err := internal.ChangeFileContent(dir, func(content string) string {
		return strings.ReplaceAll(content, "x = 1", "x = (1")
	})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, "package a\n\nvar x = 1\n", readGoFile(t, path))

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "rewritten code does not parse")
}

func Test_ChangeFileContent_Filters(t *testing.T) {
	// Not parallel: the filters are process-wide.
	dir := t.TempDir()
	keep := writeGoFile(t, dir, "keep.go", "package a\n\nvar x = 1\n")
	skip := writeGoFile(t, dir, "skip.go", "package a\n\nvar x = 1\n")

	internal.SetFileFilters(nil, []string{"skip.go"})
	defer internal.SetFileFilters(nil, nil)

	changed, err := internal.ChangeFileContent(dir, func(content string) string {
		return strings.ReplaceAll(content, "x = 1", "x = 2")
	})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Contains(t, readGoFile(t, keep), "x = 2")
	assert.Contains(t, readGoFile(t, skip), "x = 1")
}

func Test_ChangeFileContentInfo_PassesTheFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeGoFile(t, dir, "pkg/a.go", "package a\n")

	var seen []internal.FileInfo
	changed, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		seen = append(seen, fi)
		return content
	})
	require.NoError(t, err)
	assert.False(t, changed)

	require.Len(t, seen, 1)
	assert.Equal(t, dir, seen[0].Root)
	assert.Equal(t, filepath.Join(dir, "pkg", "a.go"), seen[0].Path)
	assert.Equal(t, filepath.Join("pkg", "a.go"), seen[0].Rel)
}

// runChange begins a run, applies the processors one after the other as the migrations of a run do and finishes it.
func runChange(t *testing.T, dir string, processors ...func(string) string) map[string]func(int) int {
	t.Helper()

	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })
	for _, processor := range processors {
		_, err := internal.ChangeFileContent(dir, processor)
		require.NoError(t, err)
	}
	moved, err := internal.FinishRun(dir)
	require.NoError(t, err)
	return moved
}

func replacing(old, replacement string) func(string) string {
	return func(content string) string { return strings.ReplaceAll(content, old, replacement) }
}

func Test_FinishRun_KeepsTheLineEndingsOfTheFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	crlf := writeGoFile(t, dir, "crlf.go", "package a\r\n\r\nvar x = 1\r\n\r\nvar s = `a\r\nb`\r\n")
	lf := writeGoFile(t, dir, "lf.go", "package a\n\nvar x = 1\n")
	mixed := writeGoFile(t, dir, "mixed.go", "package a\r\n\r\nvar x = 1\nvar y = 1\nvar z = 1\n")

	runChange(t, dir, replacing("x = 1", "x = 2"))

	assert.Equal(t, "package a\r\n\r\nvar x = 2\r\n\r\nvar s = `a\r\nb`\r\n", readGoFile(t, crlf), "every line of a CRLF file ends with CRLF")
	assert.Equal(t, "package a\n\nvar x = 2\n", readGoFile(t, lf))
	assert.Equal(t, "package a\r\n\r\nvar x = 2\nvar y = 1\nvar z = 1\n", readGoFile(t, mixed), "most lines end with LF, so the lines the migration wrote do; the others keep theirs")
}

func Test_FinishRun_KeepsTheFormattingOfTheLinesNoMigrationChanged(t *testing.T) {
	t.Parallel()

	source := `package a

import (
	"strings"
	"fmt"
)

//Hello says hello.
func Hello(name string) string {
	fields := map[string]int{
		"a": 1,
		"bbb": 2,
	}
	return fmt.Sprint(name, fields,   strings.ToUpper(name))
}

//Bye says bye.
func Bye() string { return "bye" }
`
	dir := t.TempDir()
	path := writeGoFile(t, dir, "a.go", source)
	crlf := writeGoFile(t, dir, "crlf.go", strings.ReplaceAll(source, "\n", "\r\n"))

	// two migrations in a row, the second reads what the first wrote
	runChange(t, dir, replacing(`return "bye"`, `return "goodbye"`), replacing("goodbye", "good bye"))

	want := strings.ReplaceAll(source, `return "bye"`, `return "good bye"`)
	assert.Equal(t, want, readGoFile(t, path), "the unformatted lines no migration touched stay as they were: the comments, the import order, the spacing")
	assert.Equal(t, strings.ReplaceAll(want, "\n", "\r\n"), readGoFile(t, crlf), "and so do the line endings")
}

func Test_FinishRun_TheMigratedLinesAreFormatted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeGoFile(t, dir, "a.go", `package a

//Hello says hello.
func Hello() int {
	return 1
}
`)

	runChange(t, dir, func(content string) string {
		// the migration writes a badly indented block, and an import it needs
		content = strings.ReplaceAll(content, "return 1", "if true {\nreturn 2\n\t}\n\treturn 1")
		return strings.ReplaceAll(content, "package a\n", "package a\n\nimport \"strings\"\n\nvar _ = strings.ToUpper\n")
	})

	got := readGoFile(t, path)
	assert.Contains(t, got, "\tif true {\n\t\treturn 2\n\t}\n", "the inserted code is formatted")
	assert.Contains(t, got, "import \"strings\"\n\nvar _ = strings.ToUpper\n")
	assert.Contains(t, got, "//Hello says hello.", "the author's comment, a few lines away, is not")
}

func Test_FinishRun_AnImportTheMigrationDroppedStaysDropped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeGoFile(t, dir, "a.go", `package a

import (
	"os"
	"fmt"
)

func Hello() { fmt.Println(os.Args) }
`)

	runChange(t, dir, replacing("fmt.Println(os.Args)", "println(1)"))

	// both imports are unused now and the formatter drops them: that is the migration's change
	assert.Equal(t, "package a\n\nfunc Hello() { println(1) }\n", readGoFile(t, path))
}

func Test_FinishRun_FollowsTheLinesOfTheNotes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeGoFile(t, dir, "a.go", `package a

var a = 1


var b = 2

var c = 3
`)
	same := writeGoFile(t, dir, "same.go", "package a\n\nvar x = 1\n")

	moved := runChange(t, dir, replacing("c = 3", "c = 4"))

	remap, ok := moved["a.go"]
	require.True(t, ok, "the second blank line came back, so the lines moved")
	assert.Equal(t, 3, remap(3))
	assert.Equal(t, 7, remap(6), "var b: the formatter had removed a blank line, the author's text has it")
	assert.Equal(t, 9, remap(8), "var c")
	assert.NotContains(t, moved, "same.go")
	assert.Equal(t, "package a\n\nvar x = 1\n", readGoFile(t, same))
}

func Test_FinishRun_WithoutBeginRunTheFilesStayFormatted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeGoFile(t, dir, "a.go", "package a\r\n\r\n//Hello says hello.\r\nvar x = 1\r\n")

	_, err := internal.ChangeFileContent(dir, replacing("x = 1", "x = 2"))
	require.NoError(t, err)
	moved, err := internal.FinishRun(dir)
	require.NoError(t, err)

	assert.Empty(t, moved)
	assert.Equal(t, "package a\n\n// Hello says hello.\nvar x = 2\n", readGoFile(t, path), "what the formatter wrote is kept when no run recorded the original")
}

func Test_FinishRun_LeavesAFileAloneThatIsNoLongerFormatted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeGoFile(t, dir, "a.go", "package a\n\n//Hello says hello.\nvar x = 1\n")

	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })
	_, err := internal.ChangeFileContent(dir, replacing("x = 1", "x = 2"))
	require.NoError(t, err)
	// something else writes it afterwards, unformatted
	require.NoError(t, os.WriteFile(path, []byte("package a\n\n// Hello says hello.\nvar   x = 3\n"), 0o600))

	_, err = internal.FinishRun(dir)
	require.NoError(t, err)
	assert.Equal(t, "package a\n\n// Hello says hello.\nvar   x = 3\n", readGoFile(t, path))
}
