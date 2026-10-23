package v3

import (
	"github.com/gofiber/cli/cmd/internal"
)

// noteTally collects the places of one finding that the project is told about
// once: the note is at the first place and names the others (internal.Note.More).
type noteTally struct {
	more  []internal.NotePlace
	first internal.NotePlace
	found bool
}

// add records a place of the finding, a line of the text the migration was given (of
// the text it returns for the FileInfo of fi.Returned()).
func (t *noteTally) add(fi internal.FileInfo, line int) {
	place := fi.Place(line)
	if !t.found {
		t.first, t.found = place, true
		return
	}
	t.more = append(t.more, place)
}

// note returns the note for the finding with the message.
func (t *noteTally) note(message string) internal.Note {
	return internal.Note{
		File: t.first.File, Line: t.first.Line, Pass: t.first.Pass, Returned: t.first.Returned,
		Message: message, More: t.more,
	}
}
