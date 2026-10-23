package v3

import "github.com/gofiber/cli/cmd/internal"

// noteTally collects the places of one finding that the project is told about
// once: the note is at the first place and names the others (internal.Note.More).
type noteTally struct {
	first internal.NotePlace
	more  []internal.NotePlace
	found bool
}

// add records a place of the finding, a line of the text the migration was given.
func (t *noteTally) add(fi internal.FileInfo, line int) {
	t.addPlace(fi.Place(line))
}

// addReturned records a place of the finding, a line of the text the migration returns.
func (t *noteTally) addReturned(fi internal.FileInfo, line int) {
	t.addPlace(fi.ReturnedPlace(line))
}

func (t *noteTally) addPlace(place internal.NotePlace) {
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
