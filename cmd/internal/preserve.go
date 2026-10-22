package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// A migration writes the file it changed through the formatter, so that the
// code it inserted is formatted like the rest, and so that the next migration
// reads formatted code. The formatter also fixes whatever the author left
// unformatted in the lines the migration did not touch, and ends every line
// with LF, which shows as changes in a review that have nothing to do with the
// migration. A run that was begun with BeginRun remembers the text of every file
// before the first migration wrote it, and FinishRun puts the author's text back
// wherever the migrations changed nothing.
//
// The three texts are the original file formatted (base), the original file
// as the author wrote it (ours), and the migrated file (theirs). The differences
// between base and ours are the author's formatting, those between base and theirs
// are the migration. Wherever only one of them changed a line, that change is
// taken; wherever both did, the migrated text is.

var (
	runsMu sync.Mutex
	// runs holds, for each root of a run, the text the files had when the first
	// migration wrote them.
	runs = map[string]map[string]string{}
)

// BeginRun starts to remember the text that the files below root have before a
// migration rewrites them, for FinishRun. A run that was begun already starts again.
func BeginRun(root string) {
	runsMu.Lock()
	defer runsMu.Unlock()
	runs[filepath.Clean(root)] = map[string]string{}
}

// AbandonRun forgets a run without restoring anything.
func AbandonRun(root string) {
	runsMu.Lock()
	defer runsMu.Unlock()
	delete(runs, filepath.Clean(root))
}

// rememberOriginal keeps the text of a file before its first rewrite of a run.
func rememberOriginal(root, path, original string) {
	runsMu.Lock()
	defer runsMu.Unlock()
	if files, ok := runs[filepath.Clean(root)]; ok {
		if _, seen := files[path]; !seen {
			files[path] = original
		}
	}
}

// FinishRun ends the run of root and puts the author's formatting and line
// endings back into the files the migrations rewrote, wherever a migration
// changed nothing (see keepFormatting). A file whose current text is not
// formatted, or that cannot be merged safely, stays as the migrations left it.
//
// It returns, for the files whose line numbers moved, by their path relative to
// root with forward slashes, a function that maps a line number of the text the
// migrations left to the line of the restored text, for the notes that point there.
func FinishRun(root string) (moved map[string]func(line int) int, err error) {
	key := filepath.Clean(root)
	runsMu.Lock()
	files := runs[key]
	delete(runs, key)
	runsMu.Unlock()

	var errs []error
	for path, original := range files {
		final, readErr := os.ReadFile(path) // #nosec G304 -- a file of the project that was migrated
		if readErr != nil {
			continue // a migration removed it
		}
		restored, remap, ok := restoreFormatting(path, original, string(final))
		if !ok {
			continue
		}
		if writeErr := os.WriteFile(path, []byte(restored), 0o600); writeErr != nil {
			errs = append(errs, fmt.Errorf("write file %s: %w", path, writeErr))
			continue
		}
		if remap != nil {
			rel, relErr := filepath.Rel(key, path)
			if relErr != nil {
				rel = path
			}
			if moved == nil {
				moved = map[string]func(int) int{}
			}
			moved[filepath.ToSlash(rel)] = remap
		}
	}
	return moved, errors.Join(errs...)
}

// restoreFormatting returns the text of a file that the migrations changed from
// original to final with the author's line endings and formatting back where
// they changed nothing. ok is false when final stays as it is.
func restoreFormatting(path, original, final string) (restored string, remap func(int) int, ok bool) {
	format := func(src string) (string, error) {
		b, err := formatOnly(path, []byte(src))
		return string(b), err
	}

	onDisk := final
	crlf := usesCRLF(original)
	if crlf {
		original, final = toLF(original), toLF(final)
	}
	if formatted, err := format(final); err != nil || formatted != final {
		// Not written by the formatter: nothing can be said about what it changed.
		return "", nil, false
	}

	merged := keepFormatting(original, final, format)
	out := merged
	if crlf {
		out = toCRLF(out)
	}
	if out == onDisk {
		return "", nil, false
	}
	if merged != final {
		remap = lineMapper(strings.Split(final, "\n"), strings.Split(merged, "\n"))
	}
	return out, remap, true
}

// lineMapper returns the function that maps the number (from 1) of a line of the
// old text to the number of the line it has in the new text: the lines that are
// there in both are matched, a line that was changed maps to the start of what
// replaced it.
func lineMapper(oldLines, newLines []string) func(int) int {
	hunks, ok := diffLines(oldLines, newLines)
	if !ok {
		return nil
	}
	return func(line int) int {
		idx, offset := line-1, 0
		for _, h := range hunks {
			switch {
			case idx < h.aStart:
				return idx + offset + 1
			case idx < h.aEnd:
				return h.bStart + min(idx-h.aStart, max(h.bEnd-h.bStart-1, 0)) + 1
			}
			offset = h.bEnd - h.aEnd
		}
		return idx + offset + 1
	}
}

// usesCRLF reports whether the text ends its lines with CRLF, as most of its lines do.
func usesCRLF(src string) bool {
	crlf := strings.Count(src, "\r\n")
	lf := strings.Count(src, "\n") - crlf
	return crlf > 0 && crlf >= lf
}

// toLF replaces every CRLF with LF. A carriage return that is not part of a line
// ending is kept.
func toLF(src string) string {
	return strings.ReplaceAll(src, "\r\n", "\n")
}

// toCRLF ends every line of a text that has only LF line endings with CRLF.
func toCRLF(src string) string {
	return strings.ReplaceAll(src, "\n", "\r\n")
}

// hunk says that lines [aStart, aEnd) of one text are lines [bStart, bEnd) of another.
type hunk struct {
	aStart, aEnd, bStart, bEnd int
}

// maxDiffEdits bounds the lines that may differ between two texts for them to be
// compared: a file the formatter rewrites everywhere is taken as formatted.
const maxDiffEdits = 2000

// diffLines returns the hunks that turn a into b, in order. ok is false when the
// texts differ by more than maxDiffEdits lines.
func diffLines(a, b []string) (hunks []hunk, ok bool) {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	x, y := a[pre:len(a)-suf], b[pre:len(b)-suf]

	switch {
	case len(x) == 0 && len(y) == 0:
		return nil, true
	case len(x) == 0:
		return []hunk{{aStart: pre, aEnd: pre, bStart: pre, bEnd: pre + len(y)}}, true
	case len(y) == 0:
		return []hunk{{aStart: pre, aEnd: pre + len(x), bStart: pre, bEnd: pre}}, true
	}

	hunks, ok = myers(x, y)
	for i := range hunks {
		hunks[i].aStart += pre
		hunks[i].aEnd += pre
		hunks[i].bStart += pre
		hunks[i].bEnd += pre
	}
	return hunks, ok
}

// myers returns the hunks of a shortest edit script from a to b, which are not
// empty, by the algorithm of Eugene Myers (An O(ND) Difference Algorithm and Its
// Variations, 1986). ok is false when the script is longer than maxDiffEdits.
func myers(a, b []string) (hunks []hunk, ok bool) {
	n, m := len(a), len(b)
	limit := min(n+m, maxDiffEdits)
	offset := limit + 1
	// v[offset+k] is the furthest x reached on diagonal k = x - y.
	v := make([]int, 2*limit+3)
	// trace[d] is the window of v for the diagonals -d-1..d+1 before round d.
	trace := make([][]int, 0, limit+1)

	for d := 0; d <= limit; d++ {
		trace = append(trace, slices.Clone(v[offset-d-1:offset+d+2]))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrack(trace, n, m), true
			}
		}
	}
	return nil, false
}

// backtrack walks the trace of myers from the end of both texts to their start
// and returns the edits it found as hunks.
func backtrack(trace [][]int, n, m int) []hunk {
	var edits []hunk
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		window := trace[d]
		at := func(k int) int { return window[k+d+1] }

		k := x - y
		prevK := k - 1
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			prevK = k + 1
		}
		prevX := at(prevK)
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				edits = append(edits, hunk{aStart: x, aEnd: x, bStart: prevY, bEnd: prevY + 1})
			} else {
				edits = append(edits, hunk{aStart: prevX, aEnd: prevX + 1, bStart: prevY, bEnd: prevY})
			}
		}
		x, y = prevX, prevY
	}

	slices.Reverse(edits)
	var hunks []hunk
	for _, e := range edits {
		if last := len(hunks) - 1; last >= 0 && hunks[last].aEnd == e.aStart && hunks[last].bEnd == e.bStart {
			hunks[last].aEnd, hunks[last].bEnd = e.aEnd, e.bEnd
			continue
		}
		hunks = append(hunks, e)
	}
	return hunks
}

// merge3 merges the changes of ours and theirs to base, line by line. Where
// both changed the same lines, or inserted at the same place, theirs wins. ok is
// false when one of the texts differs from base by too many lines.
func merge3(base, ours, theirs []string) (merged []string, ok bool) {
	hunksA, okA := diffLines(base, ours)
	hunksB, okB := diffLines(base, theirs)
	if !okA || !okB {
		return nil, false
	}

	pos, i, j := 0, 0, 0
	for i < len(hunksA) || j < len(hunksB) {
		// A region starts with the next hunk, and grows over every hunk of one side
		// that is in conflict with a hunk of the other side that it holds.
		i0, j0 := i, j
		if j >= len(hunksB) || (i < len(hunksA) && !comesAfter(hunksA[i], hunksB[j])) {
			i++
		} else {
			j++
		}
		for grown := true; grown; {
			grown = false
			for i < len(hunksA) && slices.ContainsFunc(hunksB[j0:j], func(h hunk) bool { return conflict(hunksA[i], h) }) {
				i++
				grown = true
			}
			for j < len(hunksB) && slices.ContainsFunc(hunksA[i0:i], func(h hunk) bool { return conflict(hunksB[j], h) }) {
				j++
				grown = true
			}
		}

		start, end := regionBounds(hunksA[i0:i], hunksB[j0:j])
		merged = append(merged, base[pos:start]...)
		if i == i0 {
			merged = append(merged, applyHunks(base, theirs, hunksB[j0:j], start, end)...)
		} else if j == j0 {
			merged = append(merged, applyHunks(base, ours, hunksA[i0:i], start, end)...)
		} else {
			merged = append(merged, applyHunks(base, theirs, hunksB[j0:j], start, end)...)
		}
		pos = end
	}
	return append(merged, base[pos:]...), true
}

// comesAfter reports whether the hunk x of one side has to be taken after the hunk
// y of the other: it starts later, or both start at one place and x changes lines
// where y inserts before them.
func comesAfter(x, y hunk) bool {
	if x.aStart != y.aStart {
		return x.aStart > y.aStart
	}
	return x.aStart != x.aEnd && y.aStart == y.aEnd
}

// regionBounds returns the lines of the base that the hunks of both sides cover.
func regionBounds(a, b []hunk) (start, end int) {
	all := slices.Concat(a, b)
	start, end = all[0].aStart, all[0].aEnd
	for _, h := range all[1:] {
		start, end = min(start, h.aStart), max(end, h.aEnd)
	}
	return start, end
}

// conflict reports whether two hunks of different sides change the same lines of
// the base. A change of the line next to the other is none of its business: the
// formatter lays out a line by itself, except for the lines it aligns, and the
// hunk of the migration includes those. Inserting before or after the lines the
// other side changed is no conflict either; two insertions at one place are,
// because it is not clear which comes first.
func conflict(x, y hunk) bool {
	xInserts, yInserts := x.aStart == x.aEnd, y.aStart == y.aEnd
	switch {
	case xInserts && yInserts:
		return x.aStart == y.aStart
	case xInserts:
		return y.aStart < x.aStart && x.aStart < y.aEnd
	case yInserts:
		return x.aStart < y.aStart && y.aStart < x.aEnd
	default:
		return x.aStart < y.aEnd && y.aStart < x.aEnd
	}
}

// applyHunks returns lines [start, end) of base with the hunks applied, which
// take their new lines from side.
func applyHunks(base, side []string, hunks []hunk, start, end int) []string {
	var out []string
	pos := start
	for _, h := range hunks {
		out = append(out, base[pos:h.aStart]...)
		out = append(out, side[h.bStart:h.bEnd]...)
		pos = h.aEnd
	}
	return append(out, base[pos:end]...)
}

// keepFormatting returns the migrated text with the formatting of the original
// kept in the lines the migration did not change. original is the file as the
// author wrote it and formatted the migrated file after tidy ran over it. format
// only formats, it drops no import, and returns an error for a text that does not
// parse. The base of the merge is the original formatted, so that an import the
// migration left unused, and tidy dropped, is a change of the migration.
//
// The merged text is only used when formatting it gives exactly the formatted
// text, which says that nothing but formatting differs between them (a merge can
// bring back an import that tidy dropped, as the half of a move). It returns
// formatted when the original was formatted already, and whenever the merge is not
// possible or not safe.
func keepFormatting(original, formatted string, format func(string) (string, error)) string {
	base, err := format(original)
	if err != nil || base == original {
		return formatted
	}

	merged, ok := merge3(strings.Split(base, "\n"), strings.Split(original, "\n"), strings.Split(formatted, "\n"))
	if !ok {
		return formatted
	}
	text := strings.Join(merged, "\n")
	if check, err := format(text); err != nil || check != formatted {
		return formatted
	}
	return text
}
