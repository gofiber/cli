package internal

import (
	"path/filepath"
	"sort"
	"strings"
)

// A note carries the line that a file had when a migration looked at it. The
// migrations that follow, and the one that raised the note when it rewrites the
// file too, move the code below their edits: an import added at the top moves
// everything. A run that was begun with BeginRun remembers for every rewrite of a
// file how its lines moved, and RelocateNotes follows the notes through the
// rewrites to the text the last migration left.
//
// The text a migration sees is the text of a pass: one call of the processor of
// ChangeFileContentInfo for one file. Notes and places made with the FileInfo of
// the pass (Notef, FileInfo.Place) name it, so that the rewrite of the pass itself,
// and the ones after it, are the ones to follow.

// rewrite says how the lines of a file moved when a pass rewrote it.
type rewrite struct {
	// given maps a line of the text the pass was given to the line it has in the
	// text that was written. It is nil when the lines did not move, or when they
	// cannot be compared.
	given func(line int) int
	// returned maps a line of the text the processor returned, which is formatted
	// before it is written.
	returned func(line int) int
	// pass is the pass that rewrote the file.
	pass int
}

var (
	// histories holds, for each root of a run, the rewrites of its files by path
	// relative to the root with forward slashes, in the order they were made.
	histories = map[string]map[string][]rewrite{}
	// lastPass is the number of the latest pass.
	lastPass int
)

// startPass numbers a new pass over a file of the run of root, 0 outside of a run.
func startPass(root string) int {
	runsMu.Lock()
	defer runsMu.Unlock()
	if _, ok := histories[filepath.Clean(root)]; !ok {
		return 0
	}
	lastPass++
	return lastPass
}

// currentPass is the number of the latest pass, 0 outside of a run.
func currentPass(root string) int {
	runsMu.Lock()
	defer runsMu.Unlock()
	if _, ok := histories[filepath.Clean(root)]; !ok {
		return 0
	}
	return lastPass
}

// recordRewrite remembers how the lines of a file moved in a pass: given is the
// text the pass was handed, returned the text its processor returned, written
// what was written.
func recordRewrite(root, rel string, pass int, given, returned, written string) {
	lines := func(src string) []string { return strings.Split(toLF(src), "\n") }
	end := lines(written)
	r := rewrite{
		given:    lineMapper(lines(given), end),
		returned: lineMapper(lines(returned), end),
		pass:     pass,
	}

	runsMu.Lock()
	defer runsMu.Unlock()
	if files, ok := histories[filepath.Clean(root)]; ok {
		rel = filepath.ToSlash(rel)
		files[rel] = append(files[rel], r)
	}
}

// RelocateNotes gives the notes the lines their files have now, after all the
// rewrites of the run so far: the line of a note that a rewrite changed is the line
// of what replaced it that is most like it (see lineMapper). A note that was not
// made in a pass (Pass is 0) and one for a file that nothing rewrote keep their
// line. It changes notes and returns it.
func RelocateNotes(root string, notes []Note) []Note {
	runsMu.Lock()
	defer runsMu.Unlock()

	files := histories[filepath.Clean(root)]
	if len(files) == 0 {
		return notes
	}
	for i, n := range notes {
		notes[i].Line = followRewrites(files[filepath.ToSlash(n.File)], n.Line, n.Pass, n.Returned)
		notes[i].Pass, notes[i].Returned = 0, false
		if len(n.More) == 0 {
			continue
		}
		more := make([]NotePlace, len(n.More))
		for j, place := range n.More {
			place.Line = followRewrites(files[filepath.ToSlash(place.File)], place.Line, place.Pass, place.Returned)
			place.Pass, place.Returned = 0, false
			more[j] = place
		}
		notes[i].More = more
	}
	return notes
}

// followRewrites maps a line of the text that the pass saw through the rewrites of
// the file from that pass on. returned says the line is one of the text that the
// processor of the pass returned.
func followRewrites(steps []rewrite, line, pass int, returned bool) int {
	if pass == 0 || line <= 0 {
		return line
	}
	for i := sort.Search(len(steps), func(i int) bool { return steps[i].pass >= pass }); i < len(steps); i++ {
		mapper := steps[i].given
		if returned && steps[i].pass == pass {
			mapper = steps[i].returned
		}
		if mapper != nil {
			line = mapper(line)
		}
	}
	return line
}
