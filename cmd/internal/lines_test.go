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

const linesSource = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"a\")\n\tfmt.Println(\"b\")\n}\n"

// linesAddImports is a rewrite that moves the code below the imports by four lines.
func linesAddImports(content string) string {
	return strings.Replace(content, "import \"fmt\"\n", "import (\n\t\"fmt\"\n\t\"os\"\n)\n\nvar _ = os.Args\n", 1)
}

func linesProject(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return dir
}

// linesOfCallB returns the line of the file in dir that calls fmt.Println("b").
func linesOfCallB(t *testing.T, dir, name string) int {
	t.Helper()
	return linesOfCall(t, dir, name, `fmt.Println("b")`)
}

// linesOfCallA returns the line of the file in dir that calls fmt.Println("a").
func linesOfCallA(t *testing.T, dir, name string) int {
	t.Helper()
	return linesOfCall(t, dir, name, `fmt.Println("a")`)
}

func linesOfCall(t *testing.T, dir, name, text string) int {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))) //nolint:gosec // a file of the temp project of this test
	require.NoError(t, err)
	for i, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, text) {
			return i + 1
		}
	}
	require.FailNow(t, "no line has the text", "%s: %q", name, text)
	return 0
}

func Test_RelocateNotes_FollowsTheRewriteOfThePassThatMadeTheNote(t *testing.T) {
	dir := linesProject(t, map[string]string{"main.go": linesSource})
	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })

	_, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		before, _, found := strings.Cut(content, `fmt.Println("b")`)
		require.True(t, found)
		internal.Notef(fi, 1+strings.Count(before, "\n"), "about b")
		return linesAddImports(content)
	})
	require.NoError(t, err)

	notes := internal.RelocateNotes(dir, internal.DrainNotes(dir))
	require.Len(t, notes, 1)
	assert.Equal(t, linesOfCallB(t, dir, "main.go"), notes[0].Line)
	assert.Equal(t, 12, notes[0].Line)
}

func Test_RelocateNotes_FollowsTheRewritesThatCameAfter(t *testing.T) {
	dir := linesProject(t, map[string]string{"main.go": linesSource})
	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })

	// a pass that notes and does not rewrite the file
	_, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		internal.Notef(fi, 7, "about b")
		return content
	})
	require.NoError(t, err)
	// the next one rewrites it
	_, err = internal.ChangeFileContentInfo(dir, linesAddImportsProcessor)
	require.NoError(t, err)
	// and a note of a pass after the rewrite is about the text that came out of it
	_, err = internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		internal.Notef(fi, 10, "about a")
		return content
	})
	require.NoError(t, err)

	notes := internal.RelocateNotes(dir, internal.DrainNotes(dir))
	require.Len(t, notes, 2)
	assert.Equal(t, "about b", notes[0].Message)
	assert.Equal(t, linesOfCallB(t, dir, "main.go"), notes[0].Line)
	assert.Equal(t, "about a", notes[1].Message)
	assert.Equal(t, 10, notes[1].Line, "the line was the one of the rewritten file already")
}

func linesAddImportsProcessor(content string, _ internal.FileInfo) string {
	return linesAddImports(content)
}

func Test_RelocateNotes_ALineThatWasRewrittenIsWhereTheLineItBecameIs(t *testing.T) {
	dir := linesProject(t, map[string]string{"main.go": linesSource})
	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })

	_, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		internal.Notef(fi, 6, "about a")
		return strings.Replace(content, "\tfmt.Println(\"a\")\n", "\tif true {\n\t\tfmt.Println(\"a\")\n\t}\n", 1)
	})
	require.NoError(t, err)

	notes := internal.RelocateNotes(dir, internal.DrainNotes(dir))
	require.Len(t, notes, 1)
	assert.Equal(t, linesOfCallA(t, dir, "main.go"), notes[0].Line, "the call is the line of the block that is most like the line")
	assert.Equal(t, 7, notes[0].Line)
}

func Test_RelocateNotes_ReturnedLinesAreOfTheTextTheProcessorReturned(t *testing.T) {
	dir := linesProject(t, map[string]string{"main.go": linesSource})
	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })

	_, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		out := linesAddImports(content)
		// the line of the call in the text that is returned, which the formatter changes by one blank line
		out = strings.Replace(out, "var _ = os.Args\n", "var _ = os.Args\n\n\n", 1)
		before, _, found := strings.Cut(out, `fmt.Println("b")`)
		require.True(t, found)
		internal.Notef(fi.Returned(), 1+strings.Count(before, "\n"), "about b")
		return out
	})
	require.NoError(t, err)

	notes := internal.RelocateNotes(dir, internal.DrainNotes(dir))
	require.Len(t, notes, 1)
	assert.Equal(t, linesOfCallB(t, dir, "main.go"), notes[0].Line)
}

func Test_RelocateNotes_ReturnedLinesOfAPassThatDidNotRewriteAreOfTheGivenText(t *testing.T) {
	dir := linesProject(t, map[string]string{"main.go": linesSource})
	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })

	_, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		internal.Notef(fi.Returned(), 7, "about b")
		return content
	})
	require.NoError(t, err)
	_, err = internal.ChangeFileContentInfo(dir, linesAddImportsProcessor)
	require.NoError(t, err)

	notes := internal.RelocateNotes(dir, internal.DrainNotes(dir))
	require.Len(t, notes, 1)
	assert.Equal(t, linesOfCallB(t, dir, "main.go"), notes[0].Line)
}

func Test_RelocateNotes_TheOtherPlacesOfANoteFollowTheirOwnFiles(t *testing.T) {
	dir := linesProject(t, map[string]string{"a.go": linesSource, "b.go": linesSource, "c.go": linesSource})
	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })

	var first internal.NotePlace
	var more []internal.NotePlace
	_, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		place := fi.Place(7)
		switch fi.Rel {
		case "a.go":
			first = place
		default:
			more = append(more, place)
		}
		if fi.Rel == "b.go" {
			return content // b.go is not rewritten by this pass
		}
		return linesAddImports(content)
	})
	require.NoError(t, err)
	// the second pass rewrites b.go
	_, err = internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		if fi.Rel == "b.go" {
			return linesAddImports(content)
		}
		return content
	})
	require.NoError(t, err)

	internal.AddNote(dir, internal.Note{File: first.File, Line: first.Line, Pass: first.Pass, Message: "about b", More: more})
	notes := internal.RelocateNotes(dir, internal.DrainNotes(dir))
	require.Len(t, notes, 1)
	assert.Equal(t, linesOfCallB(t, dir, "a.go"), notes[0].Line)
	require.Len(t, notes[0].More, 2)
	assert.Equal(t, "b.go", notes[0].More[0].File)
	assert.Equal(t, linesOfCallB(t, dir, "b.go"), notes[0].More[0].Line)
	assert.Equal(t, "c.go", notes[0].More[1].File)
	assert.Equal(t, linesOfCallB(t, dir, "c.go"), notes[0].More[1].Line)
}

func Test_RelocateNotes_CRLFFilesAreComparedLineByLine(t *testing.T) {
	dir := linesProject(t, map[string]string{"main.go": strings.ReplaceAll(linesSource, "\n", "\r\n")})
	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })

	_, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		internal.Notef(fi, 7, "about b")
		return strings.Replace(content, "import \"fmt\"\r\n", "import (\r\n\t\"fmt\"\r\n\t\"os\"\r\n)\r\n\r\nvar _ = os.Args\r\n", 1)
	})
	require.NoError(t, err)

	notes := internal.RelocateNotes(dir, internal.DrainNotes(dir))
	require.Len(t, notes, 1)
	assert.Equal(t, linesOfCallB(t, dir, "main.go"), notes[0].Line)
}

func Test_RelocateNotes_NothingChangesOutsideOfARun(t *testing.T) {
	dir := linesProject(t, map[string]string{"main.go": linesSource})

	_, err := internal.ChangeFileContentInfo(dir, func(content string, fi internal.FileInfo) string {
		internal.Notef(fi, 7, "about b")
		return linesAddImports(content)
	})
	require.NoError(t, err)

	drained := internal.DrainNotes(dir)
	assert.Equal(t, []internal.Note{{File: "main.go", Line: 7, Message: "about b"}}, drained, "a note outside of a run is as it was made")
	assert.Equal(t, drained, internal.RelocateNotes(dir, drained))
}

func Test_RelocateNotes_NotesOfOtherFilesAndWithoutAPassStayWhereTheyAre(t *testing.T) {
	dir := linesProject(t, map[string]string{"main.go": linesSource})
	internal.BeginRun(dir)
	t.Cleanup(func() { internal.AbandonRun(dir) })

	_, err := internal.ChangeFileContentInfo(dir, linesAddImportsProcessor)
	require.NoError(t, err)

	notes := internal.RelocateNotes(dir, []internal.Note{
		{File: "go.mod", Line: 3, Message: "a file that nothing rewrote", Pass: 1},
		{File: "main.go", Line: 7, Message: "a line of the file as it is", Pass: 0},
		{Message: "no file"},
	})
	assert.Equal(t, 3, notes[0].Line)
	assert.Equal(t, 7, notes[1].Line)
	assert.Zero(t, notes[2].Line)
}
