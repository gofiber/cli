package v3

import (
	"cmp"
	"slices"
)

// clientEdit replaces the bytes [start, end) of the source with text.
type clientEdit struct {
	text  string
	start int
	end   int
	// unit identifies the rewrite that registered the edit.
	unit int
	// absorbed marks an edit whose effect is part of the text of an enclosing
	// edit, because that text was copied after the edit was registered.
	absorbed bool
}

// clientEdits collects the edits of one file. Unlike editSet it knows which
// rewrite an edit belongs to, so that two rewrites that touch the same bytes
// are detected instead of one of them being dropped silently.
type clientEdits struct {
	list []clientEdit
	unit int
}

// begin starts a new rewrite and returns its identifier.
func (e *clientEdits) begin() int {
	e.unit++
	return e.unit
}

// replace schedules the replacement of src[start:end] with text.
func (e *clientEdits) replace(unit, start, end int, text string) {
	e.list = append(e.list, clientEdit{unit: unit, start: start, end: end, text: text})
}

// textOf returns src[start:end] with the edits inside the range applied. The
// edits it takes into account count as absorbed.
func (e *clientEdits) textOf(src string, start, end int) string {
	var inner []textEdit
	for i := range e.list {
		ed := &e.list[i]
		inside := ed.start >= start && ed.end <= end
		if ed.start == ed.end {
			inside = ed.start > start && ed.end < end
		}
		if !inside {
			continue
		}
		ed.absorbed = true
		inner = append(inner, textEdit{start: ed.start - start, end: ed.end - start, text: ed.text})
	}
	set := editSet{edits: inner}
	return set.apply(src[start:end])
}

// apply returns src with the edits applied. ok is false when two rewrites
// disagree about the same bytes: nothing is applied then.
func (e *clientEdits) apply(src string) (out string, ok bool) {
	edits := slices.Clone(e.list)
	slices.SortStableFunc(edits, func(a, b clientEdit) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end))
	})

	var kept []clientEdit
	for _, ed := range edits {
		if ed.end < ed.start || ed.end > len(src) {
			return src, false
		}
		if len(kept) == 0 {
			kept = append(kept, ed)
			continue
		}
		last := kept[len(kept)-1]
		switch {
		case ed.start >= last.end:
			kept = append(kept, ed)
		case ed.end <= last.end && (ed.unit == last.unit || ed.absorbed):
			// Inside an edit of the same rewrite, or already part of its text.
		default:
			return src, false
		}
	}

	set := editSet{}
	for _, ed := range kept {
		set.replace(ed.start, ed.end, ed.text)
	}
	return set.apply(src), true
}
