package internal

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// FileInfo describes the file a migration is currently rewriting.
type FileInfo struct {
	// Root is the directory that was handed to ChangeFileContentInfo.
	Root string
	// Path is the file path, joined onto Root.
	Path string
	// Rel is Path relative to Root.
	Rel string
	// pass numbers the call of the processor that the FileInfo was made for, see
	// RelocateNotes. It is 0 for a FileInfo that was not made by ChangeFileContentInfo.
	pass int
	// returned says the lines of the notes and places made with the FileInfo are lines
	// of the text that the processor returns, see Returned.
	returned bool
}

// Returned returns the FileInfo for notes and places whose lines are lines of the
// text that the processor returns, which differs from the one it was given by its
// own edits, and not of the text it was given.
func (fi FileInfo) Returned() FileInfo {
	fi.returned = true
	return fi
}

// Place returns the place at line of the file, for a note that names more than one
// place.
func (fi FileInfo) Place(line int) NotePlace {
	return NotePlace{File: fi.Rel, Line: line, Pass: fi.pass, Returned: fi.returned}
}

// NotePlace is a place of the migrated directory that a note is about.
type NotePlace struct {
	// File is relative to the migrated directory.
	File string
	// Line is 1-based.
	Line int
	// Pass and Returned say which text of the file Line refers to, as the fields of
	// a Note do.
	Pass     int
	Returned bool
}

// String renders the place as "file:line".
func (p NotePlace) String() string {
	return filepath.ToSlash(p.File) + ":" + strconv.Itoa(p.Line)
}

// notePlacesShown is how many of the other places of a note String names.
const notePlacesShown = 5

// Note is a message a migration leaves for the user: a construct it could not
// migrate safely, a file it had to skip, or a behavior change worth reviewing.
type Note struct {
	// File is relative to the migrated directory, empty when the note is not
	// tied to a file.
	File string
	// Migration is the name of the migration that raised the note. It is filled
	// in by the caller of DrainNotes.
	Migration string
	// Message is the human readable text.
	Message string
	// More are the other places of the project that the note is about, when a
	// migration tells the project once about a finding it made in several places:
	// the note is at the first of them (File and Line), String names the others.
	More []NotePlace
	// Line is 1-based, 0 when unknown.
	Line int
	// Pass is the pass over File whose text Line refers to: the call of the
	// processor of ChangeFileContentInfo that made the note, 0 when it was not made
	// in one. RelocateNotes follows the line through the rewrites from that pass on.
	Pass int
	// Returned says Line is a line of the text that the processor of the pass
	// returned, not of the text it was given.
	Returned bool
}

// String renders the note as "file:line: migration: message", followed by the
// other places the note is about, if there are any.
func (n Note) String() string {
	prefix := ""
	if n.File != "" {
		prefix = filepath.ToSlash(n.File)
		if n.Line > 0 {
			prefix += ":" + strconv.Itoa(n.Line)
		}
		prefix += ": "
	}
	if n.Migration != "" {
		prefix += n.Migration + ": "
	}
	return prefix + n.Message + n.otherPlaces()
}

// otherPlaces renders the places of More for the end of the message.
func (n Note) otherPlaces() string {
	if len(n.More) == 0 {
		return ""
	}
	named := make([]string, 0, notePlacesShown+1)
	for _, place := range n.More[:min(len(n.More), notePlacesShown)] {
		named = append(named, place.String())
	}
	if rest := len(n.More) - notePlacesShown; rest > 0 {
		named = append(named, "and "+strconv.Itoa(rest)+" more")
	}
	return " (" + strconv.Itoa(len(n.More)) + " more in this project: " + strings.Join(named, ", ") + ")"
}

var (
	notesMu sync.Mutex
	// notes are keyed by the cleaned migration root, so concurrent runs on
	// different directories (for example parallel tests) never see each other's
	// notes.
	notes = map[string][]Note{}
)

// AddNote records n for the migration running in root. An identical note
// (same file, line and message) is only kept once.
func AddNote(root string, n Note) {
	key := filepath.Clean(root)
	if n.Pass == 0 {
		// A note that was not made with the FileInfo of a pass is taken to be about the
		// file as it is now.
		n.Pass = currentPass(root)
	}

	notesMu.Lock()
	defer notesMu.Unlock()

	for _, existing := range notes[key] {
		if existing.File == n.File && existing.Line == n.Line && existing.Message == n.Message {
			return
		}
	}
	notes[key] = append(notes[key], n)
}

// Notef records a formatted note for the file fi. Pass line 0 when the line is
// unknown; LineOf computes it from an offset into the file content.
func Notef(fi FileInfo, line int, format string, args ...any) {
	AddNote(fi.Root, Note{
		File:     fi.Rel,
		Line:     line,
		Message:  fmt.Sprintf(format, args...),
		Pass:     fi.pass,
		Returned: fi.returned,
	})
}

// DrainNotes returns the notes recorded for root and forgets them.
func DrainNotes(root string) []Note {
	key := filepath.Clean(root)

	notesMu.Lock()
	defer notesMu.Unlock()

	drained := notes[key]
	delete(notes, key)
	return drained
}

// LineOf returns the 1-based line number of the byte offset within content.
func LineOf(content string, offset int) int {
	if offset > len(content) {
		offset = len(content)
	}
	if offset < 0 {
		offset = 0
	}
	return strings.Count(content[:offset], "\n") + 1
}
