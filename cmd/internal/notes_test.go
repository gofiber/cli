package internal_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gofiber/cli/cmd/internal"
)

func Test_Notes_AreKeptPerRootAndDeduplicated(t *testing.T) {
	t.Parallel()

	rootA, rootB := t.TempDir(), t.TempDir()
	fi := internal.FileInfo{Root: rootA, Path: rootA + "/main.go", Rel: "main.go"}

	internal.Notef(fi, 12, "first %s", "note")
	internal.Notef(fi, 12, "first %s", "note")
	internal.Notef(fi, 13, "first %s", "note")
	internal.AddNote(rootA, internal.Note{Message: "project wide"})
	internal.AddNote(rootB, internal.Note{Message: "other project"})

	notes := internal.DrainNotes(rootA + "/")
	assert.Equal(t, []internal.Note{
		{File: "main.go", Line: 12, Message: "first note"},
		{File: "main.go", Line: 13, Message: "first note"},
		{Message: "project wide"},
	}, notes)
	assert.Empty(t, internal.DrainNotes(rootA))

	other := internal.DrainNotes(rootB)
	assert.Len(t, other, 1)
}

// A note names its file with forward slashes whatever the platform joined the path with, and
// the same file spelled either way is one note.
func Test_Notes_NameTheFileWithForwardSlashes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fi := internal.FileInfo{Root: root, Path: filepath.Join(root, "pkg", "sub", "file.go"), Rel: filepath.Join("pkg", "sub", "file.go")}

	internal.Notef(fi, 3, "message")
	internal.AddNote(root, internal.Note{File: "pkg/sub/file.go", Line: 3, Message: "message"})

	got := internal.DrainNotes(root)
	if assert.Len(t, got, 1) {
		assert.Equal(t, "pkg/sub/file.go", got[0].File)
		assert.Equal(t, "pkg/sub/file.go:3: message", got[0].String())
	}
}

func Test_Note_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "message", internal.Note{Message: "message"}.String())
	assert.Equal(t, "go.mod: message", internal.Note{File: "go.mod", Message: "message"}.String())
	assert.Equal(t, "main.go:7: message", internal.Note{File: "main.go", Line: 7, Message: "message"}.String())
	assert.Equal(t, "main.go:7: MigrateX: message", internal.Note{File: "main.go", Line: 7, Migration: "MigrateX", Message: "message"}.String())
	assert.Equal(t, "MigrateX: message", internal.Note{Migration: "MigrateX", Message: "message"}.String())
}

func Test_Note_StringNamesTheOtherPlaces(t *testing.T) {
	t.Parallel()

	places := func(n int) []internal.NotePlace {
		out := make([]internal.NotePlace, n)
		for i := range out {
			out[i] = internal.NotePlace{File: "handlers/users.go", Line: 10 + i}
		}
		return out
	}

	note := internal.Note{File: "main.go", Line: 7, Message: "message", More: places(1)}
	assert.Equal(t, "main.go:7: message (1 more in this project: handlers/users.go:10)", note.String())

	note.More = places(5)
	assert.Equal(t, "main.go:7: message (5 more in this project: handlers/users.go:10, handlers/users.go:11, handlers/users.go:12, "+
		"handlers/users.go:13, handlers/users.go:14)", note.String())

	note.More = places(8)
	assert.Equal(t, "main.go:7: message (8 more in this project: handlers/users.go:10, handlers/users.go:11, handlers/users.go:12, "+
		"handlers/users.go:13, handlers/users.go:14, and 3 more)", note.String())

	note.More = nil
	assert.Equal(t, "main.go:7: message", note.String())
}

func Test_LineOf(t *testing.T) {
	t.Parallel()

	content := "a\nbb\nccc"
	assert.Equal(t, 1, internal.LineOf(content, 0))
	assert.Equal(t, 1, internal.LineOf(content, 1))
	assert.Equal(t, 2, internal.LineOf(content, 2))
	assert.Equal(t, 3, internal.LineOf(content, 5))
	assert.Equal(t, 3, internal.LineOf(content, 100))
	assert.Equal(t, 1, internal.LineOf(content, -4))
}
