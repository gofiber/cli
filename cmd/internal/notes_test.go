package internal_test

import (
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

func Test_Note_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "message", internal.Note{Message: "message"}.String())
	assert.Equal(t, "go.mod: message", internal.Note{File: "go.mod", Message: "message"}.String())
	assert.Equal(t, "main.go:7: message", internal.Note{File: "main.go", Line: 7, Message: "message"}.String())
	assert.Equal(t, "main.go:7: MigrateX: message", internal.Note{File: "main.go", Line: 7, Migration: "MigrateX", Message: "message"}.String())
	assert.Equal(t, "MigrateX: message", internal.Note{Migration: "MigrateX", Message: "message"}.String())
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
