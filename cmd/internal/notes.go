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
}

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
	// Line is 1-based, 0 when unknown.
	Line int
}

// String renders the note as "file:line: migration: message".
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
	return prefix + n.Message
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
		File:    fi.Rel,
		Line:    line,
		Message: fmt.Sprintf(format, args...),
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
